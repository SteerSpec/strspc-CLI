package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

const validEntityJSON = `{
  "entity": {"id": "TST", "title": "Test Entity"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [
    {"id": "TST-001", "revision": 0, "state": "D", "body": "A test rule.", "added_by": "test", "added_at": "2026-01-01"}
  ],
  "notes": []
}`

const invalidEntityJSON = `{
  "entity": {"id": "AB", "title": "Bad EUID"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [],
  "notes": []
}`

const crossRefEntityA = `{
  "entity": {"id": "ENT", "title": "Entity A"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [
    {"id": "ENT-001", "revision": 0, "state": "D", "body": "Rule A.", "added_by": "test", "added_at": "2026-01-01"}
  ],
  "notes": []
}`

const crossRefEntityB = `{
  "entity": {"id": "RUL", "title": "Entity B"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [
    {"id": "RUL-001", "revision": 0, "state": "D", "body": "Supersedes missing.", "added_by": "test", "added_at": "2026-01-01", "supersedes": "MISSING-001"}
  ],
  "notes": []
}`

// setupLintTest overrides the schema fetcher to skip schema validation (RL002)
// so tests focus on business rule checks (RL003+) without needing network access.
func setupLintTest(t *testing.T) {
	t.Helper()
	original := newSchemaFetcher
	newSchemaFetcher = func() *schema.Fetcher { return nil }
	t.Cleanup(func() { newSchemaFetcher = original })
}

func writeEntityFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLintSingleFileValid(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", filepath.Join(dir, "TST.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestLintSingleFileInvalid(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "BAD.json", invalidEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", filepath.Join(dir, "BAD.json"))
	if err == nil {
		t.Fatal("expected error for invalid entity, got nil")
	}
	testutil.AssertContains(t, output, "RL003")
	testutil.AssertContains(t, err.Error(), "error(s)")
}

func TestLintDirectoryValid(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestLintDirectoryInvalid(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "BAD.json", invalidEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err == nil {
		t.Fatal("expected error for invalid directory, got nil")
	}
	testutil.AssertContains(t, output, "RL003")
}

func TestLintCrossRef(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "ENT.json", crossRefEntityA)
	writeEntityFile(t, dir, "RUL.json", crossRefEntityB)

	// Without --cross-ref: no RL012 warnings.
	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err != nil {
		t.Fatalf("unexpected error without --cross-ref: %v", err)
	}
	testutil.AssertNotContains(t, output, "RL012")

	// With --cross-ref: RL012 warning about MISSING-001.
	output, err = testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--cross-ref")
	if err != nil {
		t.Fatalf("unexpected error with --cross-ref: %v", err)
	}
	testutil.AssertContains(t, output, "RL012")
	testutil.AssertContains(t, output, "MISSING-001")
}

func TestLintCrossRefStrict(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "ENT.json", crossRefEntityA)
	writeEntityFile(t, dir, "RUL.json", crossRefEntityB)

	// With --cross-ref --strict: RL012 promoted to error, exit 1.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--cross-ref", "--strict")
	if err == nil {
		t.Fatal("expected error with --cross-ref --strict, got nil")
	}
	testutil.AssertContains(t, err.Error(), "error(s)")
}

func TestLintJSONOutput(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "BAD.json", invalidEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", filepath.Join(dir, "BAD.json"), "--json")
	if err == nil {
		t.Fatal("expected error for invalid entity with --json, got nil")
	}

	var diags []result.Diagnostic
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}

	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic")
	}

	found := false
	for _, d := range diags {
		if d.Code == "RL003" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RL003 diagnostic in JSON output")
	}
}

func TestLintJSONOutputValid(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", filepath.Join(dir, "TST.json"), "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var diags []result.Diagnostic
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
}

func TestLintNoPath(t *testing.T) {
	setupLintTest(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "lint")
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

func TestLintEmptyDirectory(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()

	_, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
	testutil.AssertContains(t, err.Error(), "error(s)")
}

func TestLintNonExistent(t *testing.T) {
	setupLintTest(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "lint", "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent path, got nil")
	}
	testutil.AssertContains(t, err.Error(), "accessing")
}

func TestLintHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--cross-ref")
	testutil.AssertContains(t, output, "--json")
	testutil.AssertContains(t, output, "--strict")
	testutil.AssertContains(t, output, "--schema-version")
}

func TestLintSkipsRealmJSON(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)
	// Write a realm.json that would fail entity validation.
	realmJSON := `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "test", "title": "Test", "version": "0.1.0"}, "dependencies": []}`
	writeEntityFile(t, dir, "realm.json", realmJSON)

	// Without --cross-ref.
	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")

	// With --cross-ref: LintDir also skips realm.json internally.
	output, err = testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--cross-ref")
	if err != nil {
		t.Fatalf("unexpected error with --cross-ref: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}
