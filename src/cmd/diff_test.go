package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// entityV1 returns a minimal entity JSON with a single Draft rule.
func entityV1(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "Initial rule body.",
      "state": "D",
      "revision": 0,
      "added_by": "github:alice"
    }
  ],
  "notes": []
}`)
}

// entityV2Draft returns entityV1 with an edited draft body and bumped revision/version/timestamp.
func entityV2Draft(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "Edited rule body.",
      "state": "D",
      "revision": 1,
      "added_by": "github:alice"
    }
  ],
  "notes": []
}`)
}

// entityV2Promote returns entityV1 with rule promoted to P and bumped metadata.
func entityV2Promote(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "Initial rule body.",
      "state": "P",
      "revision": 0,
      "added_by": "github:alice"
    }
  ],
  "notes": []
}`)
}

// entityV2InvalidEdit returns entityV1 with body edited on a Published rule (violates RD002).
func entityV2InvalidEdit(euid string) []byte {
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {
      "id": "` + euid + `-001",
      "body": "EDITED after published — not allowed.",
      "state": "P",
      "revision": 0,
      "added_by": "github:alice"
    }
  ],
  "notes": []
}`)
}

// makeGitRepo creates a temporary git repo, writes initialFile to filename, commits, then
// overwrites filename with headFile (working-tree version). Returns the repo directory.
func makeGitRepo(t *testing.T, filename string, initialFile, headFile []byte) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, initialFile, 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", filename)
	run("git", "commit", "-m", "initial")

	// Write head version to working tree (not committed).
	if err := os.WriteFile(path, headFile, 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

// makeGitRepoDir creates a temporary git repo with multiple entity files committed,
// then writes working-tree versions. baseFiles maps filename→content at HEAD;
// headFiles maps filename→working-tree content (nil means "delete from working tree").
func makeGitRepoDir(t *testing.T, baseFiles, headFiles map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	for name, data := range baseFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")

	// Write working-tree versions. A nil value deletes the file (simulates deletion).
	for name, data := range headFiles {
		path := filepath.Join(dir, name)
		if data == nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestDiffValidDraftEdit(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err != nil {
		t.Errorf("expected no error for valid draft edit, got: %v", err)
	}
}

func TestDiffValidPromotion(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Promote("ENT"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err != nil {
		t.Errorf("expected no error for valid promotion D→P, got: %v", err)
	}
}

func TestDiffInvalidBodyEdit(t *testing.T) {
	// First promote entity to P, then try editing body (RD002).
	promoted := entityV2Promote("ENT")
	dir := makeGitRepo(t, "ENT.json", promoted, entityV2InvalidEdit("ENT"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err == nil {
		t.Fatal("expected error for illegal body edit after publish, got nil")
	}
	testutil.AssertContains(t, err.Error(), "diff found")
}

func TestDiffNewFile(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")
	// Commit an unrelated file so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "README")
	run("git", "commit", "-m", "init")

	// New entity file — not in HEAD.
	entityPath := filepath.Join(dir, "NEW.json")
	if err := os.WriteFile(entityPath, entityV1("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", entityPath)
	if err != nil {
		t.Errorf("expected no error for valid new entity, got: %v", err)
	}
}

func TestDiffDirMode(t *testing.T) {
	base := map[string][]byte{
		"ENT.json": entityV1("ENT"),
		"OTH.json": entityV1("OTH"),
	}
	head := map[string][]byte{
		"ENT.json": entityV2Draft("ENT"),
		"OTH.json": entityV1("OTH"), // unchanged
	}
	dir := makeGitRepoDir(t, base, head)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	if err != nil {
		t.Errorf("expected no error for valid dir diff, got: %v", err)
	}
}

func TestDiffDirModeInvalid(t *testing.T) {
	promoted := entityV2Promote("ENT")
	base := map[string][]byte{"ENT.json": promoted}
	head := map[string][]byte{"ENT.json": entityV2InvalidEdit("ENT")}
	dir := makeGitRepoDir(t, base, head)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	if err == nil {
		t.Fatal("expected error for invalid dir diff, got nil")
	}
	testutil.AssertContains(t, err.Error(), "diff found")
}

func TestDiffJSONOutput(t *testing.T) {
	promoted := entityV2Promote("ENT")
	dir := makeGitRepo(t, "ENT.json", promoted, entityV2InvalidEdit("ENT"))

	output, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--json")
	if err == nil {
		t.Fatal("expected error for invalid transition in JSON mode, got nil")
	}

	var diags []result.Diagnostic
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &diags); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, output)
	}
	if len(diags) == 0 {
		t.Error("expected at least one diagnostic in JSON output")
	}
}

func TestDiffStrictMode(t *testing.T) {
	// RD006 fires as Warning when a superseding rule reaches I without the superseded being R.
	// Build an entity where a superseding rule is promoted to I while superseded stays P.
	base := []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "ENT", "title": "Test"},
  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z"},
  "rules": [
    {"id": "ENT-001", "body": "Old rule.", "state": "P", "revision": 0, "added_by": "github:alice"},
    {"id": "ENT-002", "body": "New rule.", "state": "P", "revision": 0, "added_by": "github:bob", "supersedes": "ENT-001"}
  ],
  "notes": []
}`)
	head := []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "ENT", "title": "Test"},
  "rule_set": {"version": "0.2.0", "timestamp": "` + time.Now().UTC().Format(time.RFC3339) + `"},
  "rules": [
    {"id": "ENT-001", "body": "Old rule.", "state": "P", "revision": 0, "added_by": "github:alice"},
    {"id": "ENT-002", "body": "New rule.", "state": "I", "revision": 0, "added_by": "github:bob", "supersedes": "ENT-001"}
  ],
  "notes": []
}`)
	dir := makeGitRepo(t, "ENT.json", base, head)

	// Without --strict: RD006 is a warning, command succeeds.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	if err != nil {
		t.Errorf("expected no error without --strict (RD006 is warning), got: %v", err)
	}

	// With --strict: RD006 warning → error, command fails.
	_, err = testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--strict")
	if err == nil {
		t.Error("expected error with --strict (RD006 warning promoted to error), got nil")
	}
}

