package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// jsonFieldNames returns the JSON names of the fields of the struct type t.
func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

// TestRealmFileForWriteCoversRealmFile fails when entity.RealmFile gains a
// field that realmFileForWrite lacks: every realm.json write would then drop
// it, as it once dropped notes and hash.
func TestRealmFileForWriteCoversRealmFile(t *testing.T) {
	have := jsonFieldNames(reflect.TypeFor[realmFileForWrite]())
	for _, name := range jsonFieldNames(reflect.TypeFor[entity.RealmFile]()) {
		if !slices.Contains(have, name) {
			t.Errorf("realmFileForWrite has no %q field: realm.json writes would drop it", name)
		}
	}
}

// testLinkedStandard is a Standard Reference linked to a Standard Realm, which
// an exclusion's ref names (NTE-034).
var testLinkedStandard = entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "5.0.0", Realm: "dev.steerspec.std.owasp.asvs.v5"}

// testRealmNotes are Realm Notes (NTE-029) for a manifest under test.
var testRealmNotes = []entity.RealmNote{
	{ID: "REALM/001", Type: "exclusion", Ref: "owasp.asvs:V3.1.1", Content: "The CLI renders no HTML.", AddedBy: "github:alice", AddedAt: "2026-11-07", Revision: 0},
	{ID: "REALM/002", Type: "rationale", Content: "Why this realm exists.", AddedBy: "github:alice", AddedAt: "2026-11-07", Revision: 1},
}

// testStaleHash is a well-formed manifest hash (RLMMNFST-013) that matches no
// manifest, so the first write must replace it.
const testStaleHash = "blake3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// addNotesAndHash adds testLinkedStandard, testRealmNotes and testStaleHash to
// the realm.json at path, as a hand edit would.
func addNotesAndHash(t *testing.T, path string) {
	t.Helper()
	var manifest map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["realm"].(map[string]any)["standards"] = []entity.StandardRef{testLinkedStandard}
	manifest["notes"] = testRealmNotes
	manifest["hash"] = testStaleHash
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// manifestHashOf returns the hash the realm.json at path declares and the one
// its content hashes to.
func manifestHashOf(t *testing.T, path string) (declared, computed string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rf, err := entity.ParseRealm(data)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	computed, err = entity.ComputeRealmHash(data)
	if err != nil {
		t.Fatal(err)
	}
	return rf.Hash, computed
}

// assertNotesKeptHashCurrent fails unless the realm.json at path still holds
// testRealmNotes and declares the hash of its content after step.
func assertNotesKeptHashCurrent(t *testing.T, path, step string) {
	t.Helper()
	rf, err := entity.LoadRealm(path)
	if err != nil {
		t.Fatalf("%s: loading realm.json: %v", step, err)
	}
	if !reflect.DeepEqual(rf.Notes, testRealmNotes) {
		t.Errorf("%s: notes = %+v, want %+v", step, rf.Notes, testRealmNotes)
	}
	declared, computed := manifestHashOf(t, path)
	if declared != computed {
		t.Errorf("%s: hash = %q, want the hash of the content, %q", step, declared, computed)
	}
}

// TestRealmWritesKeepNotesAndRecomputeHash runs each command that rewrites
// realm.json on a manifest with Realm Notes and a stale hash, and checks the
// notes survive and the hash is recomputed (RLMMNFST-014). realm dep upgrade,
// which needs its release lookup stubbed, is
// TestRealmDepUpgradeKeepsNotesAndRecomputesHash.
func TestRealmWritesKeepNotesAndRecomputeHash(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	path := filepath.Join(dir, "realm.json")
	addNotesAndHash(t, path)

	steps := [][]string{
		{"realm", "dep", "add", "dev.steerspec.core@1.21.0"},
		{"realm", "dep", "add", "dev.lib@1.0.0"},
		{"realm", "dep", "remove", "dev.lib"},
		{"realm", "contributor", "add", "github:alice"},
		{"realm", "contributor", "add", "github:bob"},
		{"realm", "contributor", "remove", "github:bob"},
		{"realm", "standards", "update", "--id", "owasp.asvs", "--url", "https://owasp.org/asvs"},
		{"realm", "standards", "add", "--id", "ietf.rfc2119", "--relation", "implements"},
		{"realm", "standards", "remove", "--id", "ietf.rfc2119"},
	}
	var hashes []string
	for _, step := range steps {
		if out, err := testutil.ExecuteCommand(NewRootCmd(), append(step, "--dir", dir)...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(step, " "), err, out)
		}
		assertNotesKeptHashCurrent(t, path, strings.Join(step, " "))
		h, _ := manifestHashOf(t, path)
		hashes = append(hashes, h)
	}
	if hashes[0] == testStaleHash {
		t.Error("the first write kept the stale hash")
	}
	// dep add then remove of dev.lib gives back the manifest it started from.
	if hashes[0] != hashes[2] {
		t.Errorf("hash after dep remove = %s, want %s as before dep add", hashes[2], hashes[0])
	}
	if hashes[0] == hashes[1] {
		t.Error("dep add left the hash unchanged")
	}

	// The core 1.21.0 schemas, and the manifest checks (RM023, RM025), accept
	// the manifest the writes leave.
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate rejects the manifest: %v\n%s", err, out)
	}
}

// TestRealmDepUpgradeKeepsNotesAndRecomputesHash upgrades a dependency of a
// manifest with Realm Notes and a stale hash, and checks the notes survive and
// the hash is recomputed.
func TestRealmDepUpgradeKeepsNotesAndRecomputesHash(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	path := filepath.Join(dir, "realm.json")
	addNotesAndHash(t, path)

	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	if deps := readRealmDeps(t, dir); deps[0].Version != "9.9.9" {
		t.Fatalf("core dep not upgraded: %+v", deps[0])
	}
	assertNotesKeptHashCurrent(t, path, "realm dep upgrade")
}

// TestRealmWritesLeaveNotesAndHashAbsent checks that a manifest with neither
// gains no empty notes or hash key: the commands keep a hash current, they
// don't add one.
func TestRealmWritesLeaveNotesAndHashAbsent(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "add", "github:alice", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"notes"`, `"hash"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("an absent %s must stay absent:\n%s", key, data)
		}
	}
}
