package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestSchemaClearKeepsUnownedFiles(t *testing.T) {
	_, cache := serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", "--core-version", "1.2.3")
	assertExit(t, err, exitOK)
	// The cache was marked by fetch, but these files aren't schemas.
	writeRefsFile(t, cache, "config/settings.json.bak", "keep")
	writeRefsFile(t, cache, "config/settings.json", "keep")
	writeRefsFile(t, cache, "versions/1.2.3/config/v1.json", "keep")
	writeRefsFile(t, cache, "versions/notes.txt", "keep")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	for _, p := range []string{"config/v1.json", "entity/v1.json", "versions/1.2.3/realm/v1.json"} {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(p))); err == nil {
			t.Errorf("%s survived clear", p)
		}
	}
	for _, p := range []string{"config/settings.json.bak", "config/settings.json", "versions/1.2.3/config/v1.json", "versions/notes.txt"} {
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

func TestSchemaMarkerSymlinkRefused(t *testing.T) {
	serveSchemaCache(t)
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(target, filepath.Join(cache, schemaCacheMarker)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "is not a regular file")
	if _, err := os.Lstat(target); err == nil {
		t.Error("the marker was written through a symbolic link")
	}
	if clearableSchemaCache(cache) {
		t.Error("a symlinked marker authorizes clear")
	}
}

func TestSchemaFetchUnwritableCache(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only directory still accepts new files here")
	}
	serveSchemaCache(t)
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)
	writeRefsFile(t, cache, schemaCacheMarker, "strspc schema cache\n")
	if err := os.Chmod(cache, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })

	for _, args := range [][]string{{"schema", "fetch", t.TempDir()}, {"schema", "update"}} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "can't be written")
	}
}

func TestSchemaFetchReportsSchemaNotCached(t *testing.T) {
	_, cache := serveSchemaCache(t)
	// A file where the config/ directory belongs: the fetcher downloads
	// config/v1.json but can't cache it, and says nothing.
	writeRefsFile(t, cache, "config", "not a directory")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	if err == nil {
		t.Fatal("fetch succeeded although config/v1.json couldn't be cached")
	}
	testutil.AssertContains(t, out, "couldn't be written to the cache as config/v1.json")
	testutil.AssertContains(t, out, "ok      latest                     entity/v1.json")
}

func TestSchemaClearKeepsUnknownUpdateDirs(t *testing.T) {
	_, cache := serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitOK)
	writeRefsFile(t, cache, ".update-mine/data", "keep")
	writeRefsFile(t, cache, ".update-42/config/v1.json", "{}")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	if _, err := os.Stat(filepath.Join(cache, ".update-mine", "data")); err != nil {
		t.Errorf("clear removed .update-mine, which schema update didn't create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, ".update-42")); err == nil {
		t.Error("clear left the staging directory .update-42")
	}
}
