package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/render"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// rendererFactory builds the renderer for one entity file, applying the
// rule_identifier_format of the realm that owns it.
type rendererFactory func(entityPath string, ef *entity.File) (render.Renderer, error)

// rendererKey identifies a renderer: its rule ID format and the realm whose
// inheritance chains it holds (EUIDs are unique within a realm only).
type rendererKey struct {
	format render.RuleIDFormatter
	realm  string
}

func newRenderCmd() *cobra.Command {
	var (
		outputDir     string
		format        string
		templatePath  string
		schemaVersion string
		jsonOutput    bool
	)

	cmd := &cobra.Command{
		Use:           "render [path]",
		Short:         "Render entity JSON to Markdown or JSON",
		Long:          "Convert SteerSpec entity JSON files to Markdown or normalized JSON. Accepts a single file or a directory.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOutput && (cmd.Flags().Changed("format") || templatePath != "") {
				return fmt.Errorf("--json cannot be combined with --format or --template")
			}

			inputPath := args[0]
			info, err := os.Stat(inputPath)
			if err != nil {
				return fmt.Errorf("accessing %s: %w", inputPath, err)
			}

			chains := newChainResolver(cmd.Context(), cmd.ErrOrStderr())

			if jsonOutput {
				if info.IsDir() {
					return renderDirectoryJSON(cmd, chains, inputPath, outputDir, schemaVersion)
				}
				return renderFileJSON(cmd, chains, inputPath, outputDir, schemaVersion)
			}

			var opts []render.Option
			if templatePath != "" {
				opts = append(opts, render.WithTemplate(templatePath))
			}

			// Fail fast on a bad format/template before touching any file.
			if _, err := render.New(render.Format(format), opts...); err != nil {
				return err
			}

			resolver := newRuleIDFormatResolver()
			// One renderer per distinct format and inheriting realm, so the
			// template is read and parsed once for each rather than once per
			// file.
			renderers := map[rendererKey]render.Renderer{}
			newRenderer := func(entityPath string, ef *entity.File) (render.Renderer, error) {
				f, err := resolver.formatterFor(entityPath)
				if err != nil {
					return nil, err
				}
				realm, realmChains, err := chains.chainsFor(entityPath, ef)
				if err != nil {
					return nil, err
				}
				key := rendererKey{format: f, realm: realm}
				if r, ok := renderers[key]; ok {
					return r, nil
				}
				fileOpts := append(append([]render.Option{}, opts...), render.WithRuleIDFormatter(f))
				if realmChains != nil {
					fileOpts = append(fileOpts, render.WithInheritance(realmChains))
				}
				r, err := render.New(render.Format(format), fileOpts...)
				if err != nil {
					return nil, err
				}
				renderers[key] = r
				return r, nil
			}

			if info.IsDir() {
				return renderDirectory(cmd, newRenderer, inputPath, outputDir, schemaVersion)
			}
			return renderFile(cmd, newRenderer, inputPath, outputDir, schemaVersion)
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory (default: stdout for single file, alongside source for directory)")
	cmd.Flags().StringVar(&format, "format", "markdown", "output format")
	cmd.Flags().StringVar(&templatePath, "template", "", "custom Go template file")
	cmd.Flags().StringVar(&schemaVersion, "schema-version", "v1", "entity schema version to validate against")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output normalized entity JSON")

	return cmd
}

func renderFile(cmd *cobra.Command, newRenderer rendererFactory, path, outputDir, schemaVersion string) error {
	ef, err := entity.Load(path)
	if err != nil {
		return err
	}

	if err := schema.ValidateFileSchema(ef, schemaVersion); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	r, err := newRenderer(path, ef)
	if err != nil {
		return err
	}

	if outputDir == "" {
		return r.Render(cmd.OutOrStdout(), ef)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	outPath := filepath.Join(outputDir, base+".md")

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", outPath, err)
	}
	defer func() { _ = f.Close() }()

	return r.Render(f, ef)
}

