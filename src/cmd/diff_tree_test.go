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

// diff reads no entity file or directory of the head tree through a link,
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
	t.Run("other file in the tree", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "rules/docs.txt")
		linkOrSkip(t, "docs.txt", filepath.Join(dir, "rules", "README.md"))
		_, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitOK)
	})
	// As the resolver's walk: refused without following.
	for name, c := range map[string]struct{ link, target string }{
		"other file outside the tree": {"README.md", "../README.md"},
		"absolute":                    {"README.md", "/etc/hosts"},
		"to another link":             {"README.md", "hop.txt"},
		"*.JSON in another case":      {"X.JSON", "docs.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := newPublishedRepo(t, "rules/ENT.json", "rules/docs.txt", "README.md")
			linkOrSkip(t, "docs.txt", filepath.Join(dir, "rules", "hop.txt"))
			linkOrSkip(t, c.target, filepath.Join(dir, "rules", c.link))
			if _, err := runDiff(t, filepath.Join(dir, "rules")); !errors.Is(err, errDiffLink) {
				t.Errorf("err = %v, want errDiffLink", err)
			}
		})
	}
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

// A compared path that is a link to another repository's root is refused:
// git would answer for that repository, and its base would be compared.
func TestDiffRefusesLinkToAnotherRepository(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	weak := filepath.Join(dir, "weak")
	gitRun(t, dir, "init", "-q", weak)
	writeFile(t, weak, "ENT.json", string(entityV2InvalidEdit("ENT")))
	gitRun(t, weak, "add", "-A")
	gitRun(t, weak, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "weak")
	if err := os.RemoveAll(filepath.Join(dir, "rules")); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, weak, filepath.Join(dir, "rules"))
	for _, p := range []string{filepath.Join(dir, "rules") + string(filepath.Separator), filepath.Join(dir, "rules", "ENT.json")} {
		if _, err := runDiff(t, p); !errors.Is(err, errDiffLink) {
			t.Errorf("diff %s: err = %v, want errDiffLink", p, err)
		}
	}
}

// A new Realm Note by someone not in the Realm's contributors is RD023, also
// when diff runs above the Realm.
func TestDiffChecksRealmNoteContributors(t *testing.T) {
	manifest := func(notes string) string {
		return `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "t", "version": "1.0.0",` +
			` "contributors": [{"id": "github:alice"}]}, "notes": [` + notes + `]}`
	}
	dir := newPublishedRepo(t, "rules/ENT.json")
	writeFile(t, dir, "rules/realm.json", manifest(""))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "manifest")
	writeFile(t, dir, "rules/realm.json", manifest(`{"id": "REALM/001", "type": "rationale", "content": "Because.",`+
		` "added_by": "github:mallory", "added_at": "2026-01-01", "revision": 0}`))
	for _, p := range []string{filepath.Join(dir, "rules"), dir} {
		out, err := runDiff(t, p)
		assertExit(t, err, exitViolations)
		testutil.AssertContains(t, out, "RD023")
	}
}

// An entity file rewritten as a manifest, which the resolver would no longer
// read as rules, fails diff.
func TestDiffRefusesEntityFileRewrittenAsManifest(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	writeFile(t, dir, "rules/ENT.json", `{"realm": {"id": "dev.stray", "title": "t", "version": "1.0.0"}}`)
	_, err := runDiff(t, filepath.Join(dir, "rules"))
	assertExit(t, err, exitViolations)
}

// An entity file that is a link at the base, to a file in the tree, is
// compared as that file; one to elsewhere is absent, so removing it passes.
func TestDiffReadsBaseLinksInTheTree(t *testing.T) {
	t.Run("in the tree", func(t *testing.T) {
		// L.json leads to data/lnk.txt, which only the link makes an entity file.
		dir := newPublishedRepo(t, "rules/ENT.json")
		commitBlob(t, dir, "100644", "rules/data/lnk.txt", string(entityV2Promote("LNK")))
		commitBlob(t, dir, "120000", "rules/L.json", "data/lnk.txt")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
		gitRun(t, dir, "rm", "-q", "--cached", "rules/L.json")
		writeFile(t, dir, "rules/L.json", string(entityV1("LNK"))) // valid as new, a regression from the base
		out, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitViolations)
		testutil.AssertContains(t, out, "L.json")
	})
	t.Run("elsewhere", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json")
		commitBlob(t, dir, "120000", "rules/L.json", "../../outside.json")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
		out, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitOK)
		testutil.AssertNotContains(t, out, "RD005")
	})
	// A file replacing a link diff can't read can't be compared with what the
	// link held, so it is refused rather than checked as new.
	t.Run("replaced elsewhere link", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "vendor/LNK.json")
		commitBlob(t, dir, "120000", "rules/L.json", "../vendor/LNK.json")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
		gitRun(t, dir, "rm", "-q", "--cached", "rules/L.json")
		writeFile(t, dir, "rules/L.json", string(entityV1("LNK")))
		if _, err := runDiff(t, filepath.Join(dir, "rules")); !errors.Is(err, errDiffUnreadLink) {
			t.Errorf("err = %v, want errDiffUnreadLink", err)
		}
		// For one file the tree is the repository, so the link is read and
		// the file is compared with what it held.
		_, err := runDiff(t, filepath.Join(dir, "rules", "L.json"))
		assertExit(t, err, exitViolations)
	})
	// Deleting a link whose file is still there deletes no rules.
	t.Run("deleted link to a kept file", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json")
		commitBlob(t, dir, "120000", "rules/L.json", "ENT.json")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
		out, err := runDiff(t, filepath.Join(dir, "rules"))
		assertExit(t, err, exitOK)
		testutil.AssertNotContains(t, out, "RD005")
	})
}

