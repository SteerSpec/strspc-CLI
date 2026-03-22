package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-CLI/src/internal/entity"
	"github.com/SteerSpec/strspc-CLI/src/internal/render"
)

func newRenderCmd() *cobra.Command {
	var (
		outputDir     string
		format        string
		templatePath  string
		schemaVersion string
		jsonOutput    bool
	)

	cmd := &cobra.Command{
		Use:   "render [path]",
		Short: "Render entity JSON to Markdown",
		Long:  "Convert SteerSpec entity JSON files to Markdown (or other formats). Accepts a single file or a directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOutput && (cmd.Flags().Changed("format") || templatePath != "") {
				return fmt.Errorf("--json cannot be combined with --format or --template")
			}

			inputPath := args[0]
			info, err := os.Stat(inputPath)
			if err != nil {
				return fmt.Errorf("accessing %s: %w", inputPath, err)
			}

			if jsonOutput {
				if info.IsDir() {
					return renderDirectoryJSON(cmd, inputPath, outputDir, schemaVersion)
				}
				return renderFileJSON(cmd, inputPath, outputDir, schemaVersion)
			}

			var opts []render.Option
			if templatePath != "" {
				opts = append(opts, render.WithTemplate(templatePath))
			}

			renderer, err := render.New(render.Format(format), opts...)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return renderDirectory(cmd, renderer, inputPath, outputDir, schemaVersion)
			}
			return renderFile(cmd, renderer, inputPath, outputDir, schemaVersion)
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory (default: stdout for single file, alongside source for directory)")
	cmd.Flags().StringVar(&format, "format", "markdown", "output format")
	cmd.Flags().StringVar(&templatePath, "template", "", "custom Go template file")
	cmd.Flags().StringVar(&schemaVersion, "schema-version", "v1", "entity schema version to validate against")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output parsed entity JSON (identity transform)")

	return cmd
}

func renderFile(cmd *cobra.Command, r render.Renderer, path, outputDir, schemaVersion string) error {
	ef, err := entity.Load(path)
	if err != nil {
		return err
	}

	if err := validateSchema(ef, schemaVersion); err != nil {
		return fmt.Errorf("%s: %w", path, err)
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

func renderDirectory(cmd *cobra.Command, r render.Renderer, dir, outputDir, schemaVersion string) error {
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
			_, _ = fmt.Fprintf(errW, "warning: skipping %s: %v\n", path, loadErr)
			return nil
		}

		if valErr := validateSchema(ef, schemaVersion); valErr != nil {
			if errors.Is(valErr, errNotEntity) {
				// Not an entity file (e.g. realm.json) — skip silently.
				seen--
				return nil
			}
			_, _ = fmt.Fprintf(errW, "warning: skipping %s: %v\n", path, valErr)
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
		return fmt.Errorf("no JSON files found in %s", dir)
	}
	if rendered == 0 {
		return fmt.Errorf("all %d JSON file(s) in %s were skipped (see warnings above)", seen, dir)
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Rendered %d of %d file(s)\n", rendered, seen)
	return nil
}

func renderFileJSON(cmd *cobra.Command, path, outputDir, schemaVersion string) error {
	ef, err := entity.Load(path)
	if err != nil {
		return err
	}

	if err := validateSchema(ef, schemaVersion); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	data, err := json.MarshalIndent(ef, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if outputDir == "" {
		_, err = cmd.OutOrStdout().Write(append(data, '\n'))
		return err
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	outPath := filepath.Join(outputDir, base+".json")
	return os.WriteFile(outPath, append(data, '\n'), 0o644)
}

func renderDirectoryJSON(cmd *cobra.Command, dir, outputDir, schemaVersion string) error {
	errW := cmd.ErrOrStderr()
	seen := 0
	rendered := 0

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
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
			_, _ = fmt.Fprintf(errW, "warning: skipping %s: %v\n", path, loadErr)
			return nil
		}

		if valErr := validateSchema(ef, schemaVersion); valErr != nil {
			if errors.Is(valErr, errNotEntity) {
				seen--
				return nil
			}
			_, _ = fmt.Fprintf(errW, "warning: skipping %s: %v\n", path, valErr)
			return nil
		}

		data, marshalErr := json.MarshalIndent(ef, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("marshaling %s: %w", path, marshalErr)
		}

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

// errNotEntity is returned when a file's $schema does not reference an entity schema.
var errNotEntity = fmt.Errorf("not an entity file")

func validateSchema(ef *entity.File, version string) error {
	if !isEntitySchema(ef.Schema, version) {
		// Check if it's an entity schema at all (any version).
		if !isEntitySchemaAnyVersion(ef.Schema) {
			return errNotEntity
		}
		return fmt.Errorf("schema version mismatch: file declares %q, expected version %q", ef.Schema, version)
	}
	for i := range ef.SubEntities {
		if err := validateSchema(&ef.SubEntities[i], version); err != nil {
			return fmt.Errorf("sub-entity %s: %w", ef.SubEntities[i].Entity.ID, err)
		}
	}
	return nil
}

// isEntitySchema checks whether the $schema value matches entity schema for the given version.
// Supports both relative paths (./_schema/entity.v1.schema.json) and absolute URLs
// (https://steerspec.dev/schemas/entity/v1.json).
func isEntitySchema(schema, version string) bool {
	return strings.HasSuffix(schema, "entity."+version+".schema.json") ||
		strings.HasSuffix(schema, "entity/"+version+".json")
}

// isEntitySchemaAnyVersion checks whether the $schema references any entity schema version.
func isEntitySchemaAnyVersion(schema string) bool {
	return strings.Contains(schema, "entity.") && strings.HasSuffix(schema, ".schema.json") ||
		strings.Contains(schema, "entity/") && strings.HasSuffix(schema, ".json")
}
