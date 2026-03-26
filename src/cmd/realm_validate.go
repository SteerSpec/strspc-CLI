package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// realmValidatorSet holds the linters used by realm validate.
// When recursive is true, ruleLinter.LintRealm provides cross-entity
// reference checks (RL012) across the full realm tree.
type realmValidatorSet struct {
	realmLinter *realmlint.RealmLinter
	ruleLinter  *rulelint.Linter
}

// newRealmValidator creates configured linters. Package-level so tests can override it.
var newRealmValidator = func(strict, recursive bool) *realmValidatorSet {
	fetcher := schema.New()
	rl := rulelint.New(
		rulelint.WithSchemaFetcher(fetcher),
		rulelint.WithStrict(strict),
	)

	realmOpts := []realmlint.Option{
		realmlint.WithSchemaFetcher(fetcher),
		realmlint.WithStrict(strict),
	}
	// When recursive, rulelint.LintRealm handles entity validation globally;
	// skip per-file rule linting inside realmlint to avoid duplication.
	if !recursive {
		realmOpts = append(realmOpts, realmlint.WithRuleLinter(rl))
	}

	return &realmValidatorSet{
		realmLinter: realmlint.New(realmOpts...),
		ruleLinter:  rl,
	}
}

func newRealmValidateCmd() *cobra.Command {
	var (
		jsonOutput bool
		strict     bool
		recursive  bool
	)

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a Realm directory",
		Long: "Validate a SteerSpec Realm directory structure, realm.json, schemas, and entity files.\n\n" +
			"With --recursive, also validates sub-realm directories declared in realm.json and checks " +
			"cross-entity references (e.g. supersedes) globally across the realm tree.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "./rules"
			if len(args) > 0 {
				dir = args[0]
			}

			vs := newRealmValidator(strict, recursive)
			res := vs.realmLinter.Lint(dir)

			if recursive {
				realmRes := vs.ruleLinter.LintRealm(dir)
				mergeDiagnostics(res, realmRes)
			}

			w := cmd.OutOrStdout()

			if jsonOutput {
				if err := writeJSON(w, res); err != nil {
					return err
				}
				if !res.OK() {
					return fmt.Errorf("realm validate found %d error(s)", len(res.Errors()))
				}
				return nil
			}

			if recursive {
				outputGroupedText(w, res, dir)
			} else {
				outputText(w, res)
			}

			if !res.OK() {
				return fmt.Errorf("realm validate found %d error(s)", len(res.Errors()))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output diagnostics as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "validate sub-realms and check cross-entity references globally")

	return cmd
}

// mergeDiagnostics appends diagnostics from extra into base, skipping duplicates.
func mergeDiagnostics(base, extra *result.Result) {
	seen := make(map[string]bool, len(base.Diagnostics))
	for _, d := range base.Diagnostics {
		seen[d.Code+"|"+d.Path+"|"+d.Message] = true
	}
	for _, d := range extra.Diagnostics {
		key := d.Code + "|" + d.Path + "|" + d.Message
		if !seen[key] {
			base.Add(d)
			seen[key] = true
		}
	}
}

// outputGroupedText prints diagnostics grouped by sub-realm with section headers.
// Falls back to outputText if realm.json cannot be loaded or has no sub-realms.
func outputGroupedText(w io.Writer, res *result.Result, dir string) {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil || len(rf.SubRealms) == 0 {
		outputText(w, res)
		return
	}

	// Partition diagnostics by sub-realm path prefix.
	type group struct {
		label string
		diags []result.Diagnostic
	}
	rootGroup := &group{label: "Root realm"}
	subGroups := make(map[string]*group, len(rf.SubRealms))
	for _, sr := range rf.SubRealms {
		subGroups[sr] = &group{label: fmt.Sprintf("Sub-realm %q", sr)}
	}

	absDir, _ := filepath.Abs(dir)
	for _, d := range res.Diagnostics {
		absPath, _ := filepath.Abs(d.Path)
		matched := false
		for _, sr := range rf.SubRealms {
			srPrefix := filepath.Join(absDir, sr)
			if strings.HasPrefix(absPath, srPrefix+string(filepath.Separator)) || absPath == srPrefix {
				subGroups[sr].diags = append(subGroups[sr].diags, d)
				matched = true
				break
			}
		}
		if !matched {
			rootGroup.diags = append(rootGroup.diags, d)
		}
	}

	// Print each group.
	var totalErrors, totalWarnings int
	groups := make([]*group, 0, 1+len(rf.SubRealms))
	groups = append(groups, rootGroup)
	for _, sr := range rf.SubRealms {
		groups = append(groups, subGroups[sr])
	}

	for _, g := range groups {
		if len(g.diags) == 0 {
			continue
		}
		writeln(w, brandStyle.Render(fmt.Sprintf("── %s ──", g.label)))
		for _, d := range g.diags {
			switch d.Severity {
			case result.Error:
				totalErrors++
				writeln(w, brandStyle.Render(d.String()))
			case result.Warning:
				totalWarnings++
				writeln(w, descStyle.Render(d.String()))
			case result.Info:
				writeln(w, descStyle.Render(d.String()))
			}
		}
		writeln(w)
	}

	if totalErrors == 0 && totalWarnings == 0 {
		writeln(w, brandStyle.Render("No errors or warnings found"))
		return
	}

	summary := fmt.Sprintf("%d error(s), %d warning(s)", totalErrors, totalWarnings)
	if totalErrors > 0 {
		writeln(w, brandStyle.Render(summary))
	} else {
		writeln(w, descStyle.Render(summary))
	}
}
