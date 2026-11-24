package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// setupTransitiveProject makes dev.lib depend on dev.base, which holds BASE.
func setupTransitiveProject(t *testing.T) (dir, diff string) {
	t.Helper()
	dir, diff = setupInheritProject(t, "null", "./rules/")
	base := filepath.Join(dir, "base")
	writeRealmManifest(t, base, "dev.base")
	writeInheritEntity(t, base, "BASE", "", inheritRule{id: "BASE-001", state: "I"})
	writeRealmManifest(t, filepath.Join(dir, "lib"), "dev.lib",
		`{"realm_id": "dev.base", "version": "1.0.0", "source": "../base/"}`)
	return dir, diff
}

func TestCheckValidatesTransitiveDependencies(t *testing.T) {
	for name, link := range map[string]func(t *testing.T, dir string){
		"inherits": func(t *testing.T, dir string) {
			writeInheritEntity(t, filepath.Join(dir, "lib"), "ACCT", "dev.base:BASE", inheritRule{id: "ACCT-002", state: "I"})
		},
		"extends": func(t *testing.T, dir string) {
			body := inheritEntityJSON("ACCT", "", "", inheritRule{id: "ACCT-002", state: "I"})
			writeInheritFile(t, filepath.Join(dir, "lib"), "ACCT", withNote(body, "ACCT-002", "extends", "Builds on dev.base:BASE-001."))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, diff := setupTransitiveProject(t)
			link(t, dir)
			writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})

			_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitOK)

			breakEntity(t, filepath.Join(dir, "base"), "BASE.json")
			_, err = runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "BASE.json does not match the entity/v1.json schema")
		})
	}
}

func TestCheckValidatesSubRealmsOwnDependency(t *testing.T) {
	dir, diff := setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeRefsFile(t, rules, "realm.json",
		`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "a", "version": "1.0.0"}, "sub_realms": ["child"]}`)
	writeRealmManifest(t, filepath.Join(rules, "child"), "dev.app.child",
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../../lib/"}`)
	breakEntity(t, filepath.Join(dir, "lib"), "ACCT.json")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "ACCT.json does not match the entity/v1.json schema")
}

func TestCheckRefusesDependencyChildManifest(t *testing.T) {
	for name, c := range map[string]struct{ child, want string }{
		"malformed": {"{", "is not valid JSON"},
		"missing":   {"", "reading"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, diff := setupInheritProject(t, "null", "./rules/")
			lib := filepath.Join(dir, "lib")
			writeRefsFile(t, lib, "realm.json",
				`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.lib", "title": "l", "version": "1.0.0"}, "sub_realms": ["sub"]}`)
			if c.child != "" {
				writeRefsFile(t, filepath.Join(lib, "sub"), "realm.json", c.child)
			}

			_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), c.want)
		})
	}
}

func TestCheckValidatesUnlistedNestedManifest(t *testing.T) {
	dir, diff := setupInheritProject(t, "null", "./rules/")
	nested := filepath.Join(dir, "rules", "nested")
	writeInheritEntity(t, nested, "NEST", "", inheritRule{id: "NEST-001", state: "I"})
	writeRefsFile(t, nested, "realm.json", `{"realm": {"id": "dev.other", "version": "x"}}`)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "realm.json does not match the realm/v1.json schema")
}

