package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// tweakDoctorEnv applies f to the environment of every doctor run of the test,
// after fakeDoctorHost has set it up.
func tweakDoctorEnv(t *testing.T, f func(*doctorEnv)) {
	t.Helper()
	original := newDoctorEnv
	newDoctorEnv = func(ctx context.Context, cwd string) *doctorEnv {
		env := original(ctx, cwd)
		f(env)
		return env
	}
	t.Cleanup(func() { newDoctorEnv = original })
}

func TestDoctorPathWithoutRunningBinary(t *testing.T) {
	fakeDoctorHost(t)
	tweakDoctorEnv(t, func(env *doctorEnv) {
		env.executable = func() (string, error) { return "", errors.New("no /proc") }
	})
	report, err := runDoctorJSON(t, t.TempDir())
	assertExit(t, err, exitOK)
	c := assertDoctorStatus(t, report, "install.path", "", doctorSkip)
	testutil.AssertContains(t, c.Message, "can't locate the running binary: no /proc")
}

// On Windows, paths that differ only in case name the same binary.
func TestDoctorPathIgnoresCaseOnWindows(t *testing.T) {
	h := fakeDoctorHost(t)
	h.paths["strspc"] = strings.ToUpper(h.self)
	tweakDoctorEnv(t, func(env *doctorEnv) {
		env.goos = "windows"
		env.executable = func() (string, error) { return strings.ToLower(h.self), nil }
	})
	report, _ := runDoctorJSON(t, t.TempDir())
	assertDoctorStatus(t, report, "install.path", "", doctorPass)
}

func TestDoctorConfigUnreadable(t *testing.T) {
	fakeDoctorHost(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".strspc", "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := runDoctorJSON(t, dir)
	assertExit(t, err, exitViolations)
	c := assertDoctorStatus(t, report, "config.present", "", doctorFail)
	testutil.AssertContains(t, c.Message, "can't read .strspc/config.yaml")
	assertDoctorStatus(t, report, "config.parse", "", doctorSkip)
}

func TestDoctorConfigSchemaEdgeCases(t *testing.T) {
	fakeDoctorHost(t)
	dir := writeDoctorProject(t, "evaluator:\n  provider: null\nfail_on: [I]\n")

	writeRefsFile(t, dir, ".strspc/cache/schemas/config/v1.json", "{not json")
	report, _ := runDoctorJSON(t, dir)
	c := assertDoctorStatus(t, report, "config.schema", "", doctorSkip)
	testutil.AssertContains(t, c.Message, "parsing")

	// Two violations at two locations.
	writeRefsFile(t, dir, ".strspc/cache/schemas/config/v1.json", `{"type":"object","properties":{"evaluator":{"type":"string"},"fail_on":{"type":"string"}}}`)
	report, _ = runDoctorJSON(t, dir)
	c = assertDoctorStatus(t, report, "config.schema", "", doctorWarn)
	testutil.AssertContains(t, c.Message, "(and 1 more)")
}

func TestDoctorProviderConfigFailures(t *testing.T) {
	cases := []struct {
		name, config, want string
	}{
		{"provider with a control character", "evaluator:\n  provider: \"cla\\x07ude\"\n", "evaluator.provider"},
		{"invalid effort", "evaluator:\n  provider: claude\n  effort: bogus\n", "invalid effort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeDoctorHost(t)
			report, err := runDoctorJSON(t, writeDoctorProject(t, tc.config))
			assertExit(t, err, exitViolations)
			c := assertDoctorStatus(t, report, "config.provider", "", doctorFail)
			testutil.AssertContains(t, c.Message, tc.want)
		})
	}
}

// A malformed github:// source is left to sources.state; credentials.github
// then has nothing to report.
func TestDoctorGitHubTokenSkipsMalformedSource(t *testing.T) {
	fakeDoctorHost(t)
	report, _ := runDoctorJSON(t, writeDoctorProject(t, "rules:\n  - source: github://nothing\n    scope: global\n"))
	assertDoctorStatus(t, report, "credentials.github", "", doctorSkip)
	assertDoctorStatus(t, report, "sources.state", "github://nothing", doctorFail)
}

// The realm and entity schemas are both needed: a cache holding only the realm
// schema still skips the validation.
func TestDoctorRealmValidateNeedsEntitySchema(t *testing.T) {
	fakeDoctorHost(t)
	project, _ := realmProject(t)
	cacheRealmSchemas(t, "realm/v1.json")
	report, _ := runDoctorJSON(t, project)
	c := assertDoctorStatus(t, report, "realm.validate", "./rules", doctorSkip)
	testutil.AssertContains(t, c.Message, "not cached")
}

// writeCachedRealm puts the realm id, holding one valid entity, in the user
// cache where `realm validate` stores github://acme/<repo>@v1.0.0/rules.
func writeCachedRealm(t *testing.T, cacheDir, repo, id string) {
	t.Helper()
	sum := sha256.Sum256([]byte("rules"))
	dir := filepath.Join(cacheDir, "strspc", "realms", "github", "acme", repo, "v1.0.0", hex.EncodeToString(sum[:])[:12])
	writeRefsFile(t, dir, "realm.json", `{"realm": {"id": "`+id+`", "title": "t", "version": "1.0.0"}, "dependencies": []}`)
	writeEntityFile(t, dir, "TST.json", validEntityJSON)
}

