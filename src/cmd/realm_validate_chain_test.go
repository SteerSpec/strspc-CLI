package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// setupValidateInherit is setupInheritProject as realm validate reads it: the
// realm dev.app in rules/, depending on dev.lib in lib/, whose ACCT has the
// Published ACCT-001 and the Implemented ACCT-002.
func setupValidateInherit(t *testing.T) (rules, lib string) {
	t.Helper()
	setupRealmValidateTest(t)
	dir, _ := setupInheritProject(t, "null", "./rules/")
	rules, lib = filepath.Join(dir, "rules"), filepath.Join(dir, "lib")
	for _, d := range []string{rules, lib} {
		writeFile(t, d, "_schema/entity.v1.schema.json", `{"$id": "entity-v1"}`)
	}
	return rules, lib
}

// validateCodes runs realm validate --json with args and returns its exit
// error and the severity of each code it reports.
func validateCodes(t *testing.T, args ...string) (map[string]result.Severity, error) {
	t.Helper()
	out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "validate", "--json"}, args...)...)
	var diags []result.Diagnostic
	if jerr := json.Unmarshal([]byte(out), &diags); jerr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jerr, out)
	}
	codes := map[string]result.Severity{}
	for _, d := range diags {
		codes[d.Code] = d.Severity
	}
	return codes, err
}

// realm validate resolves the realm as check does, so an override that
// breaks the chain or visibility fails it too, with or without --recursive.
func TestRealmValidateReportsOverrideChain(t *testing.T) {
	for name, c := range map[string]struct {
		protected bool
		// visibility is what SAVACCT declares; undeclared, it takes the
		// narrowest one it inherits (ENT-011).
		visibility string
		rule       inheritRule
		want       []string
	}{
		"unknown target":              {rule: inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-009"}, want: []string{"RR015"}},
		"protected entity's rule":     {protected: true, rule: inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"}, want: []string{"RR023"}},
		"wider than what it inherits": {protected: true, visibility: "public", rule: inheritRule{id: "SAVACCT-001", state: "P"}, want: []string{"RR024"}},
	} {
		for _, mode := range [][]string{nil, {"--recursive"}, {"--recursive", "--strict"}} {
			t.Run(name+" "+strings.Join(mode, " "), func(t *testing.T) {
				rules, lib := setupValidateInherit(t)
				if c.protected {
					body := inheritEntityJSON("ACCT", "", "", inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "I"})
					writeInheritFile(t, lib, "ACCT", strings.Replace(body, `"description": "d"`, `"description": "d", "visibility": "protected"`, 1))
				}
				if c.visibility == "" {
					writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", c.rule)
				} else {
					body := inheritEntityJSON("SAVACCT", "dev.lib:ACCT", "", c.rule)
					writeInheritFile(t, rules, "SAVACCT", strings.Replace(body, `"description": "d"`, `"description": "d", "visibility": "`+c.visibility+`"`, 1))
				}

				codes, err := validateCodes(t, append([]string{rules}, mode...)...)
				if err == nil {
					t.Errorf("validate passed; codes %v", codes)
				}
				for _, code := range c.want {
					if codes[code] != result.Error {
						t.Errorf("%s: severity %q, want error (codes %v)", code, codes[code], codes)
					}
				}
			})
		}
	}
}

// A warning of the resolver is one for validate, and an error with --strict.
func TestRealmValidateStrictPromotesResolverWarnings(t *testing.T) {
	rules, lib := setupValidateInherit(t)
	writeInheritEntity(t, lib, "ACCT", "", inheritRule{id: "ACCT-001", state: "D"}, inheritRule{id: "ACCT-002", state: "I"})
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})

	codes, err := validateCodes(t, rules)
	if err != nil || codes["RR016"] != result.Warning {
		t.Errorf("without --strict: err %v, codes %v; want a passing RR016 warning", err, codes)
	}
	codes, err = validateCodes(t, rules, "--strict")
	if err == nil || codes["RR016"] != result.Error {
		t.Errorf("with --strict: err %v, codes %v; want a failing RR016 error", err, codes)
	}
}

// A sound chain still passes.
func TestRealmValidatePassesInheritingRealm(t *testing.T) {
	rules, _ := setupValidateInherit(t)
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})
	for _, mode := range [][]string{nil, {"--recursive", "--strict"}} {
		if codes, err := validateCodes(t, append([]string{rules}, mode...)...); err != nil {
			t.Errorf("%v: %v (codes %v)", mode, err, codes)
		}
	}
}

