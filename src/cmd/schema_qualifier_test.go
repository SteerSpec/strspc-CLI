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

// dev.other declares the child dev.lib.sub as a realm of its own and the
// source declares its parent dev.lib, which lists it. A qualifier binds to one
// root, so neither realm counts as a copy of the other.
func TestCheckBindsAQualifierToItsRoot(t *testing.T) {
	ext := func(euid, target string) string {
		return withNote(inheritEntityJSON(euid, "", "", inheritRule{id: euid + "-001", state: "I"}),
			euid+"-001", "extends", "Builds on "+target+".")
	}
	cases := map[string]struct {
		twoReleases bool
		third       bool // dev.third also declares release 1.0.0 of the child
		setup       func(t *testing.T, rules, lib, oth string)
		want        error
	}{
		"two releases, child inherited": {twoReleases: true},
		"two releases, child extended": {twoReleases: true, setup: func(t *testing.T, _, _, oth string) {
			writeInheritFile(t, oth, "OTH", ext("OTH", "dev.lib.sub:SUBX-002"))
		}},
		"two releases, the child's note naming its own rule": {twoReleases: true, setup: func(t *testing.T, rules, _, _ string) {
			writeInheritFile(t, filepath.Join(filepath.Dir(rules), "child"), "SUBX", withNote(inheritEntityJSON("SUBX", "", "",
				inheritRule{id: "SUBX-001", state: "I"}, inheritRule{id: "SUBX-002", state: "P"}),
				"SUBX-001", "extends", "Builds on SUBX-002."))
		}},
		// The parent's dependency doesn't resolve the child's notes, so the
		// resolver omits the target and its releases aren't compared.
		"two releases, the child naming its parent's dependency": {twoReleases: true, setup: func(t *testing.T, rules, lib, _ string) {
			dir := filepath.Dir(rules)
			for _, b := range []struct{ name, version string }{{"base1", "1.0.0"}, {"base2", "2.0.0"}} {
				d := filepath.Join(dir, b.name)
				writeRefsFile(t, d, "realm.json", fmt.Sprintf(`{"$schema": "./_schema/realm.v1.schema.json",`+
					` "realm": {"id": "dev.base", "title": "t", "version": %q}, "sub_realms": ["x"]}`, b.version))
				writeRealmManifest(t, filepath.Join(d, "x"), "dev.base.x")
				writeInheritEntity(t, filepath.Join(d, "x"), "BXE", "", inheritRule{id: "BXE-001", state: "I"})
			}
			// Live in one release only, so comparing them would refuse.
			writeInheritFile(t, filepath.Join(dir, "base1"), "SUP", strings.Replace(
				inheritEntityJSON("SUP", "", "", inheritRule{id: "SUP-001", state: "P"}),
				`"supersedes": null`, `"supersedes": "BXE-001"`, 1,
			))
			writeRefsFile(t, lib, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
				` "realm": {"id": "dev.lib", "title": "t", "version": "1.0.0"}, "sub_realms": ["sub"],`+
				` "dependencies": [{"realm_id": "dev.base", "version": "1.0.0", "source": "../base1/"}]}`)
			writeRealmManifest(t, rules, "dev.app",
				`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`,
				`{"realm_id": "dev.other", "version": "1.0.0", "source": "../other/"}`,
				`{"realm_id": "dev.base", "version": "2.0.0", "source": "../base2/"}`)
			writeInheritEntity(t, rules, "APPBXE", "dev.base.x:BXE", inheritRule{id: "APPBXE-001", state: "I"})
			writeInheritEntity(t, lib, "LIBBXE", "dev.base.x:BXE", inheritRule{id: "LIBBXE-001", state: "I"})
			writeInheritEntity(t, rules, "APPLIB", "dev.lib:LIBBXE", inheritRule{id: "APPLIB-001", state: "I"})
			writeInheritFile(t, filepath.Join(dir, "child"), "SUBX", withNote(inheritEntityJSON("SUBX", "", "",
				inheritRule{id: "SUBX-001", state: "I"}, inheritRule{id: "SUBX-002", state: "P"}),
				"SUBX-001", "extends", "Builds on dev.base.x:BXE-001."))
		}},
		"one release, parent's rule named through the child": {setup: func(t *testing.T, rules, _, _ string) {
			writeInheritFile(t, rules, "EXT", ext("EXT", "dev.lib.sub:ACCT-002"))
		}},
		"one release, parent supersedes the child's rule": {setup: func(t *testing.T, _, lib, oth string) {
			writeInheritFile(t, oth, "OTH", ext("OTH", "dev.lib.sub:SUBX-001"))
			writeInheritFile(t, lib, "NEWER", strings.Replace(
				inheritEntityJSON("NEWER", "", "", inheritRule{id: "NEWER-001", state: "P"}),
				`"supersedes": null`, `"supersedes": "SUBX-001"`, 1,
			))
		}},
		"one release, child inherited": {},
		"two releases of the child itself": {twoReleases: true, third: true, setup: func(t *testing.T, _, _, oth string) {
			writeInheritFile(t, oth, "OTH", ext("OTH", "dev.lib.sub:SUBX-002"))
		}, want: errDifferingCopies},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, diff := setupInheritProject(t, "null", "./rules/")
			rules, lib, oth := filepath.Join(dir, "rules"), filepath.Join(dir, "lib"), filepath.Join(dir, "other")
			listSubRealm(t, lib, "1.0.0")
			writeInheritEntity(t, filepath.Join(lib, "sub"), "SUBX", "", inheritRule{id: "SUBX-001", state: "I"})

			child, version := "../lib/sub/", "1.0.0"
			if c.twoReleases {
				child, version = "../child/", "2.0.0"
				d := filepath.Join(dir, "child")
				writeRefsFile(t, d, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
					` "realm": {"id": "dev.lib.sub", "title": "t", "version": "2.0.0"}}`)
				writeInheritEntity(t, d, "SUBX", "",
					inheritRule{id: "SUBX-001", state: "I"}, inheritRule{id: "SUBX-002", state: "P"})
			}
			writeRealmManifest(t, oth, "dev.other",
				fmt.Sprintf(`{"realm_id": "dev.lib.sub", "version": %q, "source": %q}`, version, child))
			deps := []string{
				`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`,
				`{"realm_id": "dev.other", "version": "1.0.0", "source": "../other/"}`,
			}
			if c.third {
				third := filepath.Join(dir, "third")
				writeRealmManifest(t, third, "dev.third",
					`{"realm_id": "dev.lib.sub", "version": "1.0.0", "source": "../lib/sub/"}`)
				writeInheritEntity(t, third, "THD", "dev.lib.sub:SUBX", inheritRule{id: "THD-001", state: "I"})
				writeInheritEntity(t, rules, "APPTHD", "dev.third:THD", inheritRule{id: "APPTHD-001", state: "I"})
				deps = append(deps, `{"realm_id": "dev.third", "version": "1.0.0", "source": "../third/"}`)
			}
			writeRealmManifest(t, rules, "dev.app", deps...)
			writeInheritEntity(t, rules, "APP", "dev.other:OTH", inheritRule{id: "APP-001", state: "I"})
			writeInheritEntity(t, oth, "OTH", "dev.lib.sub:SUBX", inheritRule{id: "OTH-001", state: "I"})
			if c.setup != nil {
				c.setup(t, rules, lib, oth)
			}

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
