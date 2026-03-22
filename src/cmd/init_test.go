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

	testutil.AssertContains(t, output, "Initialized SteerSpec")

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
