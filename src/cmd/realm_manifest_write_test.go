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

// testRealmNotes are Realm Notes (NTE-029) for a manifest under test.
var testRealmNotes = []entity.RealmNote{
	{ID: "REALM/001", Type: "exclusion", Ref: "owasp.asvs:V3.1.1", Content: "The CLI renders no HTML.", AddedBy: "github:alice", AddedAt: "2026-11-07", Revision: 0},
	{ID: "REALM/002", Type: "rationale", Content: "Why this realm exists.", AddedBy: "github:alice", AddedAt: "2026-11-07", Revision: 1},
}

// testRealmHash is a well-formed manifest hash (RLMMNFST-013). Nothing
// recomputes it yet, so every write must keep it as written.
const testRealmHash = "blake3:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// TestRealmWritesKeepNotesAndHash runs each command that rewrites realm.json
// on a manifest with Realm Notes and a hash, and checks both survive.
func TestRealmWritesKeepNotesAndHash(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	path := filepath.Join(dir, "realm.json")

	var manifest map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["notes"] = testRealmNotes
	manifest["hash"] = testRealmHash
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	steps := [][]string{
		{"realm", "dep", "add", "dev.steerspec.core@1.21.0"},
		{"realm", "dep", "add", "dev.lib@1.0.0"},
		{"realm", "dep", "remove", "dev.lib"},
		{"realm", "contributor", "add", "github:alice"},
		{"realm", "contributor", "add", "github:bob"},
		{"realm", "contributor", "remove", "github:bob"},
		{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "4.0.3"},
		{"realm", "standards", "update", "--id", "owasp.asvs", "--version", "5.0.0"},
		{"realm", "standards", "add", "--id", "ietf.rfc2119", "--relation", "implements"},
		{"realm", "standards", "remove", "--id", "ietf.rfc2119"},
	}
	for _, step := range steps {
		if out, err := testutil.ExecuteCommand(NewRootCmd(), append(step, "--dir", dir)...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(step, " "), err, out)
		}
		rf, err := entity.LoadRealm(path)
		if err != nil {
			t.Fatalf("%s: loading realm.json: %v", strings.Join(step, " "), err)
		}
		if !reflect.DeepEqual(rf.Notes, testRealmNotes) {
			t.Errorf("%s: notes = %+v, want %+v", strings.Join(step, " "), rf.Notes, testRealmNotes)
		}
		if rf.Hash != testRealmHash {
			t.Errorf("%s: hash = %q, want %q", strings.Join(step, " "), rf.Hash, testRealmHash)
		}
	}

	// The core 1.21.0 schemas accept the manifest the writes leave.
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate rejects the manifest: %v\n%s", err, out)
	}
}

// TestRealmWritesLeaveNotesAndHashAbsent checks that a manifest with neither
// gains no empty notes or hash key.
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
