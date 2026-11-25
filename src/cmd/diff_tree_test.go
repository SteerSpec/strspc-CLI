package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// commitBlob adds content as the blob at path, with mode, to the index of the
// repository in dir, without touching the working tree.
func commitBlob(t *testing.T, dir, mode, path, content string) {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sha := gitRun(t, dir, "hash-object", "-w", tmp)
	gitRun(t, dir, "update-index", "--add", "--cacheinfo", mode+","+sha+","+path)
}

// The base tree diff writes for the tree checks holds no link: a base with a
// link Link and a file link/planted.txt writes nothing outside it, also on a
// case-insensitive file system.
func TestDiffBaseTreeStaysInItsDirectory(t *testing.T) {
	outside := t.TempDir()
	dir := newPublishedRepo(t, "rules/ENT.json")
	commitBlob(t, dir, "120000", "rules/Link", outside)
	commitBlob(t, dir, "100644", "rules/link/planted.txt", "planted")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "plant")
	gitRun(t, dir, "reset", "-q", "--hard")

	if _, err := runDiff(t, filepath.Join(dir, "rules")); err != nil {
		t.Logf("diff: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the base tree wrote outside its directory (stat: %v)", err)
	}
}

// diff reads no entity file or directory through a link, on either side,
// and ignores a link that the resolver wouldn't read either.
func TestDiffRefusesLinksInTree(t *testing.T) {
	t.Run("entity file", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "elsewhere/X.json")
		linkOrSkip(t, filepath.Join(dir, "elsewhere", "X.json"), filepath.Join(dir, "rules", "X.json"))
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		if !errors.Is(err, errDiffLink) {
			t.Errorf("err = %v, want errDiffLink", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "elsewhere/X.json")
		linkOrSkip(t, filepath.Join(dir, "elsewhere"), filepath.Join(dir, "rules", "sub"))
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		if !errors.Is(err, errDiffLink) {
			t.Errorf("err = %v, want errDiffLink", err)
		}
	})
	t.Run("other file", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "README.md")
		linkOrSkip(t, filepath.Join(dir, "README.md"), filepath.Join(dir, "rules", "README.md"))
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitOK)
	})
	t.Run("entity file at the base", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json")
		commitBlob(t, dir, "120000", "rules/L.json", "ENT.json")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		if !errors.Is(err, errDiffLink) {
			t.Errorf("err = %v, want errDiffLink", err)
		}
	})
	t.Run("file mode", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "elsewhere/ENT.json")
		if err := os.RemoveAll(filepath.Join(dir, "rules")); err != nil {
			t.Fatal(err)
		}
		linkOrSkip(t, "elsewhere", filepath.Join(dir, "rules"))
		_, err := runDiff(t, filepath.Join(dir, "rules", "ENT.json"))
		if !errors.Is(err, errDiffLink) {
			t.Errorf("err = %v, want errDiffLink", err)
		}
	})
}

// diff runs the tree checks of rulediff.CompareRealms: repointing a
// dependency to another directory is RD031.
func TestDiffRunsRealmChecks(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	writeRealmManifest(t, filepath.Join(dir, "rules"), "dev.app",
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "manifest")
	writeRealmManifest(t, filepath.Join(dir, "rules"), "dev.app",
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../weak/"}`)

	out, err := runDiff(t, filepath.Join(dir, "rules"))
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RD031")
}

// Deleting a .json file that held no entity, such as a stray manifest, isn't
// the deletion of rules (rulediff.DeletionAllowed).
func TestDiffAllowsDeletingFileWithoutEntity(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	writeFile(t, dir, "rules/stray.json", `{"realm": {"id": "dev.stray", "title": "t", "version": "1.0.0"}}`)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "stray")
	if err := os.Remove(filepath.Join(dir, "rules", "stray.json")); err != nil {
		t.Fatal(err)
	}
	out, err := runDiff(t, filepath.Join(dir, "rules"))
	assertExit(t, err, exitOK)
	testutil.AssertNotContains(t, out, "RD005")
}
