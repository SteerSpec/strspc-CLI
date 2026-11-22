package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// What a proxy, a captive portal or a broken deploy serves with HTTP 200.
var notSchemas = map[string]string{
	"html":            "<html>sign in</html>",
	"empty":           "",
	"empty object":    "{}",
	"error object":    `{"error": "down for maintenance"}`,
	"problem details": `{"type": "about:blank", "title": "Unavailable"}`,
	"boolean":         "true",
}

func TestSchemaDownloadKeepsUsableCopy(t *testing.T) {
	const good = `{"type":"object"}`
	for name, body := range notSchemas {
		for _, sub := range []string{"update", "fetch", "any command"} {
			t.Run(name+"/"+sub, func(t *testing.T) {
				s, cache := serveSchemaCache(t)
				old := time.Now().Add(-2 * time.Hour) // due for a refresh
				for _, p := range latestSchemaPaths {
					writeRefsFile(t, cache, p, good)
					if err := os.Chtimes(filepath.Join(cache, filepath.FromSlash(p)), old, old); err != nil {
						t.Fatal(err)
					}
				}
				s.body.Store(body)

				switch sub {
				case "update":
					_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "update")
					assertExit(t, err, exitViolations)
				case "fetch":
					// The usable copy is served, as when offline.
					_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
					assertExit(t, err, exitOK)
				default:
					if _, err := newSchemaFetcher().Fetch(t.Context(), schema.EntityV1Path); err != nil {
						t.Errorf("the cached copy wasn't served: %v", err)
					}
				}
				for _, p := range latestSchemaPaths {
					if got := cachedSchema(t, cache, p); got != good {
						t.Errorf("%s = %q, want the usable copy kept", p, got)
					}
				}
			})
		}
	}
}

func TestSchemaFetchRefusesBadDownloadOnColdCache(t *testing.T) {
	s, cache := serveSchemaCache(t)
	s.body.Store(notSchemas["html"])
	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "failed")
	if _, err := os.Stat(filepath.Join(cache, "config", "v1.json")); err == nil {
		t.Error("what isn't a schema was cached")
	}
}

func TestSchemaFetchReportsBadCachedRelease(t *testing.T) {
	_, cache := serveSchemaCache(t)
	// A release is never downloaded again, so a bad copy would stay for good.
	writeRefsFile(t, cache, "versions/1.2.3/entity/v1.json", notSchemas["html"])
	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir(), "--core-version", "1.2.3")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "strspc schema clear")
}
