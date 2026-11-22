package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// refuseBelow makes paths below root refused, standing in for a share.
func refuseBelow(t *testing.T, root string) {
	t.Helper()
	orig := refusedPath
	refusedPath = func(p string) bool { return strings.HasPrefix(p, root) }
	t.Cleanup(func() { refusedPath = orig })
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

func TestSchemaFetchRefusesLinkToRefusedPath(t *testing.T) {
	for _, rel := range []string{".strspc/config.yaml", ".strspc", "rules", "rules/realm.json", "rules/sub", "rules/sub/realm.json"} {
		t.Run(rel, func(t *testing.T) {
			serveSchemaCache(t)
			share := t.TempDir()
			refuseBelow(t, share)
			dir := writeSchemaProject(t)
			linkOrSkip(t, filepath.Join(share, "target"), filepath.Join(dir, filepath.FromSlash(rel)))

			_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "network or device path")
		})
	}
}

func TestCheckProjectPathFollowsChains(t *testing.T) {
	share := t.TempDir()
	refuseBelow(t, share)
	dir := t.TempDir()
	linkOrSkip(t, share, filepath.Join(dir, "last"))
	linkOrSkip(t, "last", filepath.Join(dir, "first"))
	if err := checkProjectPath(dir, "first/realm.json"); err == nil {
		t.Error("a chain of links to a refused path passed")
	}

	if err := os.MkdirAll(filepath.Join(dir, "real", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, filepath.Join("real", "inner"), filepath.Join(dir, "in"))
	linkOrSkip(t, share, filepath.Join(dir, "real", "out"))
	// in/.. is real, not dir.
	if err := checkProjectPath(dir, "in/../out/realm.json"); err == nil {
		t.Error("a .. after a link hid a refused path")
	}
	if err := checkProjectPath(dir, "in/realm.json"); err != nil {
		t.Errorf("a link inside the project was refused: %v", err)
	}

	linkOrSkip(t, "loop", filepath.Join(dir, "loop"))
	if err := checkProjectPath(dir, "loop"); !errors.Is(err, errTooManyLinks) {
		t.Errorf("loop: error = %v, want errTooManyLinks", err)
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
