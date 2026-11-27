package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// goOffline makes every schema download and the latest-release lookup fail.
func goOffline(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(nil)
	server.Close() // nothing listens there any more
	original := schemaBaseURL
	schemaBaseURL = server.URL
	t.Cleanup(func() { schemaBaseURL = original })
	latest := latestReleaseTag
	latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) {
		return "", errors.New("connection refused")
	}
	t.Cleanup(func() { latestReleaseTag = latest })
}

// cacheRelease writes the schemas realm init vendors for version into cache,
// each with a body naming its version.
func cacheRelease(t *testing.T, cache, version string) {
	t.Helper()
	for _, sf := range schemaFiles {
		writeRefsFile(t, cache, "versions/"+version+"/"+sf.cache, releaseBody(version, sf.local))
	}
}

func releaseBody(version, local string) string {
	return fmt.Sprintf(`{"$id": "cached-%s-%s"}`, version, local)
}

func assertVendored(t *testing.T, realmDir string, want func(local string) string) {
	t.Helper()
	for _, sf := range schemaFiles {
		got, err := os.ReadFile(filepath.Join(realmDir, "_schema", sf.local))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want(sf.local) {
			t.Errorf("%s = %q, want %q", sf.local, got, want(sf.local))
		}
	}
}

// TestRealmInitFillsAndReadsSchemaCache: a pinned init caches the release's
// schemas, and a later init of that release, pinned or through the core
// dependency, needs no network.
func TestRealmInitFillsAndReadsSchemaCache(t *testing.T) {
	tags := setupSchemaServer(t)
	cache := os.Getenv(envSchemaCache)
	served := func(local string) string { return fmt.Sprintf(`{"$id": "test-%s"}`, local) }

	first := filepath.Join(t.TempDir(), "rules")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", first, "--id", "com.test.demo", "--core-version", "1.17.1"); err != nil {
		t.Fatal(err)
	}
	for _, sf := range schemaFiles {
		if got := cachedSchema(t, cache, "versions/1.17.1/"+sf.cache); got != served(sf.local) {
			t.Errorf("cached %s = %q, want %q", sf.cache, got, served(sf.local))
		}
	}
	if _, err := os.Lstat(filepath.Join(cache, schemaCacheMarker)); err != nil {
		t.Errorf("the cache isn't marked: %v", err)
	}

	goOffline(t)
	for _, args := range [][]string{{"--core-version", "1.17.1"}, {"--dependency", coreRealmID + "@1.17.1"}} {
		dir := filepath.Join(t.TempDir(), "rules")
		out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "init", "--dir", dir, "--id", "com.test.demo"}, args...)...)
		if err != nil {
			t.Fatalf("%v offline: %v", args, err)
		}
		testutil.AssertNotContains(t, out, "warning:")
		assertVendored(t, dir, served)
	}
	if len(*tags) != len(schemaFiles) {
		t.Errorf("got %d schema requests, want only the first init's %d", len(*tags), len(schemaFiles))
	}

	// Every schema is listed, and schema clear removes them all.
	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "entity/bootstrap.json")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "versions")); err == nil {
		t.Error("schema clear left versions/ behind")
	}
}

// TestRealmInitReplacesUnusableCachedSchema: a cached copy that isn't a schema
// is downloaded again, and the download replaces it.
func TestRealmInitReplacesUnusableCachedSchema(t *testing.T) {
	tags := setupSchemaServer(t)
	cache := os.Getenv(envSchemaCache)
	writeRefsFile(t, cache, "versions/1.17.1/entity/v1.json", "{}")

	dir := filepath.Join(t.TempDir(), "rules")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", dir, "--id", "com.test.demo", "--core-version", "1.17.1"); err != nil {
		t.Fatal(err)
	}
	if len(*tags) != len(schemaFiles) {
		t.Errorf("got %d schema requests, want %d", len(*tags), len(schemaFiles))
	}
	if got, want := cachedSchema(t, cache, "versions/1.17.1/entity/v1.json"), `{"$id": "test-entity.v1.schema.json"}`; got != want {
		t.Errorf("cached entity schema = %q, want %q", got, want)
	}
}

// TestRealmInitSkipsSchemaCache: a prerelease isn't cached, and an unusable
// cache or none at all leaves realm init working online.
func TestRealmInitSkipsSchemaCache(t *testing.T) {
	runInit := func(t *testing.T, version string) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "rules")
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", dir, "--id", "com.test.demo", "--core-version", version); err != nil {
			t.Fatal(err)
		}
		assertVendored(t, dir, func(local string) string { return fmt.Sprintf(`{"$id": "test-%s"}`, local) })
	}

	t.Run("prerelease", func(t *testing.T) {
		setupSchemaServer(t)
		runInit(t, "1.17.1-rc.1")
		if _, err := os.Stat(filepath.Join(os.Getenv(envSchemaCache), "versions")); err == nil {
			t.Error("a prerelease's schemas were cached")
		}
	})
	t.Run("cache is a file", func(t *testing.T) {
		setupSchemaServer(t)
		file := filepath.Join(t.TempDir(), "file")
		writeRefsFile(t, filepath.Dir(file), "file", "x")
		t.Setenv(envSchemaCache, file)
		runInit(t, "1.17.1")
	})
	t.Run("no cache directory", func(t *testing.T) {
		setupSchemaServer(t)
		noUserCache(t)
		runInit(t, "1.17.1")
	})
}

