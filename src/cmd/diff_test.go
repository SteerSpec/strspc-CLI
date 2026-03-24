package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// entityV1 returns a minimal entity JSON with a single Draft rule.
func entityV1(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "Initial rule body.",
      "state": "D",
      "revision": 0,
      "added_by": "@alice"
    }
  ],
  "notes": []
}`)
}

// entityV2Draft returns entityV1 with an edited draft body and bumped revision/version/timestamp.
func entityV2Draft(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "Edited rule body.",
      "state": "D",
      "revision": 1,
      "added_by": "@alice"
    }
  ],
  "notes": []
}`)
}

// entityV2Promote returns entityV1 with rule promoted to P and bumped metadata.
func entityV2Promote(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "Initial rule body.",
      "state": "P",
      "revision": 0,
      "added_by": "@alice"
    }
  ],
  "notes": []
}`)
}

// entityV2InvalidEdit returns entityV1 with body edited on a Published rule (violates RD002).
func entityV2InvalidEdit(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "EDITED after published — not allowed.",
      "state": "P",
      "revision": 0,
      "added_by": "@alice"
    }
  ],
  "notes": []
}`)
}

// makeGitRepo creates a temporary git repo, writes initialFile to filename, commits, then
// overwrites filename with headFile (working-tree version). Returns the repo directory.
func makeGitRepo(t *testing.T, filename string, initialFile, headFile []byte) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, initialFile, 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", filename)
	run("git", "commit", "-m", "initial")

	// Write head version to working tree (not committed).
	if err := os.WriteFile(path, headFile, 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

// makeGitRepoDir creates a temporary git repo with multiple entity files committed,
// then writes working-tree versions. baseFiles maps filename→content at HEAD;
// headFiles maps filename→working-tree content (nil means "delete from working tree").
func makeGitRepoDir(t *testing.T, baseFiles, headFiles map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	for name, data := range baseFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	// Write working-tree versions.
	for name, data := range headFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestDiffValidDraftEdit(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err != nil {
		t.Errorf("expected no error for valid draft edit, got: %v", err)
	}
}

func TestDiffValidPromotion(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Promote("ENT"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err != nil {
		t.Errorf("expected no error for valid promotion D→P, got: %v", err)
	}
}

func TestDiffInvalidBodyEdit(t *testing.T) {
	// First promote entity to P, then try editing body (RD002).
	promoted := entityV2Promote("ENT")
	dir := makeGitRepo(t, "ENT.json", promoted, entityV2InvalidEdit("ENT"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err == nil {
		t.Fatal("expected error for illegal body edit after publish, got nil")
	}
	testutil.AssertContains(t, err.Error(), "diff found")
}

func TestDiffNewFile(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")
	// Commit an unrelated file so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "README")
	run("git", "commit", "-m", "init")

	// New entity file — not in HEAD.
	entityPath := filepath.Join(dir, "NEW.json")
	if err := os.WriteFile(entityPath, entityV1("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", entityPath)
	if err != nil {
		t.Errorf("expected no error for valid new entity, got: %v", err)
	}
}

func TestDiffDirMode(t *testing.T) {
	base := map[string][]byte{
		"ENT.json": entityV1("ENT"),
		"OTH.json": entityV1("OTH"),
	}
	head := map[string][]byte{
		"ENT.json": entityV2Draft("ENT"),
		"OTH.json": entityV1("OTH"), // unchanged
	}
	dir := makeGitRepoDir(t, base, head)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	if err != nil {
		t.Errorf("expected no error for valid dir diff, got: %v", err)
	}
}

func TestDiffDirModeInvalid(t *testing.T) {
	promoted := entityV2Promote("ENT")
	base := map[string][]byte{"ENT.json": promoted}
	head := map[string][]byte{"ENT.json": entityV2InvalidEdit("ENT")}
	dir := makeGitRepoDir(t, base, head)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	if err == nil {
		t.Fatal("expected error for invalid dir diff, got nil")
	}
	testutil.AssertContains(t, err.Error(), "diff found")
}

func TestDiffJSONOutput(t *testing.T) {
	promoted := entityV2Promote("ENT")
	dir := makeGitRepo(t, "ENT.json", promoted, entityV2InvalidEdit("ENT"))

	output, _ := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--json")

	var diags []result.Diagnostic
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &diags); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, output)
	}
	if len(diags) == 0 {
		t.Error("expected at least one diagnostic in JSON output")
	}
}

func TestDiffStrictMode(t *testing.T) {
	// RD006 fires as Warning when a superseding rule reaches I without the superseded being R.
	// Build an entity where a superseding rule is promoted to I while superseded stays P.
	base := []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "ENT", "title": "Test"},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z"},
  "rules": [
    {"id": "ENT-001", "body": "Old rule.", "state": "P", "revision": 0, "added_by": "@alice"},
    {"id": "ENT-002", "body": "New rule.", "state": "P", "revision": 0, "added_by": "@bob", "supersedes": "ENT-001"}
  ],
  "notes": []
}`)
	head := []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "ENT", "title": "Test"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {"id": "ENT-001", "body": "Old rule.", "state": "P", "revision": 0, "added_by": "@alice"},
    {"id": "ENT-002", "body": "New rule.", "state": "I", "revision": 0, "added_by": "@bob", "supersedes": "ENT-001"}
  ],
  "notes": []
}`)
	dir := makeGitRepo(t, "ENT.json", base, head)

	// Without --strict: RD006 is a warning, command succeeds.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err != nil {
		t.Errorf("expected no error without --strict (RD006 is warning), got: %v", err)
	}

	// With --strict: RD006 warning → error, command fails.
	_, err = testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--strict")
	if err == nil {
		t.Error("expected error with --strict (RD006 warning promoted to error), got nil")
	}
}

func TestDiffBaseFlag(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) string {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	// Commit v1.
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), entityV1("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "ENT.json")
	run("git", "commit", "-m", "v1")
	v1SHA := run("git", "rev-parse", "HEAD")

	// Commit v2 (promoted).
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), entityV2Promote("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "ENT.json")
	run("git", "commit", "-m", "v2")

	// Working tree: further valid edit.
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), entityV2Promote("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Diff against v1SHA (earlier commit): should still pass.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--base", v1SHA)
	if err != nil {
		t.Errorf("expected no error diffing against explicit SHA, got: %v", err)
	}
}

func TestDiffFileNotExist(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", "/no/such/path/ENT.json")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
	testutil.AssertContains(t, err.Error(), "does not exist")
}
