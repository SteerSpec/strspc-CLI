package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestResolveDiffSourcePrecedence(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	noEnv := env(nil)
	ghEnv := env(map[string]string{"GITHUB_BASE_REF": "main"})

	tests := []struct {
		name   string
		flags  diffFlags
		getenv func(string) string
		want   diffSource
	}{
		{"diff-file beats everything", diffFlags{DiffFile: "x.diff", Staged: true, PR: 7, Base: "dev"}, ghEnv, diffSource{Kind: diffFromFile, Ref: "x.diff"}},
		{"staged beats pr", diffFlags{Staged: true, PR: 7, Base: "dev"}, ghEnv, diffSource{Kind: diffFromStaged}},
		{"pr beats base", diffFlags{PR: 7, Base: "dev"}, ghEnv, diffSource{Kind: diffFromPR, Ref: "7"}},
		{"base beats GITHUB_BASE_REF", diffFlags{Base: "dev"}, ghEnv, diffSource{Kind: diffFromBase, Ref: "dev"}},
		{"GITHUB_BASE_REF beats HEAD", diffFlags{}, ghEnv, diffSource{Kind: diffFromGitHubBase, Ref: "main"}},
		{"HEAD by default", diffFlags{}, noEnv, diffSource{Kind: diffFromHead}},
		{"blank GITHUB_BASE_REF is ignored", diffFlags{}, env(map[string]string{"GITHUB_BASE_REF": "  "}), diffSource{Kind: diffFromHead}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveDiffSource(tc.flags, tc.getenv); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGitDiffArgs(t *testing.T) {
	got := strings.Join(gitDiffArgs(".", "HEAD"), " ")
	want := "diff --no-color --no-ext-diff -U3 --diff-filter=ACMR HEAD -- . :(exclude,literal).strspc :(exclude,literal)rules"
	if got != want {
		t.Errorf("root args:\n got %s\nwant %s", got, want)
	}
	got = strings.Join(gitDiffArgs("svc/api", "--cached"), " ")
	want = "diff --no-color --no-ext-diff -U3 --diff-filter=ACMR --cached -- :(literal)svc/api :(exclude,literal)svc/api/.strspc :(exclude,literal)svc/api/rules"
	if got != want {
		t.Errorf("subdir args:\n got %s\nwant %s", got, want)
	}
}

func TestCollectDiffLiteralProjectName(t *testing.T) {
	dir := newDiffRepo(t)
	writeFile(t, dir, "svc[1]/x.go", "package svc\n")
	writeFile(t, dir, "svc[1]/.strspc/config.yaml", "rules: []\n")
	writeFile(t, dir, "svc[1]/rules/R.json", "{}\n")
	writeFile(t, dir, "svc1/y.go", "package sibling\n")
	writeFile(t, dir, "svc1/.strspc/config.yaml", "rules: []\n")
	gitRun(t, dir, "add", "-A")
	diff, err := collectDiff(context.Background(), dir, "svc[1]", diffSource{Kind: diffFromStaged})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, diff, "svc[1]/x.go")
}

// gitRun runs git in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newDiffRepo builds a repository with:
//
//	main:    a.go (v1), rules/R.json, .strspc/config.yaml
//	feature: a.go (v2) committed, then main moves on with main-only.go,
//	         working tree on feature edits b.go (unstaged) and stages c.go.
func newDiffRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "rules/R.json", "{}\n")
	writeFile(t, dir, ".strspc/config.yaml", "rules: []\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")

	gitRun(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	gitRun(t, dir, "commit", "-q", "-am", "feature: a")

	gitRun(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, "main-only.go", "package a\n\nfunc M() {}\n")
	gitRun(t, dir, "add", "main-only.go")
	gitRun(t, dir, "commit", "-q", "-m", "main moves on")
	gitRun(t, dir, "checkout", "-q", "feature")

	// Working tree: unstaged b.go edit (tracked), staged c.go, and changes
	// under the excluded directories.
	writeFile(t, dir, "b.go", "package a\n")
	gitRun(t, dir, "add", "b.go")
	gitRun(t, dir, "commit", "-q", "-m", "feature: b")
	writeFile(t, dir, "b.go", "package a\n\nfunc B() {}\n")
	writeFile(t, dir, "c.go", "package a\n\nfunc C() {}\n")
	gitRun(t, dir, "add", "c.go")
	writeFile(t, dir, "rules/R.json", "{\"changed\": true}\n")
	writeFile(t, dir, ".strspc/config.yaml", "rules: [x]\n")
	return dir
}

func files(diff string) []string {
	var out []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git a/") {
			out = append(out, strings.TrimPrefix(strings.Fields(line)[2], "a/"))
		}
	}
	return out
}