// Each file differs when resolved from when it is validated.
func TestCheckRefusesContentChangedAfterResolution(t *testing.T) {
	cases := map[string]struct {
		file     string
		old, new string
	}{
		"source entity":       {"rules/APP.json", "Rule APP-001.", "Rule swapped."},
		"inherited entity":    {"lib/ACCT.json", "Rule ACCT-002.", "Rule swapped."},
		"source manifest":     {"rules/realm.json", `"title": "t"`, `"title": "swapped"`},
		"dependency manifest": {"lib/realm.json", `"title": "t"`, `"title": "swapped"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := setupInheritProject(t, "null", "./rules/")
			writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "dev.lib:ACCT", inheritRule{id: "APP-001", state: "I"})
			r := &checkRun{ctx: context.Background(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, cwd: dir}
			if err := r.loadConfig(); err != nil {
				t.Fatal(err)
			}
			if err := r.resolveRules(); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, filepath.FromSlash(c.file))
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(data, []byte(c.old), []byte(c.new), 1)
			if bytes.Equal(changed, data) {
				t.Fatalf("%s has no %s to change", p, c.old)
			}
			if err := os.WriteFile(p, changed, 0o600); err != nil {
				t.Fatal(err)
			}

			if err := r.validateRuleSchemas(); !errors.Is(err, errNotValidated) {
				t.Errorf("error = %v, want errNotValidated", err)
			}
		})
	}
}

func TestCheckConsumedRefusesUnknownExtendedRule(t *testing.T) {
	for _, id := range []string{"dev.base:BASE-001", "malformed"} {
		r := &checkRun{extended: map[string]map[string][]inherit.Extended{
			"./rules/": {"dev.app:APP-001": {{ID: id, Body: "Never validated."}}},
		}}
		if err := r.checkConsumed(newSchemaValidator(context.Background())); !errors.Is(err, errNotValidated) {
			t.Errorf("%s: error = %v, want errNotValidated", id, err)
		}
	}
}

func TestDependencyOrigin(t *testing.T) {
	origin, err := remote.ParseGitHub("github://acme/rules@v1.2.3/rules/cli")
	if err != nil {
		t.Fatal(err)
	}
	other, err := remote.ParseGitHub("github://acme/core@v2.0.0/rules/core")
	if err != nil {
		t.Fatal(err)
	}
	sibling := origin
	sibling.Path = "rules/core"
	top := origin
	top.Path = ""

	for source, want := range map[string]*remote.Ref{
		"../core/":                             &sibling,
		"../..":                                &top,
		"../../../up":                          nil,
		"":                                     nil,
		"github://acme/core@v2.0.0/rules/core": &other,
		"github://":                            nil,
	} {
		got := dependencyOrigin(&origin, source)
		if (got == nil) != (want == nil) || (got != nil && got.String() != want.String()) {
			t.Errorf("dependencyOrigin(%q) = %v, want %v", source, got, want)
		}
	}
	if got := dependencyOrigin(nil, "../core/"); got != nil {
		t.Errorf("a local realm got the origin %v", got)
	}
}

func TestCheckValidatesUnlistedNestedManifestInDependency(t *testing.T) {
	dir, diff := setupInheritProject(t, "null", "./rules/")
	nested := filepath.Join(dir, "lib", "nested")
	writeInheritEntity(t, nested, "NEST", "", inheritRule{id: "NEST-001", state: "I"})
	writeRefsFile(t, nested, "realm.json", `{"realm": {"id": "dev.other", "version": "x"}}`)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "realm.json does not match the realm/v1.json schema")
}

// The resolver resolves a dependency's entities, those of its sub-realms too,
// against the dependency's root dependencies only (realmresolve depNode), so
// a sub-realm's own dependency is never loaded and needn't be queued.
func TestCheckDependencySubRealmsOwnDependencyIsNotLoaded(t *testing.T) {
	dir, diff := setupTransitiveProject(t)
	lib := filepath.Join(dir, "lib")
	writeRefsFile(t, lib, "realm.json",
		`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.lib", "title": "l", "version": "1.0.0"}, "sub_realms": ["sub"]}`)
	sub := filepath.Join(lib, "sub")
	writeRealmManifest(t, sub, "dev.lib.sub", `{"realm_id": "dev.base", "version": "1.0.0", "source": "../../base/"}`)
	writeInheritEntity(t, sub, "SUBACCT", "dev.base:BASE", inheritRule{id: "SUBACCT-001", state: "I"})
	writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "dev.lib:SUBACCT", inheritRule{id: "APP-001", state: "I"})

	out, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, out, "realm dev.base is not a dependency of realm dev.lib")
}

