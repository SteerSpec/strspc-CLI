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

			// Resolve base ref: --pr takes precedence over --base.
			baseRef := base
			if prNumber > 0 {
				sha, err := prBaseSHA(prNumber)
				if err != nil {
					return err
				}
				baseRef = sha
			}

			info, err := os.Stat(headPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("path does not exist: %s", headPath)
				}
				return fmt.Errorf("accessing %s: %w", headPath, err)
			}

			// Resolve the git repo root from the given path so git commands run
			// from the correct repo, not the process working directory.
			repoDir, err := gitRoot(headPath)
			if err != nil {
				return err
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
	if err != nil {
		if isGitNotFound(err) {
			return rulediff.CompareNew(headData, opts...), nil
		}
		return nil, err
	}
	return rulediff.Compare(baseData, headData, opts...), nil
}

// diffDir compares entity JSON files in headDir against their versions at baseRef.
// Only files whose content differs from the base (or are new) are validated.
// Files deleted from headDir are reported as RD005.
func diffDir(headDir, baseRef, repoDir string, strict bool) (*result.Result, error) {
	res := &result.Result{}
	opts := diffOpts(strict)

	// Index JSON files present in headDir.
	entries, err := os.ReadDir(headDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", headDir, err)
	}
	headFiles := make(map[string]string) // basename → full path
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			headFiles[e.Name()] = filepath.Join(headDir, e.Name())
		}
	}

	// Index JSON files present at baseRef.
	baseNames, err := gitListJSONFiles(baseRef, headDir, repoDir)
	if err != nil {
		return nil, err
	}
	baseSet := make(map[string]bool, len(baseNames))
	for _, name := range baseNames {
		baseSet[name] = true
	}

	// For each file in headDir: compare or validate as new.
	for name, headPath := range headFiles {
		headData, readErr := os.ReadFile(headPath)
		if readErr != nil {
			return nil, fmt.Errorf("reading %s: %w", headPath, readErr)
		}
		if baseSet[name] {
			baseData, showErr := gitShow(baseRef, headPath, repoDir)
			if showErr != nil && !isGitNotFound(showErr) {
				return nil, showErr
			}
			if showErr == nil && bytes.Equal(baseData, headData) {
				continue // file unchanged — nothing to validate
			}
			if showErr == nil {
				for _, d := range rulediff.Compare(baseData, headData, opts...).Diagnostics {
					res.Add(d)
				}
				continue
			}
		}
		// File is new (not in base).
		for _, d := range rulediff.CompareNew(headData, opts...).Diagnostics {
			res.Add(d)
		}
	}

	// Report files deleted from headDir.
	for _, name := range baseNames {
		if _, ok := headFiles[name]; !ok {
			res.Add(result.Diagnostic{
				Module:   "rule-diff",
				Code:     "RD005",
				Severity: result.Error,
				Message:  "entity file deleted: " + name,
				Path:     filepath.Join(headDir, name),
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
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", path)
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

// gitListJSONFiles returns the basenames of *.json files tracked at baseRef under dir,
// running git from repoDir.
func gitListJSONFiles(ref, dir, repoDir string) ([]string, error) {
	// Compute dir relative to the repo root for git ls-tree.
	realDir, _ := realAbs(dir)
	realRepo, repoErr := filepath.EvalSymlinks(repoDir)
	if repoErr != nil {
		realRepo = repoDir
	}
	relDir := dir
	if rel, err := filepath.Rel(realRepo, realDir); err == nil {
		relDir = rel
	}
	out, err := exec.Command("git", "-C", repoDir, "ls-tree", "--name-only", ref, "--", relDir).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if isExitError(err, &exitErr) {
			// Silently return empty — dir may not exist at base ref (all files are new).
			return nil, nil
		}
		return nil, fmt.Errorf("git ls-tree %s %s: %w", ref, dir, err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		name := filepath.Base(strings.TrimSpace(line))
		if name != "" && strings.HasSuffix(name, ".json") {
			files = append(files, name)
		}
	}
	return files, nil
}

// prBaseSHA returns the base commit SHA for a GitHub PR using the gh CLI.
func prBaseSHA(prNum int) (string, error) {
	out, err := exec.Command("gh", "pr", "view", strconv.Itoa(prNum),
		"--json", "baseRefSha", "--jq", ".baseRefSha").Output()
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