func assertFiles(t *testing.T, diff string, want ...string) {
	t.Helper()
	got := files(diff)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("diff files = %v, want %v\n%s", got, want, diff)
	}
}

func TestCollectDiffHead(t *testing.T) {
	dir := newDiffRepo(t)
	diff, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromHead})
	if err != nil {
		t.Fatal(err)
	}
	// Unstaged b.go and staged c.go; excludes hide rules/ and .strspc/.
	assertFiles(t, diff, "b.go", "c.go")
	if !strings.Contains(diff, "+func B() {}") {
		t.Errorf("expected unified diff body, got:\n%s", diff)
	}
}

func TestCollectDiffStaged(t *testing.T) {
	dir := newDiffRepo(t)
	diff, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromStaged})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, diff, "c.go")
}

func TestCollectDiffBaseUsesMergeBase(t *testing.T) {
	dir := newDiffRepo(t)
	diff, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromBase, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	// Everything the feature branch changed since it forked, plus the
	// working tree; main-only.go (added on main afterwards) must not appear.
	assertFiles(t, diff, "a.go", "b.go", "c.go")
}

func TestCollectDiffBaseInvalidRef(t *testing.T) {
	dir := newDiffRepo(t)
	_, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromBase, Ref: "nope"})
	if err == nil || !strings.Contains(err.Error(), `invalid base ref "nope"`) {
		t.Fatalf("expected invalid ref error, got %v", err)
	}
}

func TestCollectDiffPRUsesStubbedBase(t *testing.T) {
	dir := newDiffRepo(t)
	orig := resolvePRBase
	t.Cleanup(func() { resolvePRBase = orig })
	var gotPR int
	resolvePRBase = func(pr int, repoDir string) (string, error) {
		gotPR = pr
		return gitRun(t, repoDir, "rev-parse", "main"), nil
	}

	diff, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromPR, Ref: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPR != 42 {
		t.Errorf("resolvePRBase called with %d, want 42", gotPR)
	}
	assertFiles(t, diff, "a.go", "b.go", "c.go")
}

func TestCollectDiffGitHubBaseRefFetchesWhenMissing(t *testing.T) {
	upstream := newDiffRepo(t)
	// A clone of the feature branch only, with origin pointing at upstream
	// but no origin/main tracking ref (like a single-ref CI checkout).
	clone := t.TempDir()
	gitRun(t, clone, "init", "-q", "-b", "feature")
	gitRun(t, clone, "remote", "add", "origin", upstream)
	gitRun(t, clone, "fetch", "-q", "--no-tags", "origin", "+refs/heads/feature:refs/remotes/origin/feature")
	gitRun(t, clone, "checkout", "-q", "feature")
	writeFile(t, clone, "b.go", "package a\n\nfunc B2() {}\n")
	if _, err := git(context.Background(), clone, "rev-parse", "--verify", "--quiet", "origin/main^{commit}"); err == nil {
		t.Fatal("precondition: origin/main must be absent before the check")
	}

	diff, err := collectDiff(context.Background(), clone, ".", diffSource{Kind: diffFromGitHubBase, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, diff, "a.go", "b.go")
	if _, err := git(context.Background(), clone, "rev-parse", "--verify", "--quiet", "origin/main^{commit}"); err != nil {
		t.Errorf("origin/main should have been fetched: %v", err)
	}
}

func TestCollectDiffFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.diff")
	if err := os.WriteFile(p, []byte(checkDiff), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := collectDiff(context.Background(), "", ".", diffSource{Kind: diffFromFile, Ref: p})
	if err != nil {
		t.Fatal(err)
	}
	if diff != checkDiff {
		t.Errorf("diff file content not returned verbatim")
	}
	if _, err := collectDiff(context.Background(), "", ".", diffSource{Kind: diffFromFile, Ref: filepath.Join(dir, "missing")}); err == nil {
		t.Error("missing diff file must error")
	}
}

func TestCollectDiffSubdirRestrictsAndExcludes(t *testing.T) {
	dir := newDiffRepo(t)
	writeFile(t, dir, "svc/x.go", "package svc\n")
	writeFile(t, dir, "svc/rules/R.json", "{}\n")
	gitRun(t, dir, "add", "svc")
	diff, err := collectDiff(context.Background(), dir, "svc", diffSource{Kind: diffFromStaged})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, diff, "svc/x.go")
}

