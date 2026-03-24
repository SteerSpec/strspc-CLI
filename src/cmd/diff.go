package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var (
		base       string
		prNumber   int
		jsonOutput bool
		strict     bool
	)

	cmd := &cobra.Command{
		Use:   "diff <path>",
		Short: "Validate rule lifecycle transitions between git refs",
		Long: "Compare entity JSON files between a base git ref and the working tree, " +
			"enforcing lifecycle rules (Rule Manager Spec §7.2).\n\n" +
			"Accepts a single entity file or a directory of entity files. " +
			"Exit code 0 means all checks pass; 1 means violations were found.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			headPath := args[0]

			info, err := os.Stat(headPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("path does not exist: %s", headPath)
				}
				return fmt.Errorf("accessing %s: %w", headPath, err)
			}

			// Resolve the git repo root from the given path so all git and gh
			// commands run from the correct repo, not the process working directory.
			repoDir, err := gitRoot(headPath)
			if err != nil {
				return err
			}

			// Resolve base ref: --pr takes precedence over --base.
			baseRef := base
			if prNumber > 0 {
				sha, err := prBaseSHA(prNumber, repoDir)
				if err != nil {
					return err
				}
				baseRef = sha
			}

			var res *result.Result
			if info.IsDir() {
				res, err = diffDir(headPath, baseRef, repoDir, strict)
			} else {
				res, err = diffFile(headPath, baseRef, repoDir, strict)
			}
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOutput {
				if writeErr := writeJSON(w, res); writeErr != nil {
					return writeErr
				}
				if !res.OK() {
					return fmt.Errorf("diff found %d error(s)", len(res.Errors()))
				}
				return nil
			}

			outputText(w, res)

			if !res.OK() {
				return fmt.Errorf("diff found %d error(s)", len(res.Errors()))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&base, "base", "HEAD", "base git ref to compare against")
	cmd.Flags().IntVar(&prNumber, "pr", 0, "GitHub PR number (resolves base ref via gh CLI; requires gh)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output diagnostics as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")

	return cmd
}

// diffFile compares a single entity file against its version at baseRef.
// If the file is absent at baseRef it is validated as a new entity.
func diffFile(headPath, baseRef, repoDir string, strict bool) (*result.Result, error) {
	headData, err := os.ReadFile(headPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", headPath, err)
	}

	opts := diffOpts(strict)
	baseData, err := gitShow(baseRef, headPath, repoDir)

	var res *result.Result
	if err != nil {
		if isGitNotFound(err) {
			res = rulediff.CompareNew(headData, opts...)
		} else {
			return nil, err
		}
	} else {
		res = rulediff.Compare(baseData, headData, opts...)
	}

	// Attach file path context to each diagnostic (matching lintDirPerFile pattern).
	for i := range res.Diagnostics {
		if res.Diagnostics[i].Path == "" {
			res.Diagnostics[i].Path = headPath
		} else {
			res.Diagnostics[i].Path = headPath + ": " + res.Diagnostics[i].Path
		}
	}
	return res, nil
}

// diffDir compares entity JSON files in headDir against their versions at baseRef.
// Walks the directory tree recursively, skipping _-prefixed dirs/files and realm.json.
// Only files whose content differs from the base (or are new) are validated.
// Files deleted from headDir are reported as RD005.
func diffDir(headDir, baseRef, repoDir string, strict bool) (*result.Result, error) {
	res := &result.Result{}
	opts := diffOpts(strict)

	// Walk headDir to find entity JSON files and compare each against baseRef.
	headRelPaths := make(map[string]bool)
	walkErr := filepath.WalkDir(headDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), "_") || !strings.HasSuffix(d.Name(), ".json") || d.Name() == "realm.json" {
			return nil
		}

		rel, relErr := filepath.Rel(headDir, path)
		if relErr != nil {
			return relErr
		}
		headRelPaths[rel] = true

		headData, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		baseData, showErr := gitShow(baseRef, path, repoDir)
		if showErr != nil && !isGitNotFound(showErr) {
			return showErr
		}
		if showErr == nil && bytes.Equal(baseData, headData) {
			return nil // file unchanged — nothing to validate
		}
		addWithPath := func(diags []result.Diagnostic) {
			for _, diag := range diags {
				if diag.Path == "" {
					diag.Path = path
				} else {
					diag.Path = path + ": " + diag.Path
				}
				res.Add(diag)
			}
		}
		if showErr == nil {
			addWithPath(rulediff.Compare(baseData, headData, opts...).Diagnostics)
		} else {
			addWithPath(rulediff.CompareNew(headData, opts...).Diagnostics)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking %s: %w", headDir, walkErr)
	}

	// Report files deleted from headDir (exist at baseRef but absent from the walk).
	baseRelPaths, err := gitListRelPaths(baseRef, headDir, repoDir)
	if err != nil {
		return nil, err
	}
	for _, rel := range baseRelPaths {
		if !headRelPaths[rel] {
			res.Add(result.Diagnostic{
				Module:   "rule-diff",
				Code:     "RD005",
				Severity: result.Error,
				Message:  "entity file deleted: " + rel,
				Path:     filepath.Join(headDir, rel),
			})
		}
	}

	return res, nil
}

