package cmd

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// ruleSpy records the ID of every rule sent to the provider.
type ruleSpy struct {
	stubProvider
	ids []string
}

func (s *ruleSpy) Evaluate(ctx context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	s.mu.Lock()
	s.ids = append(s.ids, req.Rule.ID)
	s.mu.Unlock()
	return s.stubProvider.Evaluate(ctx, req)
}

func (s *ruleSpy) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ids)
}

const standardEntityJSON = `{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "STD", "title": "Standard", "description": "A transcribed standard."},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [
    {"id": "STD-001", "revision": 0, "state": "P", "body": "The subject of the first clause MUST hold.",
     "added_by": "github:test", "added_at": "2026-01-01", "supersedes": null, "clause": "1.1"}
  ],
  "notes": []
}`

// addStandardSource adds to the project in dir a second rule source, std/,
// holding a Standard Realm with the Published rule STD-001.
func addStandardSource(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "std/realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.std", "title": "t", "version": "1.0.0", "kind": "standard"}}`)
	writeFile(t, dir, "std/STD.json", standardEntityJSON)
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules/\n    scope: local\n  - source: ./std/\n    scope: local\n"+
		"evaluator:\n  provider: claude\n  effort: low\ncache:\n  ttl: 24h\nfail_on: [I]\n")
}

func hasCode(diags []explainedDiagnostic, code string) bool {
	return slices.ContainsFunc(diags, func(d explainedDiagnostic) bool { return d.Code == code })
}

// A directly configured Standard Realm source is not sent to the provider
// (STDRLM-011), and the code rules beside it still are.
func TestCheckSkipsStandardRealmSource(t *testing.T) {
	dir, diff := setupCheckProject(t, "claude", []string{"I"})
	addStandardSource(t, dir)
	spy := &ruleSpy{}
	stubEvalProvider(t, spy, nil)

	out, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--format", "json")
	assertExit(t, err, exitOK)
	sent := spy.sent()
	if slices.Contains(sent, "STD-001") {
		t.Errorf("the Standard Realm rule was sent: %v", sent)
	}
	if !slices.Contains(sent, "TST-001") {
		t.Errorf("the code rules were not sent: %v", sent)
	}
	var rep checkOutput
	if jerr := json.Unmarshal([]byte(out), &rep); jerr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jerr, out)
	}
	if !hasCode(rep.Diagnostics, ruleeval.CodeStandardSkipped) {
		t.Errorf("no RE005 in %+v", rep.Diagnostics)
	}
}

// rule verify skips it too; the skip blocks (RE006) because verify judges
// the rules it is given whatever their state.
func TestRuleVerifySkipsStandardRealmSource(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	addStandardSource(t, dir)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "std")
	spy := &ruleSpy{}
	stubEvalProvider(t, spy, nil)

	out, err := runVerifyCmd(t, "STD-001", "TST-002", "--project", dir, "--format", "json")
	assertExit(t, err, exitViolations)
	if sent := spy.sent(); slices.Contains(sent, "STD-001") || !slices.Contains(sent, "TST-002") {
		t.Errorf("sent %v, want TST-002 and not STD-001", sent)
	}
	if vo := decodeVerify(t, out); !hasCode(vo.Diagnostics, ruleeval.CodeUnsafeSkip) {
		t.Errorf("no RE006 in %+v", vo.Diagnostics)
	}
}
