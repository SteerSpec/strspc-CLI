package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Files that are not JSON are not linted, so a directory holding one entity
// next to them lints that entity alone.
func TestLintDirectorySkipsNonJSONFiles(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)
	writeEntityFile(t, dir, "README.md", "# not an entity")

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, output)
	}
	testutil.AssertContains(t, output, "No errors or warnings")
}

func TestLintJSONWriteFailure(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)

	cmd := NewRootCmd()
	cmd.SetOut(failingWriter{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"lint", dir, "--json"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("error %v, want the write error", err)
	}
}

// An entity file or a directory that cannot be read is an RL000 error naming
// it, and the rest of the tree is still linted.
func TestLintDirectoryReportsUnreadablePaths(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs paths the current user cannot read")
	}
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "TST.json", validEntityJSON)
	writeEntityFile(t, dir, "SEC.json", validEntityJSON)
	if err := os.Chmod(filepath.Join(dir, "SEC.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	writeEntityFile(t, locked, "LCK.json", validEntityJSON)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--json")
	if err == nil {
		t.Fatal("expected an error for unreadable paths")
	}
	var diags []result.Diagnostic
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, output)
	}
	var reading, accessing bool
	for _, d := range diags {
		switch {
		case d.Code == "RL000" && strings.Contains(d.Message, "reading file") && strings.HasSuffix(d.Path, "SEC.json"):
			reading = true
		case d.Code == "RL000" && strings.Contains(d.Message, "accessing path") && strings.HasSuffix(d.Path, "locked"):
			accessing = true
		}
	}
	if !reading || !accessing {
		t.Errorf("want RL000 for the unreadable file and directory, got %+v", diags)
	}
}