func TestDiffBaseFlag(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) string {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}

	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")

	// Commit v1.
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), entityV1("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "ENT.json")
	run("git", "commit", "-m", "v1")
	v1SHA := run("git", "rev-parse", "HEAD")

	// Commit v2 (promoted).
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), entityV2Promote("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "ENT.json")
	run("git", "commit", "-m", "v2")

	// Working tree: further valid edit.
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), entityV2Promote("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Diff against v1SHA (earlier commit): should still pass.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--base", v1SHA)
	if err != nil {
		t.Errorf("expected no error diffing against explicit SHA, got: %v", err)
	}
}

func TestDiffFileNotExist(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", "/no/such/path/ENT.json")
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
	testutil.AssertContains(t, err.Error(), "does not exist")
}

// fakeGH puts a gh on PATH that records its arguments and answers with out
// and exit code code. It is a shell script, so the tests that use it skip on
// Windows.
func fakeGH(t *testing.T, out string, code int) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake gh is a shell script")
	}
	bin := t.TempDir()
	argsFile = filepath.Join(bin, "args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\nprintf '%s' \"$FAKE_GH_OUT\"\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GH_OUT", out)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

func TestDiffPRResolvesBaseWithGH(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
	argsFile := fakeGH(t, gitRun(t, dir, "rev-parse", "HEAD")+"\n", 0)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--pr", "7")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "No errors or warnings found")
	args, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	testutil.AssertContains(t, string(args), "pr view 7 --json baseRefOid --jq .baseRefOid")
}

func TestDiffPRErrors(t *testing.T) {
	tests := []struct {
		name string
		out  string
		code int
		want string
	}{
		{"gh fails with a message", "no pull requests found", 1, "requires gh CLI): no pull requests found"},
		{"gh fails silently", "", 1, "requires gh CLI): exit status 1"},
		{"gh answers nothing", "", 0, "gh returned empty base SHA for PR #7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
			fakeGH(t, tt.out, tt.code)
			_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--pr", "7")
			assertExit(t, err, exitViolations)
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestDiffNotAGitRepository(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ENT.json")
	if err := os.WriteFile(p, entityV1("ENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", p)
	if err == nil {
		t.Fatal("expected an error outside a git repository")
	}
	testutil.AssertContains(t, err.Error(), "not a git repository")
}

func TestDiffPathBelowAFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path below a file as not existing")
	}
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV1("ENT"))
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json", "x"))
	if err == nil {
		t.Fatal("expected an error for a path below a file")
	}
	testutil.AssertContains(t, err.Error(), "accessing")
}

func TestDiffJSONClean(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
	out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--json")
	assertExit(t, err, exitOK)
	var diags []result.Diagnostic
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &diags); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(diags) != 0 {
		t.Errorf("want no diagnostics, got %v", diags)
	}
}

func TestDiffJSONWriteError(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
	root := NewRootCmd()
	root.SetOut(failingWriter{})
	root.SetErr(failingWriter{})
	root.SetArgs([]string{"diff", filepath.Join(dir, "ENT.json"), "--json"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected the write error")
	}
	testutil.AssertContains(t, err.Error(), "broken pipe")
}

func TestDiffInvalidBaseRef(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--base", "nosuchref")
	if err == nil {
		t.Fatal("expected an error for an unknown base ref")
	}
	// gitShowError carries git's own message.
	testutil.AssertContains(t, err.Error(), "git show nosuchref:")
	testutil.AssertContains(t, err.Error(), "nosuchref")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "diff", dir, "--base", "nosuchref")
	if err == nil {
		t.Fatal("expected an error for an unknown base ref in directory mode")
	}
	testutil.AssertContains(t, err.Error(), "walking "+dir)
}

