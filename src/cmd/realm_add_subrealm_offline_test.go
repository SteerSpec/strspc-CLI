package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// TestRealmAddSubrealmOfflineUsesCachedRelease: a parent that pins no core
// release and lacks schemas, offline, gets them from the newest release in
// the schema cache, with a warning that says why and escapes the error.
func TestRealmAddSubrealmOfflineUsesCachedRelease(t *testing.T) {
	for _, drop := range []string{"all", "one"} {
		t.Run(drop, func(t *testing.T) {
			cache := t.TempDir()
			t.Setenv(envSchemaCache, cache)
			goOffline(t)
			latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) {
				return "", errors.New("rate limited\x1b[2J")
			}
			cacheRelease(t, cache, "1.9.0")
			cacheRelease(t, cache, "1.25.0")

			parentDir := makeParentRealm(t, "com.test.parent", nil)
			if drop == "all" {
				if err := os.RemoveAll(filepath.Join(parentDir, "_schema")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(filepath.Join(parentDir, "_schema", schemaFiles[1].local)); err != nil {
				t.Fatal(err)
			}

			childDir := filepath.Join(t.TempDir(), "child")
			out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm", "--id", "com.test.parent.child", "--dir", childDir, "--parent-dir", parentDir)
			if err != nil {
				t.Fatalf("offline add-subrealm: %v", err)
			}
			testutil.AssertContains(t, out, `warning: couldn't resolve the latest `+coreRealmID+` release (rate limited\x1b[2J); using cached 1.25.0`)
			testutil.AssertContains(t, out, "pin "+coreRealmID+" in the parent realm")
			testutil.AssertNotContains(t, out, "\x1b")

			for i, sf := range schemaFiles {
				got, err := os.ReadFile(filepath.Join(childDir, "_schema", sf.local))
				if err != nil {
					t.Fatal(err)
				}
				want := releaseBody("1.25.0", sf.local)
				if drop == "one" && i != 1 {
					want = `{"$id": "test-` + sf.local + `"}` // copied from the parent
				}
				if string(got) != want {
					t.Errorf("%s = %q, want %q", sf.local, got, want)
				}
			}
		})
	}
}

// TestRealmAddSubrealmOfflineWithoutCache keeps the lookup error.
func TestRealmAddSubrealmOfflineWithoutCache(t *testing.T) {
	t.Setenv(envSchemaCache, t.TempDir())
	goOffline(t)
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	if err := os.RemoveAll(filepath.Join(parentDir, "_schema")); err != nil {
		t.Fatal(err)
	}
	childDir := filepath.Join(t.TempDir(), "child")
	assertErrContains(t, runAddSubrealm(t, parentDir, childDir), "connection refused")
	if _, err := os.Stat(filepath.Join(childDir, "realm.json")); err == nil {
		t.Error("realm.json was written although the schemas failed")
	}
}
