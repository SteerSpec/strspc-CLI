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
// bytes needed to put it back.
type schemaSync struct {
	path     string
	original []byte
}

// syncCoreSchemas copies each _schema/*.json of the realm in dir that differs
// from the same-named file of the upgraded core dependency, mirroring RM015:
// only files both hold are touched, and formatting-only differences are left.
func syncCoreSchemas(ctx context.Context, dir string, up depUpgrade) ([]schemaSync, error) {
	if up.realmID != coreRealmID {
		return nil, nil
	}
	schemaDir := filepath.Join(dir, "_schema")
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		return nil, nil // no local copies; RM003 reports a missing _schema/
	}
	coreDir, err := fetchUpgradeDep(ctx, dir, entity.RealmDep{RealmID: up.realmID, Version: up.to, Source: up.source})
	if err != nil {
		return nil, fmt.Errorf("fetching %s@%s: %w", up.realmID, up.to, err)
	}

	var done []schemaSync
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		localPath := filepath.Join(schemaDir, e.Name())
		want, err := os.ReadFile(filepath.Join(coreDir, "_schema", e.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return done, fmt.Errorf("reading core schema %s: %w", e.Name(), err)
		}
		have, err := os.ReadFile(localPath)
		if err != nil {
			return done, fmt.Errorf("reading %s: %w", localPath, err)
		}
		if sameJSONBytes(have, want) {
			continue
		}
		if err := os.WriteFile(localPath, want, 0o644); err != nil {
			return done, fmt.Errorf("writing %s: %w", localPath, err)
		}
		done = append(done, schemaSync{path: localPath, original: have})
	}
	return done, nil
}

// restoreSchemas puts the synced files back as they were.
func restoreSchemas(synced []schemaSync) error {
	for _, s := range synced {
		if err := os.WriteFile(s.path, s.original, 0o644); err != nil {
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
