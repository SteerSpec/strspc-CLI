package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestSchemaFetchRefusesDanglingConfigLink(t *testing.T) {
	serveSchemaCache(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, filepath.Join(dir, "nowhere"), filepath.Join(dir, ".strspc", "config.yaml"))

	// Not "a project without a config", whose pins there would be none to fetch.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", dir)
	assertExit(t, err, exitConfig)
}

func TestMarkSchemaCacheConcurrently(t *testing.T) {
	for range 200 {
		dir := t.TempDir()
		start := make(chan struct{})
		errs := make([]error, 8)
		var wg sync.WaitGroup
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = markSchemaCache(dir)
			}()
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("a concurrent fetch failed: %v", err)
			}
		}
	}
}

func TestSchemaFetchReportsFailedOverwrite(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only directory still accepts new files here")
	}
	_, cache := serveSchemaCache(t)
	// A bad copy due for a refresh, in a directory that can't take the new one.
	writeRefsFile(t, cache, "config/v1.json", "<html>")
	p := filepath.Join(cache, "config", "v1.json")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	out, err := testutil.ExecuteCommand(NewRootCmd(), "schema", "fetch", t.TempDir())
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "not in the cache as config/v1.json")
}
