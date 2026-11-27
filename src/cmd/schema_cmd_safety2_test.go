package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	writeRefsFile(t, cache, "versions/1.2.3/config/settings.json", "keep")
	writeRefsFile(t, cache, "versions/notes.txt", "keep")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	for _, p := range []string{"config/v1.json", "entity/v1.json", "versions/1.2.3/realm/v1.json"} {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(p))); err == nil {
			t.Errorf("%s survived clear", p)
		}
	}
	for _, p := range []string{"config/settings.json.bak", "config/settings.json", "versions/1.2.3/config/settings.json", "versions/notes.txt"} {
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
		{"schema", "extra"},
		{"schema", "update", "extra"},
		{"schema", "clear", "extra"},
		{"schema", "fetch", "a", "b"},
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
	testutil.AssertContains(t, out, "not in the cache as config/v1.json: it couldn't be written")
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

func TestSchemaClearKeepsForeignFilesInStagingDirs(t *testing.T) {
	_, cache := serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitOK)
	kept := []string{".update-123/data", ".update-123/config/settings.json", ".update-123/nested/config/v1.json"}
	gone := []string{".update-123/entity/v1.json", ".update-123/realm/.schema-cache-7", ".update-456/config/v1.json"}
	for _, p := range append(append([]string{}, kept...), gone...) {
		writeRefsFile(t, cache, p, "{}")
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	for _, p := range kept {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(p))); err != nil {
			t.Errorf("clear removed %s, which schema update didn't write: %v", p, err)
		}
	}
	for _, p := range append(gone, ".update-456", ".update-123/entity") {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(p))); err == nil {
			t.Errorf("%s survived clear", p)
		}
	}
}

func TestSchemaUpdateKeepsNonEmptyDirectoryAtSchemaPath(t *testing.T) {
	_, cache := serveSchemaCache(t)
	writeRefsFile(t, cache, "config/v1.json/data", "keep")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "update")
	if err == nil {
		t.Fatal("update succeeded over a directory that holds files")
	}
	testutil.AssertContains(t, out, "failed")
	if _, err := os.Stat(filepath.Join(cache, "config", "v1.json", "data")); err != nil {
		t.Errorf("update removed a file that isn't a schema: %v", err)
	}
}

func TestSchemaListShowsOnlySchemas(t *testing.T) {
	_, cache := serveSchemaCache(t)
	writeRefsFile(t, cache, "config/v1.json", "{}")
	writeRefsFile(t, cache, "config/settings.json", "{}")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "config/v1.json")
	if strings.Contains(out, "settings.json") {
		t.Errorf("the listing shows a file that isn't a schema:\n%s", out)
	}
}

func TestSchemaFetchRefusesUnreadableManifest(t *testing.T) {
	serveSchemaCache(t)
	t.Run("sub-realm", func(t *testing.T) {
		dir := writeSchemaProject(t)
		writeRefsFile(t, dir, "rules/sub/realm.json", "{")
		_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "sub-realm sub")
	})
	t.Run("dangling link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("creating a link needs a privilege here")
		}
		dir := writeSchemaProject(t)
		p := filepath.Join(dir, "rules", "realm.json")
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "nowhere"), p); err != nil {
			t.Fatal(err)
		}
		_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
		assertExit(t, err, exitConfig)
	})
}

func TestSchemaFetchAbsoluteSource(t *testing.T) {
	_, cache := serveSchemaCache(t)
	realm := filepath.Join(writeSchemaProject(t), "rules")
	project := t.TempDir()
	writeRefsFile(t, project, ".strspc/config.yaml", "rules:\n  - source: '"+realm+"'\n    scope: local\n")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", project)
	assertExit(t, err, exitOK)
	cachedSchema(t, cache, "versions/1.2.3/entity/v1.json")
	cachedSchema(t, cache, "versions/1.3.0/entity/v1.json")
}

func TestNetworkOrDeviceVolume(t *testing.T) {
	for vol, want := range map[string]bool{
		"": false, "C:": false, "z:": false,
		`\\host\share`: true, `//host/share`: true, `\\?\UNC\host\share`: true,
		`\??\UNC\host\share`: true, `\\.\pipe`: true, `\\`: true,
	} {
		if got := networkOrDeviceVolume(vol); got != want {
			t.Errorf("networkOrDeviceVolume(%q) = %v, want %v", vol, got, want)
		}
	}
}

func TestSchemaFetchRefusesNetworkSource(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows has UNC and device paths")
	}
	serveSchemaCache(t)
	for _, src := range []string{
		`\\host.invalid\share\rules`, `//host.invalid/share/rules`,
		`\\?\UNC\host.invalid\share\rules`, `\??\UNC\host.invalid\share\rules`,
	} {
		project := t.TempDir()
		writeRefsFile(t, project, ".strspc/config.yaml", "rules:\n  - source: '"+src+"'\n    scope: local\n")
		_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", project)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "network or device path")
	}
}

func TestSchemaUpdateKeepsCopyWhenStagingIsEmpty(t *testing.T) {
	cache := t.TempDir()
	writeRefsFile(t, cache, "config/v1.json", "the last usable copy")
	// The fetcher ignores a failed write, so staging can lack the file.
	staging := filepath.Join(cache, ".update-1")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := moveIntoCache(staging, cache, "config/v1.json"); err == nil {
		t.Error("moving a file that was never staged succeeded")
	}
	if got := cachedSchema(t, cache, "config/v1.json"); got != "the last usable copy" {
		t.Errorf("cached copy = %q, want it kept", got)
	}
}

func TestSchemaClearStaysInCacheWithPatternInPath(t *testing.T) {
	parent := t.TempDir()
	cache := filepath.Join(parent, "cache[1]")
	writeRefsFile(t, cache, ".update-7/config/v1.json", "{}")
	// cache[1], read as a pattern, matches this directory.
	writeRefsFile(t, parent, "cache1/.update-7/config/v1.json", "keep")

	if _, err := removeSchemaFiles(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "cache1", ".update-7", "config", "v1.json")); err != nil {
		t.Errorf("clear deleted outside the cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, ".update-7")); err == nil {
		t.Error("clear left its own staging directory")
	}
}
