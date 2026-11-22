package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// breakEntity rewrites dir/name so that it no longer matches the entity
// schema: rule_set.version isn't SemVer.
func breakEntity(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	broken := bytes.Replace(data, []byte(`"version": "0.1.0"`), []byte(`"version": "one"`), 1)
	if bytes.Equal(broken, data) {
		t.Fatalf("%s has no version to break", p)
	}
	if err := os.WriteFile(p, broken, 0o644); err != nil {
		t.Fatal(err)
	}
}

// offlineSchemas makes every schema of the SEC-004 validation unavailable.
func offlineSchemas(t *testing.T) {
	t.Helper()
	orig := newValidationSchemaFetcher
	newValidationSchemaFetcher = func(opts ...schema.Option) *schema.Fetcher {
		return newCachedSchemaFetcher(append(opts,
			schema.WithCacheDir(t.TempDir()), schema.WithBaseURL("http://127.0.0.1:1/schemas"))...)
	}
	t.Cleanup(func() { newValidationSchemaFetcher = orig })
}

func TestCheckRefusesRuleFileThatFailsItsSchema(t *testing.T) {
	dir, diff := setupCheckProject(t, "null", []string{"I"})
	breakEntity(t, filepath.Join(dir, "rules"), "TST.json")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "TST.json does not match the entity/v1.json schema")
	testutil.AssertContains(t, err.Error(), "/rule_set/version")
}

func TestCheckRefusesWhenSchemaUnavailable(t *testing.T) {
	dir, diff := setupCheckProject(t, "null", []string{"I"})
	offlineSchemas(t)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "run `strspc schema fetch` while online")
	if !errors.Is(err, errSchemaUnavailable) {
		t.Errorf("error = %v, want errSchemaUnavailable", err)
	}
}

func TestCheckValidatesDependencyTree(t *testing.T) {
	dir, diff := setupInheritProject(t, "null", "./rules/")
	breakEntity(t, filepath.Join(dir, "lib"), "ACCT.json")

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "ACCT.json does not match the entity/v1.json schema")
}

func TestCheckRefusesRealmManifestThatFailsItsSchema(t *testing.T) {
	dir, diff := setupInheritProject(t, "null", "./rules/")
	writeRefsFile(t, filepath.Join(dir, "rules"), "realm.json",
		`{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "t", "version": "1.0.0"}, `+
			`"dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}], "unknown": true}`)

	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "realm.json does not match the realm/v1.json schema")
}

func TestRuleAddRefusesEntityThatFailsItsSchema(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "ENT")
	breakEntity(t, dir, "ENT.json")
	before, _ := os.ReadFile(filepath.Join(dir, "ENT.json"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "ENT", "--body", "strspc MUST do x.", "--added-by", "github:alice", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "does not match the entity/v1.json schema") {
		t.Fatalf("error = %v, want a schema refusal", err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "ENT.json")); !bytes.Equal(before, after) {
		t.Error("the entity file changed although it failed its schema")
	}
}

func TestRuleAddRefusesWhenSchemaUnavailable(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "ENT")
	offlineSchemas(t)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "ENT", "--body", "strspc MUST do x.", "--added-by", "github:alice", "--dir", dir)
	if !errors.Is(err, errSchemaUnavailable) {
		t.Fatalf("error = %v, want errSchemaUnavailable", err)
	}
}

