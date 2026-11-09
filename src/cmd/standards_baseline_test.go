package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/coverage"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestLoadCoverageBaseline(t *testing.T) {
	valid := `{"evaluator": {"provider": "null"}, "declared": [{"id": "owasp.asvs", "relation": "implements", "version": "5.0.0"}],
  "clauses": [{"standard": "owasp.asvs", "version": "5.0.0", "clause": "V1", "realm": "dev.app", "entity": "AUTH",
    "status": "covered", "rules": [{"rule": "dev.app:AUTH-001", "state": "I", "note": "AUTH-001/01"}]}],
  "figures": [{"tree": "dev.app", "standard": "owasp.asvs", "realm": "dev.std", "total": 4, "figure": 0.25}],
  "unresolved": [], "warnings": [], "gate": {"passed": false, "failures": []}}`
	tests := []struct {
		name, content, wantErr string
	}{
		{name: "missing", wantErr: "--baseline: open"},
		{name: "not JSON", content: "coverage", wantErr: "not a JSON report"},
		{name: "error document", content: `{"error": "no config"}`, wantErr: "not a coverage report"},
		{name: "no clauses key", content: `{"declared": []}`, wantErr: "not a coverage report"},
		{
			name:    "no realm",
			content: `{"declared": [], "clauses": [{"standard": "owasp.asvs", "clause": "V1", "entity": "AUTH", "status": "covered", "rules": []}]}`,
			wantErr: "owasp.asvs:V1 in AUTH has no realm",
		},
		{
			name:    "unknown status",
			content: `{"declared": [], "clauses": [{"standard": "owasp.asvs", "clause": "V1", "realm": "dev.app", "entity": "AUTH", "status": "done", "rules": []}]}`,
			wantErr: `unknown status "done"`,
		},
		{name: "empty report", content: `{"declared": [], "clauses": []}`},
		{name: "full report", content: valid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.content != "" {
				if err := os.WriteFile(filepath.Join(dir, "base.json"), []byte(tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := loadCoverageBaseline(dir, "base.json")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.content == valid {
				c := got.Clauses[0]
				if len(got.Declared) != 1 || c.Realm != "dev.app" || c.Status != coverage.StatusCovered || len(c.Rules) != 1 || c.Rules[0].Note != "AUTH-001/01" {
					t.Errorf("baseline = %+v", got)
				}
			}
		})
	}

	// An absolute path isn't joined to the working directory.
	path := filepath.Join(t.TempDir(), "base.json")
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCoverageBaseline(t.TempDir(), path); err != nil {
		t.Errorf("absolute path: %v", err)
	}
}

func TestParseCoverageGateBaseline(t *testing.T) {
	tests := []struct {
		name    string
		flags   coverageGateFlags
		wantErr string
	}{
		{name: "empty baseline", flags: coverageGateFlags{baselineSet: true, baseline: " "}, wantErr: "--baseline: want the path"},
		{name: "allow-rebase alone", flags: coverageGateFlags{allowRebase: true}, wantErr: "--allow-rebase allows a regression against the baseline, so it needs --baseline"},
		{name: "allow-excluded alone", flags: coverageGateFlags{allowExcluded: true}, wantErr: "--allow-excluded"},
		{name: "baseline", flags: coverageGateFlags{baselineSet: true, baseline: "b.json", allowRebase: true, allowExcluded: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseCoverageGate(&tt.flags)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !opts.enabled() || opts.baselinePath != "b.json" || !opts.allowRebase || !opts.allowExcluded {
				t.Errorf("opts = %+v", opts)
			}
		})
	}
}

// regressionBase is a baseline declaring owasp.asvs 4.0.3 as implements, with
// V1 covered and V2 mapped in AUTH of dev.app, each by its own note.
func regressionBase() *coverage.Report {
	clause := func(c string, st coverage.Status, rule string) coverage.Clause {
		return coverage.Clause{
			Standard: "owasp.asvs", Version: "4.0.3", Clause: c, Realm: "dev.app", Entity: "AUTH", Status: st,
			Rules: []coverage.Mapping{{Rule: rule, State: "I", Note: rule[len("dev.app:"):] + "/01"}},
		}
	}
	return &coverage.Report{
		Declared: []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "4.0.3"}},
		Clauses: []coverage.Clause{
			clause("V1", coverage.StatusCovered, "dev.app:AUTH-001"),
			clause("V2", coverage.StatusMapped, "dev.app:AUTH-002"),
		},
		Unresolved: []coverage.Mapping{},
	}
}

