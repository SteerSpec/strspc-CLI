package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// setupExtendsProject has APP-001 extend target and inherit nothing: a chain
// compares whole entities, which would hide what the extends check misses.
func setupExtendsProject(t *testing.T, target string) (dir, diff string) {
	t.Helper()
	dir, diff = setupInheritProject(t, "null", "./rules/")
	app := withNote(inheritEntityJSON("APP", "", "", inheritRule{id: "APP-001", state: "I"}),
		"APP-001", "extends", "Builds on "+target+".")
	writeInheritFile(t, filepath.Join(dir, "rules"), "APP", app)
	writeInheritEntity(t, filepath.Join(dir, "rules"), "BASE", "", inheritRule{id: "BASE-001", state: "I"})
	return dir, diff
}

func supersederJSON(euid, state, target string) string {
	return strings.Replace(inheritEntityJSON(euid, "", "", inheritRule{id: euid + "-001", state: state}),
		`"supersedes": null`, fmt.Sprintf(`"supersedes": %q`, target), 1)
}

// An extends target is used only while it is live (RUL-011), which its ID,
// revision and body don't show.
func TestCheckRefusesExtendsTargetNoLongerLive(t *testing.T) {
	setState := func(state string) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "",
				inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: state})
		}
	}
	supersede := func(realm, state, target string) func(*testing.T, string) {
		return func(t *testing.T, dir string) {
			writeInheritFile(t, filepath.Join(dir, realm), "NEWER", supersederJSON("NEWER", state, target))
		}
	}
	cases := map[string]struct {
		target string
		change func(*testing.T, string)
		want   error
	}{
		"terminated": {"dev.lib:ACCT-002", setState("T"), errNotValidated},
		"abandoned":  {"dev.lib:ACCT-002", setState("A"), errNotValidated},
		"superseded by another entity": {
			"dev.lib:ACCT-002", supersede("lib", "D", "ACCT-002"), errNotValidated,
		},
		"superseded by a sub-entity": {
			"dev.lib:ACCT-002",
			func(t *testing.T, dir string) {
				writeInheritFile(t, filepath.Join(dir, "lib"), "HOLDER", inheritEntityJSON("HOLDER", "",
					"["+supersederJSON("NEWER", "P", "ACCT-002")+"]", inheritRule{id: "HOLDER-001", state: "P"}))
			},
			errNotValidated,
		},
		"a dead rule first of two with the ID": {
			"dev.lib:ACCT-002",
			func(t *testing.T, dir string) {
				writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "",
					inheritRule{id: "ACCT-002", state: "T"}, inheritRule{id: "ACCT-002", state: "I"})
			},
			errNotValidated,
		},
		"retired is live":             {"dev.lib:ACCT-002", setState("R"), nil},
		"draft is live":               {"dev.lib:ACCT-002", setState("D"), nil},
		"abandoned superseder":        {"dev.lib:ACCT-002", supersede("lib", "A", "ACCT-002"), nil},
		"superseder in another realm": {"dev.lib:ACCT-002", supersede("rules", "P", "ACCT-002"), nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := setupExtendsProject(t, c.target)
			r := &checkRun{ctx: context.Background(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, cwd: dir}
			if err := r.loadConfig(); err != nil {
				t.Fatal(err)
			}
			if err := r.resolveRules(); err != nil {
				t.Fatal(err)
			}
			if len(r.extended["./rules/"]["dev.app:APP-001"]) != 1 {
				t.Fatal("the target wasn't resolved, so nothing would be checked")
			}
			c.change(t, dir)

			if err := r.validateRuleSchemas(); !errors.Is(err, c.want) {
				t.Errorf("error = %v, want %v", err, c.want)
			}
		})
	}
}

// A target that was never live isn't consumed, so nothing is refused.
func TestCheckAcceptsExtendsTargetAlreadyDead(t *testing.T) {
	dir, diff := setupExtendsProject(t, "dev.lib:ACCT-002")
	writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "",
		inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "T"})

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
}

// Two versions of the realm that differ in whether the target is live differ
// in what may be evaluated.
func TestCheckRefusesCopiesDifferingInLiveness(t *testing.T) {
	dir, diff := setupSameIDCopy(t, "the same source")
	rules := filepath.Join(dir, "rules")
	app := withNote(inheritEntityJSON("APP", "", "", inheritRule{id: "APP-001", state: "I"}),
		"APP-001", "extends", "Builds on dev.lib:ACCT-002.")
	writeInheritFile(t, rules, "APP", app)
	mid := withNote(inheritEntityJSON("MID", "", "", inheritRule{id: "MID-001", state: "I"}),
		"MID-001", "extends", "Builds on dev.lib:ACCT-002.")
	writeInheritFile(t, filepath.Join(dir, "mid"), "MID", mid)
	if _, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache"); err != nil {
		t.Fatalf("identical copies: %v", err)
	}
	writeInheritFile(t, filepath.Join(dir, "copy"), "NEWER", supersederJSON("NEWER", "P", "ACCT-002"))

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	if !errors.Is(err, errDifferingCopies) {
		t.Errorf("error = %v, want errDifferingCopies", err)
	}
}

// What resolution left out depends on files too, and has nothing to compare
// with what was validated.
func TestCheckRefusesResolutionOutOfDate(t *testing.T) {
	cases := map[string]struct {
		setup, change func(t *testing.T, dir string)
	}{
		"an extends target that became live": {
			setup: func(t *testing.T, dir string) {
				writeInheritFile(t, filepath.Join(dir, "rules"), "APP", withNote(
					inheritEntityJSON("APP", "", "", inheritRule{id: "APP-001", state: "I"}),
					"APP-001", "extends", "Builds on dev.lib:ACCT-002.",
				))
				writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "", inheritRule{id: "ACCT-002", state: "T"})
			},
			change: func(t *testing.T, dir string) {
				writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "", inheritRule{id: "ACCT-002", state: "I"})
			},
		},
		"an override through a sub-realm no longer listed": {
			setup: func(t *testing.T, dir string) {
				lib, base := filepath.Join(dir, "lib"), filepath.Join(dir, "base")
				writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})
				writeRealmManifest(t, lib, "dev.lib", `{"realm_id": "dev.base", "version": "1.0.0", "source": "../base/"}`)
				writeInheritEntity(t, lib, "ACCT", "dev.base:BASE",
					inheritRule{id: "ACCT-001", state: "P", overrides: "dev.base.sub:BASE-001"})
				writeRefsFile(t, base, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
					` "realm": {"id": "dev.base", "title": "t", "version": "1.0.0"}, "sub_realms": ["sub"]}`)
				writeRealmManifest(t, filepath.Join(base, "sub"), "dev.base.sub")
				writeInheritEntity(t, base, "BASE", "", inheritRule{id: "BASE-001", state: "P"})
			},
			change: func(t *testing.T, dir string) {
				writeRealmManifest(t, filepath.Join(dir, "base"), "dev.base")
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := setupInheritProject(t, "null", "./rules/")
			c.setup(t, dir)
			r := &checkRun{ctx: context.Background(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, cwd: dir}
			if err := r.loadConfig(); err != nil {
				t.Fatal(err)
			}
			if err := r.resolveRules(); err != nil {
				t.Fatal(err)
			}
			c.change(t, dir)

			if err := r.validateRuleSchemas(); !errors.Is(err, errNotValidated) {
				t.Errorf("error = %v, want errNotValidated", err)
			}
		})
	}
}
