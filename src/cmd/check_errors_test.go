package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/evalcache"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/providers"
)

// reportingProvider is a stub that also names its model and reports usage,
// as the real Claude provider does.
type reportingProvider struct {
	stubProvider
	model string
	usage ruleeval.Usage
}

func (p *reportingProvider) Model() string         { return p.model }
func (p *reportingProvider) PromptVersion() string { return "test" }
func (p *reportingProvider) Usage() ruleeval.Usage { return p.usage }

func TestCheckConfigLoadErrors(t *testing.T) {
	const tail = "evaluator:\n  provider: null\nfail_on: [I]\n"
	tests := []struct {
		name, config, want string
	}{
		{"unparsable YAML", "rules: [\n", "parsing config.yaml"},
		{"unknown scope", "rules:\n  - source: ./rules/\n    scope: everywhere\n" + tail, "invalid scope"},
		{"bad cache ttl", "rules:\n  - source: ./rules/\n    scope: local\n" + tail + "cache:\n  ttl: forever\n", "parsing cache.ttl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRawCheckConfig(t, dir, tt.config)
			_, err := runCheckCmd(t, dir)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestCheckModelWithControlCharactersRefused(t *testing.T) {
	t.Run("provider from the flag", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, &stubProvider{}, nil)
		_, err := runCheckCmd(t, dir, "--diff-file", diff, "--provider", "cla\x07ude")
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "evaluator.provider")
	})
	t.Run("from the flag", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, &stubProvider{}, nil)
		_, err := runCheckCmd(t, dir, "--diff-file", diff, "--model", "m\x1b[2J")
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "evaluator.model")
	})
	t.Run("named by the provider", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, &reportingProvider{model: "m\x07"}, nil)
		_, err := runCheckCmd(t, dir, "--diff-file", diff)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "evaluator.model")
	})
}

func TestCheckProviderConstructionOutcomes(t *testing.T) {
	t.Run("static provider", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, nil, providers.ErrStatic)
		out, err := runCheckCmd(t, dir, "--diff-file", diff)
		assertExit(t, err, exitOK)
		testutil.AssertContains(t, out, "(static-only)")
	})
	t.Run("other error", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, nil, errors.New("bad effort"))
		_, err := runCheckCmd(t, dir, "--diff-file", diff)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), `configuring evaluator "claude": bad effort`)
	})
}

// The JSON report names the provider's model and carries its usage, and the
// verbose lines describe the run.
func TestCheckJSONReportsProviderModelAndUsage(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, &reportingProvider{model: "claude-test", usage: ruleeval.Usage{Requests: 2, InputTokens: 10}}, nil)
	out, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--format", "json", "--verbose")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "evaluator: claude model=claude-test")
	testutil.AssertContains(t, out, "usage: 2 request(s)")
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON document in output:\n%s", out)
	}
	var doc checkOutput
	if err := json.NewDecoder(strings.NewReader(out[start:])).Decode(&doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Model != "claude-test" || doc.Usage == nil || doc.Usage.Requests != 2 {
		t.Errorf("model = %q, usage = %+v; want claude-test with 2 requests", doc.Model, doc.Usage)
	}
}

func TestCheckJSONWriteFailure(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})
	root := NewRootCmd()
	root.SetOut(failingWriter{})
	root.SetErr(&strings.Builder{})
	root.SetArgs([]string{"check", dir, "--format", "json"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "writing JSON output") || !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("error %v, want the JSON write failure", err)
	}
}

func TestCheckUnloadableRealmManifest(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})
	if err := os.WriteFile(filepath.Join(dir, "rules", "realm.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCheckCmd(t, dir)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, out, "RR008")
	testutil.AssertContains(t, out, "cannot load realm.json")
}

func TestCheckMissingDiffFile(t *testing.T) {
	dir, _ := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, &stubProvider{}, nil)
	_, err := runCheckCmd(t, dir, "--diff-file", filepath.Join(dir, "missing.diff"))
	if err == nil {
		t.Fatal("expected an error for a missing diff file")
	}
	testutil.AssertContains(t, err.Error(), "missing.diff")
}

// A source naming the project directory itself is labelled "./".
func TestCheckSourceIsTheProjectDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TST.json"), []byte(checkEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCheckConfig(t, dir, "./", "null", []string{"I"}, nil)
	out, err := runCheckCmd(t, dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Checked 4 rule(s) from 1 source(s)")
}

func TestCheckCacheStates(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, &stubProvider{}, nil)
		if err := os.MkdirAll(filepath.Join(dir, ".strspc", "cache.db"), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := runCheckCmd(t, dir, "--diff-file", diff)
		assertExit(t, err, exitOK)
		testutil.AssertContains(t, out, "evaluation cache unavailable")
	})
	t.Run("locked", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		stubEvalProvider(t, &stubProvider{}, nil)
		holder, err := evalcache.Open(context.Background(), filepath.Join(dir, ".strspc", "cache.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Close() }()
		out, err := runCheckCmd(t, dir, "--diff-file", diff)
		assertExit(t, err, exitOK)
		testutil.AssertContains(t, out, "locked by another process")
	})
	t.Run("expired entries are pruned", func(t *testing.T) {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		p := &batchStubProvider{stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
			"TST-001": {Verdict: ruleeval.Compliant, Explanation: "ok"},
		}}}
		stubEvalProvider(t, p, nil)
		cfg := "rules:\n  - source: ./rules/\n    scope: local\nevaluator:\n  provider: claude\ncache:\n  ttl: 1ns\nfail_on: [I]\n"
		writeRawCheckConfig(t, dir, cfg)
		if _, err := runCheckCmd(t, dir, "--diff-file", diff); exitCodeOf(err) != exitOK {
			t.Fatalf("first run: %v", err)
		}
		out, err := runCheckCmd(t, dir, "--diff-file", diff, "--verbose")
		assertExit(t, err, exitOK)
		testutil.AssertContains(t, out, "cache: pruned")
	})
}

// A GITHUB_STEP_SUMMARY that can't be written is a warning; the annotations
// are still printed and the exit code is unchanged.
func TestCheckGitHubStepSummaryUnwritable(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})
	t.Setenv("GITHUB_STEP_SUMMARY", t.TempDir())
	out, err := runCheckCmd(t, dir, "--format", "github")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "writing GITHUB_STEP_SUMMARY")
	testutil.AssertContains(t, out, "Checked 4 rule(s)")
}
