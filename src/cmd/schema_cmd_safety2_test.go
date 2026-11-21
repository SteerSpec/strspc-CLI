package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestSchemaClearKeepsUnownedFiles(t *testing.T) {
	_, cache := serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", "--core-version", "1.2.3")
	assertExit(t, err, exitOK)
	// The cache was marked by fetch, but these files aren't schemas.
	writeRefsFile(t, cache, "config/settings.json.bak", "keep")
	writeRefsFile(t, cache, "versions/notes.txt", "keep")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	for _, p := range []string{"config/v1.json", "entity/v1.json", "versions/1.2.3/realm/v1.json"} {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(p))); err == nil {
			t.Errorf("%s survived clear", p)
		}
	}
	for _, p := range []string{"config/settings.json.bak", "versions/notes.txt"} {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(p))); err != nil {
			t.Errorf("clear removed %s, which isn't a schema: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cache, "entity")); err == nil {
		t.Error("clear left an empty entity/ directory")
	}
}

func TestSchemaUpdateRefusesSymlinkedParent(t *testing.T) {
	_, cache := serveSchemaCache(t)
	outside := t.TempDir()
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cache, "config")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "update")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "is not a plain directory of the schema cache")
	if _, err := os.Stat(filepath.Join(outside, "v1.json")); err == nil {
		t.Error("update wrote a schema outside the cache through a symlink")
	}
}

func TestSchemaInvalidCacheLocation(t *testing.T) {
	parent := t.TempDir()
	writeRefsFile(t, parent, "cache", "not a directory")
	t.Setenv(envSchemaCache, filepath.Join(parent, "cache"))

	for _, args := range [][]string{{"schema"}, {"schema", "clear"}} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "is not a directory")
	}
}

func TestSchemaFlagErrors(t *testing.T) {
	for _, args := range [][]string{
		{"schema", "fetch", "--no-such-flag"},
		{"schema", "fetch", "--dir"},
		{"schema", "--no-such-flag"},
		{"schema", "clear", "--no-such-flag"},
	} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		assertExit(t, err, exitConfig)
	}
}
