package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// diffSourceKind names where `strspc check` takes its diff from.
type diffSourceKind string

// Diff sources in precedence order (highest first).
const (
	diffFromFile       diffSourceKind = "file"        // --diff-file
	diffFromStaged     diffSourceKind = "staged"      // --staged: index vs HEAD
	diffFromPR         diffSourceKind = "pr"          // --pr: merge-base of the PR base vs working tree
	diffFromBase       diffSourceKind = "base"        // --base: merge-base of a ref vs working tree
	diffFromGitHubBase diffSourceKind = "github-base" // $GITHUB_BASE_REF (origin/<ref>, fetched if missing)
	diffFromHead       diffSourceKind = "head"        // working tree vs HEAD
)

// diffFlags are the diff-selection flags of `strspc check`.
type diffFlags struct {
	DiffFile string
	Staged   bool
	PR       int
	Base     string
}

// diffSource is a resolved diff selection.
type diffSource struct {
	Kind diffSourceKind
	Ref  string // file path, git ref, remote branch name, or PR number
}

// String describes the selection for humans (verbose output, JSON).
func (s diffSource) String() string {
	switch s.Kind {
	case diffFromFile:
		return "diff file " + s.Ref
	case diffFromStaged:
		return "staged changes (index vs HEAD)"
	case diffFromPR:
		return "PR #" + s.Ref + " base vs working tree"
	case diffFromBase:
		return "base " + s.Ref + " vs working tree"
	case diffFromGitHubBase:
		return "GITHUB_BASE_REF origin/" + s.Ref + " vs working tree"
	default:
		return "working tree vs HEAD"
	}
}

// resolveDiffSource applies the precedence
// diff-file > staged > pr > base > $GITHUB_BASE_REF > HEAD.
// getenv is injected so the chain is testable without touching the process
// environment.
func resolveDiffSource(f diffFlags, getenv func(string) string) diffSource {
	switch {
	case f.DiffFile != "":
		return diffSource{Kind: diffFromFile, Ref: f.DiffFile}
	case f.Staged:
		return diffSource{Kind: diffFromStaged}
	case f.PR > 0:
		return diffSource{Kind: diffFromPR, Ref: strconv.Itoa(f.PR)}
	case f.Base != "":
		return diffSource{Kind: diffFromBase, Ref: f.Base}
	}
	if ref := strings.TrimSpace(getenv("GITHUB_BASE_REF")); ref != "" {
		return diffSource{Kind: diffFromGitHubBase, Ref: ref}
	}
	return diffSource{Kind: diffFromHead}
}

// resolvePRBase returns the base commit SHA of a GitHub PR. Package-level so
// tests can stub the gh CLI.
var resolvePRBase = prBaseSHA

// diffExcludes are never evaluated: the Configuration itself and the
// project's own rule files (lint and diff cover those).
var diffExcludes = []string{".strspc/**", "rules/**"}

// collectDiff produces the unified diff for src. Git commands run from
// repoDir so that paths in the diff are repository-relative; subDir (also
// repository-relative, "." for the root) restricts the diff to the checked
// project. The returned diff is empty (not an error) when nothing changed.
func collectDiff(ctx context.Context, repoDir, subDir string, src diffSource) (string, error) {
	switch src.Kind {
	case diffFromFile:
		data, err := os.ReadFile(src.Ref)
		if err != nil {
			return "", fmt.Errorf("reading diff file: %w", err)
		}
		return filterDiff(string(data), subDir), nil

	case diffFromStaged:
		return runGitDiff(ctx, repoDir, subDir, "--cached")

	case diffFromPR:
		pr, err := strconv.Atoi(src.Ref)
		if err != nil {
			return "", fmt.Errorf("invalid PR number %q", src.Ref)
		}
		sha, err := resolvePRBase(pr, repoDir)
		if err != nil {
			return "", err
		}
		return diffAgainstBase(ctx, repoDir, subDir, sha)

	case diffFromBase:
		return diffAgainstBase(ctx, repoDir, subDir, src.Ref)

	case diffFromGitHubBase:
		ref, err := ensureRemoteBranch(ctx, repoDir, src.Ref)
		if err != nil {
			return "", err
		}
		return diffAgainstBase(ctx, repoDir, subDir, ref)

	default:
		return runGitDiff(ctx, repoDir, subDir, "HEAD")
	}
}

