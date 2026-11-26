package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// baseRepo makes a git repository whose rules/ holds TST.json, written by
// setup, and returns rules/ and a commit function returning the new HEAD.
func baseRepo(t *testing.T, setup func(dir string)) (dir string, commit func(msg string) string) {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	dir = filepath.Join(repo, "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	setup(dir)
	commit = func(msg string) string {
		gitRun(t, repo, "add", "-A")
		gitRun(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", msg)
		return gitRun(t, repo, "rev-parse", "HEAD")
	}
	return dir, commit
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), args...)
}

func mustRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := run(t, args...); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func oneRule(dir string, t *testing.T) {
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "Rule", "github:alice"))
}

// A note added in the branch and revised before it merges: without --base
// it reaches revision 1, which diff refuses (RD015); with --base it stays 0.
func TestNoteUpdateBaseNewNote(t *testing.T) {
	for _, withBase := range []bool{false, true} {
		dir, commit := baseRepo(t, func(d string) { oneRule(d, t) })
		base := commit("base")
		addTestNote(t, dir, "TST-001", "rationale", "first")
		commit("add")

		args := []string{"note", "update", "TST-001/01", "--body", "reworded", "--dir", dir}
		if withBase {
			args = append(args, "--base", base)
		}
		mustRun(t, args...)

		out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base)
		if !withBase {
			testutil.AssertContains(t, out, "RD015")
			continue
		}
		if err != nil {
			t.Fatalf("diff: %v\n%s", err, out)
		}
		if n := loadEntityFile(t, dir, "TST").Notes[0]; n.Revision != 0 || n.Content != "reworded" {
			t.Errorf("note = %+v, want revision 0", n)
		}
		assertFileHashFresh(t, dir, "TST")
	}
}

// A note the base holds, revised twice in the branch, is one revision past
// the base, as diff wants (RD013).
func TestNoteUpdateBaseRevisedTwice(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) {
		oneRule(d, t)
		addTestNote(t, d, "TST-001", "rationale", "released")
	})
	base := commit("base")
	// The base's timestamp is now, to the second; the update's must differ
	// (RD009).
	time.Sleep(time.Second)
	for _, body := range []string{"one", "two"} {
		mustRun(t, "note", "update", "TST-001/01", "--body", body, "--dir", dir, "--base", base)
	}
	if out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if n := loadEntityFile(t, dir, "TST").Notes[0]; n.Revision != 1 {
		t.Errorf("revision %d, want 1", n.Revision)
	}
	// Back to the base's wording: the base revision.
	mustRun(t, "note", "update", "TST-001/01", "--body", "released", "--dir", dir, "--base", base)
	if n := loadEntityFile(t, dir, "TST").Notes[0]; n.Revision != 0 {
		t.Errorf("revision %d, want 0", n.Revision)
	}
}

// --base at a ref older than the real base can't hide the revision of a
// released note: diff against the real base still refuses it.
func TestNoteUpdateBaseOlderRefIsCaught(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) { oneRule(d, t) })
	old := commit("old")
	addTestNote(t, dir, "TST-001", "rationale", "released")
	base := commit("base")

	mustRun(t, "note", "update", "TST-001/01", "--body", "rewritten", "--dir", dir, "--base", old)
	out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base)
	if err == nil {
		t.Fatalf("diff passed a released note at revision 0:\n%s", out)
	}
	testutil.AssertContains(t, out, "RD013")
}

// A note of a sub-entity is found at the base, and the file's hashes stay
// fresh.
func TestNoteUpdateBaseSubEntity(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) {
		setupEntityFileWithSub(t, d, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "github:alice"))
	})
	base := commit("base")
	addTestNote(t, dir, "SUB-001", "rationale", "first")
	mustRun(t, "note", "update", "SUB-001/01", "--body", "reworded", "--dir", dir, "--base", base)

	if out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if n := loadEntityFile(t, dir, "TST").SubEntities[0].Notes[0]; n.Revision != 0 {
		t.Errorf("revision %d, want 0", n.Revision)
	}
	assertFileHashFresh(t, dir, "TST")
}

