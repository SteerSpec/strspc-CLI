package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// realmDirOf returns the directory of the nearest realm.json at or above the
// directory of file, looking no higher than stop, or "" when there is none:
// an entity belongs to the Realm with the nearest manifest above it. Both
// paths are resolved first, so /var and /private/var on macOS compare equal.
func realmDirOf(file, stop string) (string, error) {
	dir, stop := resolvedPath(filepath.Dir(file)), resolvedPath(stop)
	for {
		_, statErr := os.Stat(filepath.Join(dir, "realm.json"))
		if statErr == nil {
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

// checkAddedBy checks addedBy before a Rule or Note is written to the entity
// file of t under dir: it must be a contributor identifier, or during the
// transition (#248) an @handle or email, and must name one of the Realm's
// contributors when the Realm lists any (CTRB-005).
func checkAddedBy(addedBy string, t entityTarget, dir string) error {
	list, err := realmContributorsOf(t.path, dir)
	if err != nil {
		return err
	}
	return entityops.CheckAddedBy(addedBy, list)
}
