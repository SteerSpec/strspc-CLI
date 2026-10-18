package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/providers"
)

// checkEntityJSON is a minimal entity file with rules in several states:
// TST-001 Implemented (in fail_on), TST-002 Published, TST-003 Draft and
// TST-004 Retired (never evaluated).
const checkEntityJSON = `{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {
    "id": "TST",
    "title": "Test",
    "description": "A test entity."
  },
  "rule_set": {
    "version": "0.1.0",
    "timestamp": "2026-01-01T00:00:00Z",
    "hash": null
  },
  "rules": [
    {
      "id": "TST-001",
      "revision": 0,
      "state": "I",
      "body": "Exported functions must have a doc comment.",
      "added_by": "@test",
      "added_at": "2026-01-01",
      "supersedes": null
    },
    {
      "id": "TST-002",
      "revision": 0,
      "state": "P",
      "body": "Errors must be wrapped with context.",
      "added_by": "@test",
      "added_at": "2026-01-01",
      "supersedes": null
    },
    {
      "id": "TST-003",
      "revision": 0,
      "state": "D",
      "body": "Prefer early returns.",
      "added_by": "@test",
      "added_at": "2026-01-01",
      "supersedes": null
    },
    {
      "id": "TST-004",
      "revision": 0,
      "state": "R",
      "body": "Use tabs.",
      "added_by": "@test",
      "added_at": "2026-01-01",
      "supersedes": null
    }
  ],
  "sub_entities": [],
  "notes": []
}
`

// checkDiff is the unified diff evaluated by the tests.
const checkDiff = `diff --git a/src/main.go b/src/main.go
index 0000000..1111111 100644
--- a/src/main.go
+++ b/src/main.go
@@ -1,3 +1,5 @@
 package main

+func Run() {}
+
 func main() {}
`

// stubProvider is a ruleeval.Provider whose verdicts are fixed per rule ID.
// It implements Describer so it can be used with the cache.
type stubProvider struct {
	mu       sync.Mutex
	verdicts map[string]*ruleeval.EvalResponse
	err      error // returned by every Evaluate call when set
	calls    int
}

func (s *stubProvider) Evaluate(_ context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if v, ok := s.verdicts[req.Rule.ID]; ok {
		return v, nil
	}
	return &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable, Explanation: "subject absent"}, nil
}

func (s *stubProvider) Model() string { return "stub-model" }

func (s *stubProvider) PromptVersion() string { return "test/v1" }

func (s *stubProvider) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// batchStubProvider additionally implements BatchProvider so the grouped
// request path of the evaluator is exercised.
type batchStubProvider struct{ stubProvider }

func (b *batchStubProvider) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
	out := &ruleeval.BatchResponse{Verdicts: map[string]*ruleeval.EvalResponse{}}
	for i := range req.Inputs {
		rule := req.Inputs[i].Rule
		v, err := b.Evaluate(ctx, ruleeval.EvalRequest{Rule: rule, Diff: req.Diff})
		if err != nil {
			return nil, err
		}
		out.Verdicts[rule.ID] = v
	}
	return out, nil
}

// stubEvalProvider replaces newEvalProvider for the duration of the test.
// It records the provider name and options it was asked for.
func stubEvalProvider(t *testing.T, p ruleeval.Provider, err error) *providers.Options {
	t.Helper()
	orig := newEvalProvider
	var got providers.Options
	newEvalProvider = func(name string, o providers.Options) (ruleeval.Provider, error) {
		got = o
		if strings.EqualFold(name, "null") || name == "" {
			return nil, providers.ErrStatic
		}
		if err != nil {
			return nil, err
		}
		return p, nil
	}
	t.Cleanup(func() { newEvalProvider = orig })
	return &got
}

// violatedResponse is a violated verdict pointing at the diff.
func violatedResponse(msg string) *ruleeval.EvalResponse {
	return &ruleeval.EvalResponse{
		Verdict:     ruleeval.Violated,
		Explanation: msg,
		Confidence:  0.9,
		Evidence:    []ruleeval.Location{{File: "src/main.go", Line: 3, Excerpt: "func Run() {}"}},
	}
}

