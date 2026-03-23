package cmd

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRuleRetireImplementedToRetired(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "I", "Implemented rule", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "retire", "TST-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "Implemented → Retired")

	f := loadEntityFile(t, dir, "TST")
	if f.Rules[0].State != "R" {
		t.Errorf("expected state R, got %s", f.Rules[0].State)
	}
}

func TestRuleRetireRetiredToTerminated(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "R", "Retired rule", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "retire", "TST-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, out, "Retired → Terminated")

	f := loadEntityFile(t, dir, "TST")
	if f.Rules[0].State != "T" {
		t.Errorf("expected state T, got %s", f.Rules[0].State)
	}
}

func TestRuleRetireJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "I", "Implemented", "@alice"))

	root := NewRootCmd()
	out, err := testutil.ExecuteCommand(root, "rule", "retire", "TST-001", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["state"] != "R" {
		t.Errorf("expected state R, got %s", result["state"])
	}
}

func TestRuleRetireInvalidState(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Draft", "@alice"))

	root := NewRootCmd()
	_, err := testutil.ExecuteCommand(root, "rule", "retire", "TST-001", "--dir", dir)
	if err == nil {
		t.Fatal("expected error retiring Draft rule")
	}
}
