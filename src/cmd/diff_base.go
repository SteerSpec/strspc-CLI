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
// -z keeps git from quoting a name, and rel is a path, not a pattern. A path
// absent at ref gives no entry, and an unknown ref is an error, as is a link
// or a submodule on the way to rel or at rel (baseOnTheWay).
func listBase(ref, rel, repoDir string) (map[string]baseEntry, error) {
	out, err := gitOutput(repoDir, "--literal-pathspecs", "ls-tree", "-r", "-z", ref, "--", rel)
	if err != nil {
		var exitErr *exec.ExitError
		if isExitError(err, &exitErr) && bytes.Contains(exitErr.Stderr, []byte("Not a valid object name")) {
			return nil, fmt.Errorf("invalid git ref %q: %s", ref, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("listing %s at %s: %w", rel, ref, err)
	}
	entries, err := parseLsTree(ref, out)
	if err != nil {
		return nil, err
	}
	if err := baseOnTheWay(ref, rel, repoDir); err != nil {
		return nil, err
	}
	return entries, nil
}

// baseOnTheWay refuses a link or a submodule at ref on the way to rel, the
// path diff compares, or at rel: what the base held behind it can't be
// listed at rel, so nothing would be compared with it. A path absent at ref,
// or under a regular file there, is new.
func baseOnTheWay(ref, rel, repoDir string) error {
	if rel == "." {
		return nil
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		out, err := gitOutput(repoDir, "--literal-pathspecs", "ls-tree", "-z", ref, "--", p)
		if err != nil {
			return fmt.Errorf("listing %s at %s: %w", p, ref, err)
		}
		entries, err := parseLsTree(ref, out)
		if err != nil {
			return err
		}
		e, ok := entries[p]
		switch {
		case !ok:
			return nil
		case e.kind == "commit":
			return fmt.Errorf("%s at the base %w", p, errDiffSubmodule)
		case e.kind == "tree":
			continue
		case e.mode == "120000":
			return fmt.Errorf("%s at the base %w", p, errDiffLink)
		default:
			return nil
		}
	}
	return nil
}

// parseLsTree parses the -z output of git ls-tree, keyed by path.
func parseLsTree(ref string, out []byte) (map[string]baseEntry, error) {
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

// isJSONName reports whether name ends in .json in any case. A link or
// special file of such a name is refused below a tree's root: no reader
// follows it, so what it holds can't be compared.
func isJSONName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".json")
}

// baseEntityFiles returns the entity files of base, regular files keyed by
// their slash path from the repository root. It refuses a submodule, whose
// files the head side can't be compared with, and a link named *.json:
// keeping, deleting, replacing or renaming it can't be compared with what it
// held, as rulediff refuses it (RD000). Other links are never read.
func baseEntityFiles(base map[string]baseEntry) (map[string]baseEntry, error) {
	files := map[string]baseEntry{}
	for p, e := range base {
		switch {
		case e.kind == "commit":
			return nil, fmt.Errorf("%s at the base %w", p, errDiffSubmodule)
		case e.kind != "blob":
			continue
		case e.mode != "100644" && e.mode != "100755":
			if isJSONName(path.Base(p)) {
				return nil, fmt.Errorf("%s at the base %w", p, errDiffLink)
			}
			continue
		case underSchemaDir(p) || !isEntityFileName(path.Base(p)):
			continue
		}
		files[p] = e
	}
	return files, nil
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
	if err := refuseLinksInRepository(abs, repoDir); err != nil {
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

// checkHeadLink refuses the link p in the head tree when it is named *.json
// in any case, as the resolver's walk does, and leaves any other link alone:
// it is never followed, read or looked at, and whatever is behind it doesn't
// count.
func checkHeadLink(p string) error {
	if isJSONName(filepath.Base(p)) {
		return fmt.Errorf("%s %w", p, errDiffLink)
	}
	return nil
}

// refuseLinksInRepository refuses abs when one of its components is a link
// below a repository root: a link in the repository, including one to another
// repository's root. A repository root is a directory with a .git entry, read
// from the file system alone so that no git error lets a link through, or
// repoDir, the root git named, which needn't have one. A link above every repository
// root, such as macOS's /var, leads to the repository rather than inside it.
func refuseLinksInRepository(abs, repoDir string) error {
	root, err := os.Stat(repoDir)
	if err != nil {
		return fmt.Errorf("accessing %s: %w", repoDir, err)
	}
	// isRoot reports whether dir is a repository root: one with a .git entry,
	// or the one git named, which may have none (GIT_DIR, core.worktree).
	isRoot := func(dir string) bool {
		if hasGitEntry(dir) {
			return true
		}
		fi, err := os.Stat(dir)
		return err == nil && os.SameFile(fi, root)
	}
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	inRepo := isRoot(cur)
	for _, name := range strings.Split(abs[len(vol):], string(filepath.Separator)) {
		if name == "" {
			continue
		}
		next := filepath.Join(cur, name)
		fi, err := os.Lstat(next)
		if err != nil {
			return fmt.Errorf("accessing %s: %w", next, err)
		}
		if inRepo && isLinkOrJunction(next, fi.Mode()) {
			return fmt.Errorf("%s %w", next, errDiffLink)
		}
		cur = next
		inRepo = inRepo || isRoot(cur)
	}
	return nil
}

// hasGitEntry reports whether dir holds a .git entry, or can't tell: an error
// other than its absence counts as one, so that the check fails closed.
func hasGitEntry(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return !errors.Is(err, fs.ErrNotExist)
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
		// Regular files only: no link is recreated or followed.
		e := base[full]
		if e.kind != "blob" || (e.mode != "100644" && e.mode != "100755") {
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