// noUserCache leaves no schema cache directory to locate.
func noUserCache(t *testing.T) {
	t.Helper()
	for _, k := range []string{envSchemaCache, "HOME", "XDG_CACHE_HOME", "LocalAppData", "home"} {
		t.Setenv(k, "")
	}
	if _, err := schemaCacheDir(); err == nil {
		t.Skip("the user cache directory is still found on this platform")
	}
}

// TestRealmInitOfflineUsesNewestCachedRelease: without a pin and without the
// network, the newest release cached in full is used, with a warning.
func TestRealmInitOfflineUsesNewestCachedRelease(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)
	goOffline(t)
	cacheRelease(t, cache, "1.9.0")
	cacheRelease(t, cache, "1.10.0")
	// Newer, but each unusable: a schema missing, one not a schema, one too
	// large, a version with a leading zero, a file and a directory not named
	// for a version.
	cacheRelease(t, cache, "1.11.0")
	if err := os.Remove(filepath.Join(cache, "versions", "1.11.0", "entity", "bootstrap.json")); err != nil {
		t.Fatal(err)
	}
	cacheRelease(t, cache, "1.12.0")
	writeRefsFile(t, cache, "versions/1.12.0/realm/v1.json", `{"error": "down"}`)
	cacheRelease(t, cache, "1.13.0")
	writeRefsFile(t, cache, "versions/1.13.0/realm/v1.json", `{"type": "object", "x": "`+strings.Repeat("x", maxSchemaDownload)+`"}`)
	cacheRelease(t, cache, "01.20.0")
	writeRefsFile(t, cache, "versions/2.0.0", "not a directory")
	cacheRelease(t, cache, "latest")

	dir := filepath.Join(t.TempDir(), "rules")
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", dir, "--id", "com.test.demo")
	if err != nil {
		t.Fatalf("offline init: %v", err)
	}
	testutil.AssertContains(t, out, "warning: couldn't resolve the latest "+coreRealmID+" release (connection refused); using cached 1.10.0")
	assertVendored(t, dir, func(local string) string { return releaseBody("1.10.0", local) })
}

// TestRealmInitFallbackWarningIsPrintable: any failed lookup, such as a rate
// limit, falls back, and the warning shows the error with its control
// characters escaped.
func TestRealmInitFallbackWarningIsPrintable(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)
	goOffline(t)
	latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) {
		return "", errors.New("rate limited\x1b[2J\rretry later")
	}
	cacheRelease(t, cache, "1.25.0")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", filepath.Join(t.TempDir(), "rules"), "--id", "com.test.demo")
	if err != nil {
		t.Fatalf("init after a failed lookup: %v", err)
	}
	testutil.AssertContains(t, out, `(rate limited\x1b[2J\rretry later); using cached 1.25.0`)
	testutil.AssertNotContains(t, out, "\x1b")
	testutil.AssertNotContains(t, out, "\r")
}

// TestRealmInitOfflineWithoutUsableCache keeps the error that asks for a pin.
func TestRealmInitOfflineWithoutUsableCache(t *testing.T) {
	check := func(t *testing.T) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "rules")
		_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", dir, "--id", "com.test.demo")
		if err == nil {
			t.Fatal("offline init without a cached release: want an error, got none")
		}
		testutil.AssertContains(t, err.Error(), "pass --core-version to pin one")
		testutil.AssertContains(t, err.Error(), "connection refused")
		if _, err := os.Stat(dir); err == nil {
			t.Error("a failed init left the directory behind")
		}
	}
	t.Run("empty cache", func(t *testing.T) {
		t.Setenv(envSchemaCache, t.TempDir())
		goOffline(t)
		check(t)
	})
	t.Run("incomplete release", func(t *testing.T) {
		cache := t.TempDir()
		t.Setenv(envSchemaCache, cache)
		goOffline(t)
		writeRefsFile(t, cache, "versions/1.9.0/entity/v1.json", releaseBody("1.9.0", "entity"))
		check(t)
	})
	t.Run("no cache directory", func(t *testing.T) {
		goOffline(t)
		noUserCache(t)
		check(t)
	})
}

func TestSchemaFileCacheKey(t *testing.T) {
	if got := schemaFileCacheKey("v1.0.0", "realm.v1.schema.json"); got != "versions/1.0.0/realm/v1.json" {
		t.Errorf("realm schema key = %q", got)
	}
	if got := schemaFileCacheKey("v1.0.0", "other.schema.json"); got != "" {
		t.Errorf("a schema realm init doesn't vendor: key = %q, want none", got)
	}
}

