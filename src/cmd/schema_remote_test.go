package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// cachedRemoteDir returns where the fetcher of a check in dir caches path of
// github://acme/rules@v1.0.0, which it serves without a request.
func cachedRemoteDir(dir, path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(dir, ".strspc", "cache", "github", "acme", "rules", "v1.0.0", hex.EncodeToString(sum[:])[:12])
}

// Each realm is cached as a subtree of its own, so a relative dependency
// resolves only when it is rebased on the GitHub reference it came from.
func TestCheckValidatesTransitiveGitHubDependencies(t *testing.T) {
	const ref = "github://acme/rules@v1.0.0/"
	for _, tc := range []struct {
		name, source string
		write        func(t *testing.T, dir string)
	}{
		{"local source, remote dependency", "./rules/", func(t *testing.T, dir string) {
			app := filepath.Join(dir, "rules")
			writeRealmManifest(t, app, "dev.app", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "`+ref+`rules/lib"}`)
			writeInheritEntity(t, app, "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})
		}},
		{"remote source", ref + "rules/app", func(t *testing.T, dir string) {
			app := cachedRemoteDir(dir, "rules/app")
			writeRealmManifest(t, app, "dev.app", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib"}`)
			writeInheritEntity(t, app, "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})
		}},
		{"remote source's sub-realm", ref + "rules/app", func(t *testing.T, dir string) {
			app := cachedRemoteDir(dir, "rules/app")
			writeRefsFile(t, app, "realm.json",
				`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "a", "version": "1.0.0"}, "sub_realms": ["child"]}`)
			child := filepath.Join(app, "child")
			writeRealmManifest(t, child, "dev.app.child", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "../../lib"}`)
			writeInheritEntity(t, child, "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRawCheckConfig(t, dir, fmt.Sprintf(
				"rules:\n  - source: %s\n    scope: global\nevaluator:\n  provider: \"null\"\n  effort: low\ncache:\n  ttl: 24h\n", tc.source,
			))
			tc.write(t, dir)
			lib := cachedRemoteDir(dir, "rules/lib")
			writeRealmManifest(t, lib, "dev.lib", `{"realm_id": "dev.base", "version": "1.0.0", "source": "../base"}`)
			writeInheritEntity(t, lib, "ACCT", "dev.base:BASE", inheritRule{id: "ACCT-001", state: "I"})
			base := cachedRemoteDir(dir, "rules/base")
			writeRealmManifest(t, base, "dev.base")
			writeInheritEntity(t, base, "BASE", "", inheritRule{id: "BASE-001", state: "I"})
			diff := filepath.Join(dir, "change.diff")
			if err := os.WriteFile(diff, []byte(checkDiff), 0o600); err != nil {
				t.Fatal(err)
			}

			// A wrong origin would leave BASE unvalidated, and checkConsumed
			// would refuse it.
			_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitOK)

			breakEntity(t, base, "BASE.json")
			_, err = runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "BASE.json does not match the entity/v1.json schema")
		})
	}
}
