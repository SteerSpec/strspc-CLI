package cmd

import (
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
	)

	cmd := &cobra.Command{
		Use:   "render [path]",
		Short: "Render entity JSON to Markdown",
		Long:  "Convert SteerSpec entity JSON files to Markdown (or other formats). Accepts a single file or a directory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var opts []render.Option
			if templatePath != "" {
				opts = append(opts, render.WithTemplate(templatePath))
			}

			renderer, err := render.New(render.Format(format), opts...)
			if err != nil {
				return err
			}

			inputPath := args[0]
			info, err := os.Stat(inputPath)
			if err != nil {
				return fmt.Errorf("accessing %s: %w", inputPath, err)
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

func validateSchema(ef *entity.File, version string) error {
	expected := schemaURL(version)
	if ef.Schema != expected {
		return fmt.Errorf("schema mismatch: file declares %q, expected %q", ef.Schema, expected)
	}
	for i := range ef.SubEntities {
		if err := validateSchema(&ef.SubEntities[i], version); err != nil {
			return fmt.Errorf("sub-entity %s: %w", ef.SubEntities[i].Entity.ID, err)
		}
	}
	return nil
}

func schemaURL(version string) string {
	return fmt.Sprintf("https://steerspec.dev/schemas/entity/%s.json", version)
}