func TestDiffDirInvalidBaseRefWithoutEntities(t *testing.T) {
	dir := makeGitRepo(t, "README", []byte("x"), []byte("x"))
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir, "--base", "nosuchref")
	if err == nil {
		t.Fatal("expected an error for an unknown base ref")
	}
	testutil.AssertContains(t, err.Error(), `invalid git ref "nosuchref"`)
}

func TestDiffMalformedRealmManifest(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(dir, "ENT.json"), dir} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", target)
		if err == nil {
			t.Fatalf("diff %s: expected an error for a malformed realm.json", target)
		}
		testutil.AssertContains(t, err.Error(), "reading the contributors of the realm")
	}
}

// A file that doesn't parse is reported against the file itself.
func TestDiffUnparsableFile(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), []byte("{"))
	out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"))
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, filepath.Join(dir, "ENT.json"))

	writeFile(t, dir, "NEW.json", "{")
	out, err = testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, filepath.Join(dir, "NEW.json"))
}

func TestDiffDirDeletedFile(t *testing.T) {
	base := map[string][]byte{"ENT.json": entityV1("ENT"), "OTH.json": entityV1("OTH")}
	head := map[string][]byte{"OTH.json": nil}
	dir := makeGitRepoDir(t, base, head)
	out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RD005")
	testutil.AssertContains(t, out, "entity file deleted: OTH.json")
}

func TestDiffDirNoEntityFiles(t *testing.T) {
	dir := makeGitRepo(t, "README", []byte("x"), []byte("x"))
	out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RD000")
	testutil.AssertContains(t, out, "no entity JSON files found")
}

// A sub-directory realm: committed paths are listed relative to it, and
// _-prefixed directories (schemas) are skipped on both sides.
func TestDiffDirSubdirectorySkipsUnderscoreDirs(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	writeFile(t, dir, "rules/ENT.json", string(entityV1("ENT")))
	writeFile(t, dir, "rules/_schema/x.json", "{")
	writeFile(t, dir, "rules/notes.txt", "x")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init")
	writeFile(t, dir, "rules/ENT.json", string(entityV2Draft("ENT")))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "rules"))
	assertExit(t, err, exitOK)
	testutil.AssertNotContains(t, out, "RD005")
	testutil.AssertNotContains(t, out, "_schema")

	files, err := gitListRelPaths("HEAD", filepath.Join(dir, "rules"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "ENT.json" {
		t.Errorf("gitListRelPaths = %v, want [ENT.json]", files)
	}
}

func TestGitListRelPaths(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV1("ENT"))

	// A directory absent at the ref: everything is new.
	if err := os.MkdirAll(filepath.Join(dir, "fresh"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := gitListRelPaths("HEAD", filepath.Join(dir, "fresh"), dir)
	if err != nil || files != nil {
		t.Errorf("absent directory: %v, %v; want nil, nil", files, err)
	}

	// git can't run in the repository: treated as absent, not as a bad ref.
	files, err = gitListRelPaths("HEAD", dir, filepath.Join(dir, "missing"))
	if err != nil || files != nil {
		t.Errorf("missing repository: %v, %v; want nil, nil", files, err)
	}
}

func TestRealAbsMissingParent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing", "ENT.json")
	got, err := realAbs(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != p {
		t.Errorf("realAbs(%q) = %q, want the path unchanged", p, got)
	}
}

func TestIsGitNotFound(t *testing.T) {
	if isGitNotFound(errors.New("plain")) {
		t.Error("a plain error is not a git not-found")
	}
	if isGitNotFound(&gitShowError{ref: "HEAD", path: "x", cause: errors.New("no exit")}) {
		t.Error("a git-show error without an exit status is not a not-found")
	}
	e := &gitShowError{ref: "HEAD", path: "x", cause: errors.New("boom")}
	if got := e.Error(); got != "git show HEAD:x: boom" {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(e, e.cause) {
		t.Error("gitShowError must unwrap to its cause")
	}
}

func TestDiffUnreadableFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file the current user cannot read")
	}
	dir := makeGitRepo(t, "ENT.json", entityV1("ENT"), entityV2Draft("ENT"))
	p := filepath.Join(dir, "ENT.json")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	for _, target := range []string{p, dir} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", target)
		if err == nil || !strings.Contains(err.Error(), "reading "+p) {
			t.Errorf("diff %s: got %v, want a read error", target, err)
		}
	}

	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, err := testutil.ExecuteCommand(NewRootCmd(), "diff", dir)
	if err == nil || !strings.Contains(err.Error(), "walking "+dir) {
		t.Errorf("unreadable directory: got %v, want a walk error", err)
	}
}
