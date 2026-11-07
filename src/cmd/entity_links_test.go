package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// TestEntityWritersRefuseLinks checks that the commands which edit an entity
// file refuse one that is a symbolic link, before reading it: the file it
// points to, outside --dir, is unchanged and the link stays a link.
func TestEntityWritersRefuseLinks(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"rule add", []string{"rule", "add", "TST", "--body", "The system MUST log.", "--added-by", "github:alice"}},
		{"rule update", []string{"rule", "update", "TST-001", "--body", "The system MUST log twice."}},
		{"note add", []string{"note", "add", "TST-001", "--type", "rationale", "--body", "Why.", "--added-by", "github:alice"}},
		{"entity add", []string{"entity", "add", "TSTSUB", "--parent", "TST", "--title", "Sub"}},
		{"entity standards add", []string{"entity", "standards", "add", "TST", "--id", "ietf.rfc2119", "--relation", "derived_from"}},
	}
	for _, c := range commands {
		t.Run(c.name, func(t *testing.T) {
			outside := t.TempDir()
			setupEntityFile(t, outside, "TST", makeRule("TST-001", "D", "The system MUST validate.", "github:alice"))
			before := readFiles(t, outside, "TST.json")
			dir := t.TempDir()
			link := filepath.Join(dir, "TST.json")
			if err := os.Symlink(filepath.Join(outside, "TST.json"), link); err != nil {
				t.Skipf("cannot create a symlink here: %v", err)
			}

			_, err := testutil.ExecuteCommand(NewRootCmd(), append(c.args, "--dir", dir)...)
			if err == nil {
				t.Fatal("want an error for a linked entity file, got none")
			}
			testutil.AssertContains(t, err.Error(), "symbolic link")
			if !bytes.Equal(readFiles(t, outside, "TST.json")["TST.json"], before["TST.json"]) {
				t.Error("the file outside --dir was written through the link")
			}
			if isLink, err := isLinkPath(link); err != nil || !isLink {
				t.Errorf("%s is no longer a link (err %v)", link, err)
			}
		})
	}
}

// TestEntityWriterLeavesNoTempFile checks that an ordinary edit leaves only
// the entity file behind.
func TestEntityWriterLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "D", "The system MUST validate.", "github:alice"))
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST", "--body", "The system MUST log.", "--added-by", "github:alice", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "TST.json" {
		t.Errorf("files left in --dir: %v", entries)
	}
	assertFileHashFresh(t, dir, "TST")
}

// TestEntityTargetRefusesLinks checks target resolution: a link that points
// nowhere is refused as a link, not reported as a missing entity, and a
// sub-entity found in a linked file is refused before it is edited.
func TestEntityTargetRefusesLinks(t *testing.T) {
	t.Run("dangling link", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing.json"), filepath.Join(dir, "TST.json")); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST", "--body", "The system MUST log.", "--added-by", "github:alice", "--dir", dir)
		if err == nil {
			t.Fatal("want an error, got none")
		}
		testutil.AssertContains(t, err.Error(), "symbolic link")
	})
	t.Run("sub-entity in a linked file", func(t *testing.T) {
		outside := t.TempDir()
		setupEntityFileWithSub(t, outside, "TST", "TSTSUB")
		before := readFiles(t, outside, "TST.json")
		dir := t.TempDir()
		if err := os.Symlink(filepath.Join(outside, "TST.json"), filepath.Join(dir, "TST.json")); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TSTSUB", "--body", "The system MUST log.", "--added-by", "github:alice", "--dir", dir)
		if err == nil {
			t.Fatal("want an error, got none")
		}
		testutil.AssertContains(t, err.Error(), "symbolic link")
		if !bytes.Equal(readFiles(t, outside, "TST.json")["TST.json"], before["TST.json"]) {
			t.Error("the file outside --dir was written through the link")
		}
	})
}

// TestEntityStandardsListFollowsLinks checks that the read-only list command
// still reads a linked entity file, top-level or holding a sub-entity.
func TestEntityStandardsListFollowsLinks(t *testing.T) {
	outside := t.TempDir()
	setupEntityFileWithSub(t, outside, "TST", "TSTSUB")
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "TST.json"), filepath.Join(dir, "TST.json")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	for _, euid := range []string{"TST", "TSTSUB"} {
		out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "list", euid, "--json", "--dir", dir)
		if err != nil {
			t.Fatalf("list %s: %v", euid, err)
		}
		testutil.AssertContains(t, out, `"entity": "`+euid+`"`)
	}
}
