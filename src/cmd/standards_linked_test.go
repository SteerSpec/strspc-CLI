package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// linkedStdRealm is the realm_id of the Standard Realm the linked coverage
// tests measure against.
const linkedStdRealm = "dev.test.std"

// writeLinkedStandardRealm writes, in dir, a Standard Realm transcribing
// owasp.asvs 5.0.0 whose live clauses are V1 (L1, L2), V2 (L2), V3 (L1, split
// across two rules), V4 (L1) and V5 (L1). Profile L1 lists V1, V3, V4 and V5.
func writeLinkedStandardRealm(t *testing.T, dir string) {
	t.Helper()
	writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Realm: entity.RealmMeta{
		ID: linkedStdRealm, Title: "ASVS", Version: "1.0.0", Kind: entity.KindStandard, Edition: "5.0.0", Profiles: []string{"L1", "L2"},
	}})
	clause := func(id, c string, profiles ...string) entity.Rule {
		return entity.Rule{ID: id, State: entity.StatePublished, Body: "MUST " + id + ".", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: c, Profiles: profiles}
	}
	writeJSONFile(t, filepath.Join(dir, "V.json"), &entity.File{
		Entity: entity.Entity{ID: "V", Title: "V"},
		Rules: []entity.Rule{
			clause("V-001", "V1", "L1", "L2"),
			clause("V-002", "V2", "L2"),
			clause("V-003", "V3", "L1"),
			clause("V-004", "V3", "L1"),
			clause("V-005", "V4", "L1"),
			clause("V-006", "V5", "L1"),
		},
	})
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupLinkedCoverageProject writes the realm dev.app in rules/, depending on
// the Standard Realm in std/ and declaring owasp.asvs linked to it with
// profile L1, with relation and Realm Notes as given. AUTH covers V1, maps V3
// and covers V2, outside the profile; BILL covers V3 and has V4 lapsed.
func setupLinkedCoverageProject(t *testing.T, relation string, notes ...entity.RealmNote) string {
	t.Helper()
	dir := t.TempDir()
	writeLinkedStandardRealm(t, filepath.Join(dir, "std"))
	rules := filepath.Join(dir, "rules")
	notesJSON, err := json.Marshal(notes)
	if err != nil {
		t.Fatal(err)
	}
	writeEntityFile(t, rules, "realm.json", fmt.Sprintf(`{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0",
  "standards": [{"id": "owasp.asvs", "relation": %q, "version": "5.0.0", "realm": %q, "profile": "L1"}]},
  "dependencies": [{"realm_id": %q, "version": "1.0.0", "source": "../std/"}], "notes": %s}`,
		relation, linkedStdRealm, linkedStdRealm, notesJSON))
	writeInheritFile(t, rules, "AUTH", withStandardNotes(inheritEntityJSON("AUTH", "", "",
		inheritRule{id: "AUTH-001", state: "I"},
		inheritRule{id: "AUTH-002", state: "P"},
		inheritRule{id: "AUTH-003", state: "I"}),
		[2]string{"AUTH-001", "owasp.asvs:V1"},
		[2]string{"AUTH-002", "owasp.asvs:V3"},
		[2]string{"AUTH-003", "owasp.asvs:V2"}))
	writeInheritFile(t, rules, "BILL", withStandardNotes(inheritEntityJSON("BILL", "", "",
		inheritRule{id: "BILL-001", state: "I"},
		inheritRule{id: "BILL-002", state: "R"}),
		[2]string{"BILL-001", "owasp.asvs:V3"},
		[2]string{"BILL-002", "owasp.asvs:V4"}))
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules\n    scope: local\nevaluator:\n  provider: null\ncache:\n  ttl: 24h\n")
	return dir
}

// linkedExclusion is an exclusion Realm Note on clause of owasp.asvs.
func linkedExclusion(n int, clause, content string) entity.RealmNote {
	return entity.RealmNote{
		ID: fmt.Sprintf("REALM/%03d", n), Type: entity.NoteTypeExclusion, Ref: "owasp.asvs:" + clause,
		Content: content, AddedBy: "github:a", AddedAt: "2026-01-01",
	}
}

// clauseStatuses maps "<entity> <clause>" to the status of each owasp.asvs
// clause of the report.
func clauseStatuses(report *coverageOutput) map[string]string {
	got := make(map[string]string)
	for _, c := range report.Clauses {
		if c.Standard == "owasp.asvs" {
			got[c.Entity+" "+c.Clause] = c.Status
		}
	}
	return got
}

func findFigure(t *testing.T, report *coverageOutput, euid string) *coverageFigure {
	t.Helper()
	for i := range report.Figures {
		f := &report.Figures[i]
		if f.Entity == euid {
			return f
		}
	}
	t.Fatalf("no figure for entity %q in %+v", euid, report.Figures)
	return nil
}

func assertFigure(t *testing.T, f *coverageFigure, total, excluded, covered int) {
	t.Helper()
	if f.Standard != "owasp.asvs" || f.Version != "5.0.0" || f.Realm != linkedStdRealm || f.Profile != "L1" || f.Tree != "dev.app" {
		t.Errorf("figure %+v: wrong reference", f)
	}
	if f.Total != total || f.Excluded != excluded || f.Covered != covered {
		t.Errorf("figure %q = total %d, excluded %d, covered %d; want %d, %d, %d", f.Entity, f.Total, f.Excluded, f.Covered, total, excluded, covered)
	}
	want := float64(covered) / float64(total-excluded)
	if f.Figure == nil || *f.Figure != want {
		t.Errorf("figure %q = %v, want %v", f.Entity, f.Figure, want)
	}
}

