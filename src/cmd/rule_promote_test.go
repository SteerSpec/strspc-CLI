package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

func TestRulePromoteDraftToPublished(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft rule", "github:alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "promote", "TST-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "Draft → Published")

	f := loadEntityFile(t, dir, "TST")
	if f.Rules[0].State != "P" {
		t.Errorf("expected state P, got %s", f.Rules[0].State)
	}
}

// runPromoteGated promotes TST-002, the Published rule of setupVerifyRepo,
// with the gate reading the project's tracked files.
func runPromoteGated(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	base := []string{"rule", "promote", "TST-002", "--dir", filepath.Join(dir, "rules"), "--project", dir}
	return testutil.ExecuteCommand(NewRootCmd(), append(base, args...)...)
}

// stateOfTST002 reads back the state of TST-002.
func stateOfTST002(t *testing.T, dir string) string {
	t.Helper()
	f := loadEntityFile(t, filepath.Join(dir, "rules"), "TST")
	if r := findRule(f, "TST-002"); r != nil {
		return r.State
	}
	t.Fatal("TST-002 not found")
	return ""
}

func TestRulePromotePublishedToImplemented(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stub := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-002": {
			Verdict: ruleeval.Compliant, Explanation: "every error is wrapped", Confidence: 0.9,
			Evidence: []ruleeval.Location{{File: "src/main.go", Line: 3, Excerpt: "func main() {}"}},
		},
	}}
	stubEvalProvider(t, stub, nil)

	out, err := runPromoteGated(t, dir, "--path", "src")
	assertExit(t, err, exitOK)
	for _, want := range []string{"TST-002", "compliant", "src/main.go:3", "every error is wrapped", "Published → Implemented"} {
		testutil.AssertContains(t, out, want)
	}
	if got := stub.seenFiles(); len(got) != 1 || got[0] != "src/main.go" {
		t.Errorf("files audited = %v, want [src/main.go]", got)
	}
	if got := stateOfTST002(t, dir); got != "I" {
		t.Errorf("state = %s, want I", got)
	}
}

func TestRulePromoteRefusedByVerify(t *testing.T) {
	cases := []struct {
		name string
		resp *ruleeval.EvalResponse
		args []string
		want string
	}{
		{"violated", &ruleeval.EvalResponse{
			Verdict: ruleeval.Violated, Explanation: "bare error", Confidence: 0.95,
			Evidence: []ruleeval.Location{{File: "src/main.go", Line: 3, Excerpt: "func main() {}"}},
		}, nil, "the code violates it"},
		{
			"unsure violation", &ruleeval.EvalResponse{Verdict: ruleeval.Violated, Confidence: 0.55}, nil,
			"in breach with confidence 0.55, below --min-confidence 0.80",
		},
		{
			"unsure compliance", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant, Confidence: 0.85},
			[]string{"--min-confidence", "0.9"},
			"compliant with confidence 0.85, below --min-confidence 0.90",
		},
		{"compliance without confidence", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant}, nil, "confidence 0.00"},
		{"not applicable", &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable}, nil, "no code it applies to was found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupVerifyRepo(t, "claude")
			stubEvalProvider(t, &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{"TST-002": tc.resp}}, nil)

			out, err := runPromoteGated(t, dir, tc.args...)
			assertExit(t, err, exitViolations)
			testutil.AssertContains(t, err.Error(), tc.want)
			testutil.AssertContains(t, err.Error(), "--skip-verify")
			testutil.AssertContains(t, out, "TST-002")
			if got := stateOfTST002(t, dir); got != "P" {
				t.Errorf("state = %s, want P (refused)", got)
			}
		})
	}
}

// editingProvider changes the rules on disk while it judges them, as
// another process could during a verify run.
type editingProvider struct {
	stubProvider
	edit func()
}

func (p *editingProvider) Evaluate(ctx context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	p.edit()
	return p.stubProvider.Evaluate(ctx, req)
}

