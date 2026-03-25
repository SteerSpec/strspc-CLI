package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// checkEntityJSON is a minimal entity file with one draft rule.
const checkEntityJSON = `{
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
  "rules": [
    {
      "id": "TST.R001",
      "revision": 0,
      "state": "D",
      "body": "All code must be tested.",
      "added_by": "@test",
      "added_at": "2026-01-01T00:00:00Z",
      "supersedes": null
    }
  ],
  "sub_entities": [],
  "notes": []
}
`

// writeCheckConfig writes a .strspc/config.yaml pointing at rulesDir with provider null.
func writeCheckConfig(t *testing.T, dir, rulesDir string) {
	t.Helper()
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "rules:\n  - source: " + rulesDir + "\n    scope: local\nevaluator:\n  provider: null\ncache:\n  ttl: 24h\n"
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckNoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "check", dir)
	if err == nil {
		t.Fatal("expected error when no config present")
	}
	testutil.AssertContains(t, err.Error(), "strspc init")
}

func TestCheckStaticOnly(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(checkEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCheckConfig(t, dir, rulesDir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "check", dir, "--static-only")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Checked")
	testutil.AssertContains(t, output, "1 rule(s)")
}

func TestCheckJSON(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "TST.json"), []byte(checkEntityJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCheckConfig(t, dir, rulesDir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "check", dir, "--static-only", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var diags []map[string]any
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	// In static-only mode exactly one RE003 (Info) diagnostic is expected.
	if len(diags) != 1 {
		t.Errorf("expected 1 diagnostic, got %d", len(diags))
	}
	if code, _ := diags[0]["Code"].(string); code != "RE003" {
		t.Errorf("expected RE003 diagnostic, got %q", code)
	}
}

func TestCheckHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "check", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--pr")
	testutil.AssertContains(t, output, "--provider")
	testutil.AssertContains(t, output, "--static-only")
	testutil.AssertContains(t, output, "--json")
}
