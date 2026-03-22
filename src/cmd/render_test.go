package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRenderSingleFile(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "../internal/render/testdata/basic.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "# Entity: TST")
	testutil.AssertContains(t, output, "[TST-001.0/D]")
	testutil.AssertContains(t, output, "MUST be used for testing purposes only.")
	testutil.AssertContains(t, output, "**rationale:**")
}

func TestRenderSingleFileToOutput(t *testing.T) {
	outDir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "../internal/render/testdata/basic.json", "-o", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outPath := filepath.Join(outDir, "basic.md")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	content := string(data)
	if content == "" {
		t.Fatal("output file is empty")
	}
	testutil.AssertContains(t, content, "# Entity: TST")
}

func TestRenderDirectory(t *testing.T) {
	outDir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "../internal/render/testdata/", "-o", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"basic.md", "nested.md", "empty.md"} {
		path := filepath.Join(outDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected output file %s to exist", name)
		}
	}
}

func TestRenderMissingPath(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render")
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

func TestRenderInvalidPath(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "../internal/render/testdata/basic.json", "--format", "html")
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
}

func TestRenderDefaultFormat(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "../internal/render/testdata/basic.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "# Entity: TST")
}

func TestRenderSchemaMismatch(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "../internal/render/testdata/basic.json", "--schema-version", "v2")
	if err == nil {
		t.Fatal("expected error for schema mismatch, got nil")
	}
}

func TestRenderHelpContainsFlags(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--output")
	testutil.AssertContains(t, output, "--format")
	testutil.AssertContains(t, output, "--template")
	testutil.AssertContains(t, output, "--schema-version")
}