func TestRepoRelative(t *testing.T) {
	dir := newDiffRepo(t)
	if rel, err := repoRelative(dir, dir); err != nil || rel != "." {
		t.Errorf("same dir: %q, %v", rel, err)
	}
	sub := filepath.Join(dir, "svc", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if rel, err := repoRelative(dir, sub); err != nil || rel != "svc/api" {
		t.Errorf("subdir: %q, %v", rel, err)
	}
	if _, err := repoRelative(dir, t.TempDir()); err == nil {
		t.Error("outside dir must error")
	}
	dotdot := filepath.Join(dir, "..service")
	if err := os.MkdirAll(dotdot, 0o755); err != nil {
		t.Fatal(err)
	}
	if rel, err := repoRelative(dir, dotdot); err != nil || rel != "..service" {
		t.Errorf("..service dir: %q, %v (must not be mistaken for a parent path)", rel, err)
	}
}

func TestFilterDiff(t *testing.T) {
	section := func(p string) string {
		return "diff --git a/" + p + " b/" + p + "\n--- a/" + p + "\n+++ b/" + p + "\n@@ -1 +1 @@\n-a\n+b\n"
	}
	quoted := "diff --git \"a/sp ace/f\\303\\251.go\" \"b/sp ace/f\\303\\251.go\"\n--- \"a/sp ace/f\\303\\251.go\"\n+++ \"b/sp ace/f\\303\\251.go\"\n@@ -1 +1 @@\n-a\n+b\n"
	rulesQuoted := "diff --git \"a/rules/w x.json\" \"b/rules/w x.json\"\n--- \"a/rules/w x.json\"\n+++ \"b/rules/w x.json\"\n@@ -1 +1 @@\n-a\n+b\n"
	// Header-only (binary) sections fall back to the quoted header itself.
	rulesQuotedBinary := "diff --git \"a/rules/w\\ty.bin\" \"b/rules/w\\ty.bin\"\nBinary files differ\n"

	t.Run("root project drops excludes, keeps the rest intact", func(t *testing.T) {
		in := section("src/a.go") + section(".strspc/config.yaml") + section("rules/R.json") + quoted + rulesQuoted + rulesQuotedBinary + section("svc/rules/R.json")
		got := filterDiff(in, ".")
		want := section("src/a.go") + quoted + section("svc/rules/R.json")
		if got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("sub-project drops outside paths and both exclude forms", func(t *testing.T) {
		in := section("svc/a.go") + section("other/b.go") + section("svc/rules/R.json") + section("rules/R.json") + section(".strspc/x")
		if got, want := filterDiff(in, "svc"), section("svc/a.go"); got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("file name containing b/ cannot escape the exclusion", func(t *testing.T) {
		// A file literally named "rules/evil b/src/ok.go": the header's last
		// " b/" points at src/ok.go, the midpoint split and +++ line do not.
		tricky := "diff --git a/rules/evil b/src/ok.go b/rules/evil b/src/ok.go\n--- a/rules/evil b/src/ok.go\n+++ b/rules/evil b/src/ok.go\n@@ -1 +1 @@\n-a\n+b\n"
		if got := filterDiff(tricky+section("src/a.go"), "."); got != section("src/a.go") {
			t.Errorf("tricky name was evaluated:\n%s", got)
		}
		noPlus := "diff --git a/rules/evil b/src/ok.go b/rules/evil b/src/ok.go\nindex 1..2 100644\n"
		if got := filterDiff(noPlus, "."); got != "" {
			t.Errorf("midpoint split failed to exclude:\n%s", got)
		}
		disagree := "diff --git a/rules/x.json b/src/ok.go\nindex 1..2 100644\n"
		if got := filterDiff(disagree, "."); got != disagree {
			t.Errorf("ambiguous header must be kept, got %q", got)
		}
	})
	t.Run("embedded quotes and spaces follow git quoting rules", func(t *testing.T) {
		// .strspc/a"b.yaml is quoted by git with an escaped quote.
		escaped := "diff --git \"a/.strspc/a\\\"b.yaml\" \"b/.strspc/a\\\"b.yaml\"\n--- \"a/.strspc/a\\\"b.yaml\"\n+++ \"b/.strspc/a\\\"b.yaml\"\n@@ -1 +1 @@\n-a\n+b\n"
		if got := filterDiff(escaped, "."); got != "" {
			t.Errorf("escaped-quote config path was evaluated:\n%s", got)
		}
		escapedHeaderOnly := "diff --git \"a/.strspc/a\\\"b.yaml\" \"b/.strspc/a\\\"b.yaml\"\nBinary files differ\n"
		if got := filterDiff(escapedHeaderOnly, "."); got != "" {
			t.Errorf("escaped-quote header-only section was evaluated:\n%s", got)
		}
		// svc/a b/c.go is a real change inside the sub-project; spaces are not quoted by git.
		spaced := section("svc/a b/c.go")
		if got := filterDiff(spaced+section("other/x.go"), "svc"); got != spaced {
			t.Errorf("space-containing path inside the project was dropped:\n%s", got)
		}
		spacedHeaderOnly := "diff --git a/svc/a b/c.go b/svc/a b/c.go\nBinary files differ\n"
		if got := filterDiff(spacedHeaderOnly, "svc"); got != spacedHeaderOnly {
			t.Errorf("space-containing header-only section was dropped:\n%s", got)
		}
		// .strspc/a b/config.yaml must keep its excluded prefix.
		if got := filterDiff(section(".strspc/a b/config.yaml"), "."); got != "" {
			t.Errorf("space-containing config path was evaluated:\n%s", got)
		}
	})
	t.Run("deletions are dropped like --diff-filter=ACMR", func(t *testing.T) {
		del := func(p string) string {
			return "diff --git a/" + p + " b/" + p + "\ndeleted file mode 100644\nindex 1..0\n--- a/" + p + "\n+++ /dev/null\n@@ -1 +0,0 @@\n-secret\n"
		}
		in := del("rules/R.json") + del(".strspc/config.yaml") + del("other/x.go") + del("src/gone.go") + section("src/a.go")
		if got := filterDiff(in, "."); got != section("src/a.go") {
			t.Errorf("deleted sections reached the diff:\n%s", got)
		}
		if got := filterDiff(del("svc/x.go")+section("svc/a.go"), "svc"); got != section("svc/a.go") {
			t.Errorf("deletion inside the project kept:\n%s", got)
		}
	})
	t.Run("unparseable header is kept", func(t *testing.T) {
		in := "garbage\n" + section("rules/R.json")
		if got := filterDiff(in, "."); got != "garbage\n" {
			t.Errorf("got %q", got)
		}
		if got := filterDiff("", "."); got != "" {
			t.Errorf("empty diff changed: %q", got)
		}
	})
}

func TestCheckUsesGitWhenNoDiffFile(t *testing.T) {
	// End-to-end: a project inside a git repo, --base main, stubbed provider.
	dir := newDiffRepo(t)
	rulesDir := filepath.Join(dir, "spec")
	writeFile(t, dir, "spec/TST.json", checkEntityJSON)
	writeCheckConfig(t, dir, rulesDir, "claude", []string{"I"}, nil)
	p := &stubProvider{}
	stubEvalProvider(t, p, nil)

	output, err := runCheckCmd(t, dir, "--base", "main", "--no-cache", "--verbose")
	assertExit(t, err, exitOK)
	if p.callCount() == 0 {
		t.Fatal("provider not called: diff was probably empty")
	}
	if !strings.Contains(output, "diff: base main vs working tree") {
		t.Errorf("verbose output should describe the diff source, got:\n%s", output)
	}
}

func TestCheckNotAGitRepo(t *testing.T) {
	dir, _ := setupCheckProject(t, "claude", []string{"I"})
	stubEvalProvider(t, &stubProvider{}, nil)
	_, err := runCheckCmd(t, dir, "--no-cache")
	assertExit(t, err, exitConfig)
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIsRootedPath(t *testing.T) {
	rooted := []string{"/etc", `\etc`}
	if runtime.GOOS == "windows" {
		rooted = append(rooted, `C:\etc`, "C:etc")
	}
	for _, p := range rooted {
		if !isRootedPath(p) {
			t.Errorf("isRootedPath(%q) = false", p)
		}
	}
	for _, p := range []string{"rules", "../dep", "src/x.go"} {
		if isRootedPath(p) {
			t.Errorf("isRootedPath(%q) = true", p)
		}
	}
}

func TestGitHubAnnotationSkipsNonWorkspacePaths(t *testing.T) {
	d := result.Diagnostic{Module: "rule-resolve", Code: "RSV007", Severity: result.Error, Message: "fetching: 404", Path: "github://o/r@v1/rules"}
	got := githubAnnotation(&d)
	if strings.Contains(got, "file=") {
		t.Errorf("URI path must not become file=: %s", got)
	}
	d.Path = "/abs/path/x.json"
	if strings.Contains(githubAnnotation(&d), "file=") {
		t.Error("absolute path must not become file=")
	}
	d.Path, d.Line = "src/x.go", 7
	want := "::error file=src/x.go,line=7,title=rule-resolve RSV007::fetching: 404"
	if got := githubAnnotation(&d); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestDiffSourceString(t *testing.T) {
	tests := []struct {
		src  diffSource
		want string
	}{
		{diffSource{Kind: diffFromFile, Ref: "x.diff"}, "diff file x.diff"},
		{diffSource{Kind: diffFromStaged}, "staged changes (index vs HEAD)"},
		{diffSource{Kind: diffFromPR, Ref: "7"}, "PR #7 base vs working tree"},
		{diffSource{Kind: diffFromBase, Ref: "main"}, "base main vs working tree"},
		{diffSource{Kind: diffFromGitHubBase, Ref: "main"}, "GITHUB_BASE_REF origin/main vs working tree"},
		{diffSource{Kind: diffFromHead}, "working tree vs HEAD"},
	}
	for _, tt := range tests {
		if got := tt.src.String(); got != tt.want {
			t.Errorf("%s: String() = %q, want %q", tt.src.Kind, got, tt.want)
		}
	}
}

func TestCollectDiffPRErrors(t *testing.T) {
	dir := newDiffRepo(t)
	if _, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromPR, Ref: "x"}); err == nil ||
		!strings.Contains(err.Error(), `invalid PR number "x"`) {
		t.Errorf("non-numeric PR: got %v", err)
	}

	orig := resolvePRBase
	t.Cleanup(func() { resolvePRBase = orig })
	resolvePRBase = func(int, string) (string, error) { return "", errors.New("gh is missing") }
	if _, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromPR, Ref: "7"}); err == nil ||
		!strings.Contains(err.Error(), "gh is missing") {
		t.Errorf("failing gh: got %v", err)
	}
}

func TestCollectDiffGitHubBaseRef(t *testing.T) {
	dir := newDiffRepo(t)
	// origin/main already tracked: no fetch is needed, and there is no origin.
	gitRun(t, dir, "update-ref", "refs/remotes/origin/main", "main")
	diff, err := collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromGitHubBase, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, diff, "a.go", "b.go", "c.go")

	_, err = collectDiff(context.Background(), dir, ".", diffSource{Kind: diffFromGitHubBase, Ref: "nope"})
	if err == nil || !strings.Contains(err.Error(), `fetching GITHUB_BASE_REF "nope" from origin`) {
		t.Errorf("missing branch without origin: got %v", err)
	}
}

func TestCollectDiffOutsideARepository(t *testing.T) {
	_, err := collectDiff(context.Background(), t.TempDir(), ".", diffSource{Kind: diffFromStaged})
	if err == nil || !strings.Contains(err.Error(), "collecting diff: git diff:") {
		t.Errorf("got %v, want git's own message", err)
	}
}

func TestGitDiffArgsEmptySubDir(t *testing.T) {
	got := strings.Join(gitDiffArgs("", "HEAD"), " ")
	if !strings.Contains(got, "HEAD -- . :(exclude,literal).strspc :(exclude,literal)rules") {
		t.Errorf("gitDiffArgs(\"\") = %s, want the repository root", got)
	}
	if got := filterDiff(checkDiff, ""); got != filterDiff(checkDiff, ".") {
		t.Error(`filterDiff with "" must behave as "."`)
	}
}

func TestDiffHeaderPath(t *testing.T) {
	tests := []struct {
		name    string
		section string
		want    string
		ok      bool
	}{
		{"plus line", "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n", "x", true},
		{"hunk before any plus line", "diff --git a/x b/x\n@@ -1 +1 @@\n+++ b/y\n", "x", true},
		{"deleted file", "diff --git a/x b/x\n--- a/x\n+++ /dev/null\n", "", false},
		{"not a diff header", "index 123..456\n", "", false},
		{"quoted, one side only", "diff --git \"a/x y\" b/x\n", "", false},
		{"quoted, sides differ", "diff --git \"a/x\" \"b/y\"\n", "", false},
		{"quoted, bad escape", "diff --git \"a/\\q\" \"b/\\q\"\n", "", false},
		{"odd length", "diff --git a/x b/xy\n", "", false},
		{"halves differ", "diff --git a/x b/y\n", "", false},
		{"bad quoted plus line", "diff --git a/x b/x\n+++ \"b/\\q\"\n", "", false},
		{"empty plus path", "diff --git a/x b/x\n+++ b/\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := diffHeaderPath(tt.section)
			if got != tt.want || ok != tt.ok {
				t.Errorf("diffHeaderPath = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRepoRelativeUnresolvableRepo(t *testing.T) {
	// A relative repository that doesn't exist can't be made relative to an
	// absolute directory.
	if _, err := repoRelative(filepath.Join("no", "such", "repo"), t.TempDir()); err == nil {
		t.Error("expected an error")
	}
}
