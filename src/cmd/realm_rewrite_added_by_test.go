package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestMappedHandle(t *testing.T) {
	m := map[string]string{"@Alice": "github:alice", "bob@example.com": "github:bob"}
	tests := []struct {
		addedBy string
		want    string
		ok      bool
	}{
		{"@Alice", "github:alice", true},        // exact key
		{"@alice", "github:alice", true},        // a handle in another case
		{"bob@example.com", "github:bob", true}, // an exact non-handle key
		{"BOB@example.com", "", false},          // only handles ignore case
		{"@carol", "", false},                   // unmapped handle
	}
	for _, tt := range tests {
		got, ok := mappedHandle(m, tt.addedBy)
		if got != tt.want || ok != tt.ok {
			t.Errorf("mappedHandle(%q) = %q, %v; want %q, %v", tt.addedBy, got, ok, tt.want, tt.ok)
		}
	}
}

// TestRealmRewriteAddedByHumanOutput checks the human report: several updated
// files in path order, a skipped non-entity file, and the summary.
func TestRealmRewriteAddedByHumanOutput(t *testing.T) {
	dir := rewriteFixture(t)
	setupEntityFile(t, dir, "AAA", makeRule("AAA-001", "D", "The system MUST run.", "@carol"))
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Rewrote 1 added_by in AAA")
	testutil.AssertContains(t, out, "Rewrote 3 added_by in TST")
	testutil.AssertContains(t, out, "Entity version: 0.1.0 → 0.1.1")
	testutil.AssertContains(t, out, "metadata.json (not an entity file)")
	testutil.AssertContains(t, out, "4 added_by in 2 entity file(s) rewritten, 1 unchanged, 1 skipped")
	if strings.Index(out, "in AAA") > strings.Index(out, "in TST") {
		t.Errorf("updated files are not in path order:\n%s", out)
	}
	if got := loadEntityFile(t, dir, "AAA").Rules[0].AddedBy; got != "github:carol" {
		t.Errorf("AAA-001 added_by = %q, want github:carol", got)
	}
}

func TestRealmRewriteAddedByHumanLinks(t *testing.T) {
	outside := rewriteFixture(t)
	dir := rewriteFixture(t)
	if err := os.Symlink(filepath.Join(outside, "TST.json"), filepath.Join(dir, "LNK.json")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "LNK.json (a symbolic link, not followed: rewrite its target where it lives)")
}

func TestRealmRewriteAddedByMalformedFile(t *testing.T) {
	dir := rewriteFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "BAD.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFiles(t, dir, "TST.json")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "BAD.json") {
		t.Fatalf("error = %v, want one naming BAD.json", err)
	}
	// The walk is in name order: BAD.json fails before TST.json is reached.
	if !bytes.Equal(readFiles(t, dir, "TST.json")["TST.json"], before["TST.json"]) {
		t.Error("TST.json was rewritten although the walk failed first")
	}
}
