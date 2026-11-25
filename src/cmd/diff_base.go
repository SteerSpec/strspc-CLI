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
	// errDiffUnreadLink refuses a file replacing a base link diff can't read:
	// what the link held can't be compared with it.
	errDiffUnreadLink = errors.New("strspc diff can't read, so the change can't be compared with what it held")
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

// baseFile is an entity file of the base tree, as diff reads it.
type baseFile struct {
	baseEntry
	// target is the slash path of the regular file a link leads to, empty
	// when the entry is itself a regular file.
	target string
}

// baseFiles holds the entity files of a base tree.
type baseFiles struct {
	// read are the files diff compares, keyed by slash path from the
	// repository root: regular files, and links read as the regular file they
	// lead to inside the tree, so that the change replacing one is compared
	// with what it held, as rulediff reads a base.
	read map[string]baseFile
	// unread are the entity files that are links diff can't read: leading
	// elsewhere, dangling or looping. Deleting one deletes no rules diff could
	// compare, but a file replacing one can't be compared with what the link
	// held, so it is refused rather than checked as new.
	unread map[string]bool
}

// baseEntityFiles returns the entity files of base, the tree under relDir,
// and refuses a submodule anywhere: the head side can't be compared with
// what it holds.
func baseEntityFiles(base map[string]baseEntry, relDir, repoDir string) (baseFiles, error) {
	files := baseFiles{read: map[string]baseFile{}, unread: map[string]bool{}}
	for p, e := range base {
		if e.kind == "commit" {
			return baseFiles{}, fmt.Errorf("%s at the base %w", p, errDiffSubmodule)
		}
		if underSchemaDir(p) || !isEntityFileName(path.Base(p)) {
			continue
		}
		e, target, ok, err := regularBaseFile(base, p, relDir, repoDir)
		switch {
		case err != nil:
			return baseFiles{}, fmt.Errorf("base entity files: %w", err)
		case ok:
			files.read[p] = baseFile{baseEntry: e, target: target}
		case base[p].mode == "120000":
			files.unread[p] = true
		}
	}
	return files, nil
}

// regularBaseFile returns the regular file that the entry at p of base, the
// tree under relDir, is or leads to by links inside the tree, and the path of
// that file when p is a link. ok is false for a link that leads elsewhere,
// dangles or loops, and for anything else.
func regularBaseFile(base map[string]baseEntry, p, relDir, repoDir string) (e baseEntry, target string, ok bool, err error) {
	start := p
	for range 8 {
		e, found := base[p]
		switch {
		case !found || e.kind != "blob":
			return baseEntry{}, "", false, nil
		case e.mode == "100644" || e.mode == "100755":
			if p == start {
				return e, "", true, nil
			}
			return e, p, true, nil
		case e.mode != "120000":
			return baseEntry{}, "", false, nil
		}
		text, err := readBlob(e.sha, repoDir)
		if err != nil {
			return baseEntry{}, "", false, fmt.Errorf("reading the link %s at the base: %w", p, err)
		}
		next, ok := baseLinkTarget(base, p, string(text), relDir)
		if !ok {
			return baseEntry{}, "", false, nil
		}
		p = next
	}
	return baseEntry{}, "", false, nil
}

// baseLinkTarget returns the slash path that the link at from in base, the
// tree under relDir, names by its text. ok is false when the text is
// absolute, leaves the tree, or passes through a directory that is a link at
// the base: a link is read only through real directories.
func baseLinkTarget(base map[string]baseEntry, from, text, relDir string) (string, bool) {
	if path.IsAbs(text) || strings.Contains(text, `\`) {
		return "", false
	}
	segs := strings.Split(text, "/")
	var cur string
	for i := range segs {
		cur = path.Join(path.Dir(from), strings.Join(segs[:i+1], "/"))
		if cur != relDir && !inBaseTree(cur, relDir) {
			return "", false
		}
		if i < len(segs)-1 && !realBaseDir(base, cur) {
			return "", false
		}
	}
	if cur == relDir {
		return "", false
	}
	return cur, true
}

// realBaseDir reports whether dir, a slash path, is a real directory of
// base: some entry is below it, and no link has its name in any case, which a
// case-insensitive file system would follow instead.
func realBaseDir(base map[string]baseEntry, dir string) bool {
	below := false
	for p, e := range base {
		if e.mode == "120000" && strings.EqualFold(p, dir) {
			return false
		}
		below = below || strings.HasPrefix(p, dir+"/")
	}
	return below
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

// checkHeadLink refuses the link p in the head tree under headDir as the
// resolver's walk does, and ignores it otherwise. Refused are a link named
// *.json in any case, a link to a directory, and one whose text is absolute,
// names a volume, leaves the tree or leads to another link. Only the text is
// read, and only a target inside the tree is looked at.
func checkHeadLink(p, headDir string) error {
	if strings.HasSuffix(strings.ToLower(filepath.Base(p)), ".json") {
		return fmt.Errorf("%s %w", p, errDiffLink)
	}
	text, err := os.Readlink(p)
	if err != nil {
		return fmt.Errorf("reading the link %s: %w", p, err)
	}
	if filepath.IsAbs(text) || filepath.VolumeName(text) != "" || strings.HasPrefix(text, "/") || strings.HasPrefix(text, `\`) {
		return fmt.Errorf("%s %w", p, errDiffLink)
	}
	// Each directory on the way must be a real one: nothing below a linked
	// directory is looked at.
	segs := strings.Split(filepath.ToSlash(text), "/")
	var target string
	for i := range segs {
		target = filepath.Join(filepath.Dir(p), filepath.FromSlash(strings.Join(segs[:i+1], "/")))
		rel, err := filepath.Rel(headDir, target)
		if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
			return fmt.Errorf("%s %w", p, errDiffLink)
		}
		fi, err := os.Lstat(target)
		if err != nil {
			return nil // to nothing: nothing the resolver reads
		}
		if fi.Mode()&fs.ModeSymlink != 0 || (i == len(segs)-1 && fi.IsDir()) {
			return fmt.Errorf("%s %w", p, errDiffLink)
		}
	}
	if target == headDir {
		return fmt.Errorf("%s %w", p, errDiffLink)
	}
	return nil // a link to a file the resolver doesn't read
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
		if fi.Mode()&fs.ModeSymlink != 0 && inRepo {
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
		// A link is written as the regular file it leads to inside the tree,
		// and not at all otherwise: no link is recreated.
		e, _, ok, err := regularBaseFile(base, full, relDir, repoDir)
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
