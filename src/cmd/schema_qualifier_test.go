package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// listSubRealm makes the dev.lib in dir list the sub-realm sub.
func listSubRealm(t *testing.T, dir, version string) {
	t.Helper()
	writeRefsFile(t, dir, "realm.json", fmt.Sprintf(`{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.lib", "title": "t", "version": %q}, "sub_realms": ["sub"]}`, version))
	writeRealmManifest(t, filepath.Join(dir, "sub"), "dev.lib.sub")
}

// A qualifier naming a sub-realm means the tree of the realm that lists it,
// so every copy of that realm counts, whether it still lists the child or not.
func TestCheckComparesEveryCopyOfAQualifiersRoot(t *testing.T) {
	const target = "dev.lib.sub:ACCT-002"
	cases := map[string]struct {
		setup func(t *testing.T, rules, lib, mid, other string)
		want  error
	}{
		"live where listed, dead where not": {func(t *testing.T, rules, lib, _, other string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", target)
			writeTarget(t, other, "T")
		}, errDifferingCopies},
		"dead where listed, live where not": {func(t *testing.T, rules, lib, _, _ string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", target)
			writeTarget(t, lib, "T")
		}, errDifferingCopies},
		"absent where not listed": {func(t *testing.T, rules, lib, _, other string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", target)
			writeTarget(t, other, "")
		}, errDifferingCopies},
		"another body where not listed": {func(t *testing.T, rules, lib, _, other string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", target)
			p := filepath.Join(other, "ACCT.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			writeInheritFile(t, other, "ACCT", strings.Replace(string(data), "Rule ACCT-002.", "Rule changed.", 1))
		}, errDifferingCopies},
		"only the other release lists it, named by an inherited entity": {func(t *testing.T, rules, lib, mid, other string) {
			listSubRealm(t, other, "2.0.0")
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeExtender(t, mid, "MID", target)
			writeTarget(t, lib, "T")
		}, errDifferingCopies},
		"named from the realm itself": {func(t *testing.T, rules, lib, _, other string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeExtender(t, lib, "EXT", target)
			writeExtender(t, other, "EXT", target)
			writeInheritEntity(t, rules, "APPEXT", "dev.lib:EXT", inheritRule{id: "APPEXT-001", state: "I"})
			writeTarget(t, other, "T")
		}, errDifferingCopies},
		"live in both": {func(t *testing.T, rules, lib, _, _ string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", target)
		}, nil},
		"dead in both": {func(t *testing.T, rules, lib, _, other string) {
			listSubRealm(t, lib, "1.0.0")
			writeExtender(t, rules, "APP", target)
			writeTarget(t, lib, "T")
			writeTarget(t, other, "T")
		}, nil},
		// Declared by itself, the child is a realm of its own: another release
		// of the parent is no copy of it.
		"the child declared as a realm of its own": {func(t *testing.T, rules, lib, _, other string) {
			// By a dependency, as a source declaring both is an EUID collision.
			oth := filepath.Join(filepath.Dir(rules), "other")
			listSubRealm(t, lib, "1.0.0")
			sub := filepath.Join(lib, "sub")
			writeInheritEntity(t, sub, "SUBX", "", inheritRule{id: "SUBX-001", state: "I"})
			writeInheritEntity(t, other, "SUBX", "", inheritRule{id: "SUBX-001", state: "T"})
			writeRealmManifest(t, oth, "dev.other",
				`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`,
				`{"realm_id": "dev.lib.sub", "version": "1.0.0", "source": "../lib/sub/"}`)
			writeExtender(t, oth, "OTH", "dev.lib.sub:SUBX-001")
			writeInheritEntity(t, rules, "APPOTH", "dev.other:OTH", inheritRule{id: "APPOTH-001", state: "I"})
			writeRealmManifest(t, rules, "dev.app",
				`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`,
				`{"realm_id": "dev.other", "version": "1.0.0", "source": "../other/"}`,
				`{"realm_id": "dev.mid", "version": "1.0.0", "source": "../mid/"}`)
		}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, diff := setupTwoReleases(t)
			c.setup(t, filepath.Join(dir, "rules"), filepath.Join(dir, "lib"), filepath.Join(dir, "mid"), filepath.Join(dir, "copy"))

			_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			if c.want == nil {
				assertExit(t, err, exitOK)
				return
			}
			assertExit(t, err, exitConfig)
			if !errors.Is(err, c.want) {
				t.Errorf("error = %v, want %v", err, c.want)
			}
		})
	}
}

// A source's sub-realm can declare a dependency that the source doesn't.
func TestCheckComparesCopiesOfASubRealmsOwnDependency(t *testing.T) {
	dir, diff := setupTwoReleases(t)
	rules := filepath.Join(dir, "rules")
	writeRefsFile(t, rules, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
		` "realm": {"id": "dev.app", "title": "t", "version": "1.0.0"}, "sub_realms": ["child"],`+
		` "dependencies": [{"realm_id": "dev.mid", "version": "1.0.0", "source": "../mid/"}]}`)
	writeInheritEntity(t, rules, "APP", "", inheritRule{id: "APP-001", state: "I"})
	child := filepath.Join(rules, "child")
	writeRealmManifest(t, child, "dev.app.child", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "../../lib/"}`)
	writeExtender(t, child, "KID", "dev.lib:ACCT-002")
	writeTarget(t, filepath.Join(dir, "lib"), "T")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	if !errors.Is(err, errDifferingCopies) {
		t.Errorf("error = %v, want errDifferingCopies", err)
	}
}
