package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
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

// errBackslashSource refuses a local dependency source holding a backslash:
// a separator on Windows only, so the directory it names would depend on the
// system, as strspc-manager refuses it (RR001).
var errBackslashSource = errors.New(`source holds a backslash: use / to separate directories`)

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

// checkDependencySources walks rf, the manifest of the realm in dir, the
// manifests of the sub-realms it lists and those of the local dependencies
// they reach, and refuses each local dependency source that projectPath
// refuses, before the realm resolver opens any of them. seen holds the
// directories already walked.
func checkDependencySources(dir string, rf *entity.RealmFile, seen *dirSet) error {
	type declarer struct {
		dir  string
		deps []entity.RealmDep
	}
	var decls []declarer
	// The realm and all its sub-realms, although the resolver refuses a
	// sub-realm that lists more (RR010). No depth limit: seen bounds the walk,
	// and a directory cut off at a limit would be skipped when reached again.
	var walk func(d string, r *entity.RealmFile) error
	walk = func(d string, r *entity.RealmFile) error {
		if !seen.enter(d) {
			return nil
		}
		decls = append(decls, declarer{d, r.Dependencies})
		for _, sub := range r.SubRealms {
			if !isSubRealmName(sub) {
				continue // RM008, which the resolver reports
			}
			subDir, err := projectPath(d, sub)
			if err != nil {
				return fmt.Errorf("sub-realm of %s: %w", r.Realm.ID, err)
			}
			srf, err := loadRealmIfAny(subDir)
			if err != nil {
				return fmt.Errorf("sub-realm of %s: %w", r.Realm.ID, err)
			}
			if srf != nil {
				if err := walk(subDir, srf); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(dir, rf); err != nil {
		return err
	}
	for _, d := range decls {
		for _, dep := range d.deps {
			if !isLocalSource(dep.Source) {
				continue
			}
			p, err := "", errBackslashSource
			if !strings.Contains(dep.Source, `\`) {
				p, err = projectPath(d.dir, dep.Source)
			}
			if err == nil {
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

// dirSet holds the directories a walk has entered, by identity rather than
// by path: links leading back give a new path at every level, so only
// identity keeps a walk over sub-realms and dependencies finite.
type dirSet struct{ entered []os.FileInfo }

// enter reports whether dir is new to s, and adds it. A directory that can't
// be read isn't entered: there is nothing to walk in it.
func (s *dirSet) enter(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || slices.ContainsFunc(s.entered, func(e os.FileInfo) bool { return os.SameFile(e, fi) }) {
		return false
	}
	s.entered = append(s.entered, fi)
	return true
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
