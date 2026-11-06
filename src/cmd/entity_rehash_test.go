package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// setupEntityFileHash writes an entity file with the given hash pointer (nil = null).
func setupEntityFileHash(t *testing.T, dir, entityID string, hash *string) {
	t.Helper()
	f := entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: entityID, Title: "Test"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z", Hash: hash},
		Rules:   []entity.Rule{makeRule(entityID+"-001", "D", "Rule", "github:alice")},
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
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "github:alice"))
	// Null the sub-entity hash by hand, as a bulk import would leave it.
	f := loadEntityFile(t, dir, "TST")
	f.SubEntities[0].RuleSet.Hash = nil
	if err := replaceEntityFile(resolveEntityPath(dir, "TST"), f); err != nil {
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

func TestEntityRehashFreshParentWithNullChildIsRewritten(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Rule", "github:alice"))
	// Make the parent hash correct while the child hash is null: the
	// contract is that the file is still rewritten to repair the child.
	f := loadEntityFile(t, dir, "TST")
	f.SubEntities[0].RuleSet.Hash = nil
	if err := entityops.UpdateMeta(f); err != nil {
		t.Fatal(err)
	}
	path := resolveEntityPath(dir, "TST")
	if err := replaceEntityFile(path, f); err != nil {
		t.Fatal(err)
	}
	assertFileHashFresh(t, dir, "TST")
	parentVersion := f.RuleSet.Version

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "1 entity file(s) updated")

	f = loadEntityFile(t, dir, "TST")
	if f.SubEntities[0].RuleSet.Hash == nil {
		t.Error("child hash should be filled")
	}
	if f.RuleSet.Version != parentVersion {
		t.Errorf("rehash must not bump versions: %s → %s", parentVersion, f.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "TST")
}

// TestEntityRehashAllSkipsNonEntityJSON guards against rewriting unrelated JSON:
// entity.Parse only unmarshals, so a file without entity.id would otherwise be
// saved back as an empty entity.
func TestEntityRehashAllSkipsNonEntityJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileHash(t, dir, "AAA", nil)
	unrelated := filepath.Join(dir, "metadata.json")
	original := []byte(`{"name":"preserve-me"}` + "\n")
	if err := os.WriteFile(unrelated, original, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--all", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Updated []struct {
			Entity string `json:"entity"`
		} `json:"updated"`
		Skipped []string `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(result.Updated) != 1 || result.Updated[0].Entity != "AAA" {
		t.Errorf("unexpected updated list: %+v", result.Updated)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != unrelated {
		t.Errorf("expected skipped [%s], got %v", unrelated, result.Skipped)
	}
	after, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Errorf("unrelated JSON was rewritten:\n%s", after)
	}
	assertFileHashFresh(t, dir, "AAA")
}

func TestEntityRehashRejectsNonEntityJSON(t *testing.T) {
	dir := t.TempDir()
	unrelated := filepath.Join(dir, "metadata.json")
	original := []byte(`{"name":"preserve-me"}` + "\n")
	if err := os.WriteFile(unrelated, original, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "metadata", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "not an entity file") {
		t.Fatalf("expected 'not an entity file' error, got %v", err)
	}
	after, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Errorf("unrelated JSON was rewritten:\n%s", after)
	}
}

// TestEntityRehashAllSkipsLinks checks that --all lists entity files that are
// symbolic links and never follows them: a link to a stale file outside --dir
// leaves it untouched, a link that points nowhere is no error, and a link to a
// stale file inside --dir doesn't rehash that file a second time.
func TestEntityRehashAllSkipsLinks(t *testing.T) {
	outside := t.TempDir()
	setupEntityFileHash(t, outside, "OUT", nil)
	outsideBefore := readFiles(t, outside, "OUT.json")

	dir := t.TempDir()
	setupEntityFileHash(t, dir, "AAA", nil)
	links := []string{filepath.Join(dir, "LNK.json"), filepath.Join(dir, "DNG.json"), filepath.Join(dir, "SIB.json")}
	targets := []string{filepath.Join(outside, "OUT.json"), filepath.Join(outside, "missing.json"), filepath.Join(dir, "AAA.json")}
	for i, link := range links {
		if err := os.Symlink(targets[i], link); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--all", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res rehashResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	wantLinks := []string{links[1], links[0], links[2]} // walk order: DNG, LNK, SIB
	if strings.Join(res.Links, ",") != strings.Join(wantLinks, ",") {
		t.Errorf("links = %v, want %v", res.Links, wantLinks)
	}
	if len(res.Updated) != 1 || res.Updated[0].Path != filepath.Join(dir, "AAA.json") {
		t.Errorf("updated = %+v, want only AAA once", res.Updated)
	}
	if !bytes.Equal(readFiles(t, outside, "OUT.json")["OUT.json"], outsideBefore["OUT.json"]) {
		t.Error("the file outside --dir was rewritten through a link")
	}
	assertFileHashFresh(t, dir, "AAA")

	human, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--all", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, human, "a symbolic link, not followed")
	testutil.AssertContains(t, human, "3 skipped")
}

// TestEntityRehashAllLinksNeverNull checks that --json reports links as [] when
// there are none.
func TestEntityRehashAllLinksNeverNull(t *testing.T) {
	dir := t.TempDir()
	writeFreshEntity(t, dir, "AAA")
	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--all", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, `"links": []`)
}

// TestEntityRehashRefusesLink checks that the single-file form fails on an
// entity file that is a symbolic link and leaves its target untouched.
func TestEntityRehashRefusesLink(t *testing.T) {
	outside := t.TempDir()
	setupEntityFileHash(t, outside, "OUT", nil)
	before := readFiles(t, outside, "OUT.json")
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "OUT.json"), filepath.Join(dir, "LNK.json")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "LNK", "--dir", dir)
	if err == nil {
		t.Fatal("want an error for a linked entity file, got none")
	}
	testutil.AssertContains(t, err.Error(), "symbolic link")
	if !bytes.Equal(readFiles(t, outside, "OUT.json")["OUT.json"], before["OUT.json"]) {
		t.Error("the file outside --dir was rewritten through a link")
	}
}

// TestReplaceEntityFileNeverFollowsLink checks the write used by rehash: a
// link swapped in for an entity file after the link check is replaced by a
// regular file, and the file it pointed to is unchanged.
func TestReplaceEntityFileNeverFollowsLink(t *testing.T) {
	outside := t.TempDir()
	setupEntityFileHash(t, outside, "OUT", nil)
	before := readFiles(t, outside, "OUT.json")
	dir := t.TempDir()
	path := filepath.Join(dir, "TST.json")
	if err := os.Symlink(filepath.Join(outside, "OUT.json"), path); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	f := &entity.File{Entity: entity.Entity{ID: "TST", Title: "Test"}}
	if err := replaceEntityFile(path, f); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link, err := isLinkPath(path); err != nil || link {
		t.Errorf("%s is still a link (err %v)", path, err)
	}
	if !bytes.Equal(readFiles(t, outside, "OUT.json")["OUT.json"], before["OUT.json"]) {
		t.Error("the file outside --dir was written through the link")
	}
	if got := loadEntityFile(t, dir, "TST"); got.Entity.ID != "TST" {
		t.Errorf("entity id = %q, want TST", got.Entity.ID)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}
}

// TestReplaceEntityFileKeepsMode checks that rewriting keeps the file's
// permissions.
func TestReplaceEntityFileKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not kept on Windows")
	}
	dir := t.TempDir()
	setupEntityFileHash(t, dir, "TST", nil)
	path := filepath.Join(dir, "TST.json")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceEntityFile(path, loadEntityFile(t, dir, "TST")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// TestEntityRehashMissingFile checks that the single-file form fails on an
// entity file that isn't there, and creates nothing.
func TestEntityRehashMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "TST", "--dir", dir)
	if err == nil {
		t.Fatal("want an error for a missing entity file, got none")
	}
	testutil.AssertContains(t, err.Error(), "reading entity file")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("rehash created files: %v", entries)
	}
}
