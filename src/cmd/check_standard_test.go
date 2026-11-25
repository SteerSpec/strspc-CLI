package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
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

// standardEntityJSON is the entity STD of a Standard Realm, with the
// Published rule STD-001 and the rules it lists after it.
const standardEntityJSON = `{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "STD", "title": "Standard", "description": "A transcribed standard."},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [
    {"id": "STD-001", "revision": 0, "state": "P", "body": "The subject of the first clause MUST hold.",
     "added_by": "github:test", "added_at": "2026-01-01", "supersedes": null, "clause": "1.1"}%s
  ],
  "notes": []
}`

// implementedStandardRule is an Implemented rule of a Standard Realm, whose
// skip would switch off enforcement (RE006).
const implementedStandardRule = `,
    {"id": "STD-002", "revision": 0, "state": "I", "body": "The subject of the second clause MUST hold.",
     "added_by": "github:test", "added_at": "2026-01-01", "supersedes": null, "clause": "1.2"}`

// addStandardSource adds to the project in dir a second rule source, std/,
// holding a Standard Realm with STD-001 and the rules in more.
func addStandardSource(t *testing.T, dir, more string) {
	t.Helper()
	writeFile(t, dir, "std/realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.std", "title": "t", "version": "1.0.0", "kind": "standard"}}`)
	writeFile(t, dir, "std/STD.json", fmt.Sprintf(standardEntityJSON, more))
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules/\n    scope: local\n  - source: ./std/\n    scope: local\n"+
		"evaluator:\n  provider: claude\n  effort: low\ncache:\n  ttl: 24h\nfail_on: [I]\n")
}

func hasCode(diags []explainedDiagnostic, code string) bool {
	return slices.ContainsFunc(diags, func(d explainedDiagnostic) bool { return d.Code == code })
}

// A directly configured Standard Realm source is never sent to the provider
// (STDRLM-011), while the code rules beside it are. Its skip is reported in
// static-only mode and for an empty diff too, and blocks when it would switch
// off enforcement.
func TestCheckSkipsStandardRealmSource(t *testing.T) {
	for name, c := range map[string]struct {
		args     []string
		empty    bool
		more     string
		wantExit int
		wantCode string
		sendsTST bool
	}{
		"published":               {wantExit: exitOK, wantCode: ruleeval.CodeStandardSkipped, sendsTST: true},
		"implemented":             {more: implementedStandardRule, wantExit: exitViolations, wantCode: ruleeval.CodeUnsafeSkip, sendsTST: true},
		"static-only":             {args: []string{"--static-only"}, wantExit: exitOK, wantCode: ruleeval.CodeStandardSkipped},
		"static-only implemented": {args: []string{"--static-only"}, more: implementedStandardRule, wantExit: exitViolations, wantCode: ruleeval.CodeUnsafeSkip},
		"empty diff":              {empty: true, wantExit: exitOK, wantCode: ruleeval.CodeStandardSkipped},
		"empty diff implemented":  {empty: true, more: implementedStandardRule, wantExit: exitViolations, wantCode: ruleeval.CodeUnsafeSkip},
	} {
		t.Run(name, func(t *testing.T) {
			dir, diff := setupCheckProject(t, "claude", []string{"I"})
			addStandardSource(t, dir, c.more)
			if c.empty {
				diff = filepath.Join(dir, "empty.diff")
				writeFile(t, dir, "empty.diff", "")
			}
			spy := &ruleSpy{}
			stubEvalProvider(t, spy, nil)

			args := append([]string{dir, "--diff-file", diff, "--no-cache", "--format", "json"}, c.args...)
			out, err := runCheckCmd(t, args...)
			assertExit(t, err, c.wantExit)
			sent := spy.sent()
			if slices.ContainsFunc(sent, func(id string) bool { return strings.HasPrefix(id, "STD-") }) {
				t.Errorf("a Standard Realm rule was sent: %v", sent)
			}
			if c.sendsTST != slices.Contains(sent, "TST-001") {
				t.Errorf("sent %v; code rules sent: want %v", sent, c.sendsTST)
			}
			var rep checkOutput
			if jerr := json.Unmarshal([]byte(out), &rep); jerr != nil {
				t.Fatalf("output is not valid JSON: %v\n%s", jerr, out)
			}
			if !hasCode(rep.Diagnostics, c.wantCode) {
				t.Errorf("no %s in %+v", c.wantCode, rep.Diagnostics)
			}
			if want := 1 + strings.Count(c.more, `"id"`); rep.StandardSkipped != want {
				t.Errorf("standard_skipped = %d, want %d", rep.StandardSkipped, want)
			}
		})
	}
}

// rule verify skips it too; the skip blocks (RE006) because verify judges
// the rules it is given whatever their state.
func TestRuleVerifySkipsStandardRealmSource(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	addStandardSource(t, dir, "")
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

	// The estimate leaves out what won't be sent.
	out, err = runVerifyCmd(t, "STD-001", "--project", dir, "--format", "json", "--dry-run")
	assertExit(t, err, exitOK)
	if est := decodeVerify(t, out).Estimate; est.RuleGroups != 0 || est.EstRequests != 0 {
		t.Errorf("estimate = %+v, want no rule group and no request", est)
	}
}

// The count is in every report, 0 without a Standard Realm.
func TestCheckJSONAlwaysCountsStandardSkips(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", nil)
	out, err := runCheckCmd(t, dir, "--format", "json")
	assertExit(t, err, exitOK)
	var rep map[string]any
	if jerr := json.Unmarshal([]byte(out), &rep); jerr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jerr, out)
	}
	if v, ok := rep["standard_skipped"]; !ok || v != float64(0) {
		t.Errorf("standard_skipped = %v (present %v), want 0", v, ok)
	}
}

func TestCheckSummaryCountsStandardSkips(t *testing.T) {
	r := &checkRun{}
	line := r.summaryLine(&checkOutput{RuleCount: 5, SourceCount: 2, StaticOnly: true, StandardSkipped: 2})
	if want := "Checked 5 rule(s) from 2 source(s), 2 of a Standard Realm not evaluated (static-only)"; line != want {
		t.Errorf("summary = %q, want %q", line, want)
	}
}
