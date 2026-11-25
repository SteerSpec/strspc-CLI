package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// resolveValidatedRealm runs realm-resolve on the realm in dir, as check does,
// so that realm validate reports what only resolving the realm finds: the
// inheritance chains and overrides that cross into its dependencies (RR013 to
// RR024). It returns no result when dir holds no manifest that loads, which
// realm-lint reports. A dependency it can't load is a warning; with strict,
// the resolver's warnings are errors, as the linters' are.
func resolveValidatedRealm(ctx context.Context, dir string, strict bool) (*result.Result, error) {
	rf, err := loadRealmIfAny(dir)
	if err != nil || rf == nil {
		return nil, err
	}
	if err := checkDependencySources(dir, rf, &dirSet{}); err != nil {
		return nil, fmt.Errorf("realm %s: %w", rf.Realm.ID, err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locating user cache: %w", err)
	}
	cacheDir = filepath.Join(cacheDir, "strspc", "realms")
	_, res := realmresolve.New(
		realmresolve.WithCacheDir(cacheDir),
		realmresolve.WithRemoteFetcher(newRemoteFetcher(cacheDir, 0)),
	).Resolve(ctx, rf, dir)
	for i := range res.Diagnostics {
		d := &res.Diagnostics[i]
		if unloadableDependency[d.Code] {
			d.Severity = result.Warning
		}
		if strict && d.Severity == result.Warning {
			d.Severity = result.Error
		}
	}
	return res, nil
}

// unloadableDependency holds the codes of a dependency the resolver can't
// load. realm validate keeps them warnings, as realm-lint's RM016 for the
// same dependency, so that it still runs offline: check is the gate on them.
var unloadableDependency = map[string]bool{"RR001": true, "RR002": true, "RR012": true}
