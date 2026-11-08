package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// skippedFile is a tracked file that an audit does not read, with why.
type skippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Reasons a tracked file is not read.
const (
	skipDeleted = "deleted"
	skipSymlink = "symlink"
	skipNotFile = "not a regular file"
	skipBinary  = "binary"
)

// collectTreeFiles reads every tracked file under pathspecs (minus the
// excludes and the generated directories) from the working tree, so that
// uncommitted edits count; untracked files do not. Paths are
// repository-relative with forward slashes, in git's order.
//
// What git would not show as text is skipped and reported: a file deleted
// from the working tree, a symlink or a file reached through one (git
// tracks a link's target text, never what it points at), a submodule, and
// a file that is not valid UTF-8 or holds a NUL byte.
func collectTreeFiles(ctx context.Context, repoDir, subDir string, pathspecs []string) (files []ruleeval.File, skipped []skippedFile, err error) {
	args := append([]string{"ls-files", "-z", "--cached", "--"}, pathspecs...)
	args = append(args, diffExcludeSpecs(subDir)...)
	out, err := git(ctx, repoDir, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("listing tracked files: %w", err)
	}
	seen := map[string]bool{}
	links := map[string]bool{} // directory → whether it, or a parent, is a symlink
	for _, p := range strings.Split(out, "\x00") {
		if p == "" || seen[p] {
			continue // an unmerged path is listed once per stage
		}
		seen[p] = true
		if reason := treeFileSkip(repoDir, p, links); reason != "" {
			skipped = append(skipped, skippedFile{Path: p, Reason: reason})
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(p)))
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", p, err)
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			skipped = append(skipped, skippedFile{Path: p, Reason: skipBinary})
			continue
		}
		files = append(files, ruleeval.File{Path: p, Content: string(data)})
	}
	return files, skipped, nil
}

// treeFileSkip returns why the tracked path p is not read, or "" to read it.
// links memoizes the symlink check of each directory.
func treeFileSkip(repoDir, p string, links map[string]bool) string {
	if dir := path.Dir(p); dir != "." && underSymlink(repoDir, dir, links) {
		return skipSymlink
	}
	info, err := os.Lstat(filepath.Join(repoDir, filepath.FromSlash(p)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return skipDeleted
	case err != nil:
		return err.Error()
	case info.Mode()&fs.ModeSymlink != 0:
		return skipSymlink
	case !info.Mode().IsRegular():
		return skipNotFile
	}
	return ""
}

// underSymlink reports whether the repository-relative directory dir, or
// one of its parents, is a symlink. A missing directory is not (its files
// are then reported deleted); one that cannot be inspected counts as one,
// so that nothing is read through it.
func underSymlink(repoDir, dir string, links map[string]bool) bool {
	if v, ok := links[dir]; ok {
		return v
	}
	v := false
	if parent := path.Dir(dir); parent != "." && underSymlink(repoDir, parent, links) {
		v = true
	} else {
		info, err := os.Lstat(filepath.Join(repoDir, filepath.FromSlash(dir)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			v = true
		default:
			v = info.Mode()&fs.ModeSymlink != 0
		}
	}
	links[dir] = v
	return v
}