// setupCheckProject writes a project with one local rules directory, a
// .strspc/config.yaml using the given provider and fail_on, and a diff file.
// It returns the project dir and the diff file path.
func setupCheckProject(t *testing.T, provider string, failOn []string) (dir, diffFile string) {
	t.Helper()
	dir = t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(checkEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCheckConfig(t, dir, "./rules/", provider, failOn, nil)
	diffFile = filepath.Join(dir, "change.diff")
	if err := os.WriteFile(diffFile, []byte(checkDiff), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, diffFile
}

// writeCheckConfig writes .strspc/config.yaml under dir.
func writeCheckConfig(t *testing.T, dir, source, provider string, failOn, evaluateOn []string) {
	t.Helper()
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "rules:\n  - source: %s\n    scope: local\n", source)
	fmt.Fprintf(&b, "evaluator:\n  provider: %s\n  effort: low\n", provider)
	b.WriteString("cache:\n  ttl: 24h\n")
	if len(failOn) > 0 {
		fmt.Fprintf(&b, "fail_on: [%s]\n", strings.Join(failOn, ", "))
	}
	if len(evaluateOn) > 0 {
		fmt.Fprintf(&b, "evaluate_on: [%s]\n", strings.Join(evaluateOn, ", "))
	}
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCheckCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"check"}, args...)...)
}

func assertExit(t *testing.T, err error, want int) {
	t.Helper()
	if got := exitCodeOf(err); got != want {
		t.Fatalf("exit code = %d, want %d (err: %v)", got, want, err)
	}
}

func TestCheckNoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := runCheckCmd(t, dir)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "strspc init")
}

func TestCheckStaticOnlyFlag(t *testing.T) {
	dir, _ := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, nil, errors.New("must not be called"))

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "RE003")
	testutil.AssertContains(t, output, "Checked 4 rule(s) from 1 source(s) (static-only)")
}

func TestCheckStaticOnlyFromNullProvider(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})
	stubEvalProvider(t, nil, nil)

	// No --static-only and no git repository: provider null must still exit 0.
	output, err := runCheckCmd(t, dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "RE003")
}

func TestCheckNoSourcesWarns(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "rules: []\nevaluator:\n  provider: null\nfail_on: [I]\n"
	if err := os.WriteFile(filepath.Join(dir, ".strspc", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	// The config `strspc init` wrote before 1.15 has no active source: still exit 0.
	output, err := runCheckCmd(t, dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "[warning] CHK001: no rule sources configured")
	testutil.AssertContains(t, output, "RE003")
	testutil.AssertContains(t, output, "Checked 0 rule(s) from 0 source(s)")
}

func TestCheckStaticOnlyJSON(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})

	output, err := runCheckCmd(t, dir, "--format", "json")
	assertExit(t, err, exitOK)

	var out checkOutput
	if jsonErr := json.Unmarshal([]byte(output), &out); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	if !out.OK || out.ExitCode != 0 || !out.StaticOnly {
		t.Errorf("unexpected envelope: %+v", out)
	}
	if out.RuleCount != 4 || out.SourceCount != 1 {
		t.Errorf("rule_count=%d source_count=%d, want 4 and 1", out.RuleCount, out.SourceCount)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != "RE003" {
		t.Errorf("expected exactly one RE003 diagnostic, got %+v", out.Diagnostics)
	}
}

func TestCheckHiddenJSONAlias(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})
	output, err := runCheckCmd(t, dir, "--json")
	assertExit(t, err, exitOK)
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("--json should produce JSON, got:\n%s", output)
	}
}

func TestCheckCompliant(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &batchStubProvider{stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": {Verdict: ruleeval.Compliant, Explanation: "documented"},
	}}}
	opts := stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--model", "my-model")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "No errors or warnings found")
	testutil.AssertContains(t, output, "Checked 4 rule(s) from 1 source(s) with claude/stub-model")
	if opts.Model != "my-model" || opts.Effort != "low" {
		t.Errorf("provider options = %+v, want model my-model and effort low from flag/config", *opts)
	}
	if p.callCount() == 0 {
		t.Error("provider was never called")
	}
}

