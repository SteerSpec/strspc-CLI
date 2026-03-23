package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRuleUpdateBasic(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Original body", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "update", "TST-001",
		"--body", "Updated body MUST be better",
		"--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "TST-001")
	testutil.AssertContains(t, out, "Draft")

	f := loadEntityFile(t, dir, "TST")
	if f.Rules[0].Body != "Updated body MUST be better" {
		t.Errorf("expected updated body, got %q", f.Rules[0].Body)
	}
	if f.Rules[0].Revision != 1 {
		t.Errorf("expected revision 1, got %d", f.Rules[0].Revision)
	}
}

func TestRuleUpdateJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Original", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "update", "TST-001",
		"--body", "New body",
		"--dir", dir,
		"--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["rule_id"] != "TST-001" {
		t.Errorf("expected rule_id TST-001, got %v", result["rule_id"])
	}
}

func TestRuleUpdateNonDraft(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "P", "Published rule", "@alice"))

	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "update", "TST-001",
		"--body", "Try to update",
		"--dir", dir)
	if err == nil {
		t.Fatal("expected error for non-Draft rule")
	}
}

func TestRuleUpdateMissingBody(t *testing.T) {
	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "update", "TST-001",
		"--dir", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing --body")
	}
}
