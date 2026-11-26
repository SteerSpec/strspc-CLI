package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
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
		switch configAt(c) {
		case configRegular:
			return c
		case configBlocked:
			// The nearest config is there but unusable (a link, or behind
			// one): stop rather than rewrite some ancestor's config instead.
			return ""
		}
	}
	return ""
}

type configState int

const (
	configNone configState = iota
	configRegular
	configBlocked
)

// configAt reports whether root holds a regular .strspc/config.yaml, nothing,
// or something that must not be followed. It looks through an os.Root so links
// out of root are refused.
func configAt(root string) configState {
	r, err := os.OpenRoot(root)
	if err != nil {
		return configNone
	}
	defer func() { _ = r.Close() }()
	fi, err := r.Lstat(configRel)
	switch {
	case err == nil && fi.Mode().IsRegular():
		return configRegular
	case err == nil:
		return configBlocked
	case os.IsNotExist(err):
		return configNone
	default:
		return configBlocked
	}
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
		// The key may start a block line ("source:", "- source:") or sit in a
		// flow mapping ("{source: ..., scope: global}"); the value ends at
		// whitespace, a comma, a closing brace or the end of the code part.
		re := regexp.MustCompile(`((?:^|[\s{,])source:\s*["']?)` + regexp.QuoteMeta(up.oldSource) + `(["']?(?:[\s,}]|$))`)
		updated = rewriteCodeLines(updated, re, []byte("${1}"+up.source+"${2}"))
	}
	if err := checkConfigPinsMoved(updated, ups); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(updated, original) {
		return nil, nil
	}
	return &configPin{root: root, path: path, original: original, updated: updated}, nil
}

// checkConfigPinsMoved fails when a rule source in the (rewritten) config still
// names an upgraded dependency's old source. The text edit only recognises
// plain `source: value` entries, block or flow style, so an exotic layout (a block
// scalar, an anchor) would otherwise stay
// on the old release while the upgrade reports success.
func checkConfigPinsMoved(data []byte, ups []depUpgrade) error {
	var cfg struct {
		Rules []struct {
			Source string `yaml:"source"`
		} `yaml:"rules"`
	}
	if yaml.Unmarshal(data, &cfg) != nil {
		return nil // not parseable here; the schema check reports it
	}
	for _, r := range cfg.Rules {
		for i := range ups {
			if ups[i].oldSource != "" && ups[i].oldSource != ups[i].source && r.Source == ups[i].oldSource {
				return fmt.Errorf("still pins %s in a layout the upgrade cannot rewrite (for example a block scalar or an anchor); update it by hand", sanitizeLine(r.Source))
			}
		}
	}
	return nil
}

// rewriteCodeLines applies re to the code part of each line only: text from a
// comment start (a # at the line's start or after whitespace) on is kept as it
// is, so a commented-out or annotated source is never rewritten.
func rewriteCodeLines(data []byte, re *regexp.Regexp, repl []byte) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))
	for i, line := range lines {
		cut := len(line)
		for k := range line {
			if line[k] == '#' && (k == 0 || line[k-1] == ' ' || line[k-1] == '\t') {
				cut = k
				break
			}
		}
		code, comment := line[:cut], line[cut:]
		// A line break belongs after the code, not inside a match's tail.
		nl := []byte(nil)
		if bytes.HasSuffix(code, []byte("\n")) {
			code, nl = code[:len(code)-1], []byte("\n")
		}
		out := append(re.ReplaceAll(code, repl), nl...)
		lines[i] = append(out, comment...)
	}
	return bytes.Join(lines, nil)
}
