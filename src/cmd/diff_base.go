package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
)

// baseEntry is a file of the base tree, as git ls-tree lists it.
type baseEntry struct {
	mode, kind, sha string
}

// errDiffLink and errDiffSubmodule refuse what diff can't compare.
var (
	errDiffLink      = errors.New("is a link, which strspc diff doesn't follow")
	errDiffSubmodule = errors.New("is a submodule, whose files strspc diff can't compare")
)

// gitError is a failed git command with what git printed.
type gitError struct {
	args  string
	cause error
}

// Unwrap returns the git failure, so callers can inspect it with errors.Is and
// errors.As (CONV-004).
func (e *gitError) Unwrap() error { return e.cause }

func (e *gitError) Error() string {
	var exitErr *exec.ExitError
	if isExitError(e.cause, &exitErr) {
		if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
			return fmt.Sprintf("git %s: %s", e.args, msg)
		}
	}
	return fmt.Sprintf("git %s: %s", e.args, e.cause)
}

// gitOutput runs git in repoDir and returns its standard output.
func gitOutput(repoDir string, args ...string) ([]byte, error) {
	out, err := exec.Command("git", append([]string{"-C", repoDir}, args...)...).Output()
	if err != nil {
		return nil, &gitError{args: strings.Join(args, " "), cause: err}
	}
	return out, nil
}