func TestRulePromoteRuleChangedWhileVerifying(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	rulesDir := filepath.Join(dir, "rules")
	stubEvalProvider(t, &editingProvider{
		stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
			"TST-002": {Verdict: ruleeval.Compliant, Confidence: 0.95},
		}},
		edit: func() {
			f := loadEntityFile(t, rulesDir, "TST")
			findRule(f, "TST-002").Body = "Errors must be wrapped, and logged."
			if err := replaceEntityFile(resolveEntityPath(rulesDir, "TST"), f); err != nil {
				t.Fatal(err)
			}
		},
	}, nil)

	_, err := runPromoteGated(t, dir)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, err.Error(), "changed while it was being verified")
	if got := stateOfTST002(t, dir); got != "P" {
		t.Errorf("state = %s, want P (refused)", got)
	}
}

func TestRulePromoteRefusedUnknown(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	// Only part of the tree judged: compliant where seen is still unknown.
	stubEvalProvider(t, &partialBatchProvider{batchStubProvider{stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-002": {Verdict: ruleeval.Compliant, Confidence: 0.9},
	}}}}, nil)

	out, err := runPromoteGated(t, dir, "--json")
	assertExit(t, err, exitViolations)
	var res promoteOutput
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("decoding JSON: %v\n%s", jsonErr, out)
	}
	if res.Verify == nil || res.Verify.Verdict != verifyUnknown {
		t.Errorf("verify = %+v, want unknown", res.Verify)
	}
	if res.NewState != "" || res.OldState != "P" || !strings.Contains(res.Error, "could not be judged") {
		t.Errorf("report = %+v", res)
	}
}

func TestRulePromoteSkipVerify(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stub := &stubProvider{}
	stubEvalProvider(t, stub, nil)

	out, err := runPromoteGated(t, dir, "--skip-verify", "--json")
	assertExit(t, err, exitOK)
	var res promoteOutput
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("decoding JSON: %v\n%s", jsonErr, out)
	}
	if !res.VerifySkipped || res.Verify != nil || res.NewState != "I" {
		t.Errorf("report = %+v", res)
	}
	if stub.callCount() != 0 {
		t.Errorf("provider calls = %d, want 0", stub.callCount())
	}

	// The human report says so too.
	dir = setupVerifyRepo(t, "claude")
	out, err = runPromoteGated(t, dir, "--skip-verify")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Not verified (--skip-verify)")
}

func TestRulePromoteGateCannotRun(t *testing.T) {
	t.Run("no config", func(t *testing.T) {
		dir := t.TempDir()
		setupEntityFile(t, dir, "TST", makeRule("TST-001", "P", "Published rule", "github:alice"))
		_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "TST-001", "--dir", dir, "--project", dir)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "cannot verify TST-001")
		testutil.AssertContains(t, err.Error(), "--skip-verify")
		if f := loadEntityFile(t, dir, "TST"); f.Rules[0].State != "P" {
			t.Errorf("state = %s, want P", f.Rules[0].State)
		}
	})
	t.Run("no provider", func(t *testing.T) {
		dir := setupVerifyRepo(t, "null")
		stubEvalProvider(t, &stubProvider{}, nil)
		_, err := runPromoteGated(t, dir)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "needs an AI provider")
	})
	t.Run("provider unavailable", func(t *testing.T) {
		dir := setupVerifyRepo(t, "claude")
		stubEvalProvider(t, &stubProvider{err: fmt.Errorf("%w: 401", ruleeval.ErrUnavailable)}, nil)
		out, err := runPromoteGated(t, dir, "--json")
		assertExit(t, err, exitUnavailable)
		testutil.AssertContains(t, out, `"error": "cannot verify TST-002`)
		if got := stateOfTST002(t, dir); got != "P" {
			t.Errorf("state = %s, want P", got)
		}
	})
	t.Run("another copy of the rule", func(t *testing.T) {
		dir := setupVerifyRepo(t, "claude")
		stub := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
			"TST-002": {Verdict: ruleeval.Compliant, Confidence: 0.95},
		}}
		stubEvalProvider(t, stub, nil)
		other := t.TempDir()
		setupEntityFile(t, other, "TST", makeRule("TST-002", "P", "Another text", "github:alice"))
		out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "TST-002", "--dir", other, "--project", dir, "--json")
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "hold a different TST-002 than --dir")
		var res promoteOutput
		if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
			t.Fatalf("decoding JSON: %v\n%s", jsonErr, out)
		}
		if res.Verify != nil {
			t.Errorf("a verdict on another copy must not be reported: %+v", res.Verify)
		}
		if f := loadEntityFile(t, other, "TST"); f.Rules[0].State != "P" {
			t.Errorf("state = %s, want P", f.Rules[0].State)
		}
	})
	t.Run("rule outside the sources", func(t *testing.T) {
		dir := setupVerifyRepo(t, "claude")
		stubEvalProvider(t, &stubProvider{}, nil)
		other := t.TempDir()
		setupEntityFile(t, other, "OTH", makeRule("OTH-001", "P", "Published rule", "github:alice"))
		_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "OTH-001", "--dir", other, "--project", dir)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "not found in the configured sources")
		testutil.AssertContains(t, err.Error(), "sources of .strspc/config.yaml")
	})
}

