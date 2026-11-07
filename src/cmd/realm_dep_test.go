package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// realmManifestCommands are the commands that read or rewrite the realm.json of
// --dir, each with the arguments that get it past its own flag checks.
var realmManifestCommands = [][]string{
	{"realm", "dep", "list"},
	{"realm", "dep", "add", "dev.lib@1.0.0"},
	{"realm", "dep", "remove", "dev.lib"},
	{"realm", "dep", "upgrade"},
	{"realm", "add", "NEW", "--title", "New"},
	{"realm", "contributor", "add", "github:alice"},
	{"realm", "contributor", "remove", "github:alice"},
	{"realm", "contributor", "list"},
	{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements"},
	{"realm", "standards", "remove", "--id", "owasp.asvs"},
	{"realm", "standards", "list"},
	{"realm", "standards", "update", "--id", "owasp.asvs", "--version", "5.0.0"},
}

// A realm.json that is a directory is refused before anything is read or
// written.
func TestRealmCommandsRejectRealmJSONDirectory(t *testing.T) {
	for _, args := range realmManifestCommands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "realm.json"), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
			if err == nil || !strings.Contains(err.Error(), "realm.json is not a regular file") {
				t.Fatalf("error = %v, want realm.json is not a regular file", err)
			}
		})
	}
}

// A realm.json that doesn't parse is reported, and left as it was.
func TestRealmCommandsRejectMalformedRealmJSON(t *testing.T) {
	for _, args := range realmManifestCommands {
		if args[1] == "add" {
			continue // realm add only checks that realm.json is a file
		}
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "realm.json")
			if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if data, _ := os.ReadFile(path); string(data) != "{not json" {
				t.Errorf("realm.json was rewritten:\n%s", data)
			}
		})
	}
}

// A --dir that is a regular file can't hold a realm.json.
func TestRealmCommandsRejectFileAsDir(t *testing.T) {
	for _, args := range realmManifestCommands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "rules")
			if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", file)...)
			if err == nil || !strings.Contains(err.Error(), "realm.json") {
				t.Fatalf("error = %v, want one naming realm.json", err)
			}
		})
	}
}

func TestRealmDepAddConflictingSources(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.lib@1.0.0=../a/", "--source", "../b/", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "conflicting sources") {
		t.Fatalf("error = %v, want conflicting sources", err)
	}
	if deps := readRealmDeps(t, dir); len(deps) != 0 {
		t.Errorf("dependencies = %+v, want none", deps)
	}
}

func TestRealmDepRemoveKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	writeRealmDeps(t, dir,
		entity.RealmDep{RealmID: "dev.a", Version: "1.0.0", Source: "../a/"},
		entity.RealmDep{RealmID: "dev.b", Version: "2.0.0", Source: "../b/"},
	)
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "remove", "dev.a", "--dir", dir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	deps := readRealmDeps(t, dir)
	if len(deps) != 1 || deps[0].RealmID != "dev.b" || deps[0].Version != "2.0.0" {
		t.Errorf("dependencies = %+v, want only dev.b@2.0.0", deps)
	}
}

// A manifest with no dependencies key lists [] in JSON, not null.
func TestRealmDepListJSONWithoutDependenciesKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(`{"realm": {"id": "dev.x", "title": "x", "version": "0.1.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "list", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("output = %q, want []", out)
	}
}

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
		{"dev.steerspec.core@0.1.0=../dep\x9b2J"},
		{"dev.steerspec.core@0.1.0", "--source", "../dep\x9b2J"},
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

func TestRealmDepRewriteKeepsLanguage(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	path := filepath.Join(dir, "realm.json")

	var manifest map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if unmarshalErr := json.Unmarshal(data, &manifest); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	manifest["realm"].(map[string]any)["language"] = "en"
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if _, addErr := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir); addErr != nil {
		t.Fatalf("unexpected error: %v", addErr)
	}

	rf, err := entity.LoadRealm(path)
	if err != nil {
		t.Fatalf("loading realm.json: %v", err)
	}
	if rf.Realm.Language != "en" {
		t.Errorf("language after rewrite = %q, want en", rf.Realm.Language)
	}
}

func TestRealmDepRewriteLeavesLanguageAbsent(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "language") {
		t.Errorf("an absent language must stay absent:\n%s", data)
	}
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
