package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
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
	Error         string      `json:"error,omitempty"`
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
			"threshold. --path and --exclude choose the files read, as for rule verify, and narrow what\n" +
			"counts as existing. --skip-verify promotes without verifying, for a rule a person has\n" +
			"checked or a project without an AI provider.\n\n" +
			"Exit codes: 0 promoted, 1 refused (violated or inconclusive) or the promotion failed,\n" +
			"2 configuration or rule resolution error, 3 AI provider unavailable.",
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
	if o.minConfidence < 0 || o.minConfidence > 1 {
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
	if res.OldState, res.OldVersion, err = ruleStateOf(target, ruleID); err != nil {
		return err
	}
	if res.OldState == stateCodePublished {
		if o.skipVerify {
			res.VerifySkipped = true
		} else if gateErr := gatePromotion(ctx, errOut, &res, o); gateErr != nil {
			return reportRefusal(out, &res, o.jsonOut, gateErr)
		}
	}

	err = modifyEntity(target, func(f *entity.File) error {
		if promoteErr := entityops.PromoteRule(f, ruleID); promoteErr != nil {
			return promoteErr
		}
		res.Version = f.RuleSet.Version
		if r := findRule(f, ruleID); r != nil {
			res.NewState = r.State
		}
		return nil
	})
	if err != nil {
		return err
	}

	if o.jsonOut {
		return writeJSONOutput(out, res)
	}
	if res.Verify != nil {
		writeln(out, verifyRuleLine(res.Verify))
	}
	writeln(out, brandStyle.Render(fmt.Sprintf("Promoted rule %s: %s → %s", ruleID, stateLabel(res.OldState), stateLabel(res.NewState))))
	writeln(out, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", res.OldVersion, res.Version)))
	if res.VerifySkipped {
		writeln(out, descStyle.Render("  Not verified (--skip-verify)"))
	}
	return nil
}

// stateCodePublished is the state code of a Published rule, the one state
// whose promotion is gated.
const stateCodePublished = "P"

// ruleStateOf reads the state of ruleID and the version of its entity,
// without changing the file.
func ruleStateOf(t entityTarget, ruleID string) (state, version string, err error) {
	f, err := entity.Load(t.path)
	if err != nil {
		return "", "", fmt.Errorf("loading entity file: %w", err)
	}
	if t.subID != "" {
		if f = findSubEntity(f, t.subID); f == nil {
			return "", "", fmt.Errorf("sub-entity %q not found in %s", t.subID, t.path)
		}
	}
	if r := findRule(f, ruleID); r != nil {
		state = r.State
	}
	// A missing rule is left to entityops.PromoteRule to report.
	return state, f.RuleSet.Version, nil
}

// gatePromotion verifies the rule against the code before Published →
// Implemented and records the verdict in res. It returns why the promotion
// is refused, with the exit code it carries, or nil to promote.
func gatePromotion(ctx context.Context, errOut io.Writer, res *promoteOutput, o *promoteOptions) error {
	report, err := verifyRules(ctx, errOut, []string{res.RuleID}, &o.verify)
	if report != nil && len(report.Rules) == 1 {
		res.Verify = &report.Rules[0]
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

// refusalReason says why a verdict does not let a rule be promoted to
// Implemented, or "" when it does: only a compliant verdict at minConfidence
// or above does.
func refusalReason(vr *verifyRule, minConfidence float64) string {
	switch vr.Verdict {
	case verifyCompliant:
		if vr.Confidence < minConfidence {
			return fmt.Sprintf("the code was judged compliant with confidence %.2f, below --min-confidence %.2f", vr.Confidence, minConfidence)
		}
		return ""
	case verifyViolated:
		if vr.Confidence < minConfidence {
			return fmt.Sprintf("the code was judged in breach with confidence %.2f, below --min-confidence %.2f: "+
				"check the evidence, the verdict may be wrong", vr.Confidence, minConfidence)
		}
		return "the code violates it"
	case verifyNotApplicable:
		return "no code it applies to was found: check --path and --exclude"
	}
	return "it could not be judged"
}

// reportRefusal reports a refused promotion and returns cause, whose exit
// code stands even when the report cannot be written (CMD-008).
func reportRefusal(out io.Writer, res *promoteOutput, jsonOut bool, cause error) error {
	if jsonOut {
		res.Error = cause.Error()
		if err := writeJSONOutput(out, res); err != nil {
			return errors.Join(cause, fmt.Errorf("writing JSON output: %w", err))
		}
		return cause
	}
	if res.Verify != nil {
		writeln(out, verifyRuleLine(res.Verify))
	}
	return cause
}
