package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// An unchanged copy in another realm mustn't vouch for what changed in this
// one.
func TestCheckConsumedKeepsRealmIdentity(t *testing.T) {
	for _, how := range []string{"inherits", "extends"} {
		t.Run(how, func(t *testing.T) {
			dir, _ := setupInheritProject(t, "null", "./rules/", "./rules2/")
			app := inheritEntityJSON("APP", "", "", inheritRule{id: "APP-001", state: "I"})
			if how == "inherits" {
				app = inheritEntityJSON("APP", "dev.lib:ACCT", "", inheritRule{id: "APP-001", state: "I"})
			} else {
				app = withNote(app, "APP-001", "extends", "Builds on dev.lib:ACCT-002.")
			}
			writeInheritFile(t, filepath.Join(dir, "rules"), "APP", app)

			writeRealmManifest(t, filepath.Join(dir, "rules2"), "dev.two",
				`{"realm_id": "dev.copy", "version": "1.0.0", "source": "../copy/"}`)
			writeInheritEntity(t, filepath.Join(dir, "rules2"), "TWO", "", inheritRule{id: "TWO-001", state: "I"})
			writeRealmManifest(t, filepath.Join(dir, "copy"), "dev.copy")
			p := filepath.Join(dir, "lib", "ACCT.json")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			writeInheritFile(t, filepath.Join(dir, "copy"), "ACCT", string(data))

			r := &checkRun{ctx: context.Background(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, cwd: dir}
			if err := r.loadConfig(); err != nil {
				t.Fatal(err)
			}
			if err := r.resolveRules(); err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(data, []byte("Rule ACCT-002."), []byte("Rule changed."), 1)
			if err := os.WriteFile(p, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := r.validateRuleSchemas(); !errors.Is(err, errNotValidated) {
				t.Errorf("error = %v, want errNotValidated", err)
			}
		})
	}
}

// A qualifier may name a dependency's sub-realm; its entities are the
// dependency's.
func TestCheckConsumedAcceptsSubRealmQualifier(t *testing.T) {
	for _, how := range []string{"inherits", "extends"} {
		t.Run(how, func(t *testing.T) {
			dir, diff := setupInheritProject(t, "null", "./rules/")
			lib := filepath.Join(dir, "lib")
			writeRefsFile(t, lib, "realm.json",
				`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.lib", "title": "l", "version": "1.0.0"}, "sub_realms": ["sub"]}`)
			writeRealmManifest(t, filepath.Join(lib, "sub"), "dev.lib.sub")
			writeInheritEntity(t, filepath.Join(lib, "sub"), "SUBACCT", "", inheritRule{id: "SUBACCT-001", state: "I"})
			app := inheritEntityJSON("APP", "", "", inheritRule{id: "APP-001", state: "I"})
			if how == "inherits" {
				app = inheritEntityJSON("APP", "dev.lib.sub:SUBACCT", "", inheritRule{id: "APP-001", state: "I"})
			} else {
				app = withNote(app, "APP-001", "extends", "Builds on dev.lib.sub:SUBACCT-001.")
			}
			writeInheritFile(t, filepath.Join(dir, "rules"), "APP", app)

			_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitOK)
		})
	}
}

func TestSchemaValidatorRefusesAmbiguousKeys(t *testing.T) {
	valid := inheritEntityJSON("ENT", "", "", inheritRule{id: "ENT-001", state: "I"})
	realm := `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "t", "version": "1.0.0"}}`
	sub := func(s, old, repl string) string {
		return string(bytes.Replace([]byte(s), []byte(old), []byte(repl), 1))
	}
	for name, c := range map[string]struct{ file, body string }{
		"merged object":  {"ENT.json", sub(valid, `"entity":`, `"entity": {"visibility": "INVALID"}, "entity":`)},
		"nested":         {"ENT.json", sub(valid, `"state":`, `"state": "X", "state":`)},
		"another case":   {"ENT.json", sub(valid, `"state":`, `"STATE": "X", "state":`)},
		"after a number": {"ENT.json", sub(valid, `"revision": 0`, `"revision": 1e400, "revision": 0`)},
		"manifest":       {"realm.json", sub(realm, `"title":`, `"title": "", "title":`)},
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), c.file)
			v := newSchemaValidator(context.Background())
			validate := v.validateEntityFile
			if c.file == "realm.json" {
				validate = v.validateRealmManifest
			}
			err := validate(p, []byte(c.body))
			if !errors.Is(err, errAmbiguousKey) {
				t.Fatalf("error = %v, want errAmbiguousKey", err)
			}
			testutil.AssertContains(t, err.Error(), c.file)
		})
	}
}
