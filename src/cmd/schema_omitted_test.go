package cmd

import (
	"errors"
	"path/filepath"
	"testing"
)

// setupTwoReleases has rules/ use dev.lib 1.0.0 in lib/ and, through an
// inherited entity of dev.mid, 2.0.0 in copy/. MID's own use of dev.lib is a
// rule that is live and equal in both, so that only an extends target that a
// test adds can differ.
func setupTwoReleases(t *testing.T) (dir, diff string) {
	t.Helper()
	dir, diff = setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeRealmManifest(t, rules, "dev.app",
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`,
		`{"realm_id": "dev.mid", "version": "1.0.0", "source": "../mid/"}`)
	writeRealmManifest(t, filepath.Join(dir, "mid"), "dev.mid",
		`{"realm_id": "dev.lib", "version": "2.0.0", "source": "../copy/"}`)
	writeRefsFile(t, filepath.Join(dir, "copy"), "realm.json",
		`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.lib", "title": "t", "version": "2.0.0"}}`)
	writeExtender(t, filepath.Join(dir, "mid"), "MID", "dev.lib:ACCT-001")
	writeExtender(t, rules, "APP", "dev.lib:ACCT-002")
	writeInheritEntity(t, rules, "APPMID", "dev.mid:MID", inheritRule{id: "APPMID-001", state: "I"})
	writeTarget(t, filepath.Join(dir, "lib"), "I")
	writeTarget(t, filepath.Join(dir, "copy"), "I")
	return dir, diff
}

func writeExtender(t *testing.T, dir, euid, target string) {
	t.Helper()
	writeInheritFile(t, dir, euid, withNote(inheritEntityJSON(euid, "", "", inheritRule{id: euid + "-001", state: "I"}),
		euid+"-001", "extends", "Builds on "+target+"."))
}

// writeTarget writes ACCT with ACCT-002 in state, or without it when state is
// empty.
func writeTarget(t *testing.T, dir, state string) {
	t.Helper()
	rules := []inheritRule{{id: "ACCT-001", state: "P"}}
	if state != "" {
		rules = append(rules, inheritRule{id: "ACCT-002", state: state})
	}
	writeInheritEntity(t, dir, "ACCT", "", rules...)
}

// A target that isn't live in the release the resolver picked is left out of
// what it returns, so it is found through the notes: live in another copy
// still makes the copies differ.
func TestCheckRefusesOmittedTargetLiveInAnotherCopy(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, rules, lib, mid, other string)
		want  error
	}{
		"terminated here": {func(t *testing.T, _, lib, _, _ string) {
			writeTarget(t, lib, "T")
		}, errDifferingCopies},
		"abandoned here": {func(t *testing.T, _, lib, _, _ string) {
			writeTarget(t, lib, "A")
		}, errDifferingCopies},
		"superseded here": {func(t *testing.T, _, lib, _, _ string) {
			writeInheritFile(t, lib, "NEWER", supersederJSON("NEWER", "P", "ACCT-002"))
		}, errDifferingCopies},
		// A source's own note of a missing rule is RR018, so an ancestor's.
		"absent here, named by an inherited entity": {func(t *testing.T, rules, _, mid, other string) {
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeExtender(t, mid, "MID", "dev.lib:ACCT-002")
			writeTarget(t, other, "")
		}, errDifferingCopies},
		"quoted in backticks, which the resolver reads too": {func(t *testing.T, rules, lib, _, _ string) {
			writeExtender(t, rules, "APP", "`dev.lib:ACCT-002`")
			writeTarget(t, lib, "T")
		}, errDifferingCopies},
		"named by an inherited entity only": {func(t *testing.T, rules, _, mid, other string) {
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeExtender(t, mid, "MID", "dev.lib:ACCT-002")
			writeTarget(t, other, "T")
		}, errDifferingCopies},
		"dead in both": {func(t *testing.T, _, lib, _, other string) {
			writeTarget(t, lib, "T")
			writeTarget(t, other, "A")
		}, nil},
		"absent in both": {func(t *testing.T, rules, lib, mid, other string) {
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeExtender(t, mid, "MID", "dev.lib:ACCT-002")
			writeTarget(t, lib, "")
			writeTarget(t, other, "")
		}, nil},
		"live in both": {func(*testing.T, string, string, string, string) {}, nil},
		"Retired here, which is live": {func(t *testing.T, _, lib, _, _ string) {
			writeTarget(t, lib, "R")
		}, nil},
		"differing, but named by no note": {func(t *testing.T, rules, lib, _, _ string) {
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeTarget(t, lib, "T")
		}, nil},
		"named from a realm that doesn't declare it": {func(t *testing.T, rules, lib, _, _ string) {
			other := filepath.Join(filepath.Dir(rules), "other")
			writeRealmManifest(t, other, "dev.other")
			writeExtender(t, other, "OTH", "dev.lib:ACCT-002")
			writeRealmManifest(t, rules, "dev.app",
				`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`,
				`{"realm_id": "dev.mid", "version": "1.0.0", "source": "../mid/"}`,
				`{"realm_id": "dev.other", "version": "1.0.0", "source": "../other/"}`)
			writeInheritEntity(t, rules, "APPOTH", "dev.other:OTH", inheritRule{id: "APPOTH-001", state: "I"})
			writeExtender(t, rules, "APP", "dev.lib:ACCT-001")
			writeTarget(t, lib, "T")
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

// Two sources may use two versions, also when a target is live in one only.
func TestCheckAcceptsLivenessDifferingBySource(t *testing.T) {
	dir, diff := setupSameIDCopy(t, "another source")
	writeExtender(t, filepath.Join(dir, "rules"), "APP", "dev.lib:ACCT-002")
	writeExtender(t, filepath.Join(dir, "rules2"), "TWO", "dev.lib:ACCT-002")
	writeTarget(t, filepath.Join(dir, "lib"), "T")
	writeTarget(t, filepath.Join(dir, "copy"), "I")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitOK)
}

// A qualifier may name a sub-realm of the dependency.
func TestCheckRefusesOmittedTargetNamedThroughASubRealm(t *testing.T) {
	dir, diff := setupTwoReleases(t)
	for where, version := range map[string]string{"lib": "1.0.0", "copy": "2.0.0"} {
		d := filepath.Join(dir, where)
		writeRefsFile(t, d, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json",`+
			` "realm": {"id": "dev.lib", "title": "t", "version": "`+version+`"}, "sub_realms": ["sub"]}`)
		writeRealmManifest(t, filepath.Join(d, "sub"), "dev.lib.sub")
	}
	writeExtender(t, filepath.Join(dir, "rules"), "APP", "dev.lib.sub:ACCT-002")
	writeTarget(t, filepath.Join(dir, "lib"), "T")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	if !errors.Is(err, errDifferingCopies) {
		t.Errorf("error = %v, want errDifferingCopies", err)
	}
}