// regressionHead is the CLI report of regressionBase, which each case changes.
func regressionHead() *coverageOutput {
	base := regressionBase()
	out := &coverageOutput{Declared: base.Declared, Unresolved: []coverageMapping{}}
	for i := range base.Clauses {
		c := &base.Clauses[i]
		out.Clauses = append(out.Clauses, coverageClause{
			Standard: c.Standard, Version: c.Version, Clause: c.Clause, Realm: c.Realm, Entity: c.Entity,
			Status: string(c.Status), Rules: coverageMappings(c.Rules),
		})
	}
	return out
}

func TestCheckRegressions(t *testing.T) {
	tests := []struct {
		name                string
		change              func(h *coverageOutput)
		allowRebase, allowX bool
		want                []string // "<regression> <message>" of each failure
		wantWarnings        int
	}{
		{name: "identical", change: func(*coverageOutput) {}},
		{name: "improvement", change: func(h *coverageOutput) { h.Clauses[1].Status = string(coverage.StatusCovered) }},
		{
			name:   "downgraded",
			change: func(h *coverageOutput) { h.Clauses[0].Status = gateMapped },
			want:   []string{"downgraded owasp.asvs@4.0.3:V1 in AUTH (dev.app) was covered, is now mapped (downgraded)"},
		},
		{
			name:   "lost",
			change: func(h *coverageOutput) { h.Clauses = h.Clauses[:1] },
			want:   []string{"lost owasp.asvs@4.0.3:V2 in AUTH (dev.app) was mapped, is no longer in the report (lost)"},
		},
		{
			name: "excluded",
			change: func(h *coverageOutput) {
				h.Clauses[0].Status = string(coverage.StatusExcluded)
				h.Clauses[0].Exclusion = &coverageExclusion{Realm: "dev.app", Note: "REALM/001", Content: "Out of scope."}
			},
			want: []string{"excluded owasp.asvs@4.0.3:V1 in AUTH (dev.app) was covered, is now excluded by REALM/001 (excluded)"},
		},
		{
			name: "excluded allowed",
			change: func(h *coverageOutput) {
				h.Clauses[0].Status = string(coverage.StatusExcluded)
			},
			allowX:       true,
			wantWarnings: 1,
		},
		{
			name:   "dropped",
			change: func(h *coverageOutput) { h.Declared = nil },
			want:   []string{"dropped owasp.asvs@4.0.3 was declared as implements in the baseline, and no longer is (dropped)"},
		},
		{
			name: "rebased",
			change: func(h *coverageOutput) {
				h.Declared = []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0"}}
				h.Clauses = nil
			},
			want: []string{
				"rebased owasp.asvs@4.0.3 was declared as implements in the baseline, and only another edition now is (rebased)",
				"rebased owasp.asvs@4.0.3:V1 in AUTH (dev.app) was covered, and only another edition of the standard is now declared (rebased)",
				"rebased owasp.asvs@4.0.3:V2 in AUTH (dev.app) was mapped, and only another edition of the standard is now declared (rebased)",
			},
		},
		{
			name: "rebased allowed",
			change: func(h *coverageOutput) {
				h.Declared = []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0"}}
				h.Clauses = nil
			},
			allowRebase:  true,
			wantWarnings: 3,
		},
		{
			name: "unresolved",
			change: func(h *coverageOutput) {
				h.Unresolved = []coverageMapping{h.Clauses[0].Rules[0]}
				h.Clauses = h.Clauses[1:]
			},
			want: []string{
				"lost owasp.asvs@4.0.3:V1 in AUTH (dev.app) was covered, is no longer in the report (lost)",
				"unresolved the `standard` note AUTH-001/01 of dev.app:AUTH-001 resolved in the baseline and now resolves to no declared standard (unresolved)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := regressionHead()
			tt.change(head)
			g := evaluateCoverageGate(head, &coverageGateOptions{
				baselinePath: "base.json", baseline: regressionBase(), allowRebase: tt.allowRebase, allowExcluded: tt.allowX,
			})
			var got []string
			for _, f := range g.Failures {
				if f.Kind != gateRegression {
					t.Errorf("kind = %q, want %q", f.Kind, gateRegression)
				}
				got = append(got, f.Regression+" "+f.Message)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("failures:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			if g.Passed != (len(tt.want) == 0) {
				t.Errorf("passed = %v with %d failure(s)", g.Passed, len(got))
			}
			if len(g.warnings) != tt.wantWarnings {
				t.Errorf("warnings = %q, want %d", g.warnings, tt.wantWarnings)
			}
			for _, w := range g.warnings {
				testutil.AssertContains(t, w, "allowed regression against the baseline: ")
			}
		})
	}
}

// TestCheckRegressionsFields checks the fields of a clause regression.
func TestCheckRegressionsFields(t *testing.T) {
	head := regressionHead()
	head.Clauses[0].Status = gateMapped
	g := evaluateCoverageGate(head, &coverageGateOptions{baselinePath: "b", baseline: regressionBase()})
	if len(g.Failures) != 1 {
		t.Fatalf("failures = %+v", g.Failures)
	}
	f := g.Failures[0]
	if f.Standard != "owasp.asvs" || f.Version != "4.0.3" || f.Clause != "V1" || f.Realm != "dev.app" || f.Entity != "AUTH" ||
		f.Base != string(coverage.StatusCovered) || f.Head != gateMapped || len(f.Rules) != 1 || f.Rules[0].Rule != "dev.app:AUTH-001" {
		t.Errorf("failure = %+v", f)
	}
}

// writeCoverageAuth rewrites AUTH of setupCoverageProject with AUTH-001 and
// AUTH-002 in the given states and the given notes.
func writeCoverageAuth(t *testing.T, dir, state1, state2 string, pairs ...[2]string) {
	t.Helper()
	writeInheritFile(t, filepath.Join(dir, "rules"), "AUTH", withStandardNotes(inheritEntityJSON("AUTH", "", "",
		inheritRule{id: "AUTH-001", state: state1},
		inheritRule{id: "AUTH-002", state: state2},
		inheritRule{id: "AUTH-003", state: "I"},
		inheritRule{id: "AUTH-004", state: "I"}),
		pairs...))
}

// recordBaseline writes the project's --json report to base.json in dir.
func recordBaseline(t *testing.T, dir string) string {
	t.Helper()
	out, err := runCoverage(t, dir, "--json")
	assertExit(t, err, exitOK)
	path := filepath.Join(dir, "base.json")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStandardsCoverageBaseline records a baseline and gates changes of the
// project against it.
func TestStandardsCoverageBaseline(t *testing.T) {
	asvs := func(rule, clause string) [2]string { return [2]string{rule, "owasp.asvs:" + clause} }
	others := [][2]string{{"AUTH-003", "ietf.rfc2119:MUST"}, {"AUTH-004", "undeclared.std:1"}}
	tests := []struct {
		name           string
		state1, state2 string
		pairs          [][2]string
		wantExit       int
		want           string
	}{
		{name: "unchanged", state1: "I", state2: "P", pairs: [][2]string{asvs("AUTH-001", "2.1.1"), asvs("AUTH-002", "2.1.2")}, wantExit: exitOK},
		{name: "improved", state1: "I", state2: "I", pairs: [][2]string{asvs("AUTH-001", "2.1.1"), asvs("AUTH-002", "2.1.2")}, wantExit: exitOK},
		{
			name: "downgraded", state1: "P", state2: "P", pairs: [][2]string{asvs("AUTH-001", "2.1.1"), asvs("AUTH-002", "2.1.2")},
			wantExit: exitViolations, want: "owasp.asvs@4.0.3:2.1.1 in AUTH (dev.app) was covered, is now mapped (downgraded)",
		},
		{
			name: "lost", state1: "I", state2: "P", pairs: [][2]string{asvs("AUTH-001", "2.1.1")},
			wantExit: exitViolations, want: "owasp.asvs@4.0.3:2.1.2 in AUTH (dev.app) was mapped, is no longer in the report (lost)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupCoverageProject(t, "null", "./rules/")
			base := recordBaseline(t, dir)
			writeCoverageAuth(t, dir, tt.state1, tt.state2, append(tt.pairs, others...)...)

			out, err := runCoverage(t, dir, "--baseline", "base.json")
			assertExit(t, err, tt.wantExit)
			if tt.wantExit == exitOK {
				testutil.AssertContains(t, out, "Gate: passed")
				return
			}
			testutil.AssertContains(t, out, "Gate: 1 failure(s)\n  "+tt.want)

			out, err = runCoverage(t, dir, "--baseline", base, "--json")
			assertExit(t, err, exitViolations)
			var report coverageOutput
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatal(err)
			}
			if g := report.Gate; g == nil || len(g.Failures) != 1 || g.Failures[0].Kind != gateRegression ||
				g.Failures[0].Regression != tt.name || g.Failures[0].Message != tt.want {
				t.Errorf("gate = %+v", report.Gate)
			}
		})
	}
}

