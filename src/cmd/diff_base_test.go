package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// newPublishedRepo commits files, each name an entity file holding the
// Published ENT-001, in a new repository, and returns its directory.
func newPublishedRepo(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	for _, n := range names {
		writeFile(t, dir, n, string(entityV2Promote("ENT")))
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	return dir
}

func runDiff(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"diff"}, args...)...)
}

// A compared directory replaced by a link is refused: listing the base at
// the link's target would compare nothing with what the directory held.
func TestDiffRefusesLinkedDirectory(t *testing.T) {
	// lib/ holds ENT.json with ENT-001 rewritten: through the link, it would be
	// compared with itself rather than with app/ENT.json.
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	writeFile(t, dir, "rules/app/ENT.json", string(entityV2Promote("ENT")))
	writeFile(t, dir, "rules/lib/ENT.json", string(entityV2InvalidEdit("ENT")))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	if err := os.RemoveAll(filepath.Join(dir, "rules", "app")); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, "lib", filepath.Join(dir, "rules", "app"))
	for _, p := range []string{"rules/app/", "rules/app"} {
		_, err := runDiff(t, filepath.Join(dir, filepath.FromSlash(p))+string(filepath.Separator))
		if !errors.Is(err, errDiffLink) {
			t.Errorf("diff %s: err = %v, want errDiffLink", p, err)
		}
	}
}

// A deleted entity file is RD005 whatever bytes its name holds.
func TestDiffReportsDeletedNonASCIIFile(t *testing.T) {
	dir := newPublishedRepo(t, "rules/café.json", "rules/ENT2.json")
	if err := os.Remove(filepath.Join(dir, "rules", "café.json")); err != nil {
		t.Fatal(err)
	}
	out, err := runDiff(t, filepath.Join(dir, "rules"))
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RD005")
	testutil.AssertContains(t, out, "café.json")
}

// A file whose name starts with _ is an entity file like any other, as the
// resolver reads it: its changes are compared and its deletion is RD005.
func TestDiffComparesUnderscoreFiles(t *testing.T) {
	t.Run("edit", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/_ENT.json", "rules/ENT2.json")
		writeFile(t, dir, "rules/_ENT.json", string(entityV2InvalidEdit("ENT")))
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitViolations)
	})
	t.Run("deletion", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/_ENT.json", "rules/ENT2.json")
		if err := os.Remove(filepath.Join(dir, "rules", "_ENT.json")); err != nil {
			t.Fatal(err)
		}
		out, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitViolations)
		testutil.AssertContains(t, out, "RD005")
	})
	t.Run("directory", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/_hidden/ENT.json", "rules/ENT2.json")
		writeFile(t, dir, "rules/_hidden/ENT.json", string(entityV2InvalidEdit("ENT")))
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitViolations)
	})
}

// A submodule under the compared directory is an error: its files are in
// another repository, so none of them can be compared.
func TestDiffRefusesSubmodule(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+sha+",rules/sub")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "submodule")
	_, err := runDiff(t, filepath.Join(dir, "rules"))
	if err == nil {
		t.Fatal("diff passed a tree holding a submodule")
	}
	testutil.AssertContains(t, err.Error(), "submodule")
}
