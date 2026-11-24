package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// relink moves dir/name to dir/name.real and leaves a link to it in its place.
func relink(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.Rename(p, p+".real"); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, p+".real", p)
}

// setupLinkProject is setupInheritProject with rules/ inheriting from lib/,
// lib/ depending on base/ and rules/ listing the sub-realm child, which
// depends on other/.
func setupLinkProject(t *testing.T) (dir, diff string) {
	t.Helper()
	dir, diff = setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeRefsFile(t, rules, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.app", "title": "t", "version": "1.0.0"}, "sub_realms": ["child"],`+
		` "dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}]}`)
	writeRealmManifest(t, filepath.Join(rules, "child"), "dev.app.child",
		`{"realm_id": "dev.other", "version": "1.0.0", "source": "../../other/"}`)
	writeRealmManifest(t, filepath.Join(dir, "other"), "dev.other")
	writeRealmManifest(t, filepath.Join(dir, "lib"), "dev.lib",
		`{"realm_id": "dev.base", "version": "1.0.0", "source": "../base/"}`)
	writeRealmManifest(t, filepath.Join(dir, "base"), "dev.base")
	writeInheritEntity(t, rules, "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})
	return dir, diff
}

func TestCheckRefusesLinkedLocalPaths(t *testing.T) {
	for name, c := range map[string]struct{ at, want string }{
		"rule source":              {"rules", "rule source"},
		"dependency":               {"lib", "dependency dev.lib of dev.app"},
		"transitive dependency":    {"base", "dependency dev.base of dev.lib"},
		"sub-realm":                {"rules/child", "sub-realm of dev.app"},
		"a sub-realm's manifest":   {"rules/child/realm.json", "sub-realm of dev.app"},
		"a dependency's manifest":  {"lib/realm.json", "dependency dev.lib of dev.app"},
		"a sub-realm's dependency": {"other", "dependency dev.other of dev.app"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, diff := setupLinkProject(t)
			refuseLinks(t)
			relink(t, filepath.Dir(filepath.Join(dir, c.at)), filepath.Base(c.at))

			_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), c.want)
			testutil.AssertContains(t, err.Error(), "is a link")
		})
	}
}

// Elsewhere than on Windows links are followed, as before.
func TestCheckFollowsLinkedLocalPathsElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links are refused on Windows")
	}
	dir, diff := setupLinkProject(t)
	for _, at := range []string{"rules", "lib", "base", "other"} {
		relink(t, dir, at)
	}
	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
}

func TestSyncRefusesLinkedSource(t *testing.T) {
	dir, _ := setupLinkProject(t)
	refuseLinks(t)
	relink(t, dir, "rules")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "sync", dir)
	if err == nil {
		t.Fatal("sync accepted a linked source")
	}
	testutil.AssertContains(t, err.Error(), "is a link")
}

func TestStatusReportsLinkedSourceInvalid(t *testing.T) {
	dir, _ := setupLinkProject(t)
	refuseLinks(t)
	relink(t, dir, "rules")
	report := runStatus(t, dir)
	if len(report.Sources) != 1 {
		t.Fatalf("sources = %+v", report.Sources)
	}
	if s := report.Sources[0]; s.State != sourceInvalid {
		t.Errorf("state = %q, want %q (error %q)", s.State, sourceInvalid, s.Error)
	}
	testutil.AssertContains(t, report.Sources[0].Error, "is a link")
}

func TestRenderRefusesLinkedDependency(t *testing.T) {
	path := setupRenderInherit(t)
	refuseLinks(t)
	relink(t, filepath.Dir(filepath.Dir(path)), "lib")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "render", path)
	if err == nil {
		t.Fatal("render resolved a linked dependency")
	}
	testutil.AssertContains(t, err.Error(), "dependency dev.lib of dev.app")
}

// A share named in the config or in a realm.json is refused before anything
// opens it. Windows only: no other system has such paths.
func TestCheckRefusesNetworkPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("network paths are Windows paths")
	}
	t.Run("rule source", func(t *testing.T) {
		dir, diff := setupInheritProject(t, "null", `'\\127.0.0.1\share\rules'`)
		_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "is a network or device path")
	})
	t.Run("dependency", func(t *testing.T) {
		dir, diff := setupInheritProject(t, "null", "./rules/")
		writeRealmManifest(t, filepath.Join(dir, "rules"), "dev.app",
			`{"realm_id": "dev.lib", "version": "1.0.0", "source": "\\\\127.0.0.1\\share\\lib"}`)
		_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, err.Error(), "is a network or device path")
	})
}