func renderDirectory(cmd *cobra.Command, newRenderer rendererFactory, dir, outputDir, schemaVersion string) error {
	errW := cmd.ErrOrStderr()
	seen := 0
	rendered := 0

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip underscore-prefixed directories (e.g. _schema/)
			if strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		// Skip JSON files starting with _
		if strings.HasPrefix(d.Name(), "_") {
			return nil
		}

		seen++

		ef, loadErr := entity.Load(path)
		if loadErr != nil {
			writeWarning(errW, "skipping %s: %v", path, loadErr)
			return nil
		}

		if valErr := schema.ValidateFileSchema(ef, schemaVersion); valErr != nil {
			if errors.Is(valErr, schema.ErrNotEntity) {
				// Not an entity file (e.g. realm.json) — skip silently.
				seen--
				return nil
			}
			writeWarning(errW, "skipping %s: %v", path, valErr)
			return nil
		}

		// Preserve relative directory structure under outputDir to avoid
		// filename collisions when the input tree has subdirectories.
		relPath, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return fmt.Errorf("computing relative path: %w", relErr)
		}

		target := outputDir
		if target == "" {
			target = filepath.Dir(path)
		} else {
			target = filepath.Join(target, filepath.Dir(relPath))
		}

		if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
			return fmt.Errorf("creating output directory: %w", mkErr)
		}

		base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
		outPath := filepath.Join(target, base+".md")

		r, rErr := newRenderer(path, ef)
		if rErr != nil {
			return fmt.Errorf("%s: %w", path, rErr)
		}

		f, createErr := os.Create(outPath)
		if createErr != nil {
			return fmt.Errorf("creating %s: %w", outPath, createErr)
		}

		renderErr := r.Render(f, ef)
		_ = f.Close()
		if renderErr != nil {
			return fmt.Errorf("rendering %s: %w", path, renderErr)
		}

		rendered++
		return nil
	})
	if err != nil {
		return err
	}

	if seen == 0 {
		return fmt.Errorf("no entity JSON files found in %s", dir)
	}
	if rendered == 0 {
		return fmt.Errorf("all %d entity JSON file(s) in %s were skipped (see warnings above)", seen, dir)
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Rendered %d of %d file(s)\n", rendered, seen)
	return nil
}

// marshalRendered returns the normalized JSON of ef, followed by the rules
// it inherits when an inheritance chain resolved, even if it has none yet.
func marshalRendered(chains *chainResolver, path string, ef *entity.File) ([]byte, error) {
	_, realmChains, err := chains.chainsFor(path, ef)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(struct {
		*entity.File
		Inherited []inheritedRule `json:"inherited,omitzero"`
	}{File: ef, Inherited: inheritedRows(ef, realmChains)}, "", "  ")
}

func renderFileJSON(cmd *cobra.Command, chains *chainResolver, path, outputDir, schemaVersion string) error {
	ef, err := entity.Load(path)
	if err != nil {
		return err
	}

	if err := schema.ValidateFileSchema(ef, schemaVersion); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	data, err := marshalRendered(chains, path, ef)
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if outputDir == "" {
		_, err = cmd.OutOrStdout().Write(append(printableJSON(data), '\n'))
		return err
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	outPath := filepath.Join(outputDir, base+".json")
	return os.WriteFile(outPath, append(data, '\n'), 0o644)
}

func renderDirectoryJSON(cmd *cobra.Command, chains *chainResolver, dir, outputDir, schemaVersion string) error {
	if outputDir == "" {
		return fmt.Errorf("--json with directory input requires -o/--output to avoid overwriting source files")
	}

	// Prevent writing JSON output into the input directory tree.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving input directory path: %w", err)
	}
	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolving output directory path: %w", err)
	}
	if strings.HasPrefix(absOut, absDir+string(filepath.Separator)) || absOut == absDir {
		return fmt.Errorf("--json output directory %q must not be inside input directory %q", outputDir, dir)
	}

	errW := cmd.ErrOrStderr()
	seen := 0
	rendered := 0

	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".json" {
			return nil
		}
		if strings.HasPrefix(d.Name(), "_") {
			return nil
		}

		seen++

		ef, loadErr := entity.Load(path)
		if loadErr != nil {
			writeWarning(errW, "skipping %s: %v", path, loadErr)
			return nil
		}

		if valErr := schema.ValidateFileSchema(ef, schemaVersion); valErr != nil {
			if errors.Is(valErr, schema.ErrNotEntity) {
				seen--
				return nil
			}
			writeWarning(errW, "skipping %s: %v", path, valErr)
			return nil
		}

		data, marshalErr := marshalRendered(chains, path, ef)
		if marshalErr != nil {
			return fmt.Errorf("marshaling %s: %w", path, marshalErr)
		}

		relPath, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return fmt.Errorf("computing relative path: %w", relErr)
		}

		target := filepath.Join(outputDir, filepath.Dir(relPath))

		if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
			return fmt.Errorf("creating output directory: %w", mkErr)
		}

		base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
		outPath := filepath.Join(target, base+".json")

		if writeErr := os.WriteFile(outPath, append(data, '\n'), 0o644); writeErr != nil {
			return fmt.Errorf("writing %s: %w", outPath, writeErr)
		}

		rendered++
		return nil
	})
	if err != nil {
		return err
	}

	if seen == 0 {
		return fmt.Errorf("no entity JSON files found in %s", dir)
	}
	if rendered == 0 {
		return fmt.Errorf("all %d entity JSON file(s) in %s were skipped (see warnings above)", seen, dir)
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Rendered %d of %d file(s)\n", rendered, seen)
	return nil
}
