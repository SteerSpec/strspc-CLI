package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
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
	// A link inside a rule source's tree is refused by the resolver itself,
	// on every system, before anything reads through it: a linked sub-realm
	// directory (RR007), and a linked manifest (RSV002).
	for at, want := range map[string]string{"rules/child": "RR007", "rules/child/realm.json": "RSV002"} {
		t.Run(at, func(t *testing.T) {
			dir, diff := setupLinkProject(t)
			relink(t, filepath.Dir(filepath.Join(dir, at)), filepath.Base(at))

			out, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, out, want)
			testutil.AssertContains(t, out, "link")
		})
	}
}

// A sub-realm's own sub-realm is checked too, before the resolver refuses
// the nesting (RR010).
func TestCheckRefusesLinkedNestedSubRealmDependency(t *testing.T) {
	dir, diff := setupLinkProject(t)
	rules := filepath.Join(dir, "rules")
	writeRefsFile(t, filepath.Join(rules, "child"), "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.app.child", "title": "t", "version": "1.0.0"}, "sub_realms": ["deep"],`+
		` "dependencies": [{"realm_id": "dev.other", "version": "1.0.0", "source": "../../other/"}]}`)
	writeRealmManifest(t, filepath.Join(rules, "child", "deep"), "dev.app.child.deep",
		`{"realm_id": "dev.nested", "version": "1.0.0", "source": "../../../nested/"}`)
	writeRealmManifest(t, filepath.Join(dir, "nested"), "dev.nested")
	refuseLinks(t)
	relink(t, dir, "nested")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "dependency dev.nested of dev.app")
	testutil.AssertContains(t, err.Error(), "is a link")
}

// A manifest listing its own directory, or its parent, as a sub-realm or a
// dependency can't make the walk revisit it: without that, each "." would
// double the work at each of maxSubRealmDepth levels, and a dependency
// leading back would recurse without end.
func TestCheckDependencySourcesWalksEachDirectoryOnce(t *testing.T) {
	dir := t.TempDir()
	writeRefsFile(t, dir, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.app", "title": "t", "version": "1.0.0"},`+
		` "sub_realms": [".", ".", "..", "./", "child", "loop", "loop", "back"],`+
		` "dependencies": [{"realm_id": "dev.a", "version": "1.0.0", "source": "./loop/"},`+
		` {"realm_id": "dev.b", "version": "1.0.0", "source": "./back/"},`+
		` {"realm_id": "dev.c", "version": "1.0.0", "source": "."}]}`)
	// Off Windows a link is followed: loop and back lead to dir, each giving
	// a new path at every level.
	if runtime.GOOS != "windows" {
		linkOrSkip(t, ".", filepath.Join(dir, "loop"))
		linkOrSkip(t, ".", filepath.Join(dir, "back"))
	}
	writeRefsFile(t, filepath.Join(dir, "child"), "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.app.child", "title": "t", "version": "1.0.0"}, "sub_realms": ["..", "../child"]}`)
	rf, err := loadRealmIfAny(dir)
	if err != nil || rf == nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- checkDependencySources(dir, rf, &dirSet{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the walk revisits directories")
	}
}

// No depth limit cuts the walk short: a dependency declared deeper than lint
// follows sub-realms is checked too.
func TestCheckDependencySourcesHasNoDepthLimit(t *testing.T) {
	refuseLinks(t)
	root := t.TempDir()
	d := root
	for i := 0; i <= maxSubRealmDepth+8; i++ {
		writeRefsFile(t, d, "realm.json", fmt.Sprintf(`{"$schema": "./_schema/realm.v1.schema.json",`+
			` "realm": {"id": "dev.r%d", "title": "t", "version": "1.0.0"}, "sub_realms": ["s"]}`, i))
		d = filepath.Join(d, "s")
	}
	writeRefsFile(t, d, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.deep", "title": "t", "version": "1.0.0"},`+
		` "dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "./lib/"}]}`)
	writeRealmManifest(t, filepath.Join(root, "real"), "dev.lib")
	linkOrSkip(t, filepath.Join(root, "real"), filepath.Join(d, "lib"))

	rf, err := loadRealmIfAny(root)
	if err != nil || rf == nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	err = checkDependencySources(root, rf, &dirSet{})
	if !errors.Is(err, errProjectLink) {
		t.Errorf("error = %v, want errProjectLink", err)
	}
}

// Elsewhere than on Windows a linked rule source is followed, as before; a
// dependency reached through a link is refused by the resolver on every
// system (RR002).
func TestCheckFollowsLinkedLocalPathsElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links are refused on Windows")
	}
	dir, diff := setupLinkProject(t)
	relink(t, dir, "rules")
	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)

	for _, at := range []string{"lib", "other"} {
		t.Run(at, func(t *testing.T) {
			dir, diff := setupLinkProject(t)
			relink(t, dir, at)
			out, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, out, "RR002")
		})
	}
	// A dependency's own dependency is resolved when an inheritance chain
	// reaches it: here lib's ACCT inherits base's BASE.
	t.Run("base", func(t *testing.T) {
		dir, diff := setupLinkProject(t)
		writeInheritEntity(t, filepath.Join(dir, "base"), "BASE", "", inheritRule{id: "BASE-001", state: "P"})
		writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "dev.base:BASE",
			inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "I"})
		_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
		assertExit(t, err, exitOK) // the chain resolves while base/ is a directory

		relink(t, dir, "base")
		out, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
		assertExit(t, err, exitConfig)
		testutil.AssertContains(t, out, "RR002")
	})
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

// A local dependency source holding a backslash is refused on every system,
// as the resolver refuses it (RR001): only Windows reads it as a separator.
func TestDependencySourceWithBackslash(t *testing.T) {
	dir, diff := setupInheritProject(t, "null", "./rules/")
	writeRealmManifest(t, filepath.Join(dir, "rules"), "dev.app",
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "..\\lib"}`)
	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	if !errors.Is(err, errBackslashSource) {
		t.Errorf("check: err = %v, want errBackslashSource", err)
	}
	if _, err := fetchRealmDependency(t.Context(), dir, entity.RealmDep{RealmID: "dev.lib", Source: `..\lib`}); !errors.Is(err, errBackslashSource) {
		t.Errorf("realm validate: err = %v, want errBackslashSource", err)
	}
}
