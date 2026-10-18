package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// setupEntityFileHash writes an entity file with the given hash pointer (nil = null).
func setupEntityFileHash(t *testing.T, dir, entityID string, hash *string) {
	t.Helper()
	f := entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: entityID, Title: "Test"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z", Hash: hash},
		Rules:   []entity.Rule{makeRule(entityID+"-001", "D", "Rule", "@alice")},
		Notes:   []entity.Note{},
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, entityID+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeFreshEntity writes an entity file whose hash is already correct.
func writeFreshEntity(t *testing.T, dir, entityID string) {
	t.Helper()
	setupEntityFileHash(t, dir, entityID, nil)
	data, _ := os.ReadFile(filepath.Join(dir, entityID+".json"))
	h, err := entity.ComputeHash(data)
	if err != nil {
		t.Fatal(err)
	}
	setupEntityFileHash(t, dir, entityID, &h)
	assertFileHashFresh(t, dir, entityID)
}

func TestEntityRehashNullHash(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileHash(t, dir, "TST", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "TST")
	testutil.AssertContains(t, out, "null")
	assertFileHashFresh(t, dir, "TST")

	f := loadEntityFile(t, dir, "TST")
	if f.RuleSet.Timestamp == "2026-01-01T00:00:00Z" {
		t.Error("timestamp should be refreshed when hash changes")
	}
	if f.RuleSet.Version != "0.1.0" {
		t.Errorf("rehash must not bump version, got %s", f.RuleSet.Version)
	}
}

func TestEntityRehashStaleHash(t *testing.T) {
	dir := t.TempDir()
	stale := "blake3:0000000000000000000000000000000000000000000000000000000000000000"
	setupEntityFileHash(t, dir, "TST", &stale)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "blake3:0000")
	assertFileHashFresh(t, dir, "TST")
}

func TestEntityRehashFreshIsNoop(t *testing.T) {
	dir := t.TempDir()
	writeFreshEntity(t, dir, "TST")
	before, _ := os.ReadFile(filepath.Join(dir, "TST.json"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "unchanged")

	after, _ := os.ReadFile(filepath.Join(dir, "TST.json"))
	if !bytes.Equal(before, after) {
		t.Error("fresh entity file must not be rewritten")
	}
}

func TestEntityRehashAll(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileHash(t, dir, "AAA", nil)
	writeFreshEntity(t, dir, "BBB")
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema", "x.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(`{"realm":{"id":"x","title":"x","version":"0.1.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--all", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Updated []struct {
			Entity  string  `json:"entity"`
			OldHash *string `json:"old_hash"`
			NewHash string  `json:"new_hash"`
		} `json:"updated"`
		Unchanged int `json:"unchanged"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(result.Updated) != 1 || result.Updated[0].Entity != "AAA" || result.Updated[0].OldHash != nil ||
		!strings.HasPrefix(result.Updated[0].NewHash, "blake3:") {
		t.Errorf("unexpected updated list: %+v", result.Updated)
	}
	if result.Unchanged != 1 {
		t.Errorf("expected 1 unchanged, got %d", result.Unchanged)
	}
	assertFileHashFresh(t, dir, "AAA")
}

func TestEntityRehashArgs(t *testing.T) {
	dir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--dir", dir); err == nil {
		t.Error("expected error when neither EUID nor --all given")
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--all", "--dir", dir); err == nil {
		t.Error("expected error when both EUID and --all given")
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "NOPE", "--dir", dir); err == nil {
		t.Error("expected error for missing entity file")
	}
}

func TestEntityRehashHelp(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "--all")
	testutil.AssertContains(t, out, "--dir")
}

func TestEntityRehashFillsSubEntityHashes(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "@alice"))
	// Null the sub-entity hash by hand, as a bulk import would leave it.
	f := loadEntityFile(t, dir, "TST")
	f.SubEntities[0].RuleSet.Hash = nil
	if err := saveEntity(resolveEntityPath(dir, "TST"), f); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Rehashed TST")

	f = loadEntityFile(t, dir, "TST")
	sub := f.SubEntities[0]
	if sub.RuleSet.Hash == nil {
		t.Fatal("sub-entity hash should be filled")
	}
	data, err := json.Marshal(sub)
	if err != nil {
		t.Fatal(err)
	}
	want, err := entity.ComputeHash(data)
	if err != nil {
		t.Fatal(err)
	}
	if *sub.RuleSet.Hash != want {
		t.Errorf("sub-entity hash = %s, want %s", *sub.RuleSet.Hash, want)
	}
	assertFileHashFresh(t, dir, "TST")
}