func TestWriteCachedSchemaErrors(t *testing.T) {
	const key = "versions/1.0.0/entity/v1.json"
	good := []byte(`{"type": "object"}`)

	if err := writeCachedSchema(t.TempDir(), key, []byte("{}")); !errors.Is(err, errNotSchema) {
		t.Errorf("not a schema: got %v, want errNotSchema", err)
	}
	cacheFile := filepath.Join(t.TempDir(), "file")
	writeRefsFile(t, filepath.Dir(cacheFile), "file", "x")
	if err := writeCachedSchema(cacheFile, key, good); err == nil {
		t.Error("a cache that is a file: want an error, got none")
	}

	cache := t.TempDir()
	writeRefsFile(t, cache, "versions/1.0.0", "a file where a directory goes")
	if err := writeCachedSchema(cache, key, good); err == nil {
		t.Error("a file in the way: want an error, got none")
	}

	cache = t.TempDir()
	writeRefsFile(t, cache, key+"/held", "a directory holding files where the schema goes")
	if err := writeCachedSchema(cache, key, good); err == nil {
		t.Error("a directory in the way: want an error, got none")
	}
	entries, _ := os.ReadDir(filepath.Join(cache, "versions", "1.0.0", "entity"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".schema-cache-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}

	if runtime.GOOS != "windows" {
		cache = t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(cache, "versions")); err != nil {
			t.Fatal(err)
		}
		if err := writeCachedSchema(cache, key, good); err == nil {
			t.Error("a linked directory: want an error, got none")
		}
		if _, ok := readCachedSchema(cache, key); ok {
			t.Error("read a schema through a linked directory")
		}

		// A read-only directory where the schema's directory is created, or
		// where the schema is written.
		if os.Geteuid() == 0 {
			return // root writes anyway
		}
		for _, readOnly := range []string{"versions", "versions/1.0.0/entity"} {
			cache = t.TempDir()
			dir := filepath.Join(cache, filepath.FromSlash(readOnly))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			if err := writeCachedSchema(cache, key, good); err == nil {
				t.Errorf("read-only %s: want an error, got none", readOnly)
			}
		}
	}
}

// TestSchemaCacheKeepsLookalikes: a file named like a schema with a suffix is
// neither listed nor deleted.
func TestSchemaCacheKeepsLookalikes(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)
	cacheRelease(t, cache, "1.0.0")
	lookalikes := []string{"entity/v1.json.bak", "versions/1.0.0/realm/v1.json.old", "entity/bootstrap.json.bak"}
	for _, name := range lookalikes {
		writeRefsFile(t, cache, name, "keep")
	}
	if err := markSchemaCache(cache); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "entity/bootstrap.json")
	for _, suffix := range []string{".bak", ".old"} {
		testutil.AssertNotContains(t, out, suffix)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "clear"); err != nil {
		t.Fatal(err)
	}
	for _, name := range lookalikes {
		if _, err := os.Stat(filepath.Join(cache, filepath.FromSlash(name))); err != nil {
			t.Errorf("schema clear deleted %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(cache, "versions", "1.0.0", "entity", "v1.json")); err == nil {
		t.Error("schema clear kept a schema")
	}
}

// TestRealmInitMarksCacheOnHit: a cache another tool filled, without the
// marker, is marked when init uses it, so schema clear accepts it; a marker
// that can't be written doesn't fail init.
func TestRealmInitMarksCacheOnHit(t *testing.T) {
	runInit := func(t *testing.T) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "rules")
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", dir, "--id", "com.test.demo", "--core-version", "1.0.0"); err != nil {
			t.Fatalf("offline init from the cache: %v", err)
		}
		assertVendored(t, dir, func(local string) string { return releaseBody("1.0.0", local) })
	}
	t.Run("unmarked", func(t *testing.T) {
		cache := t.TempDir()
		t.Setenv(envSchemaCache, cache)
		goOffline(t)
		cacheRelease(t, cache, "1.0.0")
		runInit(t)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "clear"); err != nil {
			t.Errorf("schema clear after init used the cache: %v", err)
		}
	})
	t.Run("marker can't be written", func(t *testing.T) {
		cache := t.TempDir()
		t.Setenv(envSchemaCache, cache)
		goOffline(t)
		cacheRelease(t, cache, "1.0.0")
		writeRefsFile(t, cache, schemaCacheMarker+"/held", "a directory where the marker goes")
		runInit(t)
	})
}

// TestOversizedSchemaIsRefused: a download larger than a schema may be fails
// realm init, and such data is never cached.
func TestOversizedSchemaIsRefused(t *testing.T) {
	big := []byte(`{"type": "object", "x": "` + strings.Repeat("x", maxSchemaDownload) + `"}`)
	if err := writeCachedSchema(t.TempDir(), "versions/1.0.0/entity/v1.json", big); err == nil {
		t.Error("an oversized schema was cached")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(big)
	}))
	t.Cleanup(server.Close)
	original := schemaBaseURL
	schemaBaseURL = server.URL
	t.Cleanup(func() { schemaBaseURL = original })
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)

	dir := filepath.Join(t.TempDir(), "rules")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", dir, "--id", "com.test.demo", "--core-version", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("oversized download: error = %v, want one naming the limit", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "versions")); err == nil {
		t.Error("an oversized download was cached")
	}
}