// A rule added in the branch and revised before it merges: RD004 without
// --base, revision 0 with it.
func TestRuleUpdateBaseNewRule(t *testing.T) {
	for _, withBase := range []bool{false, true} {
		dir, commit := baseRepo(t, func(d string) { oneRule(d, t) })
		base := commit("base")
		mustRun(t, "rule", "add", "TST", "--body", "MUST first", "--added-by", "github:alice", "--dir", dir)
		commit("add")

		args := []string{"rule", "update", "TST-002", "--body", "MUST reworded", "--dir", dir}
		if withBase {
			args = append(args, "--base", base)
		}
		mustRun(t, args...)

		out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base)
		if !withBase {
			testutil.AssertContains(t, out, "RD004")
			continue
		}
		if err != nil {
			t.Fatalf("diff: %v\n%s", err, out)
		}
		if r := findRule(loadEntityFile(t, dir, "TST"), "TST-002"); r == nil || r.Revision != 0 {
			t.Errorf("rule = %+v, want revision 0", r)
		}
		assertFileHashFresh(t, dir, "TST")
	}
}

// A draft rule the base holds is one revision past the base however often
// it is revised.
func TestRuleUpdateBaseRevisedTwice(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) { oneRule(d, t) })
	base := commit("base")
	for _, body := range []string{"MUST one", "MUST two"} {
		mustRun(t, "rule", "update", "TST-001", "--body", body, "--dir", dir, "--base", base)
	}
	if out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if r := findRule(loadEntityFile(t, dir, "TST"), "TST-001"); r.Revision != 1 {
		t.Errorf("revision %d, want 1", r.Revision)
	}
}

// An entity file the base doesn't hold is new: its notes and rules stay at
// revision 0.
func TestUpdateBaseNewFile(t *testing.T) {
	dir, commit := baseRepo(t, func(string) {})
	base := commit("empty")
	oneRule(dir, t)
	addTestNote(t, dir, "TST-001", "rationale", "first")
	mustRun(t, "note", "update", "TST-001/01", "--body", "reworded", "--dir", dir, "--base", base)
	mustRun(t, "rule", "update", "TST-001", "--body", "MUST reworded", "--dir", dir, "--base", base)
	f := loadEntityFile(t, dir, "TST")
	if f.Notes[0].Revision != 0 || f.Rules[0].Revision != 0 {
		t.Errorf("note %d, rule %d, want 0 and 0", f.Notes[0].Revision, f.Rules[0].Revision)
	}
}

// An unknown ref, or a directory outside any repository, is an error and
// leaves the file as it was.
func TestUpdateBaseErrors(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) {
		oneRule(d, t)
		addTestNote(t, d, "TST-001", "rationale", "r")
	})
	commit("base")
	before, err := os.ReadFile(filepath.Join(dir, "TST.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"note", "update", "TST-001/01", "--body", "x", "--dir", dir, "--base", "no-such-ref"},
		{"rule", "update", "TST-001", "--body", "MUST x", "--dir", dir, "--base", "no-such-ref"},
	} {
		if _, err := run(t, args...); err == nil {
			t.Errorf("%v passed", args)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, "TST.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a failed update changed the file")
	}

	outside := t.TempDir()
	oneRule(outside, t)
	if _, err := run(t, "rule", "update", "TST-001", "--body", "MUST x", "--dir", outside, "--base", "HEAD"); err == nil {
		t.Error("update outside a repository passed")
	}
}

// A rule of a sub-entity is found at the base.
func TestRuleUpdateBaseSubEntity(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) {
		setupEntityFileWithSub(t, d, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "github:alice"))
	})
	base := commit("base")
	for _, body := range []string{"MUST one", "MUST two"} {
		mustRun(t, "rule", "update", "SUB-001", "--body", body, "--dir", dir, "--base", base)
	}
	if out, err := runDiff(t, filepath.Join(dir, "TST.json"), "--base", base); err != nil {
		t.Fatalf("diff: %v\n%s", err, out)
	}
	if r := loadEntityFile(t, dir, "TST").SubEntities[0].Rules[0]; r.Revision != 1 {
		t.Errorf("revision %d, want 1", r.Revision)
	}
}

// An entity file that is a link at the base is refused, as diff refuses it.
func TestUpdateBaseRefusesBaseLink(t *testing.T) {
	dir, commit := baseRepo(t, func(d string) { oneRule(d, t) })
	repo := filepath.Dir(dir)
	commit("base")
	gitRun(t, repo, "rm", "-q", "--cached", "rules/TST.json")
	commitBlob(t, repo, "120000", "rules/TST.json", "elsewhere.json")
	gitRun(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "link")
	base := gitRun(t, repo, "rev-parse", "HEAD")
	_, err := run(t, "rule", "update", "TST-001", "--body", "MUST x", "--dir", dir, "--base", base)
	if !errors.Is(err, errDiffLink) {
		t.Errorf("err = %v, want errDiffLink", err)
	}
}
