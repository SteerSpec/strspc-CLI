package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestParseFailOnStatuses(t *testing.T) {
	tests := []struct {
		in      []string
		want    []string
		wantErr string
	}{
		{in: nil, want: nil},
		{in: []string{"lapsed", " mapped ", "", "lapsed", "unresolved"}, want: []string{"lapsed", "mapped", "unresolved"}},
		{in: []string{"cited"}, wantErr: "STDREF-006"},
		{in: []string{"unmapped"}, wantErr: "--min-coverage"},
		{in: []string{"covered"}, wantErr: "must be one of lapsed, mapped, unresolved"},
	}
	for _, tt := range tests {
		got, err := parseFailOnStatuses(tt.in)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%q: err = %v, want %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("%q = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestParseStandardSelector(t *testing.T) {
	for in, want := range map[string]standardSelector{
		"owasp.asvs":       {id: "owasp.asvs"},
		"owasp.asvs@5.0.0": {id: "owasp.asvs", version: "5.0.0"},
	} {
		got, err := parseStandardSelector("--require", in)
		if err != nil || got != want {
			t.Errorf("%q = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "@5.0.0", "owasp.asvs@", "owasp asvs", "a\x1b[31m"} {
		if _, err := parseStandardSelector("--require", in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestParsePercent(t *testing.T) {
	for in, want := range map[string]int{"0": 0, "80": 800, "82.5": 825, "100": 1000, "75 %": 750, "100.0": 1000} {
		if got, ok := parsePercent(in); !ok || got != want {
			t.Errorf("%q = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "%", "100.1", "101", "-1", "80.25", "1e2", "eighty", "1000"} {
		if got, ok := parsePercent(in); ok {
			t.Errorf("%q = %d, want refused", in, got)
		}
	}
}

func TestParseMinCoverage(t *testing.T) {
	got, err := parseMinCoverage([]string{"60", "owasp.asvs=80", "owasp.asvs@5.0.0=90%"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].standard != nil || got[0].tenths != 600 ||
		*got[1].standard != (standardSelector{id: "owasp.asvs"}) || got[1].tenths != 800 ||
		*got[2].standard != (standardSelector{id: "owasp.asvs", version: "5.0.0"}) || got[2].tenths != 900 {
		t.Errorf("thresholds = %+v", got)
	}
	for _, in := range [][]string{{"60", "70"}, {"a=60", "a=70"}, {"a=x"}, {"=60"}, {"a@=60"}} {
		if _, err := parseMinCoverage(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestParseCoverageGate(t *testing.T) {
	opts, err := parseCoverageGate(&coverageGateFlags{require: []string{"a", "a", "b@1"}})
	if err != nil || len(opts.require) != 2 || !opts.enabled() {
		t.Errorf("opts = %+v, %v; want a and b@1, enabled", opts, err)
	}
	if opts, err := parseCoverageGate(&coverageGateFlags{failOn: []string{""}}); err != nil || opts.enabled() {
		t.Errorf("a blank --fail-on: opts = %+v, %v; want the gate off", opts, err)
	}
	for _, f := range []coverageGateFlags{{failOn: []string{"cited"}}, {require: []string{"@"}}, {minCoverage: []string{"x"}}} {
		if _, err := parseCoverageGate(&f); err == nil {
			t.Errorf("%+v: want an error", f)
		}
	}
}

// gateReport is a report with one clause of each class, an unresolved note and
// a Realm figure of 2 of 4 counted clauses.
func gateReport() *coverageOutput {
	half := 0.5
	return &coverageOutput{
		Evaluator: coverageEvaluator{Provider: "claude", Usable: true, RunsOnImplemented: true},
		Declared: []entity.StandardRef{
			{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0"},
			{ID: "ietf.rfc2119", Relation: entity.RelationDerivedFrom},
			{ID: "nist.ssdf", Relation: entity.RelationImplements, Version: "1.1"},
		},
		Clauses: []coverageClause{
			{Standard: "ietf.rfc2119", Clause: "MUST", Entity: "AUTH", Status: "cited"},
			{Standard: "owasp.asvs", Version: "5.0.0", Clause: "V1", Entity: "AUTH", Status: "covered"},
			{Standard: "owasp.asvs", Version: "5.0.0", Clause: "V2", Entity: "AUTH", Status: "mapped", Rules: []coverageMapping{{Rule: "dev.app:AUTH-002", State: "P"}}},
			{Standard: "owasp.asvs", Version: "5.0.0", Clause: "V3", Entity: "BILL", Status: "lapsed"},
			{Standard: "owasp.asvs", Version: "5.0.0", Clause: "V4", Entity: "BILL", Status: "unmapped"},
			{Standard: "owasp.asvs", Version: "5.0.0", Clause: "V5", Entity: "BILL", Status: "excluded"},
		},
		Figures: []coverageFigure{
			{Tree: "dev.app", Standard: "owasp.asvs", Version: "5.0.0", Total: 5, Excluded: 1, Covered: 2, Figure: &half},
			{Tree: "dev.app", Entity: "AUTH", Standard: "owasp.asvs", Version: "5.0.0", Total: 5, Excluded: 1, Covered: 0},
		},
		Unresolved: []coverageMapping{{Rule: "dev.app:AUTH-004", Note: "AUTH-004/01"}},
	}
}

func gateKinds(g *coverageGate) []string {
	kinds := make([]string, 0, len(g.Failures))
	for i := range g.Failures {
		kinds = append(kinds, g.Failures[i].Kind)
	}
	return kinds
}

func TestEvaluateCoverageGateFailOn(t *testing.T) {
	g := evaluateCoverageGate(gateReport(), &coverageGateOptions{failOn: []string{"lapsed", "mapped", "unresolved"}})
	if g.Passed || !slices.Equal(gateKinds(g), []string{"mapped", "lapsed", "unresolved"}) {
		t.Fatalf("gate = %+v, want mapped, lapsed and unresolved, never cited, unmapped or excluded", g)
	}
	if f := g.Failures[0]; f.Clause != "V2" || f.Entity != "AUTH" || f.Version != "5.0.0" || len(f.Rules) != 1 ||
		f.Message != "owasp.asvs@5.0.0:V2 is mapped in AUTH: only a rule not yet Implemented maps it" {
		t.Errorf("mapped failure = %+v", f)
	}
	testutil.AssertContains(t, g.Failures[1].Message, "retired or replaced")
	testutil.AssertContains(t, g.Failures[2].Message, "AUTH-004/01 of dev.app:AUTH-004")

	if g := evaluateCoverageGate(gateReport(), &coverageGateOptions{failOn: []string{"lapsed"}}); !slices.Equal(gateKinds(g), []string{"lapsed"}) {
		t.Errorf("--fail-on lapsed = %v", gateKinds(g))
	}
}

func TestEvaluateCoverageGateRequire(t *testing.T) {
	report := gateReport()
	report.Declared = append(report.Declared, entity.StandardRef{ID: "ietf.rfc2119", Relation: entity.RelationDerivedFrom, Version: "2"})
	tests := []struct {
		sel  standardSelector
		want string // empty: passes
	}{
		{standardSelector{id: "owasp.asvs"}, ""},
		{standardSelector{id: "owasp.asvs", version: "5.0.0"}, ""},
		{standardSelector{id: "owasp.asvs", version: "4.0.3"}, "declared only as owasp.asvs@5.0.0"},
		{standardSelector{id: "iso.iec27001"}, "it isn't declared"},
		{standardSelector{id: "ietf.rfc2119"}, "declared only as derived_from"},
		{standardSelector{id: "ietf.rfc2119", version: "3"}, "declared only as ietf.rfc2119, ietf.rfc2119@2"},
		{standardSelector{id: "nist.ssdf"}, "declared as implements, but no clause of it is covered"},
	}
	for _, tt := range tests {
		g := evaluateCoverageGate(report, &coverageGateOptions{require: []standardSelector{tt.sel}})
		switch {
		case tt.want == "" && !g.Passed:
			t.Errorf("%s: gate = %+v, want passed", tt.sel, g)
		case tt.want != "" && (g.Passed || g.Failures[0].Kind != gateRequired || !strings.Contains(g.Failures[0].Message, tt.want)):
			t.Errorf("%s: gate = %+v, want %q", tt.sel, g, tt.want)
		}
	}
}

// TestEvaluateCoverageGateRequireOtherEdition fails when the covered clause is
// of an edition declared only as derived_from.
func TestEvaluateCoverageGateRequireOtherEdition(t *testing.T) {
	report := gateReport()
	report.Declared[0].Relation = entity.RelationDerivedFrom
	report.Declared = append(report.Declared, entity.StandardRef{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "4.0.3"})
	g := evaluateCoverageGate(report, &coverageGateOptions{require: []standardSelector{{id: "owasp.asvs"}}})
	if g.Passed || !strings.Contains(g.Failures[0].Message, "no clause of it is covered") {
		t.Errorf("gate = %+v, want no covered clause of an implemented edition", g)
	}
}

func TestEvaluateCoverageGateEvaluator(t *testing.T) {
	tests := []struct {
		e    coverageEvaluator
		want string
	}{
		{coverageEvaluator{Provider: "claude", Usable: true, RunsOnImplemented: true}, ""},
		{coverageEvaluator{Provider: "null"}, "1 covered clause(s), but the evaluator is null"},
		{coverageEvaluator{Provider: "openai"}, "strspc can't run the evaluator openai"},
		{coverageEvaluator{Provider: "claude", Usable: true}, "isn't run on Implemented rules (evaluate_on)"},
	}
	for _, tt := range tests {
		report := gateReport()
		report.Evaluator = tt.e
		g := evaluateCoverageGate(report, &coverageGateOptions{requireEvaluator: true})
		switch {
		case tt.want == "" && !g.Passed:
			t.Errorf("%+v: gate = %+v, want passed", tt.e, g)
		case tt.want != "" && (g.Passed || g.Failures[0].Kind != gateEvaluator || !strings.Contains(g.Failures[0].Message, tt.want)):
			t.Errorf("%+v: gate = %+v, want %q", tt.e, g, tt.want)
		}
	}

	// With no covered clause, nothing claims coverage an evaluator should check.
	report := gateReport()
	report.Evaluator = coverageEvaluator{Provider: "null"}
	report.Clauses = slices.DeleteFunc(report.Clauses, func(c coverageClause) bool { return c.Status == "covered" })
	if g := evaluateCoverageGate(report, &coverageGateOptions{requireEvaluator: true}); !g.Passed {
		t.Errorf("no covered clause: gate = %+v, want passed", g)
	}
}

func TestEvaluateCoverageGateMinCoverage(t *testing.T) {
	sel := func(id, version string) *standardSelector { return &standardSelector{id: id, version: version} }
	tests := []struct {
		name       string
		thresholds []coverageThreshold
		want       []string // failure messages, in part
	}{
		{"at the threshold", []coverageThreshold{{tenths: 500}}, nil},
		{"under the threshold", []coverageThreshold{{tenths: 501}}, []string{"Realm dev.app: owasp.asvs@5.0.0 covers 2 of 4 clause(s) (50.0 %), under the minimum of 50.1 %"}},
		{"the standard's overrides the bare one", []coverageThreshold{{tenths: 900}, {standard: sel("owasp.asvs", ""), tenths: 400}}, nil},
		{"the edition's overrides the standard's", []coverageThreshold{{standard: sel("owasp.asvs", ""), tenths: 400}, {standard: sel("owasp.asvs", "5.0.0"), tenths: 600}}, []string{"under the minimum of 60.0 %"}},
		{"a standard with no figure", []coverageThreshold{{standard: sel("nist.ssdf", ""), tenths: 0}}, []string{"--min-coverage nist.ssdf: no coverage figure"}},
		{"another edition has no figure", []coverageThreshold{{standard: sel("owasp.asvs", "4.0.3"), tenths: 0}}, []string{"--min-coverage owasp.asvs@4.0.3: no coverage figure"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := evaluateCoverageGate(gateReport(), &coverageGateOptions{minCoverage: tt.thresholds})
			if len(g.Failures) != len(tt.want) || g.Passed != (len(tt.want) == 0) {
				t.Fatalf("gate = %+v, want %q", g, tt.want)
			}
			for i, want := range tt.want {
				if f := g.Failures[i]; f.Kind != gateMinCoverage || !strings.Contains(f.Message, want) {
					t.Errorf("failure %d = %+v, want %q", i, f, want)
				}
			}
		})
	}

	g := evaluateCoverageGate(gateReport(), &coverageGateOptions{minCoverage: []coverageThreshold{{tenths: 800}}})
	if f := g.Failures[0]; f.Tree != "dev.app" || f.Standard != "owasp.asvs" || f.Figure == nil || *f.Figure != 0.5 || f.Threshold == nil || *f.Threshold != 80 {
		t.Errorf("failure = %+v, want the Realm figure 0.5 under 80", f)
	}
}

func TestEvaluateCoverageGateMinCoverageNoFigure(t *testing.T) {
	report := gateReport()
	report.Figures = nil
	g := evaluateCoverageGate(report, &coverageGateOptions{minCoverage: []coverageThreshold{{tenths: 0}}})
	if g.Passed || !strings.Contains(g.Failures[0].Message, "the report has no coverage figure") {
		t.Errorf("gate = %+v, want a failure: a threshold must not pass with nothing to check", g)
	}
}

func TestEvaluateCoverageGateMinCoverageUndefined(t *testing.T) {
	report := gateReport()
	report.Figures[0].Excluded, report.Figures[0].Covered, report.Figures[0].Figure = 5, 0, nil
	g := evaluateCoverageGate(report, &coverageGateOptions{minCoverage: []coverageThreshold{{tenths: 1000}}})
	if !g.Passed || len(g.warnings) != 1 || !strings.Contains(g.warnings[0], "undefined (STDREF-028)") {
		t.Errorf("gate = %+v, want passed with a warning", g)
	}
}

func TestWriteCoverageGate(t *testing.T) {
	var b strings.Builder
	writeCoverageGate(&b, &coverageGate{Passed: true})
	testutil.AssertContains(t, b.String(), "Gate: passed")

	b.Reset()
	writeCoverageGate(&b, &coverageGate{Failures: []coverageGateFailure{{Message: "::error::x\x1b[31m"}}})
	out := b.String()
	testutil.AssertContains(t, out, "Gate: 1 failure(s)")
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\n  ::") {
		t.Errorf("repository text reached the output unescaped:\n%q", out)
	}
}

// TestStandardsCoverageNoGateUnchanged checks that the gate flags change
// nothing when none is set, and that a gate that passes only adds its verdict.
func TestStandardsCoverageNoGateUnchanged(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv(envEvaluatorEndpoint, "")
	dir := setupCoverageProject(t, "claude", "./rules/")
	out, err := runCoverage(t, dir, "--json")
	assertExit(t, err, exitOK)
	if strings.Contains(out, `"gate"`) {
		t.Errorf("no gate flag, but --json has a gate:\n%s", out)
	}
	human, err := runCoverage(t, dir)
	assertExit(t, err, exitOK)
	testutil.AssertNotContains(t, human, "Gate:")

	passed, err := runCoverage(t, dir, "--require", "owasp.asvs@4.0.3", "--require-evaluator")
	assertExit(t, err, exitOK)
	if !strings.HasPrefix(passed, human) || !strings.HasSuffix(passed, "\nGate: passed\n") {
		t.Errorf("a passing gate changed the report:\n%s\nwant\n%s", passed, human)
	}
}

// TestStandardsCoverageGateFails exits 1 on each failure the fixture has: a
// mapped and a lapsed clause, an unresolved note, an uncovered standard. The
// cited ietf.rfc2119 clause never fails.
func TestStandardsCoverageGateFails(t *testing.T) {
	dir := setupCoverageProject(t, "null", "./rules/")
	args := []string{"--fail-on", "lapsed,mapped", "--fail-on", "unresolved", "--require", "nist.ssdf", "--require-evaluator"}

	out, err := runCoverage(t, dir, args...)
	assertExit(t, err, exitViolations)
	for _, want := range []string{
		"Gate: 5 failure(s)",
		"covered clause(s), but the evaluator is null",
		"required standard nist.ssdf: it is declared as implements, but no clause of it is covered",
		"owasp.asvs@4.0.3:2.1.2 is mapped in AUTH",
		"iso.iec27001@2022:A.6 is lapsed in BILL",
		"resolves to no declared standard",
	} {
		testutil.AssertContains(t, out, want)
	}
	testutil.AssertNotContains(t, out, "ietf.rfc2119:MUST is")
	if err.Error() != "standards coverage: the gate failed with 5 failure(s)" {
		t.Errorf("err = %v", err)
	}

	out, err = runCoverage(t, dir, append(args, "--json")...)
	assertExit(t, err, exitViolations)
	var report coverageOutput
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if report.Gate == nil || report.Gate.Passed ||
		!slices.Equal(gateKinds(report.Gate), []string{"evaluator", "required", "lapsed", "mapped", "unresolved"}) {
		t.Errorf("gate = %+v", report.Gate)
	}
}

// TestStandardsCoverageGateFlagErrors exits 2 on a gate flag it can't read,
// and on a broken dependency, which wins over a gate that would fail.
func TestStandardsCoverageGateFlagErrors(t *testing.T) {
	dir := setupCoverageProject(t, "null", "./rules/")
	for _, args := range [][]string{{"--fail-on", "cited"}, {"--require", "@1"}, {"--min-coverage", "120"}} {
		out, err := runCoverage(t, dir, args...)
		assertExit(t, err, exitConfig)
		testutil.AssertNotContains(t, out, "Evaluator:")
	}
	out, err := runCoverage(t, dir, "--fail-on", "cited", "--json")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, out, `"error"`)

	if err := os.RemoveAll(filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	_, err = runCoverage(t, dir, "--fail-on", "lapsed", "--require", "missing.std")
	assertExit(t, err, exitConfig)
}

// TestStandardsCoverageGateMinCoverage gates the linked Realm figure: V1 and
// V3 of V1, V3, V4 and V5 are covered, 50 %, or 66.6 % with V5 excluded.
func TestStandardsCoverageGateMinCoverage(t *testing.T) {
	dir := setupLinkedCoverageProject(t, entity.RelationImplements)
	_, err := runCoverage(t, dir, "--min-coverage", "50")
	assertExit(t, err, exitOK)
	out, err := runCoverage(t, dir, "--min-coverage", "owasp.asvs@5.0.0=50.1%")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "Realm dev.app: owasp.asvs@5.0.0 covers 2 of 4 clause(s) (50.0 %), under the minimum of 50.1 %")

	dir = setupLinkedCoverageProject(t, entity.RelationImplements, linkedExclusion(1, "V5", "Not applicable here."))
	_, err = runCoverage(t, dir, "--min-coverage", "66.6")
	assertExit(t, err, exitOK)
	_, err = runCoverage(t, dir, "--min-coverage", "66.7")
	assertExit(t, err, exitViolations)

	// derived_from gets no figure (STDREF-026), so a threshold has nothing to check.
	dir = setupLinkedCoverageProject(t, entity.RelationDerivedFrom)
	out, err = runCoverage(t, dir, "--min-coverage", "0", "--fail-on", "lapsed,mapped")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "Gate: 1 failure(s)")
	testutil.AssertContains(t, out, "the report has no coverage figure")
}

// TestStandardsCoverageGateUndefinedFigure warns, and passes, on a figure with
// every clause excluded.
func TestStandardsCoverageGateUndefinedFigure(t *testing.T) {
	var notes []entity.RealmNote
	for i, c := range []string{"V1", "V3", "V4", "V5"} {
		notes = append(notes, linkedExclusion(i+1, c, "Out of scope."))
	}
	dir := setupLinkedCoverageProject(t, entity.RelationImplements, notes...)
	out, err := runCoverage(t, dir, "--min-coverage", "100")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "warning: Realm dev.app: owasp.asvs@5.0.0: the figure is undefined (STDREF-028), so --min-coverage isn't checked")
	testutil.AssertContains(t, out, "Gate: passed")

	report := coverageJSON(t, dir)
	if report.Gate != nil {
		t.Errorf("no gate flag, gate = %+v", report.Gate)
	}
}