// A compared path with ./ or a trailing separator at the repository root
// doesn't take the repository's own .git for a submodule.
func TestDiffRepositoryRootSpelledLoosely(t *testing.T) {
	dir := newPublishedRepo(t, "ENT.json")
	t.Chdir(dir)
	for _, p := range []string{".", "./", dir + string(filepath.Separator)} {
		if _, err := runDiff(t, p); err != nil {
			t.Errorf("diff %q: %v", p, err)
		}
	}
}

// A link is read only through real directories: one whose text passes
// through a linked directory is refused in head and unread at the base.
func TestDiffLinksThroughLinkedDirectories(t *testing.T) {
	for name, text := range map[string]string{"into it": "d/docs.txt", "out of it again": "d/../docs.txt"} {
		t.Run("head "+name, func(t *testing.T) {
			dir := newPublishedRepo(t, "rules/ENT.json", "rules/docs.txt", "rules/real/docs.txt")
			linkOrSkip(t, "real", filepath.Join(dir, "rules", "d"))
			linkOrSkip(t, text, filepath.Join(dir, "rules", "README.md"))
			// The directory link d is refused on its own too; README.md is
			// reached first or not, but the walk must fail either way.
			if _, err := runDiff(t, filepath.Join(dir, "rules")); !errors.Is(err, errDiffLink) {
				t.Errorf("err = %v, want errDiffLink", err)
			}
			if err := checkHeadLink(filepath.Join(dir, "rules", "README.md"), filepath.Join(dir, "rules")); !errors.Is(err, errDiffLink) {
				t.Errorf("checkHeadLink = %v, want errDiffLink", err)
			}
		})
	}
	t.Run("base", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "rules/real/LNK.json")
		commitBlob(t, dir, "120000", "rules/d", "real")
		commitBlob(t, dir, "120000", "rules/L.json", "d/LNK.json")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "links")
		gitRun(t, dir, "rm", "-q", "--cached", "rules/L.json", "rules/d")
		writeFile(t, dir, "rules/L.json", string(entityV1("LNK2")))
		if _, err := runDiff(t, filepath.Join(dir, "rules")); !errors.Is(err, errDiffUnreadLink) {
			t.Errorf("err = %v, want errDiffUnreadLink", err)
		}
	})
	// Through a link D, which a case-insensitive system would take d for, and
	// through a directory the base doesn't hold: unread.
	for name, c := range map[string]struct{ link, realDir string }{
		"another case":                    {link: "rules/D"},
		"another case beside a directory": {link: "rules/D", realDir: "rules/d/y.txt"},
		"no such directory":               {},
	} {
		t.Run("base "+name, func(t *testing.T) {
			dir := newPublishedRepo(t, "rules/ENT.json", "rules/real/sub/x.txt")
			if c.link != "" {
				commitBlob(t, dir, "120000", c.link, "real/sub")
			}
			if c.realDir != "" {
				commitBlob(t, dir, "100644", c.realDir, "y")
			}
			commitBlob(t, dir, "120000", "rules/L.json", "d/../ENT.json")
			gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "links")
			gitRun(t, dir, "rm", "-q", "--cached", "--ignore-unmatch", "rules/L.json", "rules/D", "rules/d/y.txt")
			writeFile(t, dir, "rules/L.json", string(entityV1("LNK2")))
			if _, err := runDiff(t, filepath.Join(dir, "rules")); !errors.Is(err, errDiffUnreadLink) {
				t.Errorf("err = %v, want errDiffUnreadLink", err)
			}
		})
	}
	// d/../ENT.json names ENT.json lexically, but through the linked d a
	// system would resolve it elsewhere: unread too.
	t.Run("base out of it again", func(t *testing.T) {
		dir := newPublishedRepo(t, "rules/ENT.json", "rules/real/sub/x.txt")
		commitBlob(t, dir, "120000", "rules/d", "real/sub")
		commitBlob(t, dir, "120000", "rules/L.json", "d/../ENT.json")
		gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "links")
		gitRun(t, dir, "rm", "-q", "--cached", "rules/L.json", "rules/d")
		writeFile(t, dir, "rules/L.json", string(entityV1("LNK2")))
		if _, err := runDiff(t, filepath.Join(dir, "rules")); !errors.Is(err, errDiffUnreadLink) {
			t.Errorf("err = %v, want errDiffUnreadLink", err)
		}
	})
}

// Deleting a base link is exempt from RD005 only when the file it led to is
// compared on both sides; a link to a file diff doesn't compare is RD005.
func TestDiffDeletedLinkToUncomparedFile(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	commitBlob(t, dir, "100644", "rules/data/lnk.txt", string(entityV2Promote("LNK")))
	commitBlob(t, dir, "120000", "rules/L.json", "data/lnk.txt")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
	gitRun(t, dir, "reset", "-q", "--hard")
	if err := os.Remove(filepath.Join(dir, "rules", "L.json")); err != nil {
		t.Fatal(err)
	}
	out, err := runDiff(t, filepath.Join(dir, "rules"))
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RD005")
}

// A repository whose root holds no .git entry (GIT_DIR) is still one: a link
// in it is refused.
func TestDiffRefusesLinkInRepositoryWithoutGitEntry(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json", "elsewhere/ENT.json")
	gitDir := filepath.Join(t.TempDir(), "git")
	if err := os.Rename(filepath.Join(dir, ".git"), gitDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", gitDir)
	t.Setenv("GIT_WORK_TREE", dir)
	if err := os.RemoveAll(filepath.Join(dir, "rules")); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, "elsewhere", filepath.Join(dir, "rules"))
	if _, err := runDiff(t, filepath.Join(dir, "rules")+string(filepath.Separator)); !errors.Is(err, errDiffLink) {
		t.Errorf("err = %v, want errDiffLink", err)
	}
}
