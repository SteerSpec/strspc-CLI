package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// isLocalSource reports whether source, a rule source or a dependency's
// source, names a directory rather than a download.
func isLocalSource(source string) bool {
	return strings.TrimSpace(source) != "" && !strings.HasPrefix(source, remote.Scheme) && !strings.Contains(source, "://")
}

// checkLocalSources refuses each local source of entries that projectPath
// refuses, before anything opens it: project content chooses them.
func checkLocalSources(cwd string, entries []ruleresolve.SourceEntry) error {
	for _, e := range entries {
		if !isLocalSource(e.Source) {
			continue
		}
		if _, err := projectPath(cwd, e.Source); err != nil {
			return fmt.Errorf("rule source: %w", err)
		}
	}
	return nil
}

// checkDependencySources refuses each local dependency source that
// projectPath refuses, in the manifest rf of the realm in dir, in those of
// the sub-realms it lists and in those of the local dependencies they reach,
// before the realm resolver opens any of them. seen holds the dependency
// directories already checked.
func checkDependencySources(dir string, rf *entity.RealmFile, seen map[string]bool) error {
	type declarer struct {
		dir  string
		deps []entity.RealmDep
	}
	decls := []declarer{{dir, rf.Dependencies}}
	for _, sub := range rf.SubRealms {
		subDir, err := projectPath(dir, sub)
		if err != nil {
			return fmt.Errorf("sub-realm of %s: %w", rf.Realm.ID, err)
		}
		srf, err := loadRealmIfAny(subDir)
		if err != nil {
			return fmt.Errorf("sub-realm of %s: %w", rf.Realm.ID, err)
		}
		if srf != nil {
			decls = append(decls, declarer{subDir, srf.Dependencies})
		}
	}
	for _, d := range decls {
		for _, dep := range d.deps {
			if !isLocalSource(dep.Source) {
				continue
			}
			p, err := projectPath(d.dir, dep.Source)
			if err == nil && !seen[p] {
				seen[p] = true
				var drf *entity.RealmFile
				if drf, err = loadRealmIfAny(p); err == nil && drf != nil {
					err = checkDependencySources(p, drf, seen)
				}
			}
			if err != nil {
				return fmt.Errorf("dependency %s of %s: %w", dep.RealmID, rf.Realm.ID, err)
			}
		}
	}
	return nil
}

// loadRealmIfAny loads dir/realm.json. It returns an error only when
// projectPath refuses the manifest, and no manifest when there is none or it
// doesn't load, which the resolver reports.
func loadRealmIfAny(dir string) (*entity.RealmFile, error) {
	manifest, err := projectPath(dir, "realm.json")
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(manifest); err != nil {
		return nil, nil //nolint:nilerr // none: nothing to check
	}
	rf, err := entity.LoadRealm(manifest)
	if err != nil {
		return nil, nil //nolint:nilerr // left to the resolver
	}
	return rf, nil
}
