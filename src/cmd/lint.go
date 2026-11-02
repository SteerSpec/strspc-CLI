package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// newSchemaFetcher creates the schema fetcher used by lint and realm validate.
// Package-level so tests can override it.
var newSchemaFetcher = schema.New

// realmSchemaOptions selects the schemas for the realm in dir: those of the core
// version it pins or, with core, its own _schema/. Only the caller can make that
// choice, because a realm declares its own ID.
func realmSchemaOptions(dir string, core bool) []schema.Option {
	if core {
		return []schema.Option{schema.WithLocalDir(dir)}
	}
	return schema.ForRealm(dir)
}

func newLintCmd() *cobra.Command {
	var (
		crossRef      bool
		jsonOutput    bool
		strict        bool
		core          bool
		explain       bool
		schemaVersion string
	)

	cmd := &cobra.Command{
		Use:   "lint [path]",
		Short: "Validate entity JSON files",
		Long: "Validate SteerSpec entity JSON files against schema and business rules. Accepts a single file or directory.\n\n" +
			"Schemas come from the core version pinned in the realm.json of the directory, or of the file's\n" +
			"directory. Without a pin, the latest published schemas are used.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			inputPath := args[0]
			info, err := os.Stat(inputPath)
			if err != nil {
				return fmt.Errorf("accessing %s: %w", inputPath, err)
			}

			realmDir := inputPath
			if !info.IsDir() {
				realmDir = filepath.Dir(inputPath)
			}
			linter := rulelint.New(
				rulelint.WithSchemaFetcher(newSchemaFetcher(realmSchemaOptions(realmDir, core)...)),
				rulelint.WithStrict(strict),
				rulelint.WithSchemaVersion(schemaVersion),
			)

			var res *result.Result
			if info.IsDir() {
				if crossRef {
					res = linter.LintDir(cmd.Context(), inputPath)
				} else {
					res = lintDirPerFile(cmd.Context(), linter, inputPath, func(subDir string) *rulelint.Linter {
						return rulelint.New(
							rulelint.WithSchemaFetcher(newSchemaFetcher(schema.ForRealm(subDir)...)),
							rulelint.WithStrict(strict),
							rulelint.WithSchemaVersion(schemaVersion),
						)
					})
				}
			} else {
				data, readErr := os.ReadFile(inputPath)
				if readErr != nil {
					return fmt.Errorf("reading %s: %w", inputPath, readErr)
				}
				res = linter.LintBytes(cmd.Context(), data)
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
				if err := writeJSON(w, res, explain); err != nil {
					return err
				}
				if !res.OK() {
					return fmt.Errorf("lint found %d error(s)", len(res.Errors()))
				}
				return nil
			}

			outputDiagnostics(w, res, explain)

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
	cmd.Flags().BoolVar(&core, "core", false, "the path is in the core realm: validate against its own _schema/")
	cmd.Flags().BoolVar(&explain, "explain", false, "explain what each diagnostic code means and how to fix it")
	cmd.Flags().StringVar(&schemaVersion, "schema-version", "v1", "entity schema version")

	return cmd
}

// lintDirPerFile walks a directory tree and lints each entity JSON file
// individually without cross-file checks. Skips underscore-prefixed
// directories (e.g. _schema/) and files, as well as realm.json.
//
// The files of a sub-realm of dir that pins its own core are linted only with
// the linter sub returns for its directory, so they are checked against that
// pin and not also against dir's (RLM-014), as realm validate does. A sub-realm
// without a core dependency keeps dir's linter.
func lintDirPerFile(ctx context.Context, linter *rulelint.Linter, dir string, sub rulelint.SubRealmLinter) *result.Result {
	res := &result.Result{}
	seen := 0
	pinned := pinnedSubRealmLinters(dir, sub)

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

		seen++
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

		fileLinter := linter
		if rel, err := filepath.Rel(dir, path); err == nil {
			if first, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && pinned[first] != nil {
				fileLinter = pinned[first]
			}
		}
		fileRes := fileLinter.LintBytes(ctx, data)
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

	if seen == 0 {
		res.Add(result.Diagnostic{
			Module:   "rule-lint",
			Code:     "RL000",
			Severity: result.Error,
			Message:  fmt.Sprintf("no entity JSON files found in %s", dir),
			Path:     dir,
		})
	}

	return res
}

func outputText(w io.Writer, res *result.Result) {
	outputDiagnostics(w, res, false)
}

// outputDiagnostics prints diagnostics and a summary, explaining each code when
// explain is set.
func outputDiagnostics(w io.Writer, res *result.Result, explain bool) {
	p := newDiagnosticPrinter(w, explain)
	for i := range res.Diagnostics {
		p.print(&res.Diagnostics[i])
	}

	if p.errors == 0 && p.warnings == 0 {
		writeln(w, brandStyle.Render("No errors or warnings found"))
		return
	}

	writeln(w)
	summary := fmt.Sprintf("%d error(s), %d warning(s)", p.errors, p.warnings)
	if p.errors > 0 {
		writeln(w, brandStyle.Render(summary))
	} else {
		writeln(w, descStyle.Render(summary))
	}
}

// writeJSON writes diagnostics as a JSON array, with an explanation on each
// when explain is set. It does not inspect result severity — callers are
// responsible for returning an appropriate error.
func writeJSON(w io.Writer, res *result.Result, explain bool) error {
	data, err := json.MarshalIndent(explainDiagnostics(res.Diagnostics, explain), "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling diagnostics: %w", err)
	}
	_, writeErr := w.Write(append(printableJSON(data), '\n'))
	return writeErr
}

// pinnedSubRealmLinters returns, by directory name, the linters of the
// sub-realms of dir whose manifest depends on dev.steerspec.core. Without a
// manifest at dir, or one that can't be loaded, there are none.
func pinnedSubRealmLinters(dir string, sub rulelint.SubRealmLinter) map[string]*rulelint.Linter {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil || sub == nil {
		return nil
	}
	out := make(map[string]*rulelint.Linter)
	for _, name := range rf.SubRealms {
		subDir := filepath.Join(dir, name)
		m, err := entity.LoadRealm(filepath.Join(subDir, "realm.json"))
		if err != nil || !m.DependsOn(coreRealmID) {
			continue
		}
		if l := sub(subDir); l != nil {
			out[name] = l
		}
	}
	return out
}
