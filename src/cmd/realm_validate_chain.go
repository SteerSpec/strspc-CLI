package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkValidatedRealm loads the manifest of the realm in dir and refuses the
// local dependency sources that projectPath refuses, before realm-lint or
// realm-resolve opens any of them. It returns no manifest when dir holds none
// that loads, which realm-lint reports.
func checkValidatedRealm(dir string) (*entity.RealmFile, error) {
	rf, err := loadRealmIfAny(dir)
	if err != nil || rf == nil {
		return nil, err
	}
	if err := checkDependencySources(dir, rf, &dirSet{}); err != nil {
		return nil, fmt.Errorf("realm %s: %w", rf.Realm.ID, err)
	}
	return rf, nil
}

// resolveValidatedRealm runs realm-resolve on rf, the realm in dir, as check
// does, so that realm validate reports what only resolving the realm finds:
// the inheritance chains and overrides that cross into its dependencies
// (RR013 to RR025). A dependency it can't load is a warning; with strict,
// the resolver's warnings are errors, as the linters' are.
func resolveValidatedRealm(ctx context.Context, dir string, rf *entity.RealmFile, strict bool) *result.Result {
	var opts []realmresolve.Option
	// Without a user cache, a remote dependency can't be fetched: RR012,
	// a warning below, rather than a failed validation of a local realm.
	if cacheDir, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(cacheDir, "strspc", "realms")
		opts = append(opts, realmresolve.WithCacheDir(cacheDir), realmresolve.WithRemoteFetcher(newRemoteFetcher(cacheDir, 0)))
	}
	_, res := realmresolve.New(opts...).Resolve(ctx, rf, dir)
	for i := range res.Diagnostics {
		d := &res.Diagnostics[i]
		if unloadableDependency(d) {
			d.Severity = result.Warning
		}
		if strict && d.Severity == result.Warning {
			d.Severity = result.Error
		}
	}
	return res
}

// unloadableDependency reports whether d says a dependency can't be loaded.
// realm validate keeps those warnings, as realm-lint's RM016 for the same
// dependency, so that it still runs offline: check is the gate on them. The
// resolver also reports a root entity file it can't read as RR002, which
// stays an error: only its dependency messages name a dependency.
func unloadableDependency(d *result.Diagnostic) bool {
	switch d.Code {
	case "RR001", "RR002", "RR012":
		return strings.Contains(d.Message, `dependency "`)
	}
	return false
}
