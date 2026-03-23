package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRuleAddBasic(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST")

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "add", "TST",
		"--body", "The system MUST validate input",
		"--added-by", "@alice",
		"--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "TST-001")
	testutil.AssertContains(t, out, "Draft")

	f := loadEntityFile(t, dir, "TST")
	if len(f.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(f.Rules))
	}
	if f.Rules[0].ID != "TST-001" {
		t.Errorf("expected rule ID TST-001, got %s", f.Rules[0].ID)
	}
	if f.Rules[0].State != "D" {
		t.Errorf("expected state D, got %s", f.Rules[0].State)
	}
}

func TestRuleAddJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST")

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "add", "TST",
		"--body", "Test rule",
		"--added-by", "@bob",
		"--dir", dir,
		"--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, out)
	}
	if result["rule_id"] != "TST-001" {
		t.Errorf("expected rule_id TST-001, got %v", result["rule_id"])
	}
	if result["state"] != "D" {
		t.Errorf("expected state D, got %v", result["state"])
	}
}

func TestRuleAddMissingBody(t *testing.T) {
	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "add", "TST",
		"--added-by", "@alice",
		"--dir", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing --body")
	}
}

func TestRuleAddMissingAddedBy(t *testing.T) {
	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "add", "TST",
		"--body", "Test",
		"--dir", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing --added-by")
	}
}

func TestRuleAddNoEntityFile(t *testing.T) {
	dir := t.TempDir()

	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "add", "NOPE",
		"--body", "Test",
		"--added-by", "@alice",
		"--dir", dir)
	if err == nil {
		t.Fatal("expected error for missing entity file")
	}
}
