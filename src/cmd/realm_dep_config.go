package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// configRel is where a repository keeps its SteerSpec configuration.
var configRel = filepath.Join(".strspc", "config.yaml")

// configPin is a .strspc/config.yaml whose rule sources name an upgraded
// dependency at its old pin. Both versions of the file are kept so the change
// can be reported without writing, written, and put back.
type configPin struct {
	root     string // directory holding .strspc/
	path     string // root joined with configRel, for messages
	original []byte
	updated  []byte
}

// write replaces the config with data. Every step resolves inside root, so a
// .strspc that is a link leading out of the repository makes the write fail
// instead of landing outside it.
func (c *configPin) write(data []byte) error {
	r, err := os.OpenRoot(c.root)
	if err != nil {
		return fmt.Errorf("opening %s: %w", c.root, err)
	}
	defer func() { _ = r.Close() }()
	return replaceFileIn(r, configRel, data, "config.yaml")
}

// findRepoConfig returns the directory holding the .strspc/config.yaml that
// governs the realm in dir, or "" when there is none. The search goes up from
// dir to the repository root, the first directory holding .git; without a
// repository only dir itself is considered, so a config in some unrelated
// parent directory is never picked up. A config that is not a regular file, or
// that sits behind a link leading out of its directory, is ignored.
func findRepoConfig(dir string) string {
	d, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	// Candidates run from dir up to the repository root. Without a repository
	// the realm's own directory is the only candidate.
	candidates := []string{d}
	inRepo := false
	for {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			inRepo = true
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
		candidates = append(candidates, d)
	}
	if !inRepo {
		candidates = candidates[:1]
	}
	for _, c := range candidates {
		if hasRegularConfig(c) {
			return c
		}
	}
	return ""
}

// hasRegularConfig reports whether root holds a regular .strspc/config.yaml,
// looked up through an os.Root so links out of root are refused.
func hasRegularConfig(root string) bool {
	r, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	defer func() { _ = r.Close() }()
	fi, err := r.Lstat(configRel)
	return err == nil && fi.Mode().IsRegular()
}

// planConfigPin rewrites, in the text of the repository's config.yaml, each
// rule source that is exactly an upgraded dependency's old source to its new
// one. The text is edited in place so comments and layout survive. It returns
// nil when there is no config or nothing in it matches.
func planConfigPin(dir string, ups []depUpgrade) (*configPin, error) {
	root := findRepoConfig(dir)
	if root == "" {
		return nil, nil
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", root, err)
	}
	defer func() { _ = r.Close() }()
	path := filepath.Join(root, configRel)
	original, err := r.ReadFile(configRel)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	updated := original
	for i := range ups {
		up := &ups[i]
		if up.oldSource == "" || up.oldSource == up.source {
			continue
		}
		re := regexp.MustCompile(`(?m)^(\s*(?:-\s+)?source:\s*["']?)` + regexp.QuoteMeta(up.oldSource) + `(["']?\s*(?:#.*)?)$`)
		updated = re.ReplaceAll(updated, []byte("${1}"+up.source+"${2}"))
	}
	if bytes.Equal(updated, original) {
		return nil, nil
	}
	return &configPin{root: root, path: path, original: original, updated: updated}, nil
}