// gitRoot returns the root of the git repository that contains path.
func gitRoot(path string) (string, error) {
	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s (git error: %v, output: %s)",
			path, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// gitShow returns the content of path at the given git ref, running from repoDir.
func gitShow(ref, path, repoDir string) ([]byte, error) {
	// Resolve symlinks on both sides so filepath.Rel works correctly on macOS
	// (where /var is a symlink to /private/var and git may return /private/var paths).
	realFile, err := realAbs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %s: %w", path, err)
	}
	realRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolving repo dir: %w", err)
	}
	relPath, err := filepath.Rel(realRepo, realFile)
	if err != nil {
		return nil, fmt.Errorf("computing relative path from %s to %s: %w", realRepo, realFile, err)
	}
	arg := ref + ":" + filepath.ToSlash(relPath)
	out, err := exec.Command("git", "-C", repoDir, "show", arg).Output()
	if err != nil {
		return nil, &gitShowError{ref: ref, path: path, cause: err}
	}
	return out, nil
}

// realAbs returns the absolute real path of p, resolving symlinks on the parent
// directory. This handles files that may not yet exist in the working tree.
func realAbs(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return abs, nil // fall back to non-symlink-resolved path
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// gitListRelPaths returns entity JSON paths (relative to headDir) tracked at ref under headDir.
// Uses git ls-tree -r for recursive traversal. Skips _-prefixed files and realm.json.
// Returns nil when headDir is absent at ref (all files are new).
// Returns an error when ref is not a valid git object name.
func gitListRelPaths(ref, headDir, repoDir string) ([]string, error) {
	// Compute headDir relative to the repo root for git ls-tree.
	realDir, _ := realAbs(headDir)
	realRepo, repoErr := filepath.EvalSymlinks(repoDir)
	if repoErr != nil {
		realRepo = repoDir
	}
	relDir := headDir
	if rel, err := filepath.Rel(realRepo, realDir); err == nil {
		relDir = filepath.ToSlash(rel)
	}

	out, err := exec.Command("git", "-C", repoDir, "ls-tree", "-r", "--name-only", ref, "--", relDir).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if isExitError(err, &exitErr) {
			stderr := exitErr.Stderr
			// Invalid ref (typo, unknown commit) — fail fast.
			if bytes.Contains(stderr, []byte("Not a valid object name")) {
				return nil, fmt.Errorf("invalid git ref %q: %s", ref, strings.TrimSpace(string(stderr)))
			}
			// Path not found at this ref — treat all files as new.
			return nil, nil
		}
		return nil, fmt.Errorf("git ls-tree %s %s: %w", ref, headDir, err)
	}

	// git ls-tree returns paths relative to repoDir. Strip the headDir prefix
	// to get paths relative to headDir.
	prefix := relDir + "/"
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel := line
		if strings.HasPrefix(line, prefix) {
			rel = line[len(prefix):]
		}
		rel = filepath.FromSlash(rel)
		base := filepath.Base(rel)
		if !strings.HasSuffix(base, ".json") || base == "realm.json" {
			continue
		}
		// Skip paths where any segment starts with _ (mirrors WalkDir SkipDir logic).
		skip := false
		for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.HasPrefix(seg, "_") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		files = append(files, rel)
	}
	return files, nil
}

// prBaseSHA returns the base commit SHA for a GitHub PR using the gh CLI.
// repoDir is used as the working directory so gh infers the correct repository.
func prBaseSHA(prNum int, repoDir string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum),
		"--json", "baseRefSha", "--jq", ".baseRefSha")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving PR #%d base SHA (requires gh CLI): %w", prNum, err)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("gh returned empty base SHA for PR #%d", prNum)
	}
	return sha, nil
}

// diffOpts builds the rulediff option slice from CLI flags.
func diffOpts(strict bool) []rulediff.Option {
	if strict {
		return []rulediff.Option{rulediff.WithStrict(true)}
	}
	return nil
}

// gitShowError wraps a git-show failure so callers can distinguish "not found" from real errors.
type gitShowError struct {
	ref   string
	path  string
	cause error
}

func (e *gitShowError) Error() string {
	return fmt.Sprintf("git show %s:%s: %s", e.ref, e.path, e.cause)
}

// isGitNotFound returns true when err represents a file that does not exist at the given ref.
func isGitNotFound(err error) bool {
	gse, ok := err.(*gitShowError)
	if !ok {
		return false
	}
	var exitErr *exec.ExitError
	if !isExitError(gse.cause, &exitErr) {
		return false
	}
	// git-show exits 128 with "does not exist" / "exists on disk, but not in" in stderr.
	return bytes.Contains(exitErr.Stderr, []byte("does not exist")) ||
		bytes.Contains(exitErr.Stderr, []byte("exists on disk"))
}

// isExitError reports whether err is an *exec.ExitError and sets target if so.
func isExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok && target != nil {
		*target = exitErr
	}
	return ok
}
