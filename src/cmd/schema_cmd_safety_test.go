package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestSchemaClearRefusesUnmarkedDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envSchemaCache, dir)
	writeRefsFile(t, dir, "config/settings.json", "{}") // someone else's data

	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "not marked as a strspc schema cache")
	if _, err := os.Stat(filepath.Join(dir, "config", "settings.json")); err != nil {
		t.Errorf("clear deleted an unmarked directory's data: %v", err)
	}

	t.Setenv(envSchemaCache, filepath.Join(dir, "missing"))
	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "clear")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "No schema cached")
}

func TestSchemaFetchUnusableCache(t *testing.T) {
	serveSchemaCache(t)
	parent := t.TempDir()
	writeRefsFile(t, parent, "cache", "not a directory")
	t.Setenv(envSchemaCache, filepath.Join(parent, "cache"))

	for _, args := range [][]string{{"schema", "fetch", t.TempDir()}, {"schema", "update"}} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "is not a directory")
	}
}

func TestSchemaFetchMalformedRealm(t *testing.T) {
	serveSchemaCache(t)
	realm := t.TempDir()
	writeRefsFile(t, realm, "realm.json", "{not json")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir(), "--dir", realm)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "reading the realm in")
}

func TestSchemaFetchDeepSubRealm(t *testing.T) {
	_, cache := serveSchemaCache(t)
	root := t.TempDir()
	dir := root
	const depth = 20 // deeper than a short traversal bound, within maxSubRealmDepth
	for i := 0; i <= depth; i++ {
		body := fmt.Sprintf(`{"realm":{"id":"dev.d%d","title":"t","version":"1.0.0"}`, i)
		if i == depth {
			body += `,"dependencies":[{"realm_id":"dev.steerspec.core","version":"4.5.6","source":"../core/"}]`
		} else {
			body += `,"sub_realms":["s"]`
		}
		writeRefsFile(t, dir, "realm.json", body+"}")
		dir = filepath.Join(dir, "s")
	}
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir(), "--dir", root)
	assertExit(t, err, exitOK)
	cachedSchema(t, cache, "versions/4.5.6/entity/v1.json")
}

func TestSchemaUpdateReplacesDirectory(t *testing.T) {
	_, cache := serveSchemaCache(t)
	// An empty directory where the schema belongs. One that holds files is kept:
	// TestSchemaUpdateKeepsNonEmptyDirectoryAtSchemaPath.
	if err := os.MkdirAll(filepath.Join(cache, "config", "v1.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "update")
	assertExit(t, err, exitOK)
	cachedSchema(t, cache, "config/v1.json")
}