func TestRulePromoteInvalidMinConfidence(t *testing.T) {
	for _, v := range []string{"1.5", "-0.1", "NaN", "Inf"} {
		t.Run(v, func(t *testing.T) {
			dir := t.TempDir()
			setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft", "github:alice"))
			_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "TST-001", "--dir", dir, "--min-confidence", v)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "invalid --min-confidence")
		})
	}
}

func TestRefusalReason(t *testing.T) {
	cases := []struct {
		vr   verifyRule
		want string
	}{
		{verifyRule{Verdict: verifyCompliant, Confidence: 0.8}, ""},
		{verifyRule{Verdict: verifyCompliant, Confidence: 0.79}, "below --min-confidence"},
		{verifyRule{Verdict: verifyViolated, Confidence: 0.9}, "the code violates it"},
		{verifyRule{Verdict: verifyViolated, Confidence: 0.5}, "the verdict may be wrong"},
		{verifyRule{Verdict: verifyCompliant, Confidence: math.NaN()}, "below --min-confidence"},
		{verifyRule{Verdict: verifyCompliant, Confidence: math.Inf(1)}, "below --min-confidence"},
		{verifyRule{Verdict: verifyCompliant, Confidence: 1.5}, "below --min-confidence"},
		{verifyRule{Verdict: verifyViolated, Confidence: math.NaN()}, "the verdict may be wrong"},
		{verifyRule{Verdict: verifyNotApplicable}, "no code it applies to"},
		{verifyRule{Verdict: verifyUnknown}, "could not be judged"},
	}
	for _, tc := range cases {
		got := refusalReason(&tc.vr, 0.8)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("refusalReason(%+v) = %q, want %q", tc.vr, got, tc.want)
		}
	}
}

func TestReportRefusalJSONWriteFailure(t *testing.T) {
	cause := withExitCode(exitViolations, errors.New("refused"))
	err := reportRefusal(failWriter{}, &promoteOutput{RuleID: "TST-002"}, true, cause)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, err.Error(), "writing JSON output")
}

func TestRulePromoteJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft", "github:alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "promote", "TST-001", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result promoteOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result.NewState != "P" || result.OldState != "D" || result.Version == "" || result.OldVersion == "" {
		t.Errorf("unexpected report: %+v", result)
	}
	if result.Verify != nil || result.VerifySkipped {
		t.Errorf("Draft → Published must not verify: %+v", result)
	}
}

func TestRulePromoteSubEntityGated(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "P", "Published", "github:alice"))

	// A Published sub-entity rule is gated too: without a project to
	// verify it in, it is refused...
	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "SUB-001", "--dir", dir, "--project", dir)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "cannot verify SUB-001")

	// ...unless verification is skipped.
	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "SUB-001", "--dir", dir, "--skip-verify")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Published → Implemented")
}

func TestRulePromoteInvalidState(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "I", "Implemented", "github:alice"))

	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "promote", "TST-001", "--dir", dir)
	if err == nil {
		t.Fatal("expected error promoting Implemented rule")
	}
}
