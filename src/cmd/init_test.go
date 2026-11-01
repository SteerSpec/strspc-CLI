package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestInitCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	output, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "Initialized SteerSpec in "+filepath.Join(dir, ".strspc"))

	configPath := filepath.Join(dir, ".strspc", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "rules:") {
		t.Error("config.yaml should contain rules section")
	}
	if !strings.Contains(content, "evaluator:") {
		t.Error("config.yaml should contain evaluator section")
	}
	if !strings.Contains(content, "cache:") {
		t.Error("config.yaml should contain cache section")
	}
}

func TestInitAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir)
	if err == nil {
		t.Fatal("expected error for existing .strspc/, got nil")
	}
	testutil.AssertContains(t, err.Error(), "already exists")
}

func TestInitForce(t *testing.T) {
	dir := t.TempDir()
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir, "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(strspcDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old" {
		t.Error("config.yaml should have been overwritten")
	}
}

func TestInitGitignore(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".strspc/cache.db") {
		t.Error(".gitignore should contain .strspc/cache.db")
	}
}

func TestInitNoGitignore(t *testing.T) {
	dir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should succeed without .gitignore.
	configPath := filepath.Join(dir, ".strspc", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.yaml should exist")
	}
}

func TestInitGitignoreAlreadyHasEntry(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(".strspc/cache.db\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), ".strspc/cache.db") != 1 {
		t.Error(".gitignore should not duplicate the entry")
	}
}

func TestInitHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "init", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--force")
}

func TestInitConfigIsValid(t *testing.T) {
	dir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := loadStrspcConfig(dir)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	entries, err := buildSourceEntries(cfg)
	if err != nil {
		t.Fatalf("generated config has invalid sources: %v", err)
	}
	if len(entries) < 1 {
		t.Fatal("generated config must declare at least one rule source (SPCFG-004)")
	}
	// Spelled out rather than compared with defaultCoreSource so a typo or a
	// stale tag in the constant fails here (the pin is the point of the default).
	const wantSource = "github://SteerSpec/strspc-rules@v1.14.0/rules/core"
	if entries[0].Source != wantSource {
		t.Errorf("got source %q, want %q", entries[0].Source, wantSource)
	}
	if entries[0].Scope != "global" {
		t.Errorf("got scope %q, want global", entries[0].Scope)
	}
	if cfg.Evaluator.Provider != "claude" {
		t.Errorf("got provider %q, want claude", cfg.Evaluator.Provider)
	}
	if cfg.Cache.TTL != "24h" {
		t.Errorf("got cache.ttl %q, want 24h", cfg.Cache.TTL)
	}
	if len(cfg.FailOn) != 1 || cfg.FailOn[0] != "I" {
		t.Errorf("got fail_on %v, want [I]", cfg.FailOn)
	}
}

func TestInitProviderNull(t *testing.T) {
	dir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "init", "--provider", "null", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := loadStrspcConfig(dir)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	// yaml.v3 decodes a bare null into the zero value.
	if cfg.Evaluator.Provider != "" {
		t.Errorf("got provider %q, want empty (null)", cfg.Evaluator.Provider)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".strspc", "config.yaml"))
	if !strings.Contains(string(data), "provider: null") {
		t.Error("config.yaml should contain `provider: null`")
	}
}

func TestInitProviderUnknown(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "init", "--provider", "gpt", t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	testutil.AssertContains(t, err.Error(), "gpt")
}

func TestInitWritesStrspcGitignore(t *testing.T) {
	dir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".strspc", ".gitignore"))
	if err != nil {
		t.Fatalf("reading .strspc/.gitignore: %v", err)
	}
	for _, want := range []string{"cache.db\n", "cache/\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf(".strspc/.gitignore should contain %q, got:\n%s", want, data)
		}
	}
}

func TestInitNoDeadLinks(t *testing.T) {
	dir := t.TempDir()
	out, err := testutil.ExecuteCommand(NewRootCmd(), "init", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertNotContains(t, out, "steerspec.dev/docs")
	testutil.AssertNotContains(t, out, "Coming soon")
	testutil.AssertContains(t, out, "https://github.com/SteerSpec/strspc-CLI#readme")

	data, _ := os.ReadFile(filepath.Join(dir, ".strspc", "config.yaml"))
	testutil.AssertNotContains(t, string(data), "steerspec.dev/docs")
	testutil.AssertNotContains(t, string(data), "implemented")
}
