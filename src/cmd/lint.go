package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// newSchemaFetcher creates the schema fetcher used by the lint command.
// Package-level so tests can override it.
var newSchemaFetcher = func() *schema.Fetcher {
	return schema.New()
}

func newLintCmd() *cobra.Command {
	var (
		crossRef      bool
		jsonOutput    bool
		strict        bool
		schemaVersion string
	)

	cmd := &cobra.Command{
		Use:           "lint [path]",
		Short:         "Validate entity JSON files",
		Long:          "Validate SteerSpec entity JSON files against schema and business rules. Accepts a single file or directory.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fetcher := newSchemaFetcher()
			linter := rulelint.New(
				rulelint.WithSchemaFetcher(fetcher),
				rulelint.WithStrict(strict),
				rulelint.WithSchemaVersion(schemaVersion),
			)

			inputPath := args[0]
			info, err := os.Stat(inputPath)
			if err != nil {
				return fmt.Errorf("accessing %s: %w", inputPath, err)
			}

			var res *result.Result
			if info.IsDir() {
				if crossRef {
					res = linter.LintDir(inputPath)
				} else {
					res = lintDirPerFile(linter, inputPath)
				}
			} else {
				data, readErr := os.ReadFile(inputPath)
				if readErr != nil {
					return fmt.Errorf("reading %s: %w", inputPath, readErr)
				}
				res = linter.LintBytes(data)
				// Add file path context to diagnostics.
				for i := range res.Diagnostics {
					if res.Diagnostics[i].Path == "" {
						res.Diagnostics[i].Path = inputPath
					} else {
						res.Diagnostics[i].Path = inputPath + ": " + res.Diagnostics[i].Path
					}
				}
			}

			w := cmd.OutOrStdout()

			if jsonOutput {
				return outputJSON(w, res)
			}

			outputText(w, res)

			if !res.OK() {
				errs := res.Errors()
				cmd.SilenceUsage = true
				return fmt.Errorf("lint found %d error(s)", len(errs))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&crossRef, "cross-ref", false, "enable cross-file reference checks (directory mode)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output diagnostics as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	cmd.Flags().StringVar(&schemaVersion, "schema-version", "v1", "entity schema version")

	return cmd
}

// lintDirPerFile walks a directory tree and lints each entity JSON file
// individually without cross-file checks. Skips underscore-prefixed
// directories (e.g. _schema/) and files, as well as realm.json.
func lintDirPerFile(linter *rulelint.Linter, dir string) *result.Result {
	res := &result.Result{}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			res.Add(result.Diagnostic{
				Module:   "rule-lint",
				Code:     "RL000",
				Severity: result.Error,
				Message:  fmt.Sprintf("accessing path: %s", walkErr),
				Path:     path,
			})
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".json" || strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		if d.Name() == "realm.json" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			res.Add(result.Diagnostic{
				Module:   "rule-lint",
				Code:     "RL000",
				Severity: result.Error,
				Message:  fmt.Sprintf("reading file: %s", readErr),
				Path:     path,
			})
			return nil
		}

		fileRes := linter.LintBytes(data)
		for _, diag := range fileRes.Diagnostics {
			if diag.Path == "" {
				diag.Path = path
			} else {
				diag.Path = path + ": " + diag.Path
			}
			res.Add(diag)
		}
		return nil
	})
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   "rule-lint",
			Code:     "RL000",
			Severity: result.Error,
			Message:  fmt.Sprintf("walking directory: %s", err),
			Path:     dir,
		})
	}

	return res
}

func outputText(w io.Writer, res *result.Result) {
	var errors, warnings int
	for _, d := range res.Diagnostics {
		switch d.Severity {
		case result.Error:
			errors++
			writeln(w, brandStyle.Render(d.String()))
		case result.Warning:
			warnings++
			writeln(w, descStyle.Render(d.String()))
		case result.Info:
			writeln(w, descStyle.Render(d.String()))
		}
	}

	if errors == 0 && warnings == 0 {
		writeln(w, brandStyle.Render("No errors or warnings found"))
		return
	}

	writeln(w)
	summary := fmt.Sprintf("%d error(s), %d warning(s)", errors, warnings)
	if errors > 0 {
		writeln(w, brandStyle.Render(summary))
	} else {
		writeln(w, descStyle.Render(summary))
	}
}

func outputJSON(w io.Writer, res *result.Result) error {
	diags := res.Diagnostics
	if diags == nil {
		diags = []result.Diagnostic{}
	}

	data, err := json.MarshalIndent(diags, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling diagnostics: %w", err)
	}
	_, writeErr := w.Write(append(data, '\n'))
	if writeErr != nil {
		return writeErr
	}

	if !res.OK() {
		return fmt.Errorf("lint found %d error(s)", len(res.Errors()))
	}
	return nil
}
