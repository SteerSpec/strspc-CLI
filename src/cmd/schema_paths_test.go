package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// refuseLinks makes projectPath refuse links, as it does on Windows.
func refuseLinks(t *testing.T) {
	t.Helper()
	orig := refuseProjectLinks
	refuseProjectLinks = true
	t.Cleanup(func() { refuseProjectLinks = orig })
}

func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.RemoveAll(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("creating a link needs a privilege here")
		}
		t.Fatal(err)
	}
}

// Each target is a realm that would load, so only a refusal fails the fetch.
func TestSchemaFetchRefusesLinks(t *testing.T) {
	for _, rel := range []string{".strspc/config.yaml", ".strspc", "rules", "rules/realm.json", "rules/sub", "rules/sub/realm.json"} {
		t.Run(rel, func(t *testing.T) {
			serveSchemaCache(t)
			refuseLinks(t)
			elsewhere := writeSchemaProject(t)
			dir := writeSchemaProject(t)
			p := filepath.FromSlash(rel)
			linkOrSkip(t, filepath.Join(elsewhere, p), filepath.Join(dir, p))

			_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "is a link")
		})
	}
}

func TestSchemaFetchRefusesLinkBehindDotDot(t *testing.T) {
	for _, src := range []string{"missing/../out/", "./missing/../out", "rules/../out/"} {
		t.Run(src, func(t *testing.T) {
			serveSchemaCache(t)
			refuseLinks(t)
			dir := writeSchemaProject(t)
			linkOrSkip(t, filepath.Join(writeSchemaProject(t), "rules"), filepath.Join(dir, "out"))
			writeRefsFile(t, dir, ".strspc/config.yaml", "rules:\n  - source: "+src+"\n    scope: local\n")

			_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "is a link")
		})
	}
}

func TestSchemaFetchRefusesLinkBelowDir(t *testing.T) {
	for _, rel := range []string{"realm.json", "sub"} {
		t.Run(rel, func(t *testing.T) {
			serveSchemaCache(t)
			refuseLinks(t)
			realm := filepath.Join(writeSchemaProject(t), "rules")
			linkOrSkip(t, filepath.Join(writeSchemaProject(t), "rules", rel), filepath.Join(realm, rel))

			_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir(), "--dir", realm)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "is a link")
		})
	}
}

func TestSchemaFetchWithoutLinks(t *testing.T) {
	_, cache := serveSchemaCache(t)
	refuseLinks(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", writeSchemaProject(t))
	assertExit(t, err, exitOK)
	cachedSchema(t, cache, "versions/1.3.0/realm/v1.json")
}

func TestSchemaFetchFollowsLinksElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses links")
	}
	_, cache := serveSchemaCache(t)
	dir := writeSchemaProject(t)
	linkOrSkip(t, filepath.Join(writeSchemaProject(t), "rules"), filepath.Join(dir, "rules"))
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
	assertExit(t, err, exitOK)
	cachedSchema(t, cache, "versions/1.3.0/realm/v1.json")
}

func TestSchemaFetchRefusesRootedSourceLink(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows has rooted paths that aren't absolute")
	}
	for _, src := range []string{`/out`, `\out`} {
		serveSchemaCache(t)
		dir := writeSchemaProject(t)
		linkOrSkip(t, filepath.Join(writeSchemaProject(t), "rules"), filepath.Join(dir, "out"))
		writeRefsFile(t, dir, ".strspc/config.yaml", "rules:\n  - source: '"+src+"'\n    scope: local\n")

		_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "is a link")
	}
}

func TestCheckCachedWantsTheReleaseItself(t *testing.T) {
	cache := t.TempDir()
	writeRefsFile(t, cache, "entity/v1.json", "{}")
	if err := checkCached(cache, "9.9.9", "entity/v1.json"); err == nil {
		t.Error("the latest schema stood in for a release that isn't cached")
	}
}

// What schema fetch caches, the fetchers of lint, check and doctor serve
// offline.
func TestSchemaCacheIsShared(t *testing.T) {
	serveSchemaCache(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", writeSchemaProject(t))
	assertExit(t, err, exitOK)

	offline := schema.WithBaseURL("http://127.0.0.1:1/schemas")
	for name, f := range map[string]*schema.Fetcher{
		"lint and check":   newCachedSchemaFetcher(offline),
		"doctor":           schema.New(append(schemaCacheOptionsFrom(os.Getenv, os.UserCacheDir), offline)...),
		"a pinned release": newCachedSchemaFetcher(offline, schema.WithCoreVersion("1.2.3")),
	} {
		if _, err := f.Fetch(t.Context(), schema.EntityV1Path); err != nil {
			t.Errorf("%s: not served from the shared cache: %v", name, err)
		}
	}
}
