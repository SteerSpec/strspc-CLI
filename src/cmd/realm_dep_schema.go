package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// coreRealmID is the dependency whose _schema/ copies RM015 compares against.
const coreRealmID = "dev.steerspec.core"

// fetchUpgradeDep returns the local directory of a dependency at its new pin.
// Package-level so tests can serve a local directory instead of GitHub.
var fetchUpgradeDep = fetchRealmDependency

// schemaSync is one _schema/ file rewritten to match the core release, with the
// bytes needed to put it back. path is relative to the realm directory.
type schemaSync struct {
	path     string
	original []byte
}

// syncCoreSchemas copies each _schema/*.json in dir that differs from the
// same-named file of the upgraded core dependency, mirroring RM015: only files
// both hold are touched, and formatting-only differences are left. Sub-realms
// that inherit the core pin are synced too, because RM015 checks them against
// it.
//
// A cloned realm may hold symlinks, so all access to the realm goes through an
// os.Root, which refuses any path that escapes dir, and only regular files are
// rewritten.
func syncCoreSchemas(ctx context.Context, dir string, up depUpgrade, subRealms []string) ([]schemaSync, error) {
	if up.realmID != coreRealmID {
		return nil, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	files := localSchemaFiles(root, subRealms)
	if len(files) == 0 {
		return nil, nil
	}
	coreDir, err := fetchUpgradeDep(ctx, dir, entity.RealmDep{RealmID: up.realmID, Version: up.to, Source: up.source})
	if err != nil {
		return nil, fmt.Errorf("fetching %s@%s: %w", up.realmID, up.to, err)
	}

	var done []schemaSync
	for _, rel := range files {
		name := filepath.Base(rel)
		want, err := os.ReadFile(filepath.Join(coreDir, "_schema", name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return done, fmt.Errorf("reading core schema %s: %w", name, err)
		}
		have, err := root.ReadFile(rel)
		if err != nil {
			return done, fmt.Errorf("reading %s: %w", rel, err)
		}
		if sameJSONBytes(have, want) {
			continue
		}
		// Recorded first: a write that fails partway has already truncated the file.
		done = append(done, schemaSync{path: rel, original: have})
		if err := root.WriteFile(rel, want, 0o644); err != nil {
			return done, fmt.Errorf("writing %s: %w", rel, err)
		}
	}
	return done, nil
}

// localSchemaFiles lists the regular _schema/*.json files of the realm in root
// and of its sub-realms that inherit the core pin. A directory that can't be
// read is left out: it is missing, or a link out of the realm, and RM003
// reports a missing _schema/.
func localSchemaFiles(root *os.Root, subRealms []string) []string {
	dirs := []string{"_schema"}
	for _, name := range subRealms {
		if inheritsCore(root, name) {
			dirs = append(dirs, filepath.Join(name, "_schema"))
		}
	}
	var files []string
	for _, d := range dirs {
		entries, err := readRootDir(root, d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type().IsRegular() && filepath.Ext(e.Name()) == ".json" {
				files = append(files, filepath.Join(d, e.Name()))
			}
		}
	}
	return files
}

// inheritsCore reports whether the sub-realm reads as one that doesn't pin its
// own core. One that does is judged against that pin, which this upgrade leaves.
func inheritsCore(root *os.Root, subRealm string) bool {
	data, err := root.ReadFile(filepath.Join(subRealm, "realm.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Dependencies []entity.RealmDep `json:"dependencies"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return false
	}
	return !slices.ContainsFunc(manifest.Dependencies, func(d entity.RealmDep) bool {
		return d.RealmID == coreRealmID
	})
}

// readRootDir lists a directory inside root.
func readRootDir(root *os.Root, name string) ([]os.DirEntry, error) {
	d, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	return d.ReadDir(-1)
}

// restoreSchemas puts the synced files back as they were.
func restoreSchemas(dir string, synced []schemaSync) error {
	if len(synced) == 0 {
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, s := range synced {
		if err := root.WriteFile(s.path, s.original, 0o644); err != nil {
			return fmt.Errorf("restoring %s: %w", s.path, err)
		}
	}
	return nil
}

func sameJSONBytes(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}
