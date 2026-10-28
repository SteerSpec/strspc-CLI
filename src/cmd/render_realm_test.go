package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

const customFormat = `{"brackets": "{,}", "euid_position": "closing", "revision_splitter": "#"}`

func writeRealm(t *testing.T, dir, id, format string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "` + id +
		`", "title": "T", "version": "0.1.0"}, "dependencies": [], "rule_identifier_format": ` + format + `}`
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyEntity(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/basic.json")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "basic.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderHonoursRealmRuleIdentifierFormat(t *testing.T) {
	dir := t.TempDir()
	writeRealm(t, dir, "com.acme", customFormat)
	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", copyEntity(t, dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "{001-TST#0/D}")
}

func TestRenderSubRealmUsesParentFormat(t *testing.T) {
	root := t.TempDir()
	writeRealm(t, root, "com.acme", customFormat)
	sub := filepath.Join(root, "sub")
	writeRealm(t, sub, "com.acme.sub", "null")
	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", copyEntity(t, sub))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "{001-TST#0/D}")
}

func TestRenderDirectoryAppliesFormatPerRealm(t *testing.T) {
	root := t.TempDir()
	writeRealm(t, root, "com.acme", customFormat)
	other := filepath.Join(root, "other")
	writeRealm(t, other, "org.other", "null")
	copyEntity(t, root)
	copyEntity(t, other)
	outDir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "render", root, "-o", outDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	custom, _ := os.ReadFile(filepath.Join(outDir, "basic.md"))
	def, _ := os.ReadFile(filepath.Join(outDir, "other", "basic.md"))
	testutil.AssertContains(t, string(custom), "{001-TST#0/D}")
	testutil.AssertContains(t, string(def), "[TST-001.0/D]")
}

func TestRenderInvalidRealmFormatErrors(t *testing.T) {
	dir := t.TempDir()
	writeRealm(t, dir, "com.acme", `{"brackets": "<<,>>"}`)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", copyEntity(t, dir))
	if err == nil {
		t.Fatal("expected error for a format outside the schema enums")
	}
	testutil.AssertContains(t, err.Error(), "rule_identifier_format")
	testutil.AssertContains(t, err.Error(), "com.acme")
}