// TestStandardsCoverageLinkedJSON measures each entity, and the Realm, against
// the profile's clause list less the excluded clause V5.
func TestStandardsCoverageLinkedJSON(t *testing.T) {
	dir := setupLinkedCoverageProject(t, entity.RelationImplements, linkedExclusion(1, "V5", "Not applicable here."))
	report := coverageJSON(t, dir)

	want := map[string]string{
		"AUTH V1": "covered", "AUTH V2": "covered", "AUTH V3": "mapped", "AUTH V4": "unmapped", "AUTH V5": "excluded",
		"BILL V1": "unmapped", "BILL V3": "covered", "BILL V4": "lapsed", "BILL V5": "excluded",
	}
	got := clauseStatuses(&report)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("clauses = %v, want %v", got, want)
	}

	// V3 counts once though two rules transcribe it, and V2 is outside L1.
	assertFigure(t, findFigure(t, &report, "AUTH"), 4, 1, 1)
	assertFigure(t, findFigure(t, &report, "BILL"), 4, 1, 1)
	// The Realm covers V1 through AUTH and V3 through BILL.
	assertFigure(t, findFigure(t, &report, ""), 4, 1, 2)
	if len(report.Figures) != 3 {
		t.Errorf("figures = %+v, want the Realm's and two entities'", report.Figures)
	}
}

// TestStandardsCoverageLinkedHuman prints the clause list, the Realm figure
// before the entities', and the statuses.
func TestStandardsCoverageLinkedHuman(t *testing.T) {
	dir := setupLinkedCoverageProject(t, entity.RelationImplements, linkedExclusion(1, "V5", "Not applicable here."))
	out, err := runCoverage(t, dir)
	assertExit(t, err, exitOK)
	for _, want := range []string{
		"Standard Realm " + linkedStdRealm + ", profile L1: 4 clause(s), 1 excluded",
		"Realm dev.app: 2 of 3 covered (66.6 %)",
		"    AUTH: 1 of 3 covered (33.3 %)",
		"    BILL: 1 of 3 covered (33.3 %)",
		"unmapped", "excluded",
		"excluded: a Realm Note excludes the clause.",
		"Without a linked Standard Realm, a clause no rule maps isn't shown.",
	} {
		testutil.AssertContains(t, out, want)
	}
	if i, j := strings.Index(out, "Realm dev.app:"), strings.Index(out, "    AUTH:"); i < 0 || j < i {
		t.Errorf("the Realm figure should precede the entities':\n%s", out)
	}
}

// TestStandardsCoverageLinkedUndefined reports a figure undefined, never 0 %
// or 100 %, when every clause of the list is excluded (STDREF-028).
func TestStandardsCoverageLinkedUndefined(t *testing.T) {
	var notes []entity.RealmNote
	for i, c := range []string{"V1", "V3", "V4", "V5"} {
		notes = append(notes, linkedExclusion(i+1, c, "Out of scope."))
	}
	dir := setupLinkedCoverageProject(t, entity.RelationImplements, notes...)

	report := coverageJSON(t, dir)
	for i := range report.Figures {
		f := &report.Figures[i]
		if f.Figure != nil || f.Total != 4 || f.Excluded != 4 || f.Covered != 0 {
			t.Errorf("figure %+v, want 4 excluded of 4 and no figure", f)
		}
	}
	out, err := runCoverage(t, dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Realm dev.app: undefined, every clause is excluded")
	if strings.Contains(out, " %)") {
		t.Errorf("an undefined figure printed as a percentage:\n%s", out)
	}
}

// TestStandardsCoverageLinkedDerivedFrom gives a derived_from reference no
// figure, its clauses cited (STDREF-026).
func TestStandardsCoverageLinkedDerivedFrom(t *testing.T) {
	dir := setupLinkedCoverageProject(t, entity.RelationDerivedFrom)
	report := coverageJSON(t, dir)
	if len(report.Figures) != 0 {
		t.Errorf("figures = %+v, want none", report.Figures)
	}
	for k, v := range clauseStatuses(&report) {
		if v != "cited" {
			t.Errorf("%s = %q, want cited", k, v)
		}
	}
}

// TestStandardsCoverageLinkedNotStandardRealm gives no figure, and lists no
// unmapped clause, when the linked realm isn't a Standard Realm.
func TestStandardsCoverageLinkedNotStandardRealm(t *testing.T) {
	dir := setupLinkedCoverageProject(t, entity.RelationImplements)
	writeJSONFile(t, filepath.Join(dir, "std", "realm.json"), &entity.RealmFile{Realm: entity.RealmMeta{
		ID: linkedStdRealm, Title: "ASVS", Version: "1.0.0",
	}})
	report := coverageJSON(t, dir)
	if len(report.Figures) != 0 {
		t.Errorf("figures = %+v, want none", report.Figures)
	}
	for k, v := range clauseStatuses(&report) {
		if v == "unmapped" || v == "excluded" {
			t.Errorf("%s = %q, want only the mapped clauses", k, v)
		}
	}
}

// TestStandardsCoverageUnlinkedHasNoFigures keeps the report of references
// with no realm as it was, with an empty figures array.
func TestStandardsCoverageUnlinkedHasNoFigures(t *testing.T) {
	dir := setupCoverageProject(t, "null", "./rules")
	out, err := runCoverage(t, dir, "--json")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, `"figures": []`)
}

func TestCoveragePercent(t *testing.T) {
	for _, tc := range []struct {
		covered, total int
		want           string
	}{
		{0, 3, "0.0 %"},
		{2, 3, "66.6 %"},
		{1999, 2000, "99.9 %"},
		{3, 3, "100.0 %"},
		{0, 0, "undefined"},
	} {
		if got := coveragePercent(tc.covered, tc.total); got != tc.want {
			t.Errorf("coveragePercent(%d, %d) = %q, want %q", tc.covered, tc.total, got, tc.want)
		}
	}
}
