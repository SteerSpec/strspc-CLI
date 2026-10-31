package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// inheritRule is a rule of a test entity written by writeInheritEntity.
type inheritRule struct {
	id, state, overrides string
}

// writeInheritEntity writes an entity file named after euid.
func writeInheritEntity(t *testing.T, dir, euid, inherits string, rules ...inheritRule) {
	t.Helper()
	writeInheritFile(t, dir, euid, inheritEntityJSON(euid, inherits, "", rules...))
}

func writeInheritFile(t *testing.T, dir, euid, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, euid+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func inheritEntityJSON(euid, inherits, subEntities string, rules ...inheritRule) string {
	inheritsField := ""
	if inherits != "" {
		inheritsField = fmt.Sprintf(`, "inherits": %q`, inherits)
	}
	entries := make([]string, 0, len(rules))
	for _, r := range rules {
		overrides := ""
		if r.overrides != "" {
			overrides = fmt.Sprintf(`, "overrides": %q`, r.overrides)
		}
		entries = append(entries, fmt.Sprintf(`{"id": %q, "revision": 0, "state": %q, "body": "Rule %s.",`+
			` "added_by": "@test", "added_at": "2026-01-01", "supersedes": null%s}`, r.id, r.state, r.id, overrides))
	}
	if subEntities == "" {
		subEntities = "[]"
	}
	return fmt.Sprintf(`{"$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": %q, "title": "Title %s", "description": "d"%s},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [%s], "notes": [], "sub_entities": %s}`, euid, euid, inheritsField, strings.Join(entries, ","), subEntities)
}

func writeRealmManifest(t *testing.T, dir, id string, deps ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"realm": {"id": %q, "title": "t", "version": "1.0.0"}, "dependencies": [%s]}`, id, strings.Join(deps, ","))
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupInheritProject writes the realm dev.app in rules/ depending on the
// realm dev.lib in lib/, which holds ACCT with two Published rules. The
// config lists the given sources.
func setupInheritProject(t *testing.T, provider string, sources ...string) (dir, diffFile string) {
	t.Helper()
	dir = t.TempDir()
	writeRealmManifest(t, filepath.Join(dir, "rules"), "dev.app",
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`)
	lib := filepath.Join(dir, "lib")
	writeRealmManifest(t, lib, "dev.lib")
	writeInheritEntity(t, lib, "ACCT", "", inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "I"})

	var b strings.Builder
	b.WriteString("rules:\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "  - source: %s\n    scope: local\n", s)
	}
	fmt.Fprintf(&b, "evaluator:\n  provider: %s\n  effort: low\ncache:\n  ttl: 24h\n", provider)
	writeRawCheckConfig(t, dir, b.String())

	diffFile = filepath.Join(dir, "change.diff")
	if err := os.WriteFile(diffFile, []byte(checkDiff), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, diffFile
}

func checkRuleCount(t *testing.T, dir string) int {
	t.Helper()
	output, err := runCheckCmd(t, dir, "--static-only", "--format", "json")
	assertExit(t, err, exitOK)
	var out checkOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	return out.RuleCount
}

// recordingProvider records the requests it receives, by rule ID.
type recordingProvider struct {
	stubProvider
	mu   sync.Mutex
	reqs []ruleeval.EvalRequest
}

func (p *recordingProvider) Evaluate(ctx context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	return p.stubProvider.Evaluate(ctx, req)
}

// requests returns the requests made for ruleID.
func (p *recordingProvider) requests(ruleID string) []ruleeval.EvalRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []ruleeval.EvalRequest
	for i := range p.reqs {
		if p.reqs[i].Rule.ID == ruleID {
			out = append(out, p.reqs[i])
		}
	}
	return out
}

func TestCheckStaticOnlyFailsOnMistypedInherits(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCNT", inheritRule{id: "SAVACCT-001", state: "P"})

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, output, "RR013")
	testutil.AssertContains(t, output, "dev.lib:ACCNT")
}

func TestCheckStaticOnlyFailsOnMistypedInheritsWithoutDependencies(t *testing.T) {
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	writeRealmManifest(t, rules, "dev.app")
	writeInheritEntity(t, rules, "SAVACCT", "ACCNT", inheritRule{id: "SAVACCT-001", state: "P"})
	writeCheckConfig(t, dir, "./rules/", "null", nil, nil)

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, output, "RR013")
}

// A realm with no dependencies, sub-realms or inheritance can still hold a
// relation note qualified with a realm it never declared, so realm-resolve
// runs for every realm.
func TestCheckResolvesRealmWithNothingToResolve(t *testing.T) {
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	writeRealmManifest(t, rules, "dev.app")
	writeInheritEntity(t, rules, "SAVACCT", "", inheritRule{id: "SAVACCT-001", state: "P"})
	writeCheckConfig(t, dir, "./rules/", "null", nil, nil)

	output, err := runCheckCmd(t, dir, "--static-only", "--verbose")
	if err != nil {
		t.Fatalf("check: %v\n%s", err, output)
	}
	testutil.AssertContains(t, output, "realm dev.app: 0 dependency(ies) resolved")
}

func TestCheckStaticOnlyFailsOnUnknownOverrideTarget(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-009"})

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, output, "RR015")
}

func TestCheckWarnsOnOverrideOfUnpublishedRule(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "",
		inheritRule{id: "ACCT-001", state: "D"}, inheritRule{id: "ACCT-002", state: "I"})
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "RR016")
}

