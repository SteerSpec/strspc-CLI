package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// syncEntityJSON is a minimal entity file accepted by ruleresolve's LocalSource.
const syncEntityJSON = `{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {
    "id": "TST",
    "title": "Test",
    "description": "A test entity."
  },
  "rule_set": {
    "version": "0.1.0",
    "timestamp": "2026-01-01T00:00:00Z",
    "hash": null
  },
  "rules": [],
  "sub_entities": [],
  "notes": []
}
`

// writeSyncConfig writes a .strspc/config.yaml pointing at rulesDir.
func writeSyncConfig(t *testing.T, dir, rulesDir string) {
	t.Helper()
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "rules:\n  - source: " + rulesDir + "\n    scope: local\ncache:\n  ttl: 24h\n"
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncNoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
	if err == nil {
		t.Fatal("expected error when no config present")
	}
	testutil.AssertContains(t, err.Error(), "strspc init")
}

func TestSyncLocalSource(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(syncEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSyncConfig(t, dir, rulesDir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Synced")
	testutil.AssertContains(t, output, "1 rule(s)")
}

func TestSyncEmptySource(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSyncConfig(t, dir, rulesDir)

	// RSV006 is a warning (empty directory), not an error — sync should succeed.
	output, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
	if err != nil {
		t.Fatalf("unexpected error for empty source: %v", err)
	}
	testutil.AssertContains(t, output, "Synced 0 rule(s)")
}

func TestSyncForce(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(syncEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSyncConfig(t, dir, rulesDir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir, "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Synced")
}

func TestSyncJSON(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(syncEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSyncConfig(t, dir, rulesDir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out syncOutput
	if jsonErr := json.Unmarshal([]byte(output), &out); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	if !out.OK {
		t.Error("expected ok=true in JSON output")
	}
	if out.RuleCount != 1 {
		t.Errorf("expected rule_count=1, got %d", out.RuleCount)
	}
}

func TestSyncVerbose(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(syncEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSyncConfig(t, dir, rulesDir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir, "--verbose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output, "TST.json") {
		t.Errorf("verbose output should list resolved files, got: %s", output)
	}
}

func TestSyncInvalidSource(t *testing.T) {
	dir := t.TempDir()
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "rules:\n  - source: github://SteerSpec/strspc-rules@latest/rules/core\n    scope: global\n"
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
	if err == nil {
		t.Fatal("expected error for github:// source (not yet implemented)")
	}
	testutil.AssertContains(t, err.Error(), "not yet implemented")
}

func TestSyncHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "sync", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--force")
	testutil.AssertContains(t, output, "--verbose")
	testutil.AssertContains(t, output, "--json")
}
