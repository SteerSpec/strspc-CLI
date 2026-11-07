package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRefsJSONWriteFailure(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "README.md", "See APP-001.\n")
	cmd := NewRootCmd()
	cmd.SetOut(failingWriter{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"refs", "--json"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Errorf("error %v, want the write error", err)
	}
}

// A realm whose rule_identifier_format the checker can't build is a
// configuration error.
func TestRefsInvalidRealmRuleIdentifierFormat(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "rules/realm.json",
		`{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0"},
		  "dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}],
		  "rule_identifier_format": {"brackets": "<<,>>"}}`)
	writeRefsFile(t, dir, "README.md", "See APP-001.\n")
	_, err := runRefsCmd(t)
	assertExit(t, err, exitConfig)
}

// A link named as an argument whose target is missing can't be checked to
// stay inside the project.
func TestRefsDanglingLinkArgument(t *testing.T) {
	dir := setupRefsProject(t)
	if err := os.Symlink(filepath.Join(dir, "gone.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := runRefsCmd(t, "link.md")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "link.md")
}

// A file or directory that can't be read is a configuration error, not a
// silent skip.
func TestRefsUnreadablePaths(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs paths the current user cannot read")
	}
	t.Run("file", func(t *testing.T) {
		dir := setupRefsProject(t)
		writeRefsFile(t, dir, "docs/secret.md", "APP-001\n")
		if err := os.Chmod(filepath.Join(dir, "docs", "secret.md"), 0o000); err != nil {
			t.Fatal(err)
		}
		_, err := runRefsCmd(t)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "reading docs/secret.md")
	})
	t.Run("directory", func(t *testing.T) {
		dir := setupRefsProject(t)
		writeRefsFile(t, dir, "docs/a.md", "APP-001\n")
		locked := filepath.Join(dir, "docs")
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		_, err := runRefsCmd(t)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "listing files")
	})
}
