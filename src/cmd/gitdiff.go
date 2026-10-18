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
		return string(data), nil

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
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside the git repository %s", dir, repoDir)
	}
	return filepath.ToSlash(rel), nil
}