// diffAgainstBase diffs the working tree against the merge base of base and
// HEAD, so that commits that landed on the base branch after the branch
// point never show up as changes. When no merge base exists (shallow clone,
// unrelated histories) base itself is used.
func diffAgainstBase(ctx context.Context, repoDir, subDir, base string) (string, error) {
	if _, err := git(ctx, repoDir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		return "", fmt.Errorf("invalid base ref %q: %w", base, err)
	}
	from := base
	if mb, err := git(ctx, repoDir, "merge-base", base, "HEAD"); err == nil && strings.TrimSpace(mb) != "" {
		from = strings.TrimSpace(mb)
	}
	return runGitDiff(ctx, repoDir, subDir, from)
}

// ensureRemoteBranch returns origin/<branch>, fetching it first when the
// remote-tracking ref is absent (CI checkouts often fetch a single ref).
func ensureRemoteBranch(ctx context.Context, repoDir, branch string) (string, error) {
	ref := "origin/" + branch
	if _, err := git(ctx, repoDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
		return ref, nil
	}
	refspec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
	if _, err := git(ctx, repoDir, "fetch", "--no-tags", "origin", refspec); err != nil {
		return "", fmt.Errorf("fetching GITHUB_BASE_REF %q from origin: %w", branch, err)
	}
	return ref, nil
}

// gitDiffArgs builds the argument list for the diff command: unified
// context of 3, no colour or external drivers, only added, copied, modified
// and renamed files, restricted to subDir minus the excludes.
func gitDiffArgs(subDir string, rangeArgs ...string) []string {
	args := []string{"diff", "--no-color", "--no-ext-diff", "-U3", "--diff-filter=ACMR"}
	args = append(args, rangeArgs...)
	if subDir == "" {
		subDir = "."
	}
	args = append(args, "--", subDir)
	for _, ex := range diffExcludes {
		p := ex
		if subDir != "." {
			p = path.Join(filepath.ToSlash(subDir), ex)
		}
		args = append(args, ":(exclude)"+p)
	}
	return args
}

// filterDiff applies to a file-backed diff the restrictions git enforces for
// the generated ones: sections touching the excluded paths are dropped, and
// when the project is a sub-directory of the repository, sections outside it
// are dropped too. Paths are read from the `diff --git a/X b/Y` header (the
// b/ side, quoted when git had to escape it); a section whose header cannot
// be parsed is kept so that nothing is silently lost. Sections stay intact.
func filterDiff(diff, subDir string) string {
	if subDir == "" {
		subDir = "."
	}
	sections := splitDiffSections(diff)
	if len(sections) == 0 {
		return diff
	}
	var b strings.Builder
	for _, sec := range sections {
		if isDeletionSection(sec) {
			continue // git-generated diffs use --diff-filter=ACMR: deletions are never evaluated
		}
		p, ok := diffHeaderPath(sec)
		if ok && excludedFromDiff(p, subDir) {
			continue
		}
		b.WriteString(sec)
	}
	return b.String()
}

// isDeletionSection reports whether a section removes a file: it carries a
// `deleted file mode` header or its post-image is /dev/null. Only the header
// lines before the first hunk are inspected.
func isDeletionSection(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "@@") {
			return false
		}
		if strings.HasPrefix(line, "deleted file mode ") || strings.TrimSpace(line) == "+++ /dev/null" {
			return true
		}
	}
	return false
}

// splitDiffSections splits a unified diff at every `diff --git ` line. Text
// before the first header forms a section of its own (and is always kept).
func splitDiffSections(diff string) []string {
	var sections []string
	start := 0
	for i := 0; i < len(diff); {
		next := strings.Index(diff[i:], "\ndiff --git ")
		if next < 0 {
			break
		}
		cut := i + next + 1
		sections = append(sections, diff[start:cut])
		start = cut
		i = cut + len("diff --git ")
	}
	if start < len(diff) {
		sections = append(sections, diff[start:])
	}
	return sections
}

