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
	// github:// is supported since strspc-manager v1.15.0; an unknown scheme
	// is still rejected at resolver construction, offline.
	cfg := "rules:\n  - source: s3://bucket/rules\n    scope: global\n"
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
	if err == nil {
		t.Fatal("expected error for unsupported source scheme")
	}
	testutil.AssertContains(t, err.Error(), "unsupported source scheme")
}

// writeRawSyncConfig writes cfg as .strspc/config.yaml under dir.
func writeRawSyncConfig(t *testing.T, dir, cfg string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".strspc", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncConfigErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  string
		want string
	}{
		{"unknown scope", "rules:\n  - source: ./rules/\n    scope: team\n", `invalid scope "team"`},
		{"unparsable ttl", "rules:\n  - source: ./rules/\n    scope: local\ncache:\n  ttl: soon\n", `parsing cache.ttl "soon"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRawSyncConfig(t, dir, tc.cfg)
			_, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

// A source that can't be resolved still gives a JSON document, then an error.
func TestSyncJSONWithErrors(t *testing.T) {
	dir := t.TempDir()
	writeSyncConfig(t, dir, filepath.Join(dir, "missing"))
	out, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir, "--json")
	if err == nil || !strings.Contains(err.Error(), "sync found") {
		t.Fatalf("error = %v, want sync found N error(s)", err)
	}
	var res struct {
		OK          bool              `json:"ok"`
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("output is not JSON: %v\n%s", jerr, out)
	}
	if res.OK || len(res.Diagnostics) == 0 {
		t.Errorf("output = %s, want ok false with diagnostics", out)
	}
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
