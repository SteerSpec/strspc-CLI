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
			// The Realm's kind turns off the body checks a Standard Realm's
			// wording is exempt from (RL021, RL026).
			newLinter := func(schemaOpts []schema.Option, kind string) *rulelint.Linter {
				return rulelint.New(
					rulelint.WithSchemaFetcher(newSchemaFetcher(schemaOpts...)),
					rulelint.WithStrict(strict),
					rulelint.WithSchemaVersion(schemaVersion),
					rulelint.WithRealmKind(kind),
				)
			}
			rootOpts := realmSchemaOptions(realmDir, core)
			linter := newLinter(rootOpts, realmKindIn(realmDir))
			rootPin := corePinKey(realmDir)
			if core {
				rootPin = "core " + realmDir
			}

			var res *result.Result
			if info.IsDir() {
				if crossRef {
					res = linter.LintDir(cmd.Context(), inputPath)
				} else {
					res = lintDirPerFile(cmd.Context(), linter, inputPath, rootOpts, rootPin, newLinter)
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

// realmLinterFactory builds the linter for the files of a Realm from the
// schemas it is validated against and its kind.
type realmLinterFactory func(schemaOpts []schema.Option, kind string) *rulelint.Linter

// lintDirPerFile walks a directory tree and lints each entity JSON file
// individually without cross-file checks. Skips underscore-prefixed
// directories (e.g. _schema/) and files, as well as realm.json.
//
// The files of each sub-realm of dir, at any depth, are linted with a linter
// of their own Realm: the schemas of its own core pin, or else of the nearest
// Realm above it that has one, so a sub-realm is checked against its pin and
// not also against dir's (RLM-014), as realm validate does; and its own kind,
// since each Realm's kind is its own manifest's (RLMMNFST-009).
func lintDirPerFile(ctx context.Context, linter *rulelint.Linter, dir string, schemaOpts []schema.Option, pin string,
	newLinter realmLinterFactory,
) *result.Result {
	res := &result.Result{}
	seen := 0
	root := filepath.Clean(dir)
	subLinters := make(map[string]*rulelint.Linter)
	if newLinter != nil {
		parent := realmLinting{linter: linter, schemaOpts: schemaOpts, pin: pin, kind: realmKindIn(dir)}
		collectSubRealmLinters(root, parent, newLinter, subLinters, 0)
	}

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
		for d := filepath.Dir(path); d != root && d != "." && d != filepath.Dir(d); d = filepath.Dir(d) {
			if l := subLinters[d]; l != nil {
				fileLinter = l
				break
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
	if _, err := w.Write(append(printableJSON(data), '\n')); err != nil {
		return withExitCode(exitOutput, fmt.Errorf("writing JSON output: %w", err))
	}
	return nil
}

// maxSubRealmDepth bounds how deep collectSubRealmLinters follows sub_realms.
const maxSubRealmDepth = 32

// realmKindIn returns the kind declared by the manifest in dir, or "" for a
// code Realm and when there is no manifest that can be loaded.
func realmKindIn(dir string) string {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return ""
	}
	return rf.Realm.Kind
}

// corePinKey identifies the core pin of the manifest in dir, which selects
// its schemas: its dev.steerspec.core version and source, or "" when it has
// none or no manifest that can be loaded.
func corePinKey(dir string) string {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return ""
	}
	if dep := corePin(rf); dep != nil {
		return dep.Version + " " + dep.Source
	}
	return ""
}

// realmLinting is how the files of one Realm are linted: its linter, the
// schemas and core pin it was built from, and its kind.
type realmLinting struct {
	linter     *rulelint.Linter
	schemaOpts []schema.Option
	pin, kind  string
}

// collectSubRealmLinters adds to out, by directory, the linter of each
// sub-realm the manifest in dir lists, and of theirs in turn. A sub-realm
// that pins dev.steerspec.core gets the schemas of its own pin; one that
// doesn't, those of its parent. Each gets its own kind. A sub-realm whose pin
// and kind match its parent's shares the parent's linter, so the same schemas
// aren't fetched twice. A name that isn't a plain directory name, or a
// manifest that can't be loaded, is skipped: realm validate reports it.
func collectSubRealmLinters(dir string, parent realmLinting, newLinter realmLinterFactory, out map[string]*rulelint.Linter, depth int) {
	if depth >= maxSubRealmDepth {
		return
	}
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return
	}
	for _, name := range rf.SubRealms {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			continue
		}
		subDir := filepath.Join(dir, name)
		m, err := entity.LoadRealm(filepath.Join(subDir, "realm.json"))
		if err != nil {
			continue
		}
		sub := parent
		sub.kind = m.Realm.Kind
		if dep := corePin(m); dep != nil {
			if key := dep.Version + " " + dep.Source; key != parent.pin {
				sub.pin, sub.schemaOpts = key, schema.ForRealm(subDir)
			}
		}
		if sub.pin != parent.pin || sub.kind != parent.kind {
			sub.linter = newLinter(sub.schemaOpts, sub.kind)
		}
		out[subDir] = sub.linter
		collectSubRealmLinters(subDir, sub, newLinter, out, depth+1)
	}
}