func TestCheckViolationInFailOnFails(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"implemented"})
	p := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": violatedResponse("Run has no doc comment"),
		"TST-002": violatedResponse("error not wrapped"),
	}}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, err.Error(), "1 violation(s)")
	// Implemented rule → error (in fail_on); Published rule → warning.
	testutil.AssertContains(t, output, "[error] RE001: rule TST-001 violated: Run has no doc comment (src/main.go:3)")
	testutil.AssertContains(t, output, "[warning] RE001: rule TST-002 violated")
	testutil.AssertContains(t, output, "1 error(s), 1 warning(s)")
}

func TestCheckViolationOutsideFailOnWarnsOnly(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-003": violatedResponse("late return"),
	}}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "[warning] RE001: rule TST-003 violated")
}

func TestCheckNotApplicableAndEvaluateOn(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	// Only Implemented rules are evaluated; TST-002/003 violations must never be asked for.
	writeCheckConfig(t, dir, "./rules/", "claude", []string{"I"}, []string{"I"})
	p := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-002": violatedResponse("should be skipped"),
		"TST-003": violatedResponse("should be skipped"),
	}}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "No errors or warnings found")
	if p.callCount() != 1 {
		t.Errorf("provider calls = %d, want 1 (only TST-001 is in evaluate_on)", p.callCount())
	}
}

func TestCheckProviderErrorIsWarning(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &stubProvider{err: errors.New("model overloaded")}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "[warning] RE002: evaluation failed for rule TST-001: model overloaded")
}

func TestCheckProviderUnavailableAtConstruction(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, nil, fmt.Errorf("%w: no API key (set ANTHROPIC_API_KEY)", ruleeval.ErrUnavailable))

	output, err := runCheckCmd(t, dir, "--diff-file", diff)
	assertExit(t, err, exitUnavailable)
	testutil.AssertContains(t, err.Error(), `provider "claude" unavailable`)
	testutil.AssertContains(t, err.Error(), "ANTHROPIC_API_KEY")
	testutil.AssertContains(t, err.Error(), "--static-only")
	if strings.Contains(output, "RE00") {
		t.Errorf("no diagnostics expected before resolution, got:\n%s", output)
	}
}

func TestCheckProviderUnavailableMidRun(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &stubProvider{err: fmt.Errorf("%w: HTTP 401 authentication_error", ruleeval.ErrUnavailable)}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitUnavailable)
	testutil.AssertContains(t, err.Error(), "HTTP 401")
	testutil.AssertNotContains(t, output, "RE002")
}

