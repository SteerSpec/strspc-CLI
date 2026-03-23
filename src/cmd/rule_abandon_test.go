package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRuleAbandonDraft(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft rule", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "abandon", "TST-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "Abandoned")

	f := loadEntityFile(t, dir, "TST")
	if f.Rules[0].State != "A" {
		t.Errorf("expected state A, got %s", f.Rules[0].State)
	}
}

func TestRuleAbandonJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "abandon", "TST-001", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["state"] != "A" {
		t.Errorf("expected state A, got %s", result["state"])
	}
}

func TestRuleAbandonNonDraft(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "P", "Published", "@alice"))

	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "abandon", "TST-001", "--dir", dir)
	if err == nil {
		t.Fatal("expected error abandoning non-Draft rule")
	}
}
