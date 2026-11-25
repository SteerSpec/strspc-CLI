package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
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
		explain    bool
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
				if writeErr := writeJSON(w, res, explain); writeErr != nil {
					return writeErr
				}
				if !res.OK() {
					return fmt.Errorf("diff found %d error(s)", len(res.Errors()))
				}
				return nil
			}

			outputDiagnostics(w, res, explain)

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
	cmd.Flags().BoolVar(&explain, "explain", false, "explain what each diagnostic code means and how to fix it")

	return cmd
}

// diffFile compares a single entity file against its version at baseRef.
// If the file is absent at baseRef it is validated as a new entity.
func diffFile(headPath, baseRef, repoDir string, strict bool) (*result.Result, error) {
	headPath = filepath.Clean(headPath)
	rel, err := repoPath(headPath, repoDir)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", headPath, err)
	}
	headData, err := os.ReadFile(headPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", headPath, err)
	}
	base, err := listBase(baseRef, rel, repoDir)
	if err != nil {
		return nil, fmt.Errorf("listing the base of %s: %w", headPath, err)
	}
	if base[rel].kind == "commit" {
		return nil, fmt.Errorf("%s at the base %w", rel, errDiffSubmodule)
	}
	if base[rel].mode == "120000" {
		// The link may lead anywhere in the repository.
		if base, err = listBase(baseRef, ".", repoDir); err != nil {
			return nil, fmt.Errorf("listing the base of %s: %w", headPath, err)
		}
	}
	e, _, inBase, err := regularBaseFile(base, rel, ".", repoDir)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", headPath, err)
	}
	if !inBase && base[rel].mode == "120000" {
		return nil, fmt.Errorf("%s replaces a link at the base that %w", rel, errDiffUnreadLink)
	}

	contributors, err := realmContributorsOf(headPath, repoDir)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", headPath, err)
	}
	opts := diffOpts(strict, contributors)

	var res *result.Result
	if inBase {
		baseData, err := readBlob(e.sha, repoDir)
		if err != nil {
			return nil, fmt.Errorf("reading %s at the base: %w", rel, err)
		}
		res = rulediff.Compare(baseData, headData, opts...)
	} else {
		res = rulediff.CompareNew(headData, opts...)
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

// diffDir compares the entity files in the tree of headDir against their
// versions at baseRef, and the Realms of the two trees (rulediff.CompareRealms).
// The entity files are those the resolver reads: every .json file but
// realm.json, outside _schema directories. The base is read at headDir as
// given, and a link on the way, a linked entity file or directory, or a
// submodule is refused: diff couldn't compare what they hold. Only files whose
// content differs from the base (or are new) are validated, and files deleted
// from headDir are reported as RD005 unless they held no entity.
func diffDir(headDir, baseRef, repoDir string, strict bool) (*result.Result, error) {
	headDir = filepath.Clean(headDir)
	relDir, err := repoPath(headDir, repoDir)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", headDir, err)
	}
	base, err := listBase(baseRef, relDir, repoDir)
	if err != nil {
		return nil, fmt.Errorf("listing the base of %s: %w", headDir, err)
	}
	baseFiles, err := baseEntityFiles(base, relDir, repoDir)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", headDir, err)
	}

	res := &result.Result{}
	// The contributors of each file's Realm, by directory: a walk from a
	// parent Realm reaches its sub-realms, and one from above reaches several
	// Realms.
	contributors := map[string][]entity.Contributor{}
	contributorsOf := func(file string) ([]entity.Contributor, error) {
		dir := filepath.Dir(file)
		list, seen := contributors[dir]
		if !seen {
			var err error
			if list, err = realmContributorsOf(file, repoDir); err != nil {
				return nil, fmt.Errorf("diff %s: %w", file, err)
			}
			contributors[dir] = list
		}
		return list, nil
	}

	headFiles := map[string]bool{}
	walkErr := filepath.WalkDir(headDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == headDir {
			return nil
		}
		if d.Name() == ".git" {
			if relDir == "." && filepath.Dir(p) == headDir {
				return filepath.SkipDir // the repository's own
			}
			return fmt.Errorf("%s %w", filepath.Dir(p), errDiffSubmodule)
		}
		if d.IsDir() {
			if d.Name() == "_schema" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return checkHeadLink(p, headDir)
		}
		if !isEntityFileName(d.Name()) {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}

		rel, relErr := filepath.Rel(headDir, p)
		if relErr != nil {
			return relErr
		}
		full := joinRepo(relDir, filepath.ToSlash(rel))
		headFiles[full] = true
		if baseFiles.unread[full] {
			return fmt.Errorf("%s replaces a link at the base that %w", p, errDiffUnreadLink)
		}

		headData, readErr := os.ReadFile(p)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", p, readErr)
		}
		e, inBase := baseFiles.read[full]
		var baseData []byte
		if inBase {
			if baseData, err = readBlob(e.sha, repoDir); err != nil {
				return fmt.Errorf("reading %s at the base: %w", full, err)
			}
			if bytes.Equal(baseData, headData) {
				return nil // file unchanged — nothing to validate
			}
		}
		list, listErr := contributorsOf(p)
		if listErr != nil {
			return listErr
		}
		opts := diffOpts(strict, list)
		var diags []result.Diagnostic
		if inBase {
			diags = rulediff.Compare(baseData, headData, opts...).Diagnostics
		} else {
			diags = rulediff.CompareNew(headData, opts...).Diagnostics
		}
		for _, diag := range diags {
			if diag.Path == "" {
				diag.Path = p
			} else {
				diag.Path = p + ": " + diag.Path
			}
			res.Add(diag)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking %s: %w", headDir, walkErr)
	}

	// Report files deleted from headDir: in the base but absent from the walk.
	for _, full := range slices.Sorted(maps.Keys(baseFiles.read)) {
		bf := baseFiles.read[full]
		if headFiles[full] || (bf.target != "" && headFiles[bf.target]) {
			continue // still there, or a link whose file still is
		}
		data, err := readBlob(bf.sha, repoDir)
		if err != nil {
			return nil, fmt.Errorf("reading %s at the base: %w", full, err)
		}
		if rulediff.DeletionAllowed(data) {
			continue // it held no entity
		}
		rel := strings.TrimPrefix(full, relDir+"/")
		if relDir == "." {
			rel = full
		}
		res.Add(result.Diagnostic{
			Module:   "rule-diff",
			Code:     "RD005",
			Severity: result.Error,
			Message:  "entity file deleted: " + rel,
			Path:     filepath.Join(headDir, filepath.FromSlash(rel)),
		})
	}

	if len(headFiles) == 0 && len(baseFiles.read) == 0 {
		res.Add(result.Diagnostic{
			Module:   "rule-diff",
			Code:     "RD000",
			Severity: result.Error,
			Message:  fmt.Sprintf("no entity JSON files found in %s", headDir),
			Path:     headDir,
		})
	}

	realms, err := compareBaseRealms(base, relDir, headDir, repoDir, strict)
	if err != nil {
		return nil, err
	}
	for _, d := range realms.Diagnostics {
		res.Add(d)
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
		return "", fmt.Errorf("not a git repository: %s (git error: %w, output: %s)",
			path, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
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

// prBaseSHA returns the base commit SHA for a GitHub PR using the gh CLI.
// repoDir is used as the working directory so gh infers the correct repository.
func prBaseSHA(prNum int, repoDir string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum),
		"--json", "baseRefOid", "--jq", ".baseRefOid")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("resolving PR #%d base SHA (requires gh CLI): %s", prNum, msg)
		}
		return "", fmt.Errorf("resolving PR #%d base SHA (requires gh CLI): %w", prNum, err)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("gh returned empty base SHA for PR #%d", prNum)
	}
	return sha, nil
}

// diffOpts builds the rulediff option slice from CLI flags and the
// contributors of the Realm holding the file, for RD023.
func diffOpts(strict bool, contributors []entity.Contributor) []rulediff.Option {
	opts := []rulediff.Option{rulediff.WithContributors(contributors)}
	if strict {
		opts = append(opts, rulediff.WithStrict(true))
	}
	return opts
}

// isExitError reports whether err is or wraps an *exec.ExitError and sets
// target if so.
func isExitError(err error, target **exec.ExitError) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if target != nil {
		*target = exitErr
	}
	return true
}