func writeCheckConfigEndpoint(t *testing.T, dir, endpoint string) {
	t.Helper()
	cfg := "rules:\n  - source: ./rules/\n    scope: local\nevaluator:\n  provider: claude\n  endpoint: " + endpoint + "\nfail_on: [I]\n"
	if err := os.WriteFile(filepath.Join(dir, ".strspc", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckEndpointFromConfigRefused(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	writeCheckConfigEndpoint(t, dir, "https://evil.example.com/v1")
	t.Setenv(envEvaluatorEndpoint, "")
	called := false
	orig := newEvalProvider
	newEvalProvider = func(string, providers.Options) (ruleeval.Provider, error) {
		called = true
		return &stubProvider{}, nil
	}
	t.Cleanup(func() { newEvalProvider = orig })

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "evil.example.com")
	testutil.AssertContains(t, err.Error(), "API key would be sent")
	testutil.AssertContains(t, err.Error(), envEvaluatorEndpoint)
	if called {
		t.Error("provider must not be constructed with a refused endpoint")
	}
}

func TestCheckEndpointAnthropicHostAllowed(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	writeCheckConfigEndpoint(t, dir, "https://api.anthropic.com/")
	t.Setenv(envEvaluatorEndpoint, "")
	opts := stubEvalProvider(t, &stubProvider{}, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
	if opts.Endpoint != "https://api.anthropic.com/" {
		t.Errorf("endpoint passed to provider = %q", opts.Endpoint)
	}
}

func TestCheckEndpointEnvOverridesConfig(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	writeCheckConfigEndpoint(t, dir, "https://evil.example.com/v1")
	t.Setenv(envEvaluatorEndpoint, "https://proxy.internal:8443")
	opts := stubEvalProvider(t, &stubProvider{}, nil)

	// Explicit opt-in from the environment wins and is passed through unchanged.
	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--endpoint", "https://flag.internal")
	assertExit(t, err, exitOK)
	if opts.Endpoint != "https://proxy.internal:8443" {
		t.Errorf("endpoint passed to provider = %q, want the env value", opts.Endpoint)
	}
}

func TestCheckEndpointFlagOptsIn(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	writeCheckConfigEndpoint(t, dir, "https://evil.example.com/v1")
	t.Setenv(envEvaluatorEndpoint, "")
	opts := stubEvalProvider(t, &stubProvider{}, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--endpoint", "https://flag.internal")
	assertExit(t, err, exitOK)
	if opts.Endpoint != "https://flag.internal" {
		t.Errorf("endpoint passed to provider = %q, want the flag value", opts.Endpoint)
	}
}

func TestResolveEndpoint(t *testing.T) {
	cases := []struct {
		cfg, flag, env, want string
		wantErr              bool
	}{
		{"", "", "", "", false},
		{"null", "", "", "", false},
		{"https://api.anthropic.com", "", "", "https://api.anthropic.com", false},
		{"https://eu.api.anthropic.com/v1", "", "", "https://eu.api.anthropic.com/v1", false},
		{"https://api.anthropic.com.evil.example", "", "", "", true},
		{"http://api.anthropic.com", "", "", "", true}, // plain http leaks the key in transit
		{"https://other.example", "", "", "", true},
		{"https://other.example", "https://flag", "", "https://flag", false},
		{"https://other.example", "https://flag", "https://env", "https://env", false},
	}
	for _, c := range cases {
		got, err := resolveEndpoint(c.cfg, c.flag, c.env)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("resolveEndpoint(%q,%q,%q) = %q, %v; want %q, err=%v", c.cfg, c.flag, c.env, got, err, c.want, c.wantErr)
		}
	}
}

func TestCheckProviderNotImplemented(t *testing.T) {
	dir, diff := setupCheckProject(t, "openai", []string{"I"})
	stubEvalProvider(t, nil, fmt.Errorf("%w: %q", providers.ErrProviderNotImplemented, "openai"))

	_, err := runCheckCmd(t, dir, "--diff-file", diff)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "not implemented yet")
}

func TestCheckUnknownProviderFlag(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, nil, fmt.Errorf("%w: %q", providers.ErrUnknownProvider, "gpt9"))

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--provider", "gpt9")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "not a known provider")
}

func TestCheckResolutionFailureExits2(t *testing.T) {
	dir := t.TempDir()
	writeCheckConfig(t, dir, "./missing-rules/", "claude", []string{"I"}, nil)
	stubEvalProvider(t, &stubProvider{}, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", filepath.Join(dir, "none.diff"))
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "rule resolution failed")
	testutil.AssertContains(t, output, "RSV002")
}

func TestCheckInvalidFailOnExits2(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"bogus"})
	stubEvalProvider(t, &stubProvider{}, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "fail_on")
}

func TestCheckInvalidFormat(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", nil)
	_, err := runCheckCmd(t, dir, "--format", "xml")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "--format")
}

func TestCheckEmptyDiffIsInfo(t *testing.T) {
	dir, _ := setupCheckProject(t, "claude", []string{"I"})
	empty := filepath.Join(dir, "empty.diff")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p := &stubProvider{}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", empty, "--no-cache")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "RE004")
	if p.callCount() != 0 {
		t.Errorf("provider called %d time(s) for an empty diff", p.callCount())
	}
}

func TestCheckCacheSkipsSecondRun(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": violatedResponse("Run has no doc comment"),
	}}
	stubEvalProvider(t, p, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff)
	assertExit(t, err, exitViolations)
	first := p.callCount()
	if first == 0 {
		t.Fatal("provider was never called on the first run")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".strspc", "cache.db")); statErr != nil {
		t.Fatalf("cache.db not created: %v", statErr)
	}

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--verbose")
	assertExit(t, err, exitViolations)
	if p.callCount() != first {
		t.Errorf("provider calls after cached run = %d, want %d", p.callCount(), first)
	}
	testutil.AssertContains(t, output, "cache hits 3")

	// --no-cache bypasses the store.
	_, err = runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitViolations)
	if p.callCount() == first {
		t.Error("--no-cache should have called the provider again")
	}
}

