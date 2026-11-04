package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// realmDirOf returns the directory of the nearest realm.json at or above the
// directory of file, looking no higher than stop, or "" when there is none:
// an entity belongs to the Realm with the nearest manifest above it. Both
// paths are resolved first, so /var and /private/var on macOS compare equal.
// A realm.json that is a link to nothing is an error, not a missing manifest:
// otherwise the lookup would go on to a parent Realm's list, or to none.
func realmDirOf(file, stop string) (string, error) {
	dir, stop := resolvedPath(filepath.Dir(file)), resolvedPath(stop)
	for {
		manifest := filepath.Join(dir, "realm.json")
		_, statErr := os.Lstat(manifest)
		if statErr == nil {
			if _, err := os.Stat(manifest); err != nil {
				return "", fmt.Errorf("reading %s: %w", manifest, err)
			}
			return dir, nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(dir)
		if dir == stop || parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// realmContributorsOf returns the contributors of the Realm holding file
// (CTRB-004), looking for its manifest no higher than stop. A sub-realm uses
// its parent's (CTRB-006). There are none when no manifest is found.
func realmContributorsOf(file, stop string) ([]entity.Contributor, error) {
	dir, err := realmDirOf(file, stop)
	if err != nil || dir == "" {
		return nil, err
	}
	list, err := entity.RealmContributors(dir)
	if err != nil {
		return nil, fmt.Errorf("reading the contributors of the realm in %s: %w", dir, err)
	}
	return list, nil
}

// addedByEnv names the environment variable that supplies --added-by when the
// flag is not given.
const addedByEnv = "STRSPC_ADDED_BY"

// Where resolveAddedBy found the value.
const (
	addedByFromFlag = "--added-by"
	addedByFromEnv  = addedByEnv
	addedByFromGit  = "git user.email"
)

// gitUserEmail returns `git config user.email` as run in dir, or "" when git
// is missing, dir is in no repository or no email is set. A var so tests can
// replace it.
var gitUserEmail = func(dir string) string {
	cmd := exec.Command("git", "config", "user.email")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveAddedBy returns the added_by for a Rule or Note written to the entity
// file of t under dir, and where it came from. The flag wins; then
// STRSPC_ADDED_BY; then, when the Realm lists contributors, the entry whose
// email is the git user.email, by its identifier. A raw email is never
// written for the default: CTRB-003 retires that form. Whatever its source,
// the value must then be a contributor identifier, or during the transition
// (#248) an @handle or email, and must name one of the Realm's contributors
// when the Realm lists any (CTRB-005). All of this runs before anything is
// written.
func resolveAddedBy(flag string, t entityTarget, dir string) (value, source string, err error) {
	list, err := realmContributorsOf(t.path, dir)
	if err != nil {
		return "", "", err
	}
	switch {
	case flag != "":
		value, source = flag, addedByFromFlag
	case os.Getenv(addedByEnv) != "":
		value, source = strings.TrimSpace(os.Getenv(addedByEnv)), addedByFromEnv
	case len(list) > 0:
		if email := gitUserEmail(filepath.Dir(t.path)); email != "" {
			if c := entity.FindContributor(list, email); c != nil {
				value, source = c.ID, addedByFromGit
			}
		}
	}
	if value == "" {
		return "", "", fmt.Errorf("--added-by is required: pass it, set %s, or list your git user.email on a contributor with `strspc realm contributor add`", addedByEnv)
	}
	if err := entityops.CheckAddedBy(value, list); err != nil {
		if source != addedByFromFlag {
			return "", "", fmt.Errorf("%w (from %s)", err, source)
		}
		return "", "", err
	}
	return value, source, nil
}

// writeAddedBySource tells, in human output, where a defaulted added_by came
// from, so an agent or CI log shows who a change was recorded under.
func writeAddedBySource(w io.Writer, value, source string) {
	if source != addedByFromFlag {
		writeln(w, descStyle.Render(fmt.Sprintf("  Added by: %s (from %s)", value, source)))
	}
}
