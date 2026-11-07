package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// setupRehashRealm writes a realm at dir pinning dev.steerspec.core at core,
// with no manifest hash, and returns the path of its realm.json.
func setupRehashRealm(t *testing.T, dir, core string) string {
	t.Helper()
	setupValidRealm(t, dir)
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@"+core, "--dir", dir); err != nil {
		t.Fatalf("dep add: %v\n%s", err, out)
	}
	return filepath.Join(dir, "realm.json")
}

// runRealmRehash runs realm rehash --json on dir and decodes its output.
func runRealmRehash(t *testing.T, dir string) realmRehashResult {
	t.Helper()
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("realm rehash: %v\n%s", err, out)
	}
	var res realmRehashResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	return res
}

// TestRealmRehashAddsHashThenLeavesItAlone adds a hash to a manifest without
// one, then checks a second run changes nothing, byte for byte.
func TestRealmRehashAddsHashThenLeavesItAlone(t *testing.T) {
	dir := t.TempDir()
	path := setupRehashRealm(t, dir, "1.21.0")

	res := runRealmRehash(t, dir)
	if len(res.Updated) != 1 || res.Unchanged != 0 {
		t.Fatalf("first run = %+v, want one manifest updated", res)
	}
	c := res.Updated[0]
	if c.Realm != "dev.steerspec.test" || c.Path != path || c.OldHash != nil {
		t.Errorf("change = %+v, want dev.steerspec.test at %s with no old hash", c, path)
	}
	declared, computed := manifestHashOf(t, path)
	if declared != computed || declared != c.NewHash {
		t.Errorf("hash = %s, new_hash = %s, want both %s", declared, c.NewHash, computed)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res = runRealmRehash(t, dir)
	if len(res.Updated) != 0 || res.Unchanged != 1 {
		t.Errorf("second run = %+v, want one manifest unchanged", res)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("a current manifest was rewritten:\n%s\nwas\n%s", after, before)
	}
}

// TestRealmRehashAfterHandEdit changes a field of a hashed manifest without
// the CLI: the hash check (RM025) fails, and rehash makes it pass again.
func TestRealmRehashAfterHandEdit(t *testing.T) {
	dir := t.TempDir()
	path := setupRehashRealm(t, dir, "1.21.0")
	runRealmRehash(t, dir)
	hashed, _ := manifestHashOf(t, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"version": "0.1.0"`, `"version": "0.2.0"`, 1)
	if edited == string(data) {
		t.Fatalf("realm.version not found in:\n%s", data)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if declared, computed := manifestHashOf(t, path); declared == computed {
		t.Fatal("the hand edit left the hash matching")
	}
	out, err := validateWithSchemas(t, dir)
	if err == nil || !strings.Contains(out+err.Error(), "RM025") {
		t.Fatalf("realm validate = %v, want RM025\n%s", err, out)
	}

	res := runRealmRehash(t, dir)
	if len(res.Updated) != 1 || res.Updated[0].OldHash == nil || *res.Updated[0].OldHash != hashed {
		t.Fatalf("rehash = %+v, want one update from %s", res, hashed)
	}
	if declared, computed := manifestHashOf(t, path); declared != computed {
		t.Errorf("hash = %s after rehash, want %s", declared, computed)
	}
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate after rehash: %v\n%s", err, out)
	}
}

// TestRealmRehashOldCore refuses to add a hash under a core pin whose realm
// schema rejects it, and leaves realm.json as it was.
func TestRealmRehashOldCore(t *testing.T) {
	dir := t.TempDir()
	path := setupRehashRealm(t, dir, "1.20.0")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error for a core 1.20.0 pin")
	}
	testutil.AssertContains(t, err.Error(), "reject `hash`")
	testutil.AssertContains(t, err.Error(), manifestMinCore.String())
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("realm.json changed:\n%s", after)
	}
}

// TestRealmRehashSubRealms hashes the manifest of each sub-realm too.
func TestRealmRehashSubRealms(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@1.21.0", "--dir", dir); err != nil {
		t.Fatalf("dep add: %v\n%s", err, out)
	}

	res := runRealmRehash(t, dir)
	if len(res.Updated) != 2 {
		t.Fatalf("updated = %+v, want the realm and its sub-realm", res.Updated)
	}
	for _, p := range []string{filepath.Join(dir, "realm.json"), filepath.Join(dir, "auth", "realm.json")} {
		if declared, computed := manifestHashOf(t, p); declared != computed {
			t.Errorf("%s: hash = %q, want %s", p, declared, computed)
		}
	}
}

// TestRealmRehashRejectsSubRealmOutsideDir refuses a sub_realms entry that
// isn't a directory name, so the walk never leaves --dir.
func TestRealmRehashRejectsSubRealmOutsideDir(t *testing.T) {
	dir := t.TempDir()
	path := setupRehashRealm(t, dir, "1.21.0")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["sub_realms"] = []string{"../elsewhere"}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error for sub-realm ../elsewhere")
	}
	testutil.AssertContains(t, err.Error(), `"../elsewhere"`)
}

// TestRealmRehashSkipsLink lists a realm.json that is a symbolic link and
// leaves its target alone.
func TestRealmRehashSkipsLink(t *testing.T) {
	target := t.TempDir()
	targetPath := setupRehashRealm(t, target, "1.21.0")
	before, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	link := filepath.Join(dir, "realm.json")
	if err := os.Symlink(targetPath, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	res := runRealmRehash(t, dir)
	if len(res.Updated) != 0 || len(res.Links) != 1 || res.Links[0] != link {
		t.Errorf("result = %+v, want only %s listed as a link", res, link)
	}
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("the link's target was rewritten:\n%s", after)
	}
}

// TestRealmRehashHumanOutput checks the human summary of a run.
func TestRealmRehashHumanOutput(t *testing.T) {
	dir := t.TempDir()
	setupRehashRealm(t, dir, "1.21.0")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Rehashed realm dev.steerspec.test")
	testutil.AssertContains(t, out, "none → blake3:")
	testutil.AssertContains(t, out, "1 manifest(s) updated, 0 unchanged, 0 skipped")

	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "0 manifest(s) updated, 1 unchanged, 0 skipped")
}

// TestRealmRehashListsDanglingLink lists a realm.json link to nothing rather
// than reporting it as missing.
func TestRealmRehashListsDanglingLink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "realm.json")
	if err := os.Symlink(filepath.Join(dir, "gone.json"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	res := runRealmRehash(t, dir)
	if len(res.Links) != 1 || res.Links[0] != link {
		t.Errorf("links = %v, want [%s]", res.Links, link)
	}
}

// TestRealmRehashSkipsLinkedSubRealm lists a sub-realm directory that is a
// symbolic link and never writes the manifest it leads to, which lies outside
// --dir.
func TestRealmRehashSkipsLinkedSubRealm(t *testing.T) {
	outside := t.TempDir()
	setupSubRealm(t, outside, "auth", "dev.steerspec.test")
	outsidePath := filepath.Join(outside, "auth", "realm.json")
	before, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	setupValidRealm(t, dir)
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@1.21.0", "--dir", dir); err != nil {
		t.Fatalf("dep add: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "realm.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["sub_realms"] = []string{"auth"}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	subLink := filepath.Join(dir, "auth")
	if err := os.Symlink(filepath.Join(outside, "auth"), subLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	res := runRealmRehash(t, dir)
	if len(res.Updated) != 1 || res.Updated[0].Path != path {
		t.Errorf("updated = %+v, want only %s", res.Updated, path)
	}
	if len(res.Links) != 1 || res.Links[0] != subLink {
		t.Errorf("links = %v, want [%s]", res.Links, subLink)
	}
	after, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("the manifest outside --dir was rewritten:\n%s", after)
	}
}

// TestRealmRehashRejectsSubRealmFile refuses a sub-realm entry naming a file.
func TestRealmRehashRejectsSubRealmFile(t *testing.T) {
	dir := t.TempDir()
	path := setupRehashRealm(t, dir, "1.21.0")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["sub_realms"] = []string{"notes.txt"}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error for a sub-realm that is a file")
	}
	testutil.AssertContains(t, err.Error(), "is not a directory")
}

// TestReplaceFileInRefusesSwappedLink swaps a directory for a link leading
// out of the root after the walk would have checked it: the write through the
// root fails, and the file outside is left alone.
func TestReplaceFileInRefusesSwappedLink(t *testing.T) {
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "realm.json")
	if err := os.WriteFile(outsidePath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	if err := os.Remove(filepath.Join(dir, "auth")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "auth")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := replaceFileIn(root, filepath.Join("auth", "realm.json"), []byte(`{"x":1}`), "realm.json"); err == nil {
		t.Fatal("expected the write through a link out of the root to fail")
	}
	if data, err := os.ReadFile(outsidePath); err != nil || string(data) != "{}" {
		t.Errorf("the file outside the root was written: %q (err %v)", data, err)
	}
}

// TestRealmRehashVisitsEachDirOnce enters a directory reached twice only once,
// as it would one swapped for a link back into the tree.
func TestRealmRehashVisitsEachDirOnce(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@1.21.0", "--dir", dir); err != nil {
		t.Fatalf("dep add: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "realm.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["sub_realms"] = []string{"auth", "auth"}
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	res := runRealmRehash(t, dir)
	if len(res.Updated) != 2 || res.Unchanged != 0 {
		t.Errorf("result = %+v, want the realm and its sub-realm updated once each", res)
	}
}

// TestRealmRehashMissingDir reports a --dir that doesn't exist as a missing
// Realm.
func TestRealmRehashMissingDir(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected an error for a missing --dir")
	}
	testutil.AssertContains(t, err.Error(), "missing realm.json")
}
