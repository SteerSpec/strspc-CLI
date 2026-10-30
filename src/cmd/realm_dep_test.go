package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
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

// writeRealmDeps stores deps as a hand-edited realm.json would, past the
// checks in realm dep add.
func writeRealmDeps(t *testing.T, dir string, deps ...entity.RealmDep) {
	t.Helper()
	err := loadAndWriteRealm(filepath.Join(dir, "realm.json"), func([]entity.RealmDep) ([]entity.RealmDep, error) {
		return deps, nil
	})
	if err != nil {
		t.Fatalf("writing realm.json: %v", err)
	}
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

func TestRealmDepAddRejectsControlChars(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	for _, args := range [][]string{
		{"dev.steerspec.core@0.1.0=../dep\x1b[2J"},
		{"dev.steerspec.core@0.1.0=../dep\u009b2J"},
		{"dev.steerspec.core@0.1.0", "--source", "../dep\x1b[2J"},
		{"dev.steerspec.core@0.1.0", "--source", "../dep\u009b2J"},
	} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "dep", "add", "--dir", dir}, args...)...)
		if err == nil {
			t.Errorf("args %q: expected error", args)
			continue
		}
		testutil.AssertContains(t, err.Error(), "control character")
	}

	if deps := readRealmDeps(t, dir); len(deps) != 0 {
		t.Errorf("expected no dependency written, got %+v", deps)
	}
}

func TestRealmDepAddDuplicateEscapesVersion(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	writeRealmDeps(t, dir, entity.RealmDep{RealmID: "dev.steerspec.core", Version: "0.1.0\x1b[2J"})

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "\x1b") {
		t.Errorf("error contains a raw escape: %q", err.Error())
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
	if _, addErr := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir); addErr != nil {
		t.Fatalf("unexpected error on add: %v", addErr)
	}

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
	if _, addErr := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir); addErr != nil {
		t.Fatalf("unexpected error on add: %v", addErr)
	}

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "dev.steerspec.core")
	testutil.AssertContains(t, output, "0.1.0")
}

func TestRealmDepListNeutralisesControlChars(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	writeRealmDeps(t, dir, entity.RealmDep{
		RealmID: "dev.example\x1b[2J.dep",
		Version: "1.0.0\u009b2J",
		Source:  "../dep\x1b]0;title\x07\n::stop-commands::x",
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, raw := range []string{"\x1b[2J", "\x1b]0", "\u009b", "\x07", "\n::stop-commands"} {
		if strings.Contains(output, raw) {
			t.Errorf("output contains raw %q:\n%q", raw, output)
		}
	}
	testutil.AssertContains(t, output, "../dep")
}

func TestRealmDepListQuotesInvalidRealmID(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	writeRealmDeps(t, dir, entity.RealmDep{RealmID: "::stop-commands::x", Version: "1.0.0"})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "::") {
			t.Errorf("line starts a workflow command: %q", line)
		}
	}
	testutil.AssertContains(t, output, `"::stop-commands::x"@1.0.0`)
}

func TestRealmDepListJSON(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	if _, addErr := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir); addErr != nil {
		t.Fatalf("unexpected error on add: %v", addErr)
	}

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