func TestRealmDepAddRefusesManifestThatFailsItsSchema(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	p := filepath.Join(dir, "realm.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	broken := bytes.Replace(data, []byte(`"$schema"`), []byte(`"$schemaX"`), 1)
	if err := os.WriteFile(p, broken, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@1.0.0", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "does not match the realm/v1.json schema") {
		t.Fatalf("error = %v, want a schema refusal", err)
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(broken, after) {
		t.Error("realm.json changed although it failed its schema")
	}
}

func TestSchemaValidatorSelectsTheRealmsSchemas(t *testing.T) {
	v := newSchemaValidator(context.Background())

	core := t.TempDir()
	writeRefsFile(t, core, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.steerspec.core", "title": "c", "version": "1.25.0"}}`)
	if _, label := v.realmSchemaFor(core); label != "dev.steerspec.core 1.25.0" {
		t.Errorf("core realm label = %q, want its own release", label)
	}

	pinned := t.TempDir()
	writeRefsFile(t, pinned, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "a", "version": "1.0.0"}, `+
		`"dependencies": [{"realm_id": "dev.steerspec.core", "version": "1.21.0", "source": "../core/"}]}`)
	if _, label := v.realmSchemaFor(pinned); label != "dev.steerspec.core 1.21.0" {
		t.Errorf("pinned realm label = %q, want its pin", label)
	}

	// A sub-realm that pins no core release takes its parent's pin, as lint
	// does; a directory the parent doesn't list doesn't.
	parent := t.TempDir()
	writeRefsFile(t, parent, "realm.json", `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app", "title": "a", "version": "1.0.0"}, `+
		`"dependencies": [{"realm_id": "dev.steerspec.core", "version": "1.21.0", "source": "../core/"}], "sub_realms": ["sub"]}`)
	unpinned := `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "dev.app.x", "title": "x", "version": "1.0.0"}}`
	writeRefsFile(t, filepath.Join(parent, "sub"), "realm.json", unpinned)
	writeRefsFile(t, filepath.Join(parent, "other"), "realm.json", unpinned)
	if _, label := v.realmSchemaFor(filepath.Join(parent, "sub")); label != "dev.steerspec.core 1.21.0" {
		t.Errorf("unpinned sub-realm label = %q, want its parent's pin", label)
	}
	if _, label := v.realmSchemaFor(filepath.Join(parent, "other")); label != "the latest schemas" {
		t.Errorf("unlisted realm label = %q, want the latest schemas", label)
	}

	if _, label := v.realmSchemaFor(""); label != "the latest schemas" {
		t.Errorf("no realm label = %q, want the latest schemas", label)
	}
}

func TestSchemaValidatorListsAFewViolations(t *testing.T) {
	v := newSchemaValidator(context.Background())
	dir := t.TempDir()
	p := filepath.Join(dir, "BAD.json")
	err := v.validateEntityFile(p, []byte(`{"entity": 1, "rule_set": 2, "rules": 3, "notes": 4, "extra": 5}`))
	if err == nil {
		t.Fatal("an invalid entity passed")
	}
	testutil.AssertContains(t, err.Error(), "and ")
	testutil.AssertContains(t, err.Error(), "strspc lint lists them")

	if err := v.validateEntityFile(p, []byte("{")); err == nil || !strings.Contains(err.Error(), "is not valid JSON") {
		t.Errorf("malformed JSON: got %v", err)
	}
}

func TestSchemaValidatorNoFetcher(t *testing.T) {
	orig := newValidationSchemaFetcher
	newValidationSchemaFetcher = func(...schema.Option) *schema.Fetcher { return nil }
	t.Cleanup(func() { newValidationSchemaFetcher = orig })

	err := newSchemaValidator(context.TODO()).validateEntityFile(filepath.Join(t.TempDir(), "X.json"), []byte("{}"))
	if !errors.Is(err, errSchemaUnavailable) {
		t.Errorf("error = %v, want errSchemaUnavailable: a missing fetcher must not skip validation", err)
	}
}

func TestSchemaValidatorTreeErrors(t *testing.T) {
	v := newSchemaValidator(context.Background())
	if err := v.validateRealmTree(t.TempDir()); err == nil || !strings.Contains(err.Error(), "reading") {
		t.Errorf("no manifest: got %v", err)
	}

	dir := t.TempDir()
	setupValidRealm(t, dir)
	writeRefsFile(t, dir, "BROKEN.json", "{")
	if err := v.validateRealmTree(dir); err == nil {
		t.Error("a tree with an unparsable entity file passed")
	}

	ok := t.TempDir()
	setupValidRealmWithSubs(t, ok)
	if err := v.validateRealmTree(ok); err != nil {
		t.Errorf("a valid tree with sub-realms: %v", err)
	}
	if len(v.done) == 0 {
		t.Error("nothing was recorded as validated")
	}
}

func TestSchemaValidatorSelectsFromTheBytesItValidates(t *testing.T) {
	manifest := func(id, pin, extra string) string {
		deps := ""
		if pin != "" {
			deps = `, "dependencies": [{"realm_id": "dev.steerspec.core", "version": "` + pin + `", "source": "../core/"}]`
		}
		return `{"$schema": "./_schema/realm.v1.schema.json", "realm": {"id": "` + id + `", "title": "t", "version": "1.0.0"}` + deps + extra + `}`
	}
	top := t.TempDir()
	sub := filepath.Join(top, "sub")
	read := manifest("dev.app", "1.21.0", `, "sub_realms": ["sub"]`)
	// What is on disk by the time the schema is selected isn't what was read.
	writeRefsFile(t, top, "realm.json", manifest("dev.app", "1.25.0", ""))
	writeRefsFile(t, sub, "realm.json", manifest("dev.app.sub", "1.25.0", ""))

	v := newSchemaValidator(context.Background())
	if err := v.validateRealmManifest(filepath.Join(top, "realm.json"), []byte(read)); err != nil {
		t.Fatal(err)
	}
	if _, label := v.realmSchemaFor(top); label != "dev.steerspec.core 1.21.0" {
		t.Errorf("label = %q, want the pin of the validated bytes", label)
	}
	if err := v.validateRealmManifest(filepath.Join(sub, "realm.json"), []byte(manifest("dev.app.sub", "", ""))); err != nil {
		t.Fatal(err)
	}
	if _, label := v.realmSchemaFor(sub); label != "dev.steerspec.core 1.21.0" {
		t.Errorf("sub-realm label = %q, want the pin of the parent's validated bytes", label)
	}
}

func TestSchemaValidatorOwnPinStopsInheritance(t *testing.T) {
	top := t.TempDir()
	writeRefsFile(t, top, "realm.json", `{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0"}, `+
		`"dependencies": [{"realm_id": "dev.steerspec.core", "version": "1.21.0"}], "sub_realms": ["sub"]}`)
	sub := filepath.Join(top, "sub")
	writeRefsFile(t, sub, "realm.json", `{"realm": {"id": "dev.app.sub", "title": "s", "version": "1.0.0"}, `+
		`"dependencies": [{"realm_id": "dev.steerspec.core", "version": "1.22.0-rc.1"}]}`)

	if _, label := newSchemaValidator(context.Background()).realmSchemaFor(sub); label != "the latest schemas" {
		t.Errorf("label = %q, want the latest schemas, as lint selects for a pin that isn't a release", label)
	}
}
