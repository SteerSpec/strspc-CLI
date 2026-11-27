package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// setupNotesRealmTree writes a realm at dir with a Realm Note by github:alice,
// and a sub-realm auth with one too, both manifests hashed.
func setupNotesRealmTree(t *testing.T, dir string) {
	t.Helper()
	setupNoteRealm(t, dir)
	addRealmNote(t, dir, "--type", "rationale", "--body", "Why.", "--added-by", "github:alice")
	setupSubRealm(t, dir, "auth", "dev.steerspec.test")
	if err := loadModifyWriteRealm(context.Background(), filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.SubRealms = append(rf.SubRealms, "auth")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "auth")
	if err := loadModifyWriteRealm(context.Background(), filepath.Join(sub, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Notes = append(rf.Notes,
			entity.RealmNote{ID: "REALM/001", Type: "rationale", Content: "Sub.", AddedBy: "github:alice", AddedAt: "2026-11-01"},
			entity.RealmNote{ID: "REALM/002", Type: "rationale", Content: "Bob.", AddedBy: "github:bob", AddedAt: "2026-11-01"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rehash", "--dir", dir); err != nil {
		t.Fatalf("realm rehash: %v\n%s", err, out)
	}
}

func runRewriteJSON(t *testing.T, args ...string) addedByResult {
	t.Helper()
	out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "rewrite-added-by", "--json"}, args...)...)
	if err != nil {
		t.Fatalf("rewrite-added-by: %v\n%s", err, out)
	}
	var res addedByResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	return res
}

// TestRealmRewriteAddedByRealmNotes rewrites the added_by of Realm Notes in
// the realm and its sub-realms, keeps the manifest hash current and the
// version as it is, and leaves a manifest with nothing mapped untouched.
func TestRealmRewriteAddedByRealmNotes(t *testing.T) {
	dir := t.TempDir()
	setupNotesRealmTree(t, dir)
	top, sub := filepath.Join(dir, "realm.json"), filepath.Join(dir, "auth", "realm.json")
	before := map[string][]byte{}
	for _, p := range []string{top, sub} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = data
	}
	unchanged := func(p string) bool {
		data, err := os.ReadFile(p)
		return err == nil && bytes.Equal(data, before[p])
	}

	// A dry run reports both manifests and writes neither.
	res := runRewriteJSON(t, "--map", "github:alice=github:alice-gh", "--dir", dir, "--dry-run")
	if len(res.Manifests) != 2 || res.Manifests[0].Path != top || res.Manifests[0].Replaced != 1 ||
		res.Manifests[1].Path != sub || res.Manifests[1].Replaced != 1 || res.Manifests[1].Realm != "dev.steerspec.test.auth" {
		t.Fatalf("dry run manifests = %+v", res.Manifests)
	}
	if !unchanged(top) || !unchanged(sub) {
		t.Fatal("a dry run wrote a manifest")
	}

	// A map that names no Realm Note's author touches no manifest.
	if res := runRewriteJSON(t, "--map", "github:carol=github:dave", "--dir", dir); len(res.Manifests) != 0 {
		t.Errorf("manifests = %+v, want none", res.Manifests)
	}
	if !unchanged(top) || !unchanged(sub) {
		t.Fatal("a manifest with nothing mapped was rewritten")
	}

	res = runRewriteJSON(t, "--map", "github:alice=github:alice-gh", "--dir", dir)
	if len(res.Manifests) != 2 {
		t.Fatalf("manifests = %+v, want 2", res.Manifests)
	}
	for _, p := range []string{top, sub} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		rf, err := entity.ParseRealm(data)
		if err != nil {
			t.Fatal(err)
		}
		if rf.Notes[0].AddedBy != "github:alice-gh" {
			t.Errorf("%s: note added_by = %q, want github:alice-gh", p, rf.Notes[0].AddedBy)
		}
		if h, err := entity.ComputeRealmHash(data); err != nil || rf.Hash != h {
			t.Errorf("%s: hash %q, computed %q (%v)", p, rf.Hash, h, err)
		}
		old, _ := entity.ParseRealm(before[p])
		if rf.Realm.Version != old.Realm.Version {
			t.Errorf("%s: version %s → %s, want it unchanged", p, old.Realm.Version, rf.Realm.Version)
		}
	}
	if rf, _ := entity.LoadRealm(sub); rf.Notes[1].AddedBy != "github:bob" {
		t.Errorf("an unmapped note was rewritten: %+v", rf.Notes[1])
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--map", "github:alice-gh=github:alice", "--dir", dir)
	if err != nil {
		t.Fatalf("rewrite-added-by: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "Rewrote 1 added_by in the Realm Notes of dev.steerspec.test.auth")
	testutil.AssertContains(t, out, "and 2 realm.json rewritten")
}

// TestRealmRewriteAddedByRealmNotesHandles maps an @handle in a Realm Note
// with --github-handles, and needs no realm.json under --dir.
func TestRealmRewriteAddedByRealmNotesHandles(t *testing.T) {
	dir := t.TempDir()
	setupNotesRealmTree(t, dir)
	if err := loadModifyWriteRealm(context.Background(), filepath.Join(dir, "auth", "realm.json"), func(rf *entity.RealmFile) error {
		rf.Notes[1].AddedBy = "@Bob"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res := runRewriteJSON(t, "--github-handles", "--map", "@bob=github:bob", "--dir", dir)
	if len(res.Manifests) != 1 || res.Manifests[0].Realm != "dev.steerspec.test.auth" {
		t.Fatalf("manifests = %+v, want the sub-realm", res.Manifests)
	}
	if rf, _ := entity.LoadRealm(filepath.Join(dir, "auth", "realm.json")); rf.Notes[1].AddedBy != "github:bob" {
		t.Errorf("added_by = %q, want the --map for the handle in any case", rf.Notes[1].AddedBy)
	}

	// Entity files without a realm.json: nothing to do for Realm Notes.
	plain := rewriteFixture(t)
	if err := os.Remove(filepath.Join(plain, "realm.json")); err != nil {
		t.Fatal(err)
	}
	if res := runRewriteJSON(t, "--github-handles", "--dir", plain); len(res.Manifests) != 0 || len(res.Updated) != 1 {
		t.Errorf("result = %+v, want TST rewritten and no manifests", res)
	}
}

// TestRealmRewriteAddedByRealmNotesSubRealmLink lists a sub-realm directory
// that is a symbolic link and doesn't follow it.
func TestRealmRewriteAddedByRealmNotesSubRealmLink(t *testing.T) {
	outside := t.TempDir()
	setupNotesRealmTree(t, outside)
	beforeSub, err := os.ReadFile(filepath.Join(outside, "auth", "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	setupNotesRealmTree(t, dir)
	if err := os.RemoveAll(filepath.Join(dir, "auth")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "auth"), filepath.Join(dir, "auth")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	res := runRewriteJSON(t, "--map", "github:alice=github:alice-gh", "--dir", dir)
	if len(res.Manifests) != 1 || res.Manifests[0].Path != filepath.Join(dir, "realm.json") {
		t.Errorf("manifests = %+v, want only the top realm", res.Manifests)
	}
	found := false
	for _, l := range res.Links {
		found = found || l == filepath.Join(dir, "auth")
	}
	if !found {
		t.Errorf("links = %v, want the sub-realm directory", res.Links)
	}
	if after, _ := os.ReadFile(filepath.Join(outside, "auth", "realm.json")); !bytes.Equal(after, beforeSub) {
		t.Error("the sub-realm outside --dir was rewritten through the link")
	}
}
