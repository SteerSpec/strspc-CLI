package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// configPin is a .strspc/config.yaml whose rule sources name an upgraded
// dependency at its old pin. Both versions of the file are kept so the change
// can be reported without writing, written, and put back.
type configPin struct {
	path     string
	original []byte
	updated  []byte
}

// findRepoConfig returns the .strspc/config.yaml that governs the realm in
// dir: the nearest one at or above dir, searched up to the repository root
// (the first directory holding .git). It returns "" when there is none. A
// config that is not a regular file, such as a link, is ignored.
func findRepoConfig(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(d, ".strspc", "config.yaml")
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return ""
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// planConfigPin rewrites, in the text of the repository's config.yaml, each
// rule source that is exactly an upgraded dependency's old source to its new
// one. The text is edited in place so comments and layout survive. It returns
// nil when there is no config or nothing in it matches.
func planConfigPin(dir string, ups []depUpgrade) (*configPin, error) {
	path := findRepoConfig(dir)
	if path == "" {
		return nil, nil
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	updated := original
	for _, up := range ups {
		if up.oldSource == "" || up.oldSource == up.source {
			continue
		}
		re := regexp.MustCompile(`(?m)^(\s*(?:-\s+)?source:\s*["']?)` + regexp.QuoteMeta(up.oldSource) + `(["']?\s*(?:#.*)?)$`)
		updated = re.ReplaceAll(updated, []byte("${1}"+up.source+"${2}"))
	}
	if bytes.Equal(updated, original) {
		return nil, nil
	}
	return &configPin{path: path, original: original, updated: updated}, nil
}
