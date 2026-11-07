package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// renderTree writes basic.json into dir and returns dir. Extra files are
// written relative to dir: a name ending in "/" is created as a directory.
func renderTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	copyEntity(t, dir)
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if body == "/" {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// aFile returns the path of a regular file, used where a directory is expected.
func aFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeTemplate(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.tmpl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderCustomTemplate(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", "testdata/basic.json",
		"--template", writeTemplate(t, "ID={{.Entity.ID}} rules={{len .Rules}}\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ID=TST rules=2\n" {
		t.Errorf("output = %q, want the custom template's exact text", out)
	}
}

func TestRenderFileErrors(t *testing.T) {
	notJSON := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(notJSON, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	outWithDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(outWithDir, "basic.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	outWithJSONDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(outWithJSONDir, "basic.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unparsable file", []string{notJSON}, "parsing entity JSON"},
		{"output is a file", []string{"testdata/basic.json", "-o", aFile(t)}, "creating output directory"},
		{"output file is a directory", []string{"testdata/basic.json", "-o", outWithDir}, "creating "},
		{"template fails to execute", []string{"testdata/basic.json", "--template", writeTemplate(t, "{{.Nope}}")}, "executing template"},
		{"json: unparsable file", []string{notJSON, "--json"}, "parsing entity JSON"},
		{"json: schema mismatch", []string{"testdata/basic.json", "--json", "--schema-version", "v2"}, "basic.json"},
		{"json: output is a file", []string{"testdata/basic.json", "--json", "-o", aFile(t)}, "creating output directory"},
		{"json: output file is a directory", []string{"testdata/basic.json", "--json", "-o", outWithJSONDir}, "basic.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"render"}, tt.args...)...)
			if err == nil {
				t.Fatal("expected an error")
			}
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

// Underscore-prefixed directories and files, and files that are not JSON, are
// not rendered; the entity next to them is.
func TestRenderDirectorySkipsUnderscoreAndNonJSON(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		name := "markdown"
		if jsonOut {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			dir := renderTree(t, map[string]string{
				"_schema/entity.json": "{not json",
				"_draft.json":         "{not json",
				"notes.txt":           "text",
			})
			outDir := t.TempDir()
			args := []string{"render", dir, "-o", outDir}
			if jsonOut {
				args = append(args, "--json")
			}
			out, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, out)
			}
			testutil.AssertContains(t, out, "Rendered 1 of 1 file(s)")
			testutil.AssertNotContains(t, out, "skipping")
		})
	}
}

func TestRenderDirectoryErrors(t *testing.T) {
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	onlyBad := t.TempDir()
	if err := os.WriteFile(filepath.Join(onlyBad, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	outWithDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(outWithDir, "basic.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	outWithJSONDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(outWithJSONDir, "basic.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	brokenRealm := renderTree(t, map[string]string{"sub/realm.json": "{not json"})
	copyEntity(t, filepath.Join(brokenRealm, "sub"))

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no entity files", []string{empty, "-o", t.TempDir()}, "no entity JSON files found"},
		{"every file skipped", []string{onlyBad, "-o", t.TempDir()}, "were skipped"},
		{"output is a file", []string{"testdata/", "-o", aFile(t)}, "creating output directory"},
		{"output file is a directory", []string{renderTree(t, nil), "-o", outWithDir}, "creating "},
		{"template fails to execute", []string{renderTree(t, nil), "-o", t.TempDir(), "--template", writeTemplate(t, "{{.Nope}}")}, "rendering "},
		{"realm manifest does not load", []string{brokenRealm, "-o", t.TempDir()}, "realm.json"},
		{"json: no entity files", []string{empty, "--json", "-o", t.TempDir()}, "no entity JSON files found"},
		{"json: output is a file", []string{renderTree(t, nil), "--json", "-o", aFile(t)}, "creating output directory"},
		{"json: output file is a directory", []string{renderTree(t, nil), "--json", "-o", outWithJSONDir}, "writing "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"render"}, tt.args...)...)
			if err == nil {
				t.Fatalf("expected an error, got:\n%s", out)
			}
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

// A file that fails the schema is skipped with a warning in a JSON directory
// render, and the other files are still written.
func TestRenderDirectoryJSONWarnsOnSchemaMismatch(t *testing.T) {
	bad, err := os.ReadFile("testdata/bad_sub_schema.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := renderTree(t, map[string]string{"bad.json": string(bad)})
	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", dir, "--json", "-o", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "skipping")
	testutil.AssertContains(t, out, "Rendered 1 of 2 file(s)")
}

// When the realm of an entity that inherits cannot be read, render --json
// fails rather than dropping the inherited rules.
func TestRenderJSONInheritingEntityWithUnreadableRealm(t *testing.T) {
	for _, dirInput := range []bool{false, true} {
		name := "file"
		if dirInput {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			rules := filepath.Join(dir, "rules")
			writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P"})
			if err := os.Mkdir(filepath.Join(rules, "realm.json"), 0o755); err != nil {
				t.Fatal(err)
			}
			args := []string{"render", filepath.Join(rules, "SAVACCT.json"), "--json"}
			if dirInput {
				args = []string{"render", rules, "--json", "-o", t.TempDir()}
			}
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil {
				t.Fatal("expected an error")
			}
			testutil.AssertContains(t, err.Error(), "not a regular file")
		})
	}
}
