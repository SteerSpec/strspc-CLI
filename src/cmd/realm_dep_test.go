package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func readRealmDeps(t *testing.T, dir string) []entity.RealmDep {
	t.Helper()
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatalf("loading realm.json: %v", err)
	}
	return rf.Dependencies
}

func TestRealmDepAddBasic(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "dev.steerspec.core@0.1.0")

	deps := readRealmDeps(t, dir)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].RealmID != "dev.steerspec.core" {
		t.Errorf("got realm_id %q, want %q", deps[0].RealmID, "dev.steerspec.core")
	}
	if deps[0].Version != "0.1.0" {
		t.Errorf("got version %q, want %q", deps[0].Version, "0.1.0")
	}
}

func TestRealmDepAddWithSource(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0",
		"--source", "github://SteerSpec/strspc-rules@v1/rules/core", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "source:")

	deps := readRealmDeps(t, dir)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].Source != "github://SteerSpec/strspc-rules@v1/rules/core" {
		t.Errorf("got source %q", deps[0].Source)
	}
}

func TestRealmDepAddDuplicate(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	// Add first.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error on first add: %v", err)
	}

	// Add same realm_id again.
	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.2.0", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for duplicate dependency, got nil")
	}
	testutil.AssertContains(t, err.Error(), "already exists")
}

func TestRealmDepAddNoRealm(t *testing.T) {
	dir := t.TempDir()

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for missing realm.json, got nil")
	}
	testutil.AssertContains(t, err.Error(), "realm.json")
}

func TestRealmDepRemoveBasic(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	// Add then remove.
	_, _ = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "remove", "dev.steerspec.core", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "Removed")

	deps := readRealmDeps(t, dir)
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies after remove, got %d", len(deps))
	}
}

func TestRealmDepRemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "remove", "dev.nonexistent", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}
	testutil.AssertContains(t, err.Error(), "not found")
}

func TestRealmDepList(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	// Add a dependency.
	_, _ = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "dev.steerspec.core")
	testutil.AssertContains(t, output, "0.1.0")
}

func TestRealmDepListJSON(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	_, _ = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--json", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var deps []entity.RealmDep
	if jsonErr := json.Unmarshal([]byte(output), &deps); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency in JSON, got %d", len(deps))
	}
}

func TestRealmDepListEmpty(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "No dependencies")
}

func TestRealmDepHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "add")
	testutil.AssertContains(t, output, "remove")
	testutil.AssertContains(t, output, "list")
}