// TestStandardsCoverageBaselineErrors exits 2 on a baseline it can't use,
// and when resolution fails whatever the baseline.
func TestStandardsCoverageBaselineErrors(t *testing.T) {
	dir := setupCoverageProject(t, "null", "./rules/")
	base := recordBaseline(t, dir)

	_, err := runCoverage(t, dir, "--baseline", "missing.json")
	assertExit(t, err, exitConfig)
	_, err = runCoverage(t, dir, "--allow-rebase")
	assertExit(t, err, exitConfig)
	_, err = runCoverage(t, dir, "--baseline", "")
	assertExit(t, err, exitConfig)
	out, err := runCoverage(t, dir, "--baseline", "missing.json", "--json")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, out, `"error"`)

	// A baseline from before clauses had a realm.
	var doc map[string]any
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc["clauses"].([]any) {
		delete(c.(map[string]any), "realm")
	}
	writeJSONFile(t, filepath.Join(dir, "old.json"), doc)
	_, err = runCoverage(t, dir, "--baseline", "old.json")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "has no realm")
	testutil.AssertContains(t, err.Error(), "record the baseline again")

	// A valid baseline doesn't make a broken dependency pass.
	if err := os.RemoveAll(filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	_, err = runCoverage(t, dir, "--baseline", "base.json")
	assertExit(t, err, exitConfig)
}

