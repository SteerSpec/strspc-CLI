package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// A JSON document that can't be written to stdout is an output error (exit 4),
// whichever helper writes it.
func TestJSONHelpersWriteError(t *testing.T) {
	tests := []struct {
		name  string
		write func() error
	}{
		{"writeJSONOutput", func() error { return writeJSONOutput(failingWriter{}, map[string]string{"k": "v"}) }},
		{"writeJSON", func() error { return writeJSON(failingWriter{}, &result.Result{}, false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.write()
			assertExit(t, err, exitOutput)
			testutil.AssertContains(t, err.Error(), "writing JSON output: broken pipe")
		})
	}
}

// Both helpers still write the document and return nil when stdout works.
func TestJSONHelpersWrite(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONOutput(&buf, map[string]string{"k": "v"}); err != nil {
		t.Fatalf("writeJSONOutput: %v", err)
	}
	testutil.AssertContains(t, buf.String(), `"k": "v"`)
	buf.Reset()
	if err := writeJSON(&buf, &result.Result{}, false); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	testutil.AssertContains(t, buf.String(), "[]")
}

// runWithFailingStdout runs the root command with args and a stdout that
// refuses every write.
func runWithFailingStdout(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var errOut bytes.Buffer
	root.SetOut(failingWriter{})
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return errOut.String(), err
}

// The commands that print --json through the shared helpers, and sync, exit 4
// when stdout can't take the document.
func TestJSONCommandsWriteError(t *testing.T) {
	setupLintTest(t)
	tests := []struct {
		name string
		args func(t *testing.T) []string
	}{
		{"rule add (writeJSONOutput)", func(t *testing.T) []string {
			dir := t.TempDir()
			setupEntityFile(t, dir, "TST")
			return []string{"rule", "add", "TST", "--body", "Test rule", "--added-by", "github:bob", "--dir", dir, "--json"}
		}},
		{"lint (writeJSON)", func(t *testing.T) []string {
			dir := t.TempDir()
			writeEntityFile(t, dir, "TST.json", validEntityJSON)
			return []string{"lint", filepath.Join(dir, "TST.json"), "--json"}
		}},
		{"refs (writeJSON)", func(t *testing.T) []string {
			dir := setupRefsProject(t)
			writeRefsFile(t, dir, "README.md", "See APP-001.\n")
			return []string{"refs", "--json"}
		}},
		{"sync", func(t *testing.T) []string {
			dir := t.TempDir()
			rulesDir := filepath.Join(dir, "rules")
			writeEntityFile(t, rulesDir, "TST.json", syncEntityJSON)
			writeSyncConfig(t, dir, rulesDir)
			return []string{"sync", dir, "--json"}
		}},
		{"status", func(t *testing.T) []string {
			return []string{"status", t.TempDir(), "--json"}
		}},
		{"explain", func(*testing.T) []string {
			return []string{"explain", "RD005", "--json"}
		}},
		{"explain --list", func(*testing.T) []string {
			return []string{"explain", "--list", "--json"}
		}},
		{"render", func(t *testing.T) []string {
			return []string{"render", setupRenderInherit(t), "--json"}
		}},
		{"realm rehash", func(t *testing.T) []string {
			dir := t.TempDir()
			setupRehashRealm(t, dir, "1.21.0")
			return []string{"realm", "rehash", "--dir", dir, "--json"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runWithFailingStdout(t, tt.args(t)...)
			assertExit(t, err, exitOutput)
			testutil.AssertContains(t, err.Error(), "writing JSON output: broken pipe")
		})
	}
}