func TestDoctorRealmDependencies(t *testing.T) {
	t.Run("cached github dependency", func(t *testing.T) {
		h := fakeDoctorHost(t)
		project, rules := realmProject(t)
		writeRealmDeps(t, rules, entity.RealmDep{RealmID: "dev.example.other", Version: "1.0.0", Source: "github://acme/other@v1.0.0/rules"})
		writeCachedRealm(t, h.cacheDir, "other", "dev.example.other")
		cacheRealmSchemas(t, "realm/v1.json", "entity/v1.json", "entity/bootstrap.json")
		report, _ := runDoctorJSON(t, project)
		c := doctorCheck(t, report, "realm.validate", "./rules")
		testutil.AssertNotContains(t, c.Message, "warning")
		if c.Status == doctorSkip {
			t.Errorf("validation skipped: %s", c.Message)
		}
	})
	t.Run("local dependency", func(t *testing.T) {
		fakeDoctorHost(t)
		project, rules := realmProject(t)
		lib := filepath.Join(project, "lib")
		writeRefsFile(t, lib, "realm.json", `{"realm": {"id": "dev.example.lib", "title": "t", "version": "1.0.0"}, "dependencies": []}`)
		writeEntityFile(t, lib, "TST.json", validEntityJSON)
		writeRealmDeps(t, rules, entity.RealmDep{RealmID: "dev.example.lib", Version: "1.0.0", Source: "../lib"})
		cacheRealmSchemas(t, "realm/v1.json", "entity/v1.json", "entity/bootstrap.json")
		report, _ := runDoctorJSON(t, project)
		c := doctorCheck(t, report, "realm.validate", "./rules")
		if c.Status == doctorSkip {
			t.Errorf("validation skipped: %s", c.Message)
		}
	})
	t.Run("malformed github dependency", func(t *testing.T) {
		fakeDoctorHost(t)
		project, rules := realmProject(t)
		writeRealmDeps(t, rules, entity.RealmDep{RealmID: "dev.example.other", Version: "1.0.0", Source: "github://nothing"})
		cacheRealmSchemas(t, "realm/v1.json", "entity/v1.json", "entity/bootstrap.json")
		report, _ := runDoctorJSON(t, project)
		c := doctorCheck(t, report, "realm.validate", "./rules")
		if c.Status == doctorPass {
			t.Errorf("a malformed dependency passed: %s", c.Message)
		}
	})
	t.Run("user cache unavailable", func(t *testing.T) {
		fakeDoctorHost(t)
		tweakDoctorEnv(t, func(env *doctorEnv) {
			env.userCacheDir = func() (string, error) { return "", errors.New("no home") }
		})
		project, rules := realmProject(t)
		writeRealmDeps(t, rules, entity.RealmDep{RealmID: "dev.example.other", Version: "1.0.0", Source: "github://acme/other@v1.0.0/rules"})
		cacheRealmSchemas(t, "realm/v1.json", "entity/v1.json", "entity/bootstrap.json")
		report, _ := runDoctorJSON(t, project)
		c := doctorCheck(t, report, "realm.validate", "./rules")
		if c.Status == doctorPass {
			t.Errorf("a dependency outside any cache passed: %s", c.Message)
		}
	})
}

// Standards declared by a sub-realm's manifest or by a sub-entity are found.
func TestDoctorRealmCoreFindsNestedDeclarations(t *testing.T) {
	const std = `"standards": [{"id": "ietf.rfc2119", "relation": "derived_from"}]`
	t.Run("sub-realm manifest", func(t *testing.T) {
		fakeDoctorHost(t)
		cacheRealmSchemas(t)
		project, rules := realmProject(t)
		writeRefsFile(t, rules, "realm.json", strings.Replace(validRealmJSON, `"dependencies": []`, `"dependencies": [], "sub_realms": ["sub"]`, 1))
		writeRefsFile(t, rules, "sub/realm.json", `{"realm": {"id": "dev.steerspec.test.sub", "title": "t", "version": "0.1.0", `+std+`}}`)
		report, _ := runDoctorJSON(t, project)
		c := assertDoctorStatus(t, report, "realm.core", "./rules", doctorPass)
		testutil.AssertContains(t, c.Message, "standards of 1 file(s)")
	})
	t.Run("sub-entity", func(t *testing.T) {
		fakeDoctorHost(t)
		cacheRealmSchemas(t)
		project, rules := realmProject(t)
		sub := `{"entity": {"id": "SUBT", "title": "Sub", ` + std + `}, "rules": [], "notes": []}`
		withSub := strings.Replace(validEntityJSON, `"notes": []`, `"notes": [], "sub_entities": [`+sub+`]`, 1)
		writeEntityFile(t, rules, "TST.json", withSub)
		report, _ := runDoctorJSON(t, project)
		c := assertDoctorStatus(t, report, "realm.core", "./rules", doctorPass)
		testutil.AssertContains(t, c.Message, "standards of 1 file(s)")
	})
}

// A git that can't run is reported, and the gitignore check is skipped.
func TestDoctorGitignoreGitFails(t *testing.T) {
	h := fakeDoctorHost(t)
	h.paths["git"] = filepath.Join(t.TempDir(), "no-such-git")
	if _, err := exec.LookPath(h.paths["git"]); err == nil {
		t.Fatal("the fake git exists")
	}
	dir := t.TempDir()
	writeRefsFile(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	report, _ := runDoctorJSON(t, dir)
	c := assertDoctorStatus(t, report, "project.gitignore", "", doctorSkip)
	testutil.AssertContains(t, c.Message, "git check-ignore failed")
}
