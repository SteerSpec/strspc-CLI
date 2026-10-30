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
	if gi := readFile(t, filepath.Join(dir, ".strspc", ".gitignore")); !strings.Contains(gi, "cache.db") || !strings.Contains(gi, "cache/") {
		t.Errorf(".strspc/.gitignore should ignore cache artifacts:\n%s", gi)
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

func TestCIInitRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".github")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--kind", "realm", "--force")
	if err == nil {
		t.Fatal("expected error for symlinked .github")
	}
	testutil.AssertContains(t, err.Error(), "symlink")
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote through symlink: %d entries", len(entries))
	}
}

func TestCIInitRejectsUnsafeValues(t *testing.T) {
	for _, flag := range []string{"--rules-path", "--rules-source", "--workflow-ref"} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", t.TempDir(), flag, "x\n  evil: true")
		if err == nil {
			t.Fatalf("%s: expected error for value with newline", flag)
		}
		testutil.AssertContains(t, err.Error(), "invalid "+flag)
	}
}

func TestCIInitBranchAndToken(t *testing.T) {
	dir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--branch", "develop"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wf := readFile(t, filepath.Join(dir, ".github", "workflows", "strspc-check.yml"))
	for _, want := range []string{"branches: [develop]", "STRSPC_GITHUB_TOKEN: ${{ secrets.STRSPC_GITHUB_TOKEN }}"} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q:\n%s", want, wf)
		}
	}

	realm := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", realm, "--kind", "realm", "--branch", "develop"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf := readFile(t, filepath.Join(realm, ".github", "workflows", "strspc-lint.yml")); strings.Count(wf, "branches: [develop]") != 2 {
		t.Errorf("realm workflow should target develop twice:\n%s", wf)
	}
}

func TestCIInitCLIVersion(t *testing.T) {
	for _, tc := range []struct {
		name, kind, workflow, want string
	}{
		{"realm", "realm", "strspc-lint.yml", "      strict: true\n      cli-version: 1.24.0\n"},
		{"consumer", "consumer", "strspc-check.yml", "      pull-requests: read\n    with:\n      cli-version: 1.24.0\n    secrets:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--kind", tc.kind, "--cli-version", "1.24.0"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			wfPath := filepath.Join(dir, ".github", "workflows", tc.workflow)
			if wf := readFile(t, wfPath); !strings.Contains(wf, tc.want) {
				t.Errorf("workflow missing %q:\n%s", tc.want, wf)
			}

			if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--kind", tc.kind, "--cli-version", "1.24.1", "--force"); err != nil {
				t.Fatalf("--force should overwrite: %v", err)
			}
			if wf := readFile(t, wfPath); !strings.Contains(wf, "cli-version: 1.24.1\n") || strings.Contains(wf, "1.24.0") {
				t.Errorf("--force should write the new pin:\n%s", wf)
			}
		})
	}
}

func TestCIInitCLIVersionOmittedByDefault(t *testing.T) {
	for _, tc := range []struct{ kind, workflow string }{
		{"realm", "strspc-lint.yml"},
		{"consumer", "strspc-check.yml"},
	} {
		dir := t.TempDir()
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", dir, "--kind", tc.kind); err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.kind, err)
		}
		wf := readFile(t, filepath.Join(dir, ".github", "workflows", tc.workflow))
		if strings.Contains(wf, "cli-version") {
			t.Errorf("%s: workflow should not set cli-version:\n%s", tc.kind, wf)
		}
		if tc.kind == "consumer" && strings.Contains(wf, "with:") {
			t.Errorf("consumer workflow should have no with: block:\n%s", wf)
		}
	}
}

func TestCIInitCLIVersionValues(t *testing.T) {
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", t.TempDir(), "--cli-version", "latest"); err != nil {
		t.Errorf("latest rejected: %v", err)
	}
	for _, v := range []string{"v1.24.0", "1.24", "1", "1.24.0-rc.1", "01.2.3", "main", "1.24.0\n      evil: true"} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", t.TempDir(), "--cli-version", v)
		if err == nil {
			t.Errorf("--cli-version %q: expected error", v)
			continue
		}
		testutil.AssertContains(t, err.Error(), "invalid --cli-version")
	}
}

func TestCIInitRejectsYAMLIndicators(t *testing.T) {
	for _, v := range []string{"@release", ":foo", "~", "-x", "foo:"} {
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", t.TempDir(), "--branch", v); err == nil {
			t.Errorf("--branch %q: expected error", v)
		}
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "ci", "init", t.TempDir(), "--rules-source", defaultCoreSource); err != nil {
		t.Errorf("default source rejected: %v", err)
	}
}