// TestStandardsCoverageBaselineExcluded fails a covered clause an exclusion
// note now excludes, unless --allow-excluded allows it.
func TestStandardsCoverageBaselineExcluded(t *testing.T) {
	base := recordBaseline(t, setupLinkedCoverageProject(t, entity.RelationImplements))
	dir := setupLinkedCoverageProject(t, entity.RelationImplements, linkedExclusion(1, "V1", "Out of scope."))

	out, err := runCoverage(t, dir, "--baseline", base)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "owasp.asvs@5.0.0:V1 in AUTH (dev.app) was covered, is now excluded by REALM/001 (excluded)")

	out, err = runCoverage(t, dir, "--baseline", base, "--allow-excluded")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "warning: allowed regression against the baseline: owasp.asvs@5.0.0:V1 in AUTH (dev.app)")
	testutil.AssertContains(t, out, "Gate: passed")
}

// TestStandardsCoverageJSONHasRealm checks each clause of --json names its
// Realm tree, so the report can be a baseline.
func TestStandardsCoverageJSONHasRealm(t *testing.T) {
	report := coverageJSON(t, setupCoverageProject(t, "null", "./rules/"))
	if len(report.Clauses) == 0 {
		t.Fatal("no clause")
	}
	for i := range report.Clauses {
		// BILL inherits from dev.lib, but stands in the tree of dev.app.
		if c := &report.Clauses[i]; c.Realm != "dev.app" {
			t.Errorf("clause %s:%s in %s: realm = %q, want dev.app", c.Standard, c.Clause, c.Entity, c.Realm)
		}
	}
}
