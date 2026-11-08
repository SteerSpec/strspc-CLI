package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// defaultMinConfidence is the confidence a compliant verdict needs to let a
// rule be promoted to Implemented. Verdicts the code bore out came at 0.85
// and above in the live runs of #291; a false violation, at 0.55.
const defaultMinConfidence = 0.8

// promoteOptions collects the flags of `strspc rule promote`.
type promoteOptions struct {
	dir           string
	jsonOut       bool
	skipVerify    bool
	minConfidence float64
	verify        verifyOptions
}

// promoteOutput is the JSON report of rule promote. NewState and Version
// are empty when the promotion was refused.
type promoteOutput struct {
	RuleID     string `json:"rule_id"`
	OldState   string `json:"old_state"`
	NewState   string `json:"new_state,omitempty"`
	OldVersion string `json:"old_version"`
	Version    string `json:"version,omitempty"`
	// Verify is the verdict the promotion to Implemented rests on.
	Verify        *verifyRule `json:"verify,omitempty"`
	VerifySkipped bool        `json:"verify_skipped,omitempty"`
	// Diagnostics are those of the verify run, such as RE002 for a request
	// that failed: what explains a verdict the provider could not give.
	Diagnostics []explainedDiagnostic `json:"diagnostics,omitempty"`
	// Record is the verify record written for a promotion to Implemented,
	// relative to --project, to be committed with the change to rules/.
	Record string `json:"record,omitempty"`
	Error  string `json:"error,omitempty"`

	// report is the verify run the gate made.
	report *verifyOutput
}

func newRulePromoteCmd() *cobra.Command {
	var o promoteOptions

	cmd := &cobra.Command{
		Use:   "promote <rule_id>",
		Short: "Advance a rule to the next state",
		Long: "Promote a rule through its lifecycle: Draft → Published or Published → Implemented.\n\n" +
			"Before Published → Implemented, the rule is verified against the code as it stands, as\n" +
			"`strspc rule verify` does, with the AI provider of .strspc/config.yaml in --project. The\n" +
			"rule is promoted only when the code complies with a confidence of at least\n" +
			"--min-confidence. A violation refuses the promotion, and so does any verdict that is not\n" +
			"conclusive: unknown, not applicable (no code it applies to was found), or below the\n" +
			"threshold. The rule is read from the configured sources, so its copy under --dir must be\n" +
			"the same. --path and --exclude choose the files read, as for rule verify, and narrow what\n" +
			"counts as existing. --skip-verify promotes without verifying, for a rule a person has\n" +
			"checked or a project without an AI provider.\n\n" +
			"A promotion to Implemented writes .strspc/verified/<rule_id>.json in --project: the rule\n" +
			"text's hash, the files read and their hash, the provider, model and effort, and the\n" +
			"verdict with its explanation, confidence and evidence (or that verification was\n" +
			"skipped). Commit it with the change to rules/. A project without .strspc/ gets none.\n\n" +
			"Exit codes: 0 promoted, 1 refused (violated or inconclusive) or the promotion failed,\n" +
			"2 configuration or rule resolution error, 3 AI provider unavailable, 4 promoted but the\n" +
			"verify record could not be written.",
		Example: "  strspc rule promote MYE-001\n" +
			"  strspc rule promote MYE-002 --path src --exclude '**/*_test.go' --effort high\n" +
			"  strspc rule promote MYE-002 --skip-verify --json",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulePromote(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], &o)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.dir, "dir", "./rules", "realm directory")
	f.BoolVar(&o.jsonOut, "json", false, "output as JSON")
	f.BoolVar(&o.skipVerify, "skip-verify", false, "promote Published → Implemented without verifying the code")
	f.Float64Var(&o.minConfidence, "min-confidence", defaultMinConfidence,
		"confidence from 0 to 1 a compliant verdict needs for Published → Implemented")
	addVerifyFlags(f, &o.verify)

	return cmd
}

