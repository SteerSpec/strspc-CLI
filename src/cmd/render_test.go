package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRenderSingleFile(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json")
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
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "-o", outDir)
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
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/", "-o", outDir)
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
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "--format", "html")
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
}

func TestRenderDefaultFormat(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "# Entity: TST")
}

func TestRenderSchemaMismatch(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "--schema-version", "v2")
	if err == nil {
		t.Fatal("expected error for schema mismatch, got nil")
	}
	testutil.AssertContains(t, err.Error(), "schema version mismatch")
}

func TestRenderSubEntitySchemaMismatch(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/bad_sub_schema.json")
	if err == nil {
		t.Fatal("expected error for sub-entity schema mismatch, got nil")
	}
	testutil.AssertContains(t, err.Error(), "sub-entity BAD")
}

func TestRenderDirectorySkipsRealmSilently(t *testing.T) {
	outDir := t.TempDir()
	// testdata/ contains realm.json (non-entity) — should be skipped without warnings.
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/", "-o", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// realm.json should not produce a warning in output.
	if strings.Contains(output, "realm.json") {
		t.Errorf("realm.json should be silently skipped, got output: %s", output)
	}
	// Should not produce a realm.md file.
	realmOut := filepath.Join(outDir, "realm.md")
	if _, err := os.Stat(realmOut); !os.IsNotExist(err) {
		t.Error("realm.md should not be created")
	}
}

func TestRenderJSONSingleFile(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, `"id": "TST"`)
	testutil.AssertContains(t, output, `"title": "Test Entity"`)
	testutil.AssertContains(t, output, `"$schema"`)
}

func TestRenderJSONToOutput(t *testing.T) {
	outDir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "--json", "-o", outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outPath := filepath.Join(outDir, "basic.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	content := string(data)
	testutil.AssertContains(t, content, `"id": "TST"`)
}

func TestRenderJSONDirectoryWithoutOutputErrors(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/", "--json")
	if err == nil {
		t.Fatal("expected error for directory render with --json and no -o, got nil")
	}
	testutil.AssertContains(t, err.Error(), "--json with directory input requires -o")
}

func TestRenderJSONOutputInsideInputErrors(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/", "--json", "-o", "testdata/output")
	if err == nil {
		t.Fatal("expected error for output inside input directory, got nil")
	}
	testutil.AssertContains(t, err.Error(), "must not be inside input directory")
}

func TestRenderJSONWithFormatErrors(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "--json", "--format", "markdown")
	if err == nil {
		t.Fatal("expected error for --json with --format, got nil")
	}
	testutil.AssertContains(t, err.Error(), "--json cannot be combined")
}

func TestRenderJSONWithTemplateErrors(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json", "--json", "--template", "foo.tmpl")
	if err == nil {
		t.Fatal("expected error for --json with --template, got nil")
	}
	testutil.AssertContains(t, err.Error(), "--json cannot be combined")
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
	testutil.AssertContains(t, output, "--json")
}