func TestCheckNeverWritesOutsideStrspc(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, &stubProvider{}, nil)

	before := listFiles(t, dir)
	_, err := runCheckCmd(t, dir, "--diff-file", diff)
	assertExit(t, err, exitOK)
	for f := range listFiles(t, dir) {
		if before[f] {
			continue
		}
		if !strings.HasPrefix(f, ".strspc"+string(filepath.Separator)) {
			t.Errorf("check created %s outside .strspc/", f)
		}
	}
}

func listFiles(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out[rel] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCheckJSONViolations(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": violatedResponse("Run has no doc comment"),
	}}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--format", "json")
	assertExit(t, err, exitViolations)

	var out checkOutput
	if jsonErr := json.Unmarshal([]byte(output), &out); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	if out.OK || out.ExitCode != exitViolations || out.StaticOnly {
		t.Errorf("unexpected envelope: %+v", out)
	}
	if out.Provider != "claude" || out.Model != "stub-model" {
		t.Errorf("provider/model = %q/%q", out.Provider, out.Model)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != "RE001" || out.Diagnostics[0].Line != 3 {
		t.Errorf("diagnostics = %+v", out.Diagnostics)
	}
}

func TestCheckGitHubFormatGolden(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	p := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": violatedResponse("Run has no doc comment, see https://example.com/doc:1"),
		"TST-002": violatedResponse("error not wrapped\nsecond line"),
	}}
	stubEvalProvider(t, p, nil)

	summary := filepath.Join(dir, "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--format", "github")
	assertExit(t, err, exitViolations)

	got := strings.ReplaceAll(output, diff, "<DIFF_FILE>")
	compareGolden(t, filepath.Join("testdata", "check_github_stdout.golden"), got)

	data, readErr := os.ReadFile(summary)
	if readErr != nil {
		t.Fatalf("step summary not written: %v", readErr)
	}
	gotSummary := strings.ReplaceAll(string(data), diff, "<DIFF_FILE>")
	compareGolden(t, filepath.Join("testdata", "check_github_summary.golden"), gotSummary)
}

func TestCheckGitHubFormatFatalError(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, nil, fmt.Errorf("%w: no API key", ruleeval.ErrUnavailable))
	t.Setenv("GITHUB_STEP_SUMMARY", "")

	output, err := runCheckCmd(t, dir, "--diff-file", diff, "--format", "github")
	assertExit(t, err, exitUnavailable)
	testutil.AssertContains(t, output, "::error title=strspc check::provider \"claude\" unavailable")
}

// compareGolden compares got with the golden file, rewriting it when
// -update is passed via UPDATE_GOLDEN=1.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s: %v (set UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Errorf("output differs from %s\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func TestCheckHelp(t *testing.T) {
	output, err := runCheckCmd(t, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, flag := range []string{"--base", "--staged", "--pr", "--diff-file", "--format", "--provider", "--model", "--endpoint", "--static-only", "--no-cache", "--verbose"} {
		testutil.AssertContains(t, output, flag)
	}
	testutil.AssertNotContains(t, output, "--json")
}

func TestNormalizeStateCodes(t *testing.T) {
	got := normalizeStateCodes([]string{"implemented", " P ", "d", "bogus"})
	want := []string{"I", "P", "D", "bogus"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("normalizeStateCodes = %v, want %v", got, want)
	}
}

func TestExitCodeOf(t *testing.T) {
	if exitCodeOf(nil) != 0 {
		t.Error("nil → 0")
	}
	if exitCodeOf(errors.New("x")) != 1 {
		t.Error("plain error → 1")
	}
	wrapped := fmt.Errorf("outer: %w", withExitCode(exitUnavailable, errors.New("inner")))
	if exitCodeOf(wrapped) != exitUnavailable {
		t.Error("wrapped exitError must keep its code")
	}
	if withExitCode(2, nil) != nil {
		t.Error("withExitCode(nil) must be nil")
	}
}