// listBase returns the files of ref's tree under rel, a slash path relative
// to the repository root, keyed by their path from that root, byte for byte:
// -z keeps git from quoting a name. A path absent at ref gives no entry, and
// an unknown ref is an error.
func listBase(ref, rel, repoDir string) (map[string]baseEntry, error) {
	out, err := gitOutput(repoDir, "ls-tree", "-r", "-z", ref, "--", rel)
	if err != nil {
		var exitErr *exec.ExitError
		if isExitError(err, &exitErr) && bytes.Contains(exitErr.Stderr, []byte("Not a valid object name")) {
			return nil, fmt.Errorf("invalid git ref %q: %s", ref, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("listing %s at %s: %w", rel, ref, err)
	}
	entries := map[string]baseEntry{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, p, ok := bytes.Cut(rec, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree %s: unexpected entry %q", ref, rec)
		}
		entries[string(p)] = baseEntry{mode: fields[0], kind: fields[1], sha: fields[2]}
	}
	return entries, nil
}

// readBlob returns the content of the blob sha.
func readBlob(sha, repoDir string) ([]byte, error) {
	return gitOutput(repoDir, "cat-file", "blob", sha)
}

// isEntityFileName reports whether a file of this name is an entity file to
// compare, as the resolver's walk reads it: any .json file but realm.json.
func isEntityFileName(name string) bool {
	return strings.HasSuffix(name, ".json") && name != "realm.json"
}

// underSchemaDir reports whether the slash path p is in a _schema directory,
// which the resolver's walk skips.
func underSchemaDir(p string) bool {
	return slices.Contains(strings.Split(path.Dir(p), "/"), "_schema")
}

// baseEntityFiles keeps of base, the tree under relDir, the entity files,
// and refuses a submodule anywhere: the head side can't be compared with
// what it holds. A link is read as the regular file it leads to inside the
// tree, so that the change replacing it is compared with what it held, and
// as absent otherwise, as rulediff reads a base.
func baseEntityFiles(base map[string]baseEntry, relDir, repoDir string) (map[string]baseEntry, error) {
	files := map[string]baseEntry{}
	for p, e := range base {
		if e.kind == "commit" {
			return nil, fmt.Errorf("%s at the base %w", p, errDiffSubmodule)
		}
		if underSchemaDir(p) || !isEntityFileName(path.Base(p)) {
			continue
		}
		e, ok, err := regularBaseFile(base, p, relDir, repoDir)
		if err != nil {
			return nil, fmt.Errorf("base entity files: %w", err)
		}
		if ok {
			files[p] = e
		}
	}
	return files, nil
}

// regularBaseFile returns the regular file that the entry at p of base, the
// tree under relDir, is or leads to by links inside the tree. ok is false for
// a link that leads elsewhere, dangles or loops, and for anything else.
func regularBaseFile(base map[string]baseEntry, p, relDir, repoDir string) (baseEntry, bool, error) {
	for range 8 {
		e, ok := base[p]
		switch {
		case !ok || e.kind != "blob":
			return baseEntry{}, false, nil
		case e.mode == "100644" || e.mode == "100755":
			return e, true, nil
		case e.mode != "120000":
			return baseEntry{}, false, nil
		}
		target, err := readBlob(e.sha, repoDir)
		if err != nil {
			return baseEntry{}, false, fmt.Errorf("reading the link %s at the base: %w", p, err)
		}
		t := string(target)
		if path.IsAbs(t) || strings.Contains(t, `\`) {
			return baseEntry{}, false, nil
		}
		p = path.Join(path.Dir(p), t)
		if !inBaseTree(p, relDir) {
			return baseEntry{}, false, nil
		}
	}
	return baseEntry{}, false, nil
}

// inBaseTree reports whether the slash path p, from the repository root, is
// in the tree under relDir.
func inBaseTree(p, relDir string) bool {
	if relDir == "." {
		return p != ".." && !strings.HasPrefix(p, "../")
	}
	return strings.HasPrefix(p, relDir+"/")
}

// repoPath returns p relative to the repository root repoDir, as a slash
// path, from p as given. No component of p may be a link inside a git work
// tree (refuseLinksInRepository), or the base would be read at the link's
// target, possibly another repository, rather than at p.
func repoPath(p, repoDir string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", p, err)
	}
	if err := refuseLinksInRepository(abs); err != nil {
		return "", fmt.Errorf("path %s: %w", p, err)
	}
	root, err := os.Stat(repoDir)
	if err != nil {
		return "", fmt.Errorf("accessing %s: %w", repoDir, err)
	}
	var rest []string
	for d := abs; ; {
		if fi, err := os.Stat(d); err == nil && os.SameFile(fi, root) {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("%s is outside the git repository %s", p, repoDir)
		}
		rest = append([]string{filepath.Base(d)}, rest...)
		d = parent
	}
	if len(rest) == 0 {
		return ".", nil
	}
	return strings.Join(rest, "/"), nil
}

// refuseLinksInRepository refuses abs when one of its components is a link
// whose directory is inside a git work tree: a link in the repository,
// including one to another repository's root. A link outside any work tree,
// such as macOS's /var, leads to the repository rather than inside it.
func refuseLinksInRepository(abs string) error {
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	for _, name := range strings.Split(abs[len(vol):], string(filepath.Separator)) {
		if name == "" {
			continue
		}
		next := filepath.Join(cur, name)
		fi, err := os.Lstat(next)
		if err != nil {
			return fmt.Errorf("accessing %s: %w", next, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 && insideWorkTree(cur) {
			return fmt.Errorf("%s %w", next, errDiffLink)
		}
		cur = next
	}
	return nil
}

// insideWorkTree reports whether dir is inside a git work tree.
func insideWorkTree(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// compareBaseRealms compares the Realms of the base tree of headDir, the
// entries base lists under relDir, with those of headDir
// (rulediff.CompareRealms), with the contributors of the Realm in headDir for
// RD023 on new Realm Notes. It writes the base to a temporary directory: its
// regular files only, so no link of the base is followed or recreated, and
// through an os.Root, so nothing is written outside it.
func compareBaseRealms(base map[string]baseEntry, relDir, headDir, repoDir string, strict bool) (*result.Result, error) {
	tmp, err := os.MkdirTemp("", "strspc-diff-base-")
	if err != nil {
		return nil, fmt.Errorf("creating the base tree: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	root, err := os.OpenRoot(tmp)
	if err != nil {
		return nil, fmt.Errorf("opening the base tree: %w", err)
	}
	defer func() { _ = root.Close() }()

	for _, full := range slices.Sorted(maps.Keys(base)) {
		if base[full].kind == "commit" {
			return nil, fmt.Errorf("%s at the base %w", full, errDiffSubmodule)
		}
		// A link is written as the regular file it leads to inside the tree,
		// and not at all otherwise: no link is recreated.
		e, ok, err := regularBaseFile(base, full, relDir, repoDir)
		if err != nil {
			return nil, fmt.Errorf("writing the base tree: %w", err)
		}
		if !ok {
			continue
		}
		rel := full
		if relDir != "." {
			rel = strings.TrimPrefix(full, relDir+"/")
		}
		data, err := readBlob(e.sha, repoDir)
		if err != nil {
			return nil, fmt.Errorf("reading %s at the base: %w", full, err)
		}
		name := filepath.FromSlash(rel)
		if err := root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			return nil, fmt.Errorf("writing the base tree: %w", err)
		}
		if err := root.WriteFile(name, data, 0o600); err != nil {
			return nil, fmt.Errorf("writing the base tree: %w", err)
		}
	}

	contributors, err := realmContributorsOf(filepath.Join(headDir, "realm.json"), repoDir)
	if err != nil {
		return nil, fmt.Errorf("contributors of %s: %w", headDir, err)
	}
	return rulediff.CompareRealms(tmp, headDir, diffOpts(strict, contributors)...), nil
}

// joinRepo joins rel, relative to the compared directory, to its slash path
// from the repository root.
func joinRepo(relDir, rel string) string {
	if relDir == "." {
		return rel
	}
	return relDir + "/" + rel
}
