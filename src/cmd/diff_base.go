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
		return nil, err
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

// baseEntityFiles keeps of base the entity files under the compared
// directory, and refuses what the head side can't be compared with: a
// submodule anywhere, and an entity file that is a link.
func baseEntityFiles(base map[string]baseEntry) (map[string]baseEntry, error) {
	files := map[string]baseEntry{}
	for p, e := range base {
		if underSchemaDir(p) {
			continue
		}
		if e.kind == "commit" {
			return nil, fmt.Errorf("%s at the base %w", p, errDiffSubmodule)
		}
		if e.kind != "blob" || !isEntityFileName(path.Base(p)) {
			continue
		}
		if e.mode == "120000" {
			return nil, fmt.Errorf("%s at the base %w", p, errDiffLink)
		}
		files[p] = e
	}
	return files, nil
}

// repoPath returns p relative to the repository root repoDir, as a slash
// path, from p as given: only the part leading to the root is resolved, so a
// link inside the repository is never followed. Each component from the root
// down to p must not be a link, or the base would be read at the link's
// target rather than at p.
func repoPath(p, repoDir string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", p, err)
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
	cur := strings.TrimSuffix(abs, filepath.Join(rest...))
	for _, name := range rest {
		cur = filepath.Join(cur, name)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", fmt.Errorf("accessing %s: %w", cur, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s %w", cur, errDiffLink)
		}
	}
	if len(rest) == 0 {
		return ".", nil
	}
	return strings.Join(rest, "/"), nil
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
		e := base[full]
		if e.kind == "commit" {
			return nil, fmt.Errorf("%s at the base %w", full, errDiffSubmodule)
		}
		if e.kind != "blob" || (e.mode != "100644" && e.mode != "100755") {
			continue // a link or anything else: read as absent
		}
		rel := full
		if relDir != "." {
			rel = strings.TrimPrefix(full, relDir+"/")
		}
		data, err := readBlob(e.sha, repoDir)
		if err != nil {
			return nil, err
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
		return nil, err
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
