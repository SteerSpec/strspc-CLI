package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

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

// syncCoreSchemas copies each _schema/*.json of the realm in dir that differs
// from the same-named file of the upgraded core dependency, mirroring RM015:
// only files both hold are touched, and formatting-only differences are left.
//
// A cloned realm may hold symlinks, so all access to the realm goes through an
// os.Root, which refuses any path that escapes dir, and only regular files are
// rewritten.
func syncCoreSchemas(ctx context.Context, dir string, up depUpgrade) ([]schemaSync, error) {
	if up.realmID != coreRealmID {
		return nil, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	entries, err := readRootDir(root, "_schema")
	if err != nil {
		return nil, nil // no local copies (or a link out of the realm); RM003 reports a missing _schema/
	}
	coreDir, err := fetchUpgradeDep(ctx, dir, entity.RealmDep{RealmID: up.realmID, Version: up.to, Source: up.source})
	if err != nil {
		return nil, fmt.Errorf("fetching %s@%s: %w", up.realmID, up.to, err)
	}

	var done []schemaSync
	for _, e := range entries {
		if !e.Type().IsRegular() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		rel := filepath.Join("_schema", e.Name())
		want, err := os.ReadFile(filepath.Join(coreDir, rel))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return done, fmt.Errorf("reading core schema %s: %w", e.Name(), err)
		}
		have, err := root.ReadFile(rel)
		if err != nil {
			return done, fmt.Errorf("reading %s: %w", rel, err)
		}
		if sameJSONBytes(have, want) {
			continue
		}
		if err := root.WriteFile(rel, want, 0o644); err != nil {
			return done, fmt.Errorf("writing %s: %w", rel, err)
		}
		done = append(done, schemaSync{path: rel, original: have})
	}
	return done, nil
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