func TestSchemaValidatorRefusesGoverningManifestChangedSinceSelection(t *testing.T) {
	dir := t.TempDir()
	writeRealmManifest(t, dir, "dev.app")
	writeInheritEntity(t, dir, "APP", "", inheritRule{id: "APP-001", state: "I"})
	p := filepath.Join(dir, "APP.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	v := newSchemaValidator(context.Background())
	if err := v.validateEntityFile(p, data); err != nil {
		t.Fatal(err)
	}
	writeRealmManifest(t, dir, "dev.changed")
	if err := v.validateGoverning(); !errors.Is(err, errNotValidated) {
		t.Errorf("error = %v, want errNotValidated", err)
	}
}

func TestSchemaValidatorValidatesInheritedFromManifest(t *testing.T) {
	top := resolvedPath(t.TempDir())
	// Invalid (no $schema), and it chooses sub's schema by listing it.
	writeRefsFile(t, top, "realm.json", `{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0"}, "sub_realms": ["sub"]}`)
	sub := filepath.Join(top, "sub")
	writeRealmManifest(t, sub, "dev.app.sub")
	writeInheritEntity(t, sub, "APP", "", inheritRule{id: "APP-001", state: "I"})
	p := filepath.Join(sub, "APP.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	v := newSchemaValidator(context.Background())
	if err := v.validateEntityFile(p, data); err != nil {
		t.Fatal(err)
	}
	err = v.validateGoverning()
	if err == nil {
		t.Fatal("the parent manifest chose a schema without being validated")
	}
	testutil.AssertContains(t, err.Error(), "does not match the realm/v1.json schema")
}

func TestSchemaValidatorKeepsTheManifestASelectionWasMadeFrom(t *testing.T) {
	dir := resolvedPath(t.TempDir())
	writeRealmManifest(t, dir, "dev.app")
	writeInheritEntity(t, dir, "APP", "", inheritRule{id: "APP-001", state: "I"})
	p := filepath.Join(dir, "APP.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	v := newSchemaValidator(context.Background())
	if err := v.validateEntityFile(p, data); err != nil {
		t.Fatal(err)
	}
	writeRealmManifest(t, dir, "dev.changed")
	// A tree or source pass validates the changed file before validateGoverning runs.
	if err := v.validateManifestIn(dir); err != nil {
		t.Fatal(err)
	}
	if err := v.validateGoverning(); !errors.Is(err, errNotValidated) {
		t.Errorf("error = %v, want errNotValidated", err)
	}
}

func TestCheckRefusesSubRealmDependenciesChangedAfterResolution(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeInheritEntity(t, rules, "APP", "", inheritRule{id: "APP-001", state: "I"})
	writeRefsFile(t, rules, "realm.json",
		`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "a", "version": "1.0.0"}, "sub_realms": ["child"]}`)
	child := filepath.Join(rules, "child")
	writeRealmManifest(t, child, "dev.app.child", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "../../lib/"}`)

	r := &checkRun{ctx: context.Background(), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, cwd: dir}
	if err := r.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if err := r.resolveRules(); err != nil {
		t.Fatal(err)
	}
	writeRealmManifest(t, child, "dev.app.child") // its dependency is gone
	if err := r.validateRuleSchemas(); !errors.Is(err, errNotValidated) {
		t.Errorf("error = %v, want errNotValidated", err)
	}
}

func TestValidateDependenciesComparesEverySnapshot(t *testing.T) {
	dir := resolvedPath(t.TempDir())
	writeRealmManifest(t, dir, "dev.lib")
	same, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	stale := *same
	stale.Realm.Title = "as another source read it"

	r := &checkRun{ctx: context.Background(), cwd: t.TempDir()}
	err = r.validateDependencies(newSchemaValidator(r.ctx), "./rules/", []depRealm{
		{realm: &realmresolve.ResolvedRealm{Dir: dir, Realm: same}},
		{realm: &realmresolve.ResolvedRealm{Dir: dir, Realm: &stale}},
	})
	if !errors.Is(err, errNotValidated) {
		t.Errorf("error = %v, want errNotValidated for the second snapshot", err)
	}
}

func TestUnavailableReleaseRemedyNamesTheRelease(t *testing.T) {
	offlineSchemas(t)
	dir := t.TempDir()
	writeRealmManifest(t, dir, "dev.app", `{"realm_id": "dev.steerspec.core", "version": "1.21.0", "source": "../core/"}`)
	data, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = newSchemaValidator(context.Background()).validateRealmManifest(filepath.Join(dir, "realm.json"), data)
	if !errors.Is(err, errSchemaUnavailable) {
		t.Fatalf("error = %v, want errSchemaUnavailable", err)
	}
	testutil.AssertContains(t, err.Error(), "run `strspc schema fetch --core-version 1.21.0` while online")
}
