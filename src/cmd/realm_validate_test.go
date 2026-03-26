package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// setupRealmValidateTest overrides newRealmValidator to skip schema validation
// (RM002) and entity linting (RM005) so tests focus on structural checks
// without needing network access.
func setupRealmValidateTest(t *testing.T) {
	t.Helper()
	original := newRealmValidator
	newRealmValidator = func(strict, recursive bool) *realmValidatorSet {
		rl := rulelint.New(rulelint.WithStrict(strict))
		realmOpts := []realmlint.Option{realmlint.WithStrict(strict)}
		if !recursive {
			realmOpts = append(realmOpts, realmlint.WithRuleLinter(rl))
		}
		return &realmValidatorSet{
			realmLinter: realmlint.New(realmOpts...),
			ruleLinter:  rl,
		}
	}
	t.Cleanup(func() { newRealmValidator = original })
}

const validRealmJSON = `{
  "$schema": "./_schema/realm.v1.schema.json",
  "realm": {
    "id": "dev.steerspec.test",
    "title": "Test Realm",
    "version": "0.1.0"
  },
  "dependencies": [],
  "rule_identifier_format": null
}`

const invalidRealmIDJSON = `{
  "$schema": "./_schema/realm.v1.schema.json",
  "realm": {
    "id": "INVALID!",
    "title": "Bad Realm",
    "version": "0.1.0"
  },
  "dependencies": []
}`

const validRealmWithSubsJSON = `{
  "$schema": "./_schema/realm.v1.schema.json",
  "realm": {
    "id": "dev.steerspec.test",
    "title": "Test Realm",
    "version": "0.1.0"
  },
  "dependencies": [],
  "sub_realms": ["auth"]
}`

// setupValidRealm creates a minimal valid realm directory structure.
func setupValidRealm(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(validRealmJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal entity schema stub.
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`{"$id": "entity-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupValidRealmWithSubs creates a realm with a sub-realm declared.
func setupValidRealmWithSubs(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(validRealmWithSubsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`{"$id": "entity-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	setupSubRealm(t, dir, "auth", "dev.steerspec.test")
}

// setupSubRealm creates a sub-realm directory inside a parent realm.
func setupSubRealm(t *testing.T, parentDir, name, parentID string) {
	t.Helper()
	dir := filepath.Join(parentDir, name)
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}

	realmJSON := `{
  "$schema": "./_schema/realm.v1.schema.json",
  "realm": {
    "id": "` + parentID + `.` + name + `",
    "title": "Sub Realm ` + name + `",
    "version": "0.1.0"
  },
  "dependencies": []
}`
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(realmJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`{"$id": "entity-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRealmValidateValid(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestRealmValidateInvalid(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	// No realm.json — RM001 should fire.

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err == nil {
		t.Fatal("expected error for missing realm.json, got nil")
	}
	testutil.AssertContains(t, err.Error(), "error(s)")
}

func TestRealmValidateDefaultDir(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()

	// Create rules/ subdirectory with valid realm.
	rulesDir := filepath.Join(dir, "rules")
	setupValidRealm(t, rulesDir)

	// Change to dir so default ./rules/ resolves.
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestRealmValidateJSONOutput(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	// No realm.json — produces errors.

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--json")
	if err == nil {
		t.Fatal("expected error for invalid realm with --json, got nil")
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
		if d.Code == "RM001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RM001 diagnostic in JSON output")
	}
}

func TestRealmValidateJSONOutputValid(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var diags []result.Diagnostic
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
}

func TestRealmValidateNoSchemaDir(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	// realm.json but no _schema/ → RM003.
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(validRealmJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err == nil {
		t.Fatal("expected error for missing _schema/, got nil")
	}
	testutil.AssertContains(t, output, "RM003")
}

func TestRealmValidateInvalidRealmID(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(invalidRealmIDJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`{"$id": "entity-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err == nil {
		t.Fatal("expected error for invalid realm ID, got nil")
	}
	testutil.AssertContains(t, output, "RM007")
}

func TestRealmValidateDuplicateEUID(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)

	// Two entity files with the same EUID → RM006.
	entityJSON := `{
  "entity": {"id": "DUP", "title": "Duplicate"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [],
  "notes": []
}`
	if err := os.WriteFile(filepath.Join(dir, "DUP1.json"), []byte(entityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DUP2.json"), []byte(entityJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err == nil {
		t.Fatal("expected error for duplicate EUID, got nil")
	}
	testutil.AssertContains(t, output, "RM006")
}

func TestRealmValidateStrict(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)

	// Without --strict: should pass (info diagnostics only).
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err != nil {
		t.Fatalf("unexpected error without --strict: %v", err)
	}

	// With --strict: info diagnostics about skipped schema/entity checks
	// remain as info (not promoted), so this should still pass.
	// The strict flag promotes warnings to errors — we need a scenario
	// that produces a warning. RM006 duplicate EUID is always an error.
	// For now, verify --strict doesn't break valid realms.
	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--strict")
	if err != nil {
		t.Fatalf("unexpected error with --strict on valid realm: %v", err)
	}
}

func TestRealmValidateHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--json")
	testutil.AssertContains(t, output, "--strict")
	testutil.AssertContains(t, output, "--recursive")
}

func TestRealmValidateRecursiveValid(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestRealmValidateRecursiveNoSubRealms(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	if err != nil {
		t.Fatalf("unexpected error with --recursive on realm without sub-realms: %v", err)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestRealmValidateRecursiveJSON(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	// Missing sub-realm directory → RM008 error.
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(validRealmWithSubsJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`{"$id": "entity-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Don't create the "auth" sub-realm dir — should trigger RM008.

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive", "--json")
	if err == nil {
		t.Fatal("expected error for missing sub-realm directory, got nil")
	}

	var diags []result.Diagnostic
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}

	found := false
	for _, d := range diags {
		if d.Code == "RM008" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected RM008 diagnostic for missing sub-realm dir, got: %v", diags)
	}
}

func TestRealmValidateRecursiveCrossRef(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)

	// Add entity in sub-realm with a supersedes reference to a non-existent rule.
	// LintRealm should catch this as RL012.
	entityJSON := `{
  "entity": {"id": "SUB", "title": "Sub Entity"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [
    {
      "id": "SUB-001",
      "revision": 0,
      "state": "D",
      "body": "A rule that supersedes a missing rule.",
      "added_by": "@test",
      "added_at": "2026-01-01",
      "supersedes": "MISSING-999"
    }
  ],
  "notes": []
}`
	if err := os.WriteFile(filepath.Join(dir, "auth", "SUB.json"), []byte(entityJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// With --recursive: LintRealm scans across sub-realms and catches RL012.
	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	// RL012 is a warning by default, not an error — the command should still succeed.
	if err != nil {
		t.Fatalf("expected realm validate to succeed despite RL012 warning, got error: %v", err)
	}
	testutil.AssertContains(t, output, "RL012")
	testutil.AssertContains(t, output, "MISSING-999")
}

func TestRealmValidateRecursiveGroupedOutput(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)

	// Add duplicate EUIDs within the sub-realm to force an RM006 error
	// scoped to the sub-realm path.
	entityJSON := `{
  "entity": {"id": "DUP", "title": "Duplicate"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [],
  "notes": []
}`
	subDir := filepath.Join(dir, "auth")
	if err := os.WriteFile(filepath.Join(subDir, "DUP1.json"), []byte(entityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "DUP2.json"), []byte(entityJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	output, _ := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	// Grouped output should include section headers.
	testutil.AssertContains(t, output, `Sub-realm "auth"`)
}