func runRulePromote(ctx context.Context, out, errOut io.Writer, ruleID string, o *promoteOptions) error {
	// Written so NaN, which pflag accepts and every comparison is false for,
	// fails it too: a NaN threshold would let any compliant verdict through.
	if !(o.minConfidence >= 0 && o.minConfidence <= 1) {
		return withExitCode(exitConfig, fmt.Errorf("invalid --min-confidence %v: must be from 0 to 1", o.minConfidence))
	}
	entityID, err := entityIDFromRuleID(ruleID)
	if err != nil {
		return err
	}
	target, err := resolveEntityTarget(o.dir, entityID)
	if err != nil {
		return err
	}

	res := promoteOutput{RuleID: ruleID}
	rule, version, err := ruleOf(target, ruleID)
	if err != nil {
		return err
	}
	res.OldVersion = version
	if rule != nil {
		res.OldState = rule.State
	}
	if res.OldState == stateCodePublished {
		if o.skipVerify {
			res.VerifySkipped = true
		} else if gateErr := gatePromotion(ctx, errOut, &res, rule, o); gateErr != nil {
			return reportRefusal(out, &res, o.jsonOut, gateErr)
		}
	}

	var promoted entity.Rule
	var stamp string
	err = modifyEntity(target, func(f *entity.File) error {
		// The file is read again to be changed: a rule edited while it was
		// being verified is not the rule the verdict is about.
		if res.Verify != nil && !sameRule(res.Verify, findRule(f, ruleID)) {
			return withExitCode(exitViolations, fmt.Errorf("refusing to promote %s to Implemented: "+
				"it changed while it was being verified; run rule promote again", ruleID))
		}
		if promoteErr := entityops.PromoteRule(f, ruleID); promoteErr != nil {
			return promoteErr
		}
		res.Version, stamp = f.RuleSet.Version, f.RuleSet.Timestamp
		if r := findRule(f, ruleID); r != nil {
			res.NewState, promoted = r.State, *r
		}
		return nil
	})
	if err != nil {
		// Nothing was promoted, whatever the change got to before failing.
		res.NewState, res.Version = "", ""
		return reportRefusal(out, &res, o.jsonOut, err)
	}

	// A promotion to Implemented leaves a record of what it rested on.
	var recordErr error
	if res.OldState == stateCodePublished {
		rec := newVerifyRecord(&promoted, res.Version, stamp, res.report, res.Verify, o.minConfidence)
		res.Record, recordErr = writeVerifyRecord(o.verify.project, &rec)
		if errors.Is(recordErr, errNoStrspcDir) {
			recordErr = nil // nowhere to keep it; said below
		} else if recordErr != nil {
			recordErr = withExitCode(exitOutput, fmt.Errorf("promoted %s, but its verify record could not be written: %w", ruleID, recordErr))
			res.Error = recordErr.Error()
		}
	}

	if o.jsonOut {
		if err := writeJSONOutput(out, res); err != nil && recordErr == nil {
			return err
		}
		return recordErr
	}
	printPromoteVerdict(out, &res)
	writeln(out, brandStyle.Render(fmt.Sprintf("Promoted rule %s: %s → %s", ruleID, stateLabel(res.OldState), stateLabel(res.NewState))))
	writeln(out, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", res.OldVersion, res.Version)))
	if res.VerifySkipped {
		writeln(out, descStyle.Render("  Not verified (--skip-verify)"))
	}
	switch {
	case res.Record != "":
		writeln(out, descStyle.Render(fmt.Sprintf("  Recorded in %s: commit it with the change to rules/", res.Record)))
	case res.OldState == stateCodePublished && recordErr == nil:
		writeln(out, descStyle.Render("  Not recorded: --project has no .strspc/ directory"))
	}
	return recordErr
}

// stateCodePublished is the state code of a Published rule, the one state
// whose promotion is gated.
const stateCodePublished = "P"

// ruleOf reads ruleID, nil when the entity has no such rule, and the version
// of its entity, without changing the file.
func ruleOf(t entityTarget, ruleID string) (rule *entity.Rule, version string, err error) {
	f, err := entity.Load(t.path)
	if err != nil {
		return nil, "", fmt.Errorf("loading entity file: %w", err)
	}
	if t.subID != "" {
		if f = findSubEntity(f, t.subID); f == nil {
			return nil, "", fmt.Errorf("sub-entity %q not found in %s", t.subID, t.path)
		}
	}
	// A missing rule is left to entityops.PromoteRule to report.
	return findRule(f, ruleID), f.RuleSet.Version, nil
}

// gatePromotion verifies rule, as --dir holds it, against the code before
// Published → Implemented and records the verdict in res. It returns why
// the promotion is refused, with the exit code it carries, or nil to
// promote.
func gatePromotion(ctx context.Context, errOut io.Writer, res *promoteOutput, rule *entity.Rule, o *promoteOptions) error {
	report, err := verifyRules(ctx, errOut, []string{res.RuleID}, &o.verify)
	if report != nil {
		res.report = report
		res.Diagnostics = report.Diagnostics
		if len(report.Rules) == 1 {
			res.Verify = &report.Rules[0]
		}
	}
	if res.Verify != nil && !sameRule(res.Verify, rule) {
		// verify reads the rule from the configured sources, promote writes
		// it under --dir: a verdict on another copy says nothing about this one.
		res.Verify = nil
		return withExitCode(exitConfig, fmt.Errorf("cannot verify %s before promoting it to Implemented: "+
			"the configured sources of .strspc/config.yaml in --project hold a different %s than --dir; "+
			"point --dir and --project at the same rules, or pass --skip-verify to promote it unverified",
			res.RuleID, res.RuleID))
	}
	if res.Verify == nil {
		// The run failed before judging the rule: configuration, sources,
		// provider. Keep its exit code.
		code := exitConfig
		if err == nil {
			err = errors.New("no verdict was returned")
		} else {
			code = exitCodeOf(err)
		}
		return withExitCode(code, fmt.Errorf("cannot verify %s before promoting it to Implemented: %w; "+
			"fix that (the rule is read from the sources of .strspc/config.yaml in --project), "+
			"or pass --skip-verify to promote it unverified", res.RuleID, err))
	}
	if reason := refusalReason(res.Verify, o.minConfidence); reason != "" {
		return withExitCode(exitViolations, fmt.Errorf("refusing to promote %s to Implemented: %s; "+
			"pass --skip-verify to promote it anyway", res.RuleID, reason))
	}
	return nil
}

// sameRule reports whether the rule verify judged is the rule promote is
// about to change: same text, revision and state.
func sameRule(judged *verifyRule, promoted *entity.Rule) bool {
	return promoted != nil && judged.ID == promoted.ID && judged.body == promoted.Body &&
		judged.revision == promoted.Revision && judged.State == promoted.State
}

// confident reports whether c is a confidence from minConfidence to 1. It
// is written so a NaN confidence, which fails every comparison, is not.
func confident(c, minConfidence float64) bool {
	return c >= minConfidence && c <= 1
}

// refusalReason says why a verdict does not let a rule be promoted to
// Implemented, or "" when it does: only a compliant verdict at minConfidence
// or above does.
func refusalReason(vr *verifyRule, minConfidence float64) string {
	if verdictWithoutReasons(vr) {
		return fmt.Sprintf("the provider answered %s without an explanation, a confidence or evidence, which is no judgement; "+
			"a cached audit in parts serves it again until a file changes, so rerun with --no-cache", vr.Verdict)
	}
	switch vr.Verdict {
	case verifyCompliant:
		if !confident(vr.Confidence, minConfidence) {
			return fmt.Sprintf("the code was judged compliant with confidence %.2f, below --min-confidence %.2f", vr.Confidence, minConfidence)
		}
		return ""
	case verifyViolated:
		if !confident(vr.Confidence, minConfidence) {
			return fmt.Sprintf("the code was judged in breach with confidence %.2f, below --min-confidence %.2f: "+
				"check the evidence, the verdict may be wrong", vr.Confidence, minConfidence)
		}
		return "the code violates it"
	case verifyNotApplicable:
		return "no code it applies to was found: check --path and --exclude"
	}
	return "it could not be judged"
}

// reportRefusal reports a promotion refused by the gate, or that failed
// once the entity file was read to be changed, and returns cause, whose
// exit code stands even when the report cannot be written (CMD-008).
func reportRefusal(out io.Writer, res *promoteOutput, jsonOut bool, cause error) error {
	if jsonOut {
		res.Error = cause.Error()
		if err := writeJSONOutput(out, res); err != nil {
			return errors.Join(cause, fmt.Errorf("writing JSON output: %w", err))
		}
		return cause
	}
	printPromoteVerdict(out, res)
	return cause
}

// printPromoteVerdict prints the diagnostics of the verify run and the
// verdict, when the gate ran.
func printPromoteVerdict(out io.Writer, res *promoteOutput) {
	if len(res.Diagnostics) > 0 {
		outputDiagnostics(out, &result.Result{Diagnostics: withLineInPath(diagsOf(res.Diagnostics))}, false)
	}
	if res.Verify != nil {
		writeln(out, verifyRuleLine(res.Verify))
	}
}