func TestCheckStaticOnlyFailsOnOverrideWithoutInherits(t *testing.T) {
	override := inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"}
	tests := []struct {
		name, body string
	}{
		{"entity", inheritEntityJSON("SAVACCT", "", "", override)},
		{"sub-entity", inheritEntityJSON("TOP", "", "["+inheritEntityJSON("SAVACCT", "", "", override)+"]")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			rules := filepath.Join(dir, "rules")
			writeRealmManifest(t, rules, "dev.app")
			writeInheritFile(t, rules, "SAVACCT", tt.body)
			writeCheckConfig(t, dir, "./rules/", "null", nil, nil)

			output, err := runCheckCmd(t, dir, "--static-only")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, output, "RR015")
		})
	}
}

// The chains of a check are keyed by EUID alone because this holds.
func TestCheckRejectsEUIDInTwoConfiguredSources(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/", "./other/")
	writeRealmManifest(t, filepath.Join(dir, "other"), "dev.other")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "", inheritRule{id: "SAVACCT-001", state: "P"})
	sub := inheritEntityJSON("SAVACCT", "", "", inheritRule{id: "SAVACCT-002", state: "P"})
	writeInheritFile(t, filepath.Join(dir, "other"), "OTHER", inheritEntityJSON("OTHER", "", "["+sub+"]"))

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, output, "RSV005")
}

func TestCheckCountsInheritedRules(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P"})

	if got := checkRuleCount(t, dir); got != 3 {
		t.Errorf("rule_count = %d, want 3 (1 own + 2 inherited)", got)
	}
}

func TestCheckOverrideReplacesInheritedRule(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})

	if got := checkRuleCount(t, dir); got != 2 {
		t.Errorf("rule_count = %d, want 2 (1 own + 1 inherited)", got)
	}
}

func TestCheckCountsInheritedRulesWithoutDependencies(t *testing.T) {
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	writeRealmManifest(t, rules, "dev.app")
	writeInheritEntity(t, rules, "ACCT", "", inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "I"})
	writeInheritEntity(t, rules, "SAVACCT", "ACCT", inheritRule{id: "SAVACCT-001", state: "P"})
	writeCheckConfig(t, dir, "./rules/", "null", nil, nil)

	if got := checkRuleCount(t, dir); got != 5 {
		t.Errorf("rule_count = %d, want 5 (2 ACCT + 1 own + 2 inherited)", got)
	}
}

func TestCheckCountsRulesInheritedBySubEntity(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	sub := inheritEntityJSON("SAVSUB", "dev.lib:ACCT", "", inheritRule{id: "SAVSUB-001", state: "P"})
	writeInheritFile(t, filepath.Join(dir, "rules"), "SAVACCT",
		inheritEntityJSON("SAVACCT", "", "["+sub+"]", inheritRule{id: "SAVACCT-001", state: "P"}))

	if got := checkRuleCount(t, dir); got != 4 {
		t.Errorf("rule_count = %d, want 4 (2 own + 2 inherited by the sub-entity)", got)
	}
}

func TestCheckSendsInheritedRulesToTheProvider(t *testing.T) {
	dir, diff := setupInheritProject(t, "claude", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})
	p := &recordingProvider{}
	stubEvalProvider(t, p, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)

	if got := p.requests("ACCT-001"); len(got) != 0 {
		t.Errorf("overridden rule ACCT-001 was evaluated: %+v", got)
	}
	inherited := p.requests("ACCT-002")
	if len(inherited) != 1 {
		t.Fatalf("ACCT-002 requests = %d, want 1", len(inherited))
	}
	if inherited[0].Owner != "dev.lib:ACCT" || inherited[0].InheritedBy != "SAVACCT" {
		t.Errorf("owner=%q inherited_by=%q", inherited[0].Owner, inherited[0].InheritedBy)
	}
	own := p.requests("SAVACCT-001")
	if len(own) != 1 || own[0].Overrides != "dev.lib:ACCT-001" {
		t.Errorf("SAVACCT-001 requests = %+v, want overrides dev.lib:ACCT-001", own)
	}
}

func TestCheckOverrideOutsideEvaluateOnRestoresInheritedRule(t *testing.T) {
	dir, diff := setupInheritProject(t, "claude", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "D", overrides: "dev.lib:ACCT-001"})
	writeCheckConfig(t, dir, "./rules/", "claude", nil, []string{"P", "I"})
	p := &recordingProvider{}
	stubEvalProvider(t, p, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)

	if got := p.requests("ACCT-001"); len(got) != 1 {
		t.Errorf("ACCT-001 requests = %d, want 1: its override is outside evaluate_on", len(got))
	}
}

func TestCheckAnnotatesOverriddenRuleOfConfiguredParent(t *testing.T) {
	dir, diff := setupInheritProject(t, "claude", "./rules/", "./lib/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})
	p := &recordingProvider{}
	stubEvalProvider(t, p, nil)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)

	got := p.requests("ACCT-001")
	if len(got) != 1 {
		t.Fatalf("ACCT-001 requests = %d, want 1 from the configured parent", len(got))
	}
	want := []string{"dev.app:SAVACCT-001 (Title SAVACCT)"}
	if !slices.Equal(got[0].OverriddenBy, want) {
		t.Errorf("overridden_by = %v, want %v", got[0].OverriddenBy, want)
	}
}

func TestCheckWarnsWhenInheritsHasNoRealm(t *testing.T) {
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P"})
	writeCheckConfig(t, dir, "./rules/", "null", nil, nil)

	output, err := runCheckCmd(t, dir, "--static-only")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "CHK003")
	testutil.AssertContains(t, output, "SAVACCT")
}
