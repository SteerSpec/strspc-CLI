package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestNoteAddBasic(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "TST-001",
		"--type", "rationale", "--body", "Because reasons.", "--added-by", "@alice", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "TST-001/01")

	f := loadEntityFile(t, dir, "TST")
	if len(f.Notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(f.Notes))
	}
	n := f.Notes[0]
	if n.ID != "TST-001/01" || n.RuleRef != "TST-001" || n.Type != "rationale" ||
		n.Content != "Because reasons." || n.AddedBy != "@alice" || n.Revision != 0 || n.AddedAt == "" {
		t.Errorf("unexpected note: %+v", n)
	}
	if f.RuleSet.Version != "0.1.1" {
		t.Errorf("expected version bump to 0.1.1, got %s", f.RuleSet.Version)
	}
	if f.RuleSet.Timestamp == "2026-01-01T00:00:00Z" {
		t.Error("rule_set.timestamp should be updated")
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestNoteAddRecordsCurrentRuleRevision(t *testing.T) {
	dir := t.TempDir()
	rule := makeRule("TST-001", "D", "Rule", "@alice")
	rule.Revision = 2
	setupEntityFile(t, dir, "TST", rule)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "TST-001",
		"--type", "clarification", "--body", "Documents revision 2.", "--added-by", "@alice", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f := loadEntityFile(t, dir, "TST")
	if len(f.Notes) != 1 || f.Notes[0].Revision != 2 {
		t.Fatalf("expected one note at revision 2, got %+v", f.Notes)
	}
}

func TestNoteAddIncrementsPerRule(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST",
		makeRule("TST-001", "D", "Rule 1", "@alice"),
		makeRule("TST-002", "D", "Rule 2", "@alice"))

	for _, args := range [][]string{
		{"TST-001", "--type", "example", "--body", "e1"},
		{"TST-002", "--type", "example", "--body", "e2"},
		{"TST-001", "--type", "reference", "--body", "r1"},
	} {
		full := append([]string{"note", "add"}, args...)
		full = append(full, "--added-by", "@alice", "--dir", dir)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), full...); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	f := loadEntityFile(t, dir, "TST")
	ids := []string{}
	for _, n := range f.Notes {
		ids = append(ids, n.ID)
	}
	want := []string{"TST-001/01", "TST-002/01", "TST-001/02"}
	for i := range want {
		if i >= len(ids) || ids[i] != want[i] {
			t.Fatalf("got note IDs %v, want %v", ids, want)
		}
	}
}

func TestNoteAddJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "TST-001",
		"--type", "applies_to", "--body", "src/cmd/*.go", "--added-by", "@alice", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result["note_id"] != "TST-001/01" || result["rule_id"] != "TST-001" || result["type"] != "applies_to" {
		t.Errorf("unexpected JSON: %v", result)
	}
}

func TestNoteAddInvalidType(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "TST-001",
		"--type", "opinion", "--body", "x", "--added-by", "@alice", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for invalid note type")
	}
	testutil.AssertContains(t, err.Error(), "opinion")
	testutil.AssertContains(t, err.Error(), "rationale")
}

func TestNoteAddUnknownRule(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "TST-009",
		"--type", "rationale", "--body", "x", "--added-by", "@alice", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for unknown rule")
	}
	testutil.AssertContains(t, err.Error(), "TST-009")
}

func TestNoteAddMissingFlags(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))

	cases := [][]string{
		{"--body", "x", "--added-by", "@a"},
		{"--type", "rationale", "--added-by", "@a"},
		{"--type", "rationale", "--body", "x"},
	}
	for _, c := range cases {
		args := append([]string{"note", "add", "TST-001", "--dir", dir}, c...)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), args...); err == nil {
			t.Errorf("expected error for args %v", c)
		}
	}
}

func TestNoteAddSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "SUB-001",
		"--type", "clarification", "--body", "c", "--added-by", "@alice", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	if len(f.Notes) != 0 {
		t.Error("parent should have no notes")
	}
	if len(f.SubEntities[0].Notes) != 1 || f.SubEntities[0].Notes[0].ID != "SUB-001/01" {
		t.Fatalf("expected SUB-001/01 on sub-entity, got %+v", f.SubEntities[0].Notes)
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestNoteAddHelp(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, flag := range []string{"--type", "--body", "--added-by", "--dir", "--json"} {
		testutil.AssertContains(t, out, flag)
	}
}

// Guard: the note type list in the CLI must match the schema enum size.
func TestNoteTypesCount(t *testing.T) {
	if len(noteTypes) != 11 {
		t.Errorf("expected 11 note types, got %d", len(noteTypes))
	}
	var _ entity.Note // keep the import honest if assertions above change
}
