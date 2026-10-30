package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestCIInitRealmRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "realm.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wf := readFile(t, filepath.Join(dir, ".github", "workflows", "strspc-lint.yml"))
	for _, want := range []string{"strspc-lint.yml@v1", "path: rules/", "strict: true"} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q:\n%s", want, wf)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".strspc")); err == nil {
		t.Error("realm repo should not get .strspc/")
	}
}

func TestCIInitConsumerRepo(t *testing.T) {
	dir := t.TempDir()
	src := "github://acme/specs@v1.0.0/rules"

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--rules-source", src); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg := readFile(t, filepath.Join(dir, ".strspc", "config.yaml")); !strings.Contains(cfg, src) {
		t.Errorf("config missing rule source %q:\n%s", src, cfg)
	}
	if wf := readFile(t, filepath.Join(dir, ".github", "workflows", "strspc-check.yml")); !strings.Contains(wf, "strspc-check.yml@v1") {
		t.Errorf("workflow missing reusable ref:\n%s", wf)
	}
}

func TestCIInitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir)
	if err == nil {
		t.Fatal("expected error for existing files, got nil")
	}
	testutil.AssertContains(t, err.Error(), "already exists")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--force"); err != nil {
		t.Fatalf("--force should overwrite: %v", err)
	}
}

func TestCIInitDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	output, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Would write")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry run wrote %d entries", len(entries))
	}
}

func TestCIInitUnknownKind(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", t.TempDir(), "--kind", "bogus")
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	testutil.AssertContains(t, err.Error(), "unknown kind")
}
