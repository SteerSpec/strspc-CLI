package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// schemaServer, from serveSchemaCache, serves every latest and release schema with body, or fails
// with 500 while failing is set, and points newSchemaFetcher at it. It
// returns the cache directory, set through STRSPC_SCHEMA_CACHE.
type schemaServer struct {
	body    atomic.Value // string
	failing atomic.Bool
	hits    atomic.Int64
}

func serveSchemaCache(t *testing.T) (s *schemaServer, cache string) {
	t.Helper()
	s = &schemaServer{}
	s.body.Store(`{"type":"object"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		if s.failing.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(srv.Close)

	orig := newSchemaFetcher
	newSchemaFetcher = func(opts ...schema.Option) *schema.Fetcher {
		return newCachedSchemaFetcher(append([]schema.Option{schema.WithBaseURL(srv.URL + "/schemas")}, opts...)...)
	}
	t.Cleanup(func() { newSchemaFetcher = orig })

	cache = filepath.Join(t.TempDir(), "schemas")
	t.Setenv(envSchemaCache, cache)
	return s, cache
}

// writeSchemaProject writes a project whose local source rules/ is a realm
// pinned to core 1.2.3, with a sub-realm sub/ pinned to 1.3.0.
func writeSchemaProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeRefsFile(t, dir, ".strspc/config.yaml", "rules:\n  - source: ./rules/\n    scope: local\n  - source: github://o/r@v1.0.0/rules\n    scope: remote\n")
	writeRefsFile(t, dir, "rules/realm.json", `{"$schema": "./_schema/realm.v1.schema.json", "realm":{"id":"dev.app","title":"t","version":"1.0.0"},`+
		`"dependencies":[{"realm_id":"dev.steerspec.core","version":"1.2.3","source":"github://SteerSpec/strspc-rules@v1.2.3/rules/core"}],`+
		`"sub_realms":["sub"]}`)
	writeRefsFile(t, dir, "rules/sub/realm.json", `{"$schema": "./_schema/realm.v1.schema.json", "realm":{"id":"dev.app.sub","title":"t","version":"1.0.0"},`+
		`"dependencies":[{"realm_id":"dev.steerspec.core","version":"1.3.0","source":"github://SteerSpec/strspc-rules@v1.3.0/rules/core"}]}`)
	return dir
}

func cachedSchema(t *testing.T, cache, p string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cache, filepath.FromSlash(p)))
	if err != nil {
		t.Fatalf("%s not cached: %v", p, err)
	}
	return string(data)
}

func TestSchemaFetchProject(t *testing.T) {
	_, cache := serveSchemaCache(t)
	dir := writeSchemaProject(t)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Schema cache: "+cache)
	for _, p := range []string{
		"config/v1.json", "entity/v1.json", "realm/v1.json",
		"versions/1.2.3/entity/v1.json", "versions/1.2.3/realm/v1.json",
		"versions/1.3.0/entity/v1.json", "versions/1.3.0/realm/v1.json",
	} {
		cachedSchema(t, cache, p)
	}
	testutil.AssertContains(t, out, "dev.steerspec.core 1.3.0")

	out, err = testutil.ExecuteCommand(NewRootCmd(), "schema")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "dev.steerspec.core 1.2.3")
	testutil.AssertContains(t, out, "latest")
	testutil.AssertContains(t, out, "config/v1.json")
}

func TestSchemaFetchFlags(t *testing.T) {
	_, cache := serveSchemaCache(t)
	realm := t.TempDir()
	writeRefsFile(t, realm, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json", "realm":{"id":"dev.x","title":"t","version":"1.0.0"},`+
		`"dependencies":[{"realm_id":"dev.steerspec.core","version":"2.0.0","source":"github://SteerSpec/strspc-rules@v2.0.0/rules/core"}]}`)

	// No .strspc/config.yaml: only the latest schemas, --dir and --core-version.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir(), "--dir", realm, "--core-version", "v1.9.0")
	assertExit(t, err, exitOK)
	cachedSchema(t, cache, "versions/2.0.0/entity/v1.json")
	cachedSchema(t, cache, "versions/1.9.0/realm/v1.json")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", "--core-version", "1.x")
	assertExit(t, err, exitConfig)
	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", "--dir", t.TempDir())
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "no realm.json")

	bad := t.TempDir()
	writeRefsFile(t, bad, ".strspc/config.yaml", "rules: [\n")
	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", bad)
	assertExit(t, err, exitConfig)
}

