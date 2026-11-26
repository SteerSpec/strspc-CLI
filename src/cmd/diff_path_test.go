package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// replaceWithDir removes dir/name, a link or an empty directory, and writes
// files under a new directory of that name.
func replaceWithDir(t *testing.T, dir, name string, files ...string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		writeFile(t, dir, name+"/"+f, string(entityV2Promote("ENT")))
	}
}

// A link or a submodule on the way to the compared path at the base is
// refused, as on the head side: what the base held there can't be listed,
// so nothing would be compared with it.
func TestDiffRefusesBaseLinkOnTheWay(t *testing.T) {
	for _, c := range []struct {
		name, mode string
		want       error
	}{
		{"link", "120000", errDiffLink},
		{"submodule", "160000", errDiffSubmodule},
	} {
		for _, target := range []string{"rules", "rules/sub", "rules/sub/ENT.json"} {
			t.Run(c.name+" "+target, func(t *testing.T) {
				dir := newPublishedRepo(t, "elsewhere/sub/ENT.json")
				content := "elsewhere"
				if c.mode == "160000" {
					content = ""
				}
				if c.mode == "160000" {
					sha := gitRun(t, dir, "rev-parse", "HEAD")
					gitRun(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+sha+",rules")
				} else {
					commitBlob(t, dir, c.mode, "rules", content)
				}
				gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
				gitRun(t, dir, "reset", "-q", "--hard")
				replaceWithDir(t, dir, "rules", "sub/ENT.json")

				_, err := runDiff(t, filepath.Join(dir, filepath.FromSlash(target)))
				if !errors.Is(err, c.want) {
					t.Errorf("err = %v, want %v", err, c.want)
				}
			})
		}
	}
}

// At the root of a work tree whose .git is a file (a linked worktree, or
// --separate-git-dir), diff compares every file.
func TestDiffAtRootWithGitFile(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "--separate-git-dir", filepath.Join(t.TempDir(), "git"))
	writeFile(t, dir, "rules/ENT.json", string(entityV2Promote("ENT")))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	if fi, err := os.Lstat(filepath.Join(dir, ".git")); err != nil || fi.IsDir() {
		t.Fatalf(".git is not a file (%v)", err)
	}
	if out, err := runDiff(t, dir); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if err := os.Remove(filepath.Join(dir, "rules", "ENT.json")); err != nil {
		t.Fatal(err)
	}
	out, _ := runDiff(t, dir)
	testutil.AssertContains(t, out, "RD005")
}

// The compared path is listed at the base as a path, not a pattern: a
// directory named rules* holds only its own files.
func TestDiffListsBaseLiterally(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("* can't be in a Windows file name")
	}
	dir := newPublishedRepo(t, "rules*/ENT.json", "rulesX/ENT2.json")
	if out, err := runDiff(t, filepath.Join(dir, "rules*")); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
}

// A JSON file named in another case is refused in single-file mode too.
func TestDiffFileRefusesJSONNameInAnotherCase(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	writeFile(t, dir, "rules/X.JSON", string(entityV1("X")))
	_, err := runDiff(t, filepath.Join(dir, "rules", "X.JSON"))
	if err == nil {
		t.Fatal("diff passed")
	}
	testutil.AssertContains(t, err.Error(), "must end in .json")
}

// Single-file mode reads a regular file only: a pipe could block the read.
func TestDiffFileRefusesPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no named pipes in the file system on Windows")
	}
	dir := newPublishedRepo(t, "rules/ENT.json")
	p := filepath.Join(dir, "rules", "PIPE.json")
	if out, err := exec.Command("mkfifo", p).CombinedOutput(); err != nil {
		t.Skipf("mkfifo: %v\n%s", err, out)
	}
	_, err := runDiff(t, p)
	if err == nil {
		t.Fatal("diff passed")
	}
	testutil.AssertContains(t, err.Error(), "is not a regular file")
}

// A base file named in another case, which the resolver refused, may be
// deleted: the resolver read nothing from it.
func TestDiffAllowsDeletingJSONNameInAnotherCase(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	writeFile(t, dir, "rules/X.JSON", string(entityV1("X")))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "x")
	if err := os.Remove(filepath.Join(dir, "rules", "X.JSON")); err != nil {
		t.Fatal(err)
	}
	if out, err := runDiff(t, filepath.Join(dir, "rules")); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
}

// junction makes link a directory junction to target: Windows only, and
// needing no privilege.
func junction(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows links")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v\n%s", err, out)
	}
}

// A junction on the way to the compared path is refused as a link is.
func TestDiffRefusesJunctionOnTheWay(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	writeFile(t, dir, "rules/app/ENT.json", string(entityV2Promote("ENT")))
	writeFile(t, dir, "rules/lib/ENT.json", string(entityV2InvalidEdit("ENT")))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	if err := os.RemoveAll(filepath.Join(dir, "rules", "app")); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dir, "rules", "lib"), filepath.Join(dir, "rules", "app"))
	_, err := runDiff(t, filepath.Join(dir, "rules", "app"))
	if !errors.Is(err, errDiffLink) {
		t.Errorf("err = %v, want errDiffLink", err)
	}
}

// A junction in the compared tree is not descended into: nothing behind it
// is read.
func TestDiffLeavesJunctionInTreeAlone(t *testing.T) {
	dir := newPublishedRepo(t, "rules/ENT.json")
	outside := t.TempDir()
	writeFile(t, outside, "BAD.json", "not json")
	junction(t, outside, filepath.Join(dir, "rules", "sub"))
	if out, err := runDiff(t, filepath.Join(dir, "rules")); err != nil {
		t.Fatalf("diff read through the junction: %v\n%s", err, out)
	}
}
