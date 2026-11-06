package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// TestCommandsRejectPathEUID checks that every command naming an entity by
// EUID, directly or through a rule or note ID, rejects one that isn't an EUID
// before touching a file. OUTSIDE.json sits next to --dir with a Draft rule, so
// each command would succeed on it if the EUID reached the path.
func TestCommandsRejectPathEUID(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "rules")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	setupEntityFile(t, root, "OUTSIDE", makeRule("OUTSIDE-001", "D", "MUST x.", "github:test"))
	outside := filepath.Join(root, "OUTSIDE.json")
	before, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"rule", "add", "../OUTSIDE", "--body", "MUST y.", "--added-by", "github:test"},
		{"rule", "update", "../OUTSIDE-001", "--body", "MUST y."},
		{"rule", "promote", "../OUTSIDE-001"},
		{"rule", "retire", "../OUTSIDE-001"},
		{"rule", "abandon", "../OUTSIDE-001"},
		{"rule", "supersede", "../OUTSIDE-001", "--body", "MUST y.", "--added-by", "github:test"},
		{"note", "add", "../OUTSIDE-001", "--type", "reference", "--body", "x", "--added-by", "github:test"},
		{"note", "update", "..-001/01", "--body", "x"},
		{"entity", "rehash", "../OUTSIDE"},
		{"entity", "standards", "add", "../OUTSIDE", "--id", "iso.27001", "--relation", "implements"},
		{"entity", "standards", "remove", "../OUTSIDE", "--id", "iso.27001"},
		{"entity", "standards", "update", "../OUTSIDE", "--id", "iso.27001", "--version", "1"},
		{"entity", "standards", "list", "../OUTSIDE"},
	} {
		name := strings.Join(args[:2], " ")
		_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
		if err == nil || !strings.Contains(err.Error(), "invalid EUID") {
			t.Errorf("%s: error = %v, want invalid EUID", name, err)
		}
	}

	after, _ := os.ReadFile(outside)
	if !bytes.Equal(before, after) {
		t.Error("file outside --dir was rewritten")
	}
}