func TestSchemaFetchFailure(t *testing.T) {
	s, _ := serveSchemaCache(t)
	s.failing.Store(true)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "failed")
	testutil.AssertContains(t, err.Error(), "3 schema(s) could not be fetched")
}

func TestSchemaUpdate(t *testing.T) {
	s, cache := serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitOK)

	// A fresh copy is downloaded again and replaces the cached one.
	s.body.Store(`{"type":"object","title":"v2"}`)
	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "update")
	assertExit(t, err, exitOK)
	if got := cachedSchema(t, cache, "config/v1.json"); !strings.Contains(got, "v2") {
		t.Errorf("config schema = %s, want the updated copy", got)
	}

	// A failed update keeps the cached copy.
	s.failing.Store(true)
	_, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "update")
	assertExit(t, err, exitViolations)
	if got := cachedSchema(t, cache, "config/v1.json"); !strings.Contains(got, "v2") {
		t.Errorf("config schema = %s, want the copy kept after a failed update", got)
	}
	if stale, _ := filepath.Glob(filepath.Join(cache, ".update-*")); len(stale) != 0 {
		t.Errorf("staging left behind: %v", stale)
	}
}

func TestSchemaClear(t *testing.T) {
	_, cache := serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", "--core-version", "1.2.3")
	assertExit(t, err, exitOK)
	writeRefsFile(t, cache, "unrelated.txt", "keep")
	writeRefsFile(t, cache, ".update-123/config/v1.json", "{}")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Cleared the schema cache")
	for _, name := range []string{"config", "entity", "realm", "versions", ".update-123"} {
		if _, err := os.Stat(filepath.Join(cache, name)); err == nil {
			t.Errorf("%s survived clear", name)
		}
	}
	if _, err := os.Stat(filepath.Join(cache, "unrelated.txt")); err != nil {
		t.Errorf("clear removed a file it doesn't own: %v", err)
	}

	out, err = testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "No schema cached")

	out, err = testutil.ExecuteCommand(NewRootCmd(), "schema")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "run `strspc schema fetch` while online")
}

func TestSchemaListExpiredLatest(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(envSchemaCache, cache)
	writeRefsFile(t, cache, "config/v1.json", "{}")
	writeRefsFile(t, cache, "versions/1.2.3/entity/v1.json", "{}")
	old := time.Now().Add(-2 * time.Hour)
	for _, p := range []string{"config/v1.json", "versions/1.2.3/entity/v1.json"} {
		if err := os.Chtimes(filepath.Join(cache, filepath.FromSlash(p)), old, old); err != nil {
			t.Fatal(err)
		}
	}

	var b strings.Builder
	if err := listSchemaCache(&b, time.Now()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(b.String(), "\n")
	var latest, release string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "config/v1.json"):
			latest = l
		case strings.Contains(l, "1.2.3"):
			release = l
		}
	}
	testutil.AssertContains(t, latest, "refreshed on next use when online")
	if strings.Contains(release, "refreshed") || !strings.Contains(release, "entity/v1.json") {
		t.Errorf("release line = %q, want entity/v1.json and no refresh note", release)
	}
}

func TestSchemaCacheDirFrom(t *testing.T) {
	user := func() (string, error) { return filepath.Join(string(filepath.Separator)+"u", "cache"), nil }
	none := func(string) string { return "" }
	got, err := schemaCacheDirFrom(none, user)
	if err != nil || got != filepath.Join(string(filepath.Separator)+"u", "cache", "strspc", "schemas") {
		t.Errorf("default = %q, %v", got, err)
	}

	rel := func(string) string { return "rel/cache" }
	got, err = schemaCacheDirFrom(rel, user)
	if err != nil || !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join("rel", "cache")) {
		t.Errorf("override = %q, %v, want an absolute path", got, err)
	}

	broken := func() (string, error) { return "", errors.New("no home") }
	if _, err := schemaCacheDirFrom(none, broken); err == nil || !strings.Contains(err.Error(), envSchemaCache) {
		t.Errorf("no user cache: got %v, want an error naming %s", err, envSchemaCache)
	}
	if opts := schemaCacheOptionsFrom(none, broken); opts != nil {
		t.Errorf("options without a cache = %v, want none", opts)
	}
}
