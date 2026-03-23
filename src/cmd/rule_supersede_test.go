package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRuleSupersedeBasic(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "I", "Implemented rule", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "supersede", "TST-001",
		"--body", "New improved rule MUST...",
		"--added-by", "@bob",
		"--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "TST-002")
	testutil.AssertContains(t, out, "supersedes TST-001")

	f := loadEntityFile(t, dir, "TST")
	if len(f.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(f.Rules))
	}

	newRule := f.Rules[1]
	if newRule.ID != "TST-002" {
		t.Errorf("expected new rule ID TST-002, got %s", newRule.ID)
	}
	if newRule.State != "D" {
		t.Errorf("expected state D, got %s", newRule.State)
	}
	if newRule.Supersedes == nil || *newRule.Supersedes != "TST-001" {
		t.Errorf("expected supersedes TST-001")
	}
}

func TestRuleSupersedeJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "P", "Published", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "supersede", "TST-001",
		"--body", "Replacement",
		"--added-by", "@bob",
		"--dir", dir,
		"--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["rule_id"] != "TST-002" {
		t.Errorf("expected rule_id TST-002, got %v", result["rule_id"])
	}
	if result["supersedes"] != "TST-001" {
		t.Errorf("expected supersedes TST-001, got %v", result["supersedes"])
	}
}

func TestRuleSupersedeInvalidState(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft", "@alice"))

	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "supersede", "TST-001",
		"--body", "Try to supersede draft",
		"--added-by", "@bob",
		"--dir", dir)
	if err == nil {
		t.Fatal("expected error superseding Draft rule")
	}
}