// An entity that declares no visibility takes the narrowest one it inherits
// (ENT-011, strspc-manager v1.57.0), so inheriting a protected entity is no
// longer wider than it (RR024).
func TestRealmValidateUndeclaredInheritsVisibility(t *testing.T) {
	rules, lib := setupValidateInherit(t)
	body := inheritEntityJSON("ACCT", "", "", inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "I"})
	writeInheritFile(t, lib, "ACCT", strings.Replace(body, `"description": "d"`, `"description": "d", "visibility": "protected"`, 1))
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P"})
	for _, mode := range [][]string{nil, {"--recursive", "--strict"}} {
		codes, err := validateCodes(t, append([]string{rules}, mode...)...)
		if _, rr024 := codes["RR024"]; err != nil || rr024 {
			t.Errorf("%v: %v (codes %v), want a pass without RR024", mode, err, codes)
		}
	}
}

// A dependency that can't be loaded stays a warning, as realm-lint's RM016
// is, so that validate runs offline; --strict makes it an error.
func TestRealmValidateUnloadableDependencyWarns(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	writeRealmManifest(t, dir, "dev.app", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "../missing/"}`)
	writeFile(t, dir, "_schema/entity.v1.schema.json", `{"$id": "entity-v1"}`)

	codes, err := validateCodes(t, dir)
	if err != nil || codes["RR002"] != result.Warning {
		t.Errorf("err %v, codes %v; want a passing RR002 warning", err, codes)
	}
	if _, err := validateCodes(t, dir, "--strict"); err == nil {
		t.Error("--strict passed an unloadable dependency")
	}
}

// A dependency that is reached but holds an entity file that doesn't load is
// an error, as in check: that isn't being offline.
func TestRealmValidateBrokenDependencyFileFails(t *testing.T) {
	rules, lib := setupValidateInherit(t)
	writeFile(t, lib, "BROKEN.json", "{")
	codes, err := validateCodes(t, rules)
	if err == nil || codes["RR002"] != result.Error {
		t.Errorf("err %v, codes %v; want a failing RR002 error", err, codes)
	}
}

// Neither realm-lint nor the resolver opens a dependency directory before
// projectPath accepts it, as in check.
func TestRealmValidateRefusesLinkedDependency(t *testing.T) {
	rules, _ := setupValidateInherit(t)
	newRealmValidator = func(string, bool, bool, bool) *realmValidatorSet {
		return &realmValidatorSet{realmLinter: realmlint.New(realmlint.WithDependencyFetcher(
			func(context.Context, string, entity.RealmDep) (string, error) {
				t.Error("realm-lint fetched a dependency before the path check")
				return "", errors.New("not fetched")
			},
		))}
	}
	refuseLinks(t)
	relink(t, filepath.Dir(rules), "lib")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", rules)
	if err == nil {
		t.Fatal("validate resolved a linked dependency")
	}
	testutil.AssertContains(t, err.Error(), "dependency dev.lib of dev.app")
	testutil.AssertContains(t, err.Error(), "is a link")
}

// Without a user cache a local realm is still validated.
func TestRealmValidateWithoutUserCache(t *testing.T) {
	rules, _ := setupValidateInherit(t)
	for _, v := range []string{"HOME", "XDG_CACHE_HOME", "LocalAppData"} {
		t.Setenv(v, "")
	}
	if _, err := os.UserCacheDir(); err == nil {
		t.Skip("this system has a user cache without HOME")
	}
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-009"})
	codes, err := validateCodes(t, rules)
	if err == nil || codes["RR015"] != result.Error {
		t.Errorf("err %v, codes %v; want RR015 reported", err, codes)
	}
}

// Only a dependency's diagnostics are warnings: the resolver reports a root
// entity file it can't read as RR002 too.
func TestUnloadableDependency(t *testing.T) {
	for msg, want := range map[string]bool{
		`dependency "dev.lib": resolved directory "/x" does not exist or is not a directory`: true,
		`dependency "dev.lib" has empty source`:                                              true,
		`cannot parse /r/ACCT.json: unexpected end of JSON input`:                            false,
		`failed to walk directory "/r": permission denied`:                                   false,
	} {
		if got := unloadableDependency(&result.Diagnostic{Code: "RR002", Message: msg}); got != want {
			t.Errorf("%q: %v, want %v", msg, got, want)
		}
	}
	if unloadableDependency(&result.Diagnostic{Code: "RR015", Message: `dependency "dev.lib"`}) {
		t.Error("RR015 counted as an unloadable dependency")
	}
}