// diffHeaderPath extracts the post-image path of a section. The `+++ b/P`
// line is authoritative when present (new and modified files). Otherwise the
// `diff --git a/P b/P` header is accepted only when both sides are the same
// path, split at the midpoint, so a file name containing " b/" cannot steer
// the parse. Quoted paths (git escapes control and non-ASCII characters) are
// unquoted. Anything ambiguous reports false and the section is kept.
func diffHeaderPath(section string) (string, bool) {
	header, rest, _ := strings.Cut(section, "\n")
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(line, "@@") {
			break // hunks start: no more headers
		}
		target, ok := strings.CutPrefix(line, "+++ ")
		if !ok {
			continue
		}
		if p, ok := stripBSide(strings.TrimSpace(target)); ok {
			return p, true
		}
		return "", false // +++ /dev/null or an unexpected form
	}
	hdr, ok := strings.CutPrefix(header, "diff --git ")
	if !ok {
		return "", false
	}
	hdr = strings.TrimSpace(hdr)
	if strings.HasPrefix(hdr, "\"") || strings.HasSuffix(hdr, "\"") {
		// `"a/P" "b/P"`: both sides quoted and identical.
		mid := strings.Index(hdr, "\" \"")
		if mid < 0 {
			return "", false
		}
		a, errA := strconv.Unquote(hdr[:mid+1])
		b, errB := strconv.Unquote(hdr[mid+2:])
		if errA != nil || errB != nil || strings.TrimPrefix(a, "a/") != strings.TrimPrefix(b, "b/") {
			return "", false
		}
		return stripBSide(b)
	}
	// `a/P b/P`: len = 2*len(P) + 5, and the two halves must agree.
	if (len(hdr)-5)%2 != 0 {
		return "", false
	}
	n := (len(hdr) - 5) / 2
	a, b := hdr[:n+2], hdr[n+3:]
	if hdr[n+2] != ' ' || !strings.HasPrefix(a, "a/") || !strings.HasPrefix(b, "b/") || a[2:] != b[2:] {
		return "", false
	}
	return stripBSide(b)
}

// stripBSide turns `b/P` or `"b/P"` into the cleaned slash path P.
func stripBSide(target string) (string, bool) {
	if strings.HasPrefix(target, "\"") {
		unq, err := strconv.Unquote(target)
		if err != nil {
			return "", false
		}
		target = unq
	}
	p, ok := strings.CutPrefix(target, "b/")
	if !ok || p == "" {
		return "", false
	}
	return path.Clean(filepath.ToSlash(p)), true
}

// excludedFromDiff reports whether a repository-relative path must not be
// evaluated: it lies outside subDir (paths in a diff file are expected to be
// repository-relative, as `git diff` prints them from the root), or under one
// of diffExcludes, matched both relative to subDir and bare so that a diff
// generated from the project directory itself is filtered too.
func excludedFromDiff(p, subDir string) bool {
	if subDir != "." && !strings.HasPrefix(p, subDir+"/") {
		return true
	}
	return underAnyExclude(p, subDir)
}

// underAnyExclude reports whether p lies under one of diffExcludes rooted at
// subDir (or at the repository root when subDir is ".").
func underAnyExclude(p, subDir string) bool {
	for _, ex := range diffExcludes {
		dir := strings.TrimSuffix(ex, "/**")
		if subDir != "." {
			if strings.HasPrefix(p, path.Join(subDir, dir)+"/") {
				return true
			}
		}
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

func runGitDiff(ctx context.Context, repoDir, subDir string, rangeArgs ...string) (string, error) {
	out, err := git(ctx, repoDir, gitDiffArgs(subDir, rangeArgs...)...)
	if err != nil {
		return "", fmt.Errorf("collecting diff: %w", err)
	}
	return out, nil
}

// git runs a git command in repoDir and returns its stdout. Failures carry
// git's stderr so the user sees the real cause (unknown ref, no commits…).
func git(ctx context.Context, repoDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", args[0], msg)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// repoRelative returns dir relative to repoDir with forward slashes ("."
// when they are the same directory), resolving symlinks on both sides.
func repoRelative(repoDir, dir string) (string, error) {
	realRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		realRepo = repoDir
	}
	realDir, err := realAbs(dir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(realDir); err == nil {
		realDir = resolved
	}
	rel, err := filepath.Rel(realRepo, realDir)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == "" {
		return ".", nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the git repository %s", dir, repoDir)
	}
	return filepath.ToSlash(rel), nil
}
