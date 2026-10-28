package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func addTestNote(t *testing.T, dir, ruleID, noteType, body string) {
	t.Helper()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", ruleID,
		"--type", noteType, "--body", body, "--added-by", "@alice", "--dir", dir); err != nil {
		t.Fatalf("note add: %v", err)
	}
}

func TestNoteUpdateBody(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))
	addTestNote(t, dir, "TST-001", "rationale", "old")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "update", "TST-001/01", "--body", "new", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "TST-001/01")

	f := loadEntityFile(t, dir, "TST")
	n := f.Notes[0]
	if n.Content != "new" || n.Type != "rationale" || n.Revision != 1 || n.AddedBy != "@alice" {
		t.Errorf("unexpected note: %+v", n)
	}
	if f.RuleSet.Version != "0.1.2" {
		t.Errorf("version %q, want 0.1.2 (add + update)", f.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestNoteUpdateTypeAndBodyBumpsRevisionOnce(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))
	addTestNote(t, dir, "TST-001", "rationale", "old")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "note", "update", "TST-001/01",
		"--body", "new", "--type", "clarification", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if got["note_id"] != "TST-001/01" || got["rule_id"] != "TST-001" || got["revision"] != float64(1) {
		t.Errorf("unexpected JSON: %v", got)
	}
	n := loadEntityFile(t, dir, "TST").Notes[0]
	if n.Type != "clarification" || n.Content != "new" || n.Revision != 1 {
		t.Errorf("unexpected note: %+v", n)
	}
}

func TestNoteUpdateTypeOnly(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))
	addTestNote(t, dir, "TST-001", "rationale", "keep")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "note", "update", "TST-001/01", "--type", "example", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n := loadEntityFile(t, dir, "TST").Notes[0]
	if n.Type != "example" || n.Content != "keep" || n.Revision != 1 {
		t.Errorf("unexpected note: %+v", n)
	}
}

func TestNoteUpdateErrors(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "@alice"))
	addTestNote(t, dir, "TST-001", "rationale", "r")
	addTestNote(t, dir, "TST-001", "changelog", "c")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"nothing to change", []string{"TST-001/01"}, "--body or --type"},
		{"unchanged", []string{"TST-001/01", "--body", "r"}, "unchanged"},
		{"invalid type", []string{"TST-001/01", "--type", "opinion"}, "opinion"},
		{"append-only note", []string{"TST-001/02", "--body", "x"}, "cannot change"},
		{"retype to append-only", []string{"TST-001/01", "--type", "related"}, "append-only"},
		{"unknown note", []string{"TST-001/09", "--body", "x"}, "not found"},
		{"malformed id", []string{"TST-001", "--body", "x"}, "invalid note ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"note", "update"}, tt.args...)
			args = append(args, "--dir", dir)
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil {
				t.Fatal("expected error")
			}
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestNoteUpdateSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "@alice"))
	addTestNote(t, dir, "SUB-001", "rationale", "old")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "note", "update", "SUB-001/01", "--body", "new", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	n := f.SubEntities[0].Notes[0]
	if n.Content != "new" || n.Revision != 1 {
		t.Errorf("unexpected note: %+v", n)
	}
	assertFileHashFresh(t, dir, "TST")
}
