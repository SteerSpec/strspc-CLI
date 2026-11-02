package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// doctorHost is the host a test runs doctor on.
type doctorHost struct {
	version  string
	env      map[string]string
	paths    map[string]string // what lookPath finds; absent means not on PATH
	self     string            // the running binary
	cacheDir string            // the user cache
}

// fakeDoctorHost points doctor at a host where strspc is the running binary and
// on PATH, git is on PATH, and no variable is set. It also clears the GitHub
// variables the manager's remote package reads itself. Tests take a t.TempDir()
// to be outside any git repository, as it is on CI.
func fakeDoctorHost(t *testing.T) *doctorHost {
	t.Helper()
	for _, v := range []string{"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_REPOSITORY_OWNER", "STRSPC_TOKEN_OWNERS"} {
		t.Setenv(v, "")
	}
	bin := t.TempDir()
	self := filepath.Join(bin, "strspc")
	writeRefsFile(t, bin, "strspc", "binary")
	h := &doctorHost{
		version:  "v1.2.3",
		env:      map[string]string{},
		paths:    map[string]string{"strspc": self, "git": "/usr/bin/git"},
		self:     self,
		cacheDir: t.TempDir(),
	}
	if git, err := exec.LookPath("git"); err == nil {
		h.paths["git"] = git // project.gitignore runs it
	}
	original := newDoctorEnv
	newDoctorEnv = func(ctx context.Context, cwd string) *doctorEnv {
		env := original(ctx, cwd)
		env.version = h.version
		env.goos, env.goarch, env.goVersion = "linux", "amd64", "go1.0"
		env.getenv = func(k string) string { return h.env[k] }
		env.lookPath = func(name string) (string, error) {
			if p, ok := h.paths[name]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		}
		env.executable = func() (string, error) { return h.self, nil }
		env.now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
		env.userCacheDir = func() (string, error) { return h.cacheDir, nil }
		return env
	}
	t.Cleanup(func() { newDoctorEnv = original })
	return h
}

// cacheRealmSchemas points the realm schema fetchers at an empty cache holding
// the given files (paths below the cache, such as realm/v1.json), behind a
// server that records every request: doctor must send none.
func cacheRealmSchemas(t *testing.T, files ...string) *[]string {
	t.Helper()
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		_, _ = w.Write([]byte(allowSchema))
	}))
	t.Cleanup(srv.Close)
	cacheDir := t.TempDir()
	for _, f := range files {
		writeRefsFile(t, cacheDir, f, allowSchema)
	}
	original := newSchemaFetcher
	newSchemaFetcher = func(opts ...schema.Option) *schema.Fetcher {
		return schema.New(append([]schema.Option{
			schema.WithBaseURL(srv.URL + "/schemas"),
			schema.WithCacheDir(cacheDir),
		}, opts...)...)
	}
	t.Cleanup(func() { newSchemaFetcher = original })
	return &requested
}

func runDoctorJSON(t *testing.T, dir string, args ...string) (*doctorReport, error) {
	t.Helper()
	cmd := NewRootCmd()
	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"doctor", dir, "--json"}, args...))
	err := cmd.Execute()
	var report doctorReport
	if jerr := json.Unmarshal([]byte(stdout.String()), &report); jerr != nil {
		t.Fatalf("output is not one JSON document: %v\n%s", jerr, stdout.String())
	}
	return &report, err
}

// doctorCheck returns the result of id for subject, failing the test when
// there is none.
func doctorCheck(t *testing.T, report *doctorReport, id, subject string) doctorResult {
	t.Helper()
	for _, c := range report.Checks {
		if c.ID == id && c.Subject == subject {
			return c
		}
	}
	t.Fatalf("no %s result for %q in %+v", id, subject, report.Checks)
	return doctorResult{}
}

func assertDoctorStatus(t *testing.T, report *doctorReport, id, subject, want string) doctorResult {
	t.Helper()
	c := doctorCheck(t, report, id, subject)
	if c.Status != want {
		t.Errorf("%s (%s): status %s, want %s: %s", id, subject, c.Status, want, c.Message)
	}
	return c
}

// writeDoctorProject writes a project whose config holds body.
func writeDoctorProject(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	writeRawCheckConfig(t, dir, body)
	return dir
}

func TestDoctorNoConfig(t *testing.T) {
	fakeDoctorHost(t)
	dir := t.TempDir()

	report, err := runDoctorJSON(t, dir)
	assertExit(t, err, exitOK)
	c := assertDoctorStatus(t, report, "config.present", "", doctorWarn)
	if c.Remedy != "strspc init" {
		t.Errorf("remedy %q, want strspc init", c.Remedy)
	}
	for _, id := range []string{"config.parse", "config.provider", "credentials.provider", "sources.state", "realm.validate"} {
		assertDoctorStatus(t, report, id, "", doctorSkip)
	}

	_, err = runDoctorJSON(t, dir, "--strict")
	assertExit(t, err, exitViolations)
}

func TestDoctorJSONShape(t *testing.T) {
	fakeDoctorHost(t)
	dir := writeDoctorProject(t, "rules:\n  - source: ./rules\n    scope: local\nevaluator:\n  provider: null\n")
	writeRefsFile(t, dir, "rules/.keep", "")

	report, err := runDoctorJSON(t, dir)
	assertExit(t, err, exitOK)
	if report.Version != "v1.2.3" {
		t.Errorf("version %q", report.Version)
	}
	var sum doctorSummary
	seen := map[string]bool{}
	for _, c := range report.Checks {
		if c.Group == "" || !strings.HasPrefix(c.ID, c.Group+".") {
			t.Errorf("check %+v: id doesn't start with its group", c)
		}
		seen[c.Group] = true
		switch c.Status {
		case doctorPass:
			sum.Pass++
		case doctorWarn:
			sum.Warn++
		case doctorFail:
			sum.Fail++
		case doctorSkip:
			sum.Skip++
		case doctorInfo:
			sum.Info++
		default:
			t.Errorf("check %s: unknown status %q", c.ID, c.Status)
		}
	}
	if sum != report.Summary {
		t.Errorf("summary %+v, counted %+v", report.Summary, sum)
	}
	for _, g := range []string{"install", "tools", "config", "credentials", "sources", "realm", "project"} {
		if !seen[g] {
			t.Errorf("no %s check", g)
		}
	}
	assertDoctorStatus(t, report, "sources.state", "./rules", doctorPass)
}

func TestDoctorHumanOutput(t *testing.T) {
	fakeDoctorHost(t)
	dir := writeDoctorProject(t, "rules:\n  - source: ./missing\n    scope: local\nevaluator:\n  provider: claude\n")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "doctor", dir)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "strspc doctor v1.2.3")
	testutil.AssertContains(t, out, "✗ state      ./missing (local): directory not found")
	testutil.AssertContains(t, out, "⚠ provider   ANTHROPIC_API_KEY is not set")
	testutil.AssertContains(t, out, "→ export ANTHROPIC_API_KEY, or set evaluator.provider: null")
	testutil.AssertContains(t, out, "1 failed, ")
	if err == nil || err.Error() != "doctor: 1 check(s) failed" {
		t.Errorf("error %v", err)
	}
}

func TestDoctorConfigFailures(t *testing.T) {
	cases := []struct {
		name, config, id, want string
	}{
		{"yaml", "rules: [\n", "config.parse", "parsing config.yaml"},
		{"ttl", "cache:\n  ttl: soon\n", "config.ttl", `"soon" is not a duration`},
		{"fail_on", "fail_on: [implemnted]\n", "config.states", "invalid fail_on state"},
		{"evaluate_on", "evaluate_on: [Q]\n", "config.states", "invalid evaluate_on state"},
		{"scope", "rules:\n  - source: ./rules\n    scope: world\n", "config.scopes", "invalid scope"},
		{"unknown provider", "evaluator:\n  provider: gpt\n", "config.provider", "not a known provider"},
		{"unimplemented provider", "evaluator:\n  provider: ollama\n", "config.provider", "not implemented yet"},
		{"model", "evaluator:\n  provider: claude\n  model: \"a\\nb\"\n", "config.provider", "evaluator.model contains a control character"},
		{"endpoint", "evaluator:\n  provider: claude\n  endpoint: http://evil.example\n", "config.provider", "not an https URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeDoctorHost(t)
			report, err := runDoctorJSON(t, writeDoctorProject(t, tc.config))
			assertExit(t, err, exitViolations)
			c := assertDoctorStatus(t, report, tc.id, "", doctorFail)
			if !strings.Contains(c.Message, tc.want) {
				t.Errorf("message %q, want it to contain %q", c.Message, tc.want)
			}
			if c.Remedy == "" {
				t.Error("no remedy")
			}
		})
	}
}

func TestDoctorParseFailureSkipsTheRest(t *testing.T) {
	fakeDoctorHost(t)
	report, _ := runDoctorJSON(t, writeDoctorProject(t, "rules: [\n"))
	for _, id := range []string{"config.schema", "config.ttl", "config.provider", "credentials.github", "sources.state"} {
		assertDoctorStatus(t, report, id, "", doctorSkip)
	}
}

func TestDoctorConfigSchemaFromCacheOnly(t *testing.T) {
	fakeDoctorHost(t)
	dir := writeDoctorProject(t, "evaluator:\n  provider: null\nextra: 1\n")

	report, err := runDoctorJSON(t, dir)
	assertExit(t, err, exitOK)
	c := assertDoctorStatus(t, report, "config.schema", "", doctorSkip)
	testutil.AssertContains(t, c.Message, "not cached")
	if _, err := os.Stat(filepath.Join(dir, ".strspc", "cache")); err == nil {
		t.Error("doctor filled the cache")
	}

	writeRefsFile(t, dir, ".strspc/cache/schemas/config/v1.json", `{"type":"object","additionalProperties":false,"properties":{"evaluator":{}}}`)
	report, err = runDoctorJSON(t, dir)
	assertExit(t, err, exitOK)
	assertDoctorStatus(t, report, "config.schema", "", doctorWarn)

	writeRefsFile(t, dir, ".strspc/cache/schemas/config/v1.json", allowSchema)
	report, _ = runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "config.schema", "", doctorPass)
}

func TestDoctorAPIKey(t *testing.T) {
	h := fakeDoctorHost(t)
	dir := writeDoctorProject(t, "evaluator:\n  provider: claude\n")

	report, err := runDoctorJSON(t, dir)
	assertExit(t, err, exitOK) // a warning
	assertDoctorStatus(t, report, "config.provider", "", doctorPass)
	assertDoctorStatus(t, report, "credentials.provider", "", doctorWarn)
	_, err = runDoctorJSON(t, dir, "--strict")
	assertExit(t, err, exitViolations)

	h.env["ANTHROPIC_API_KEY"] = "sk-secret-value"
	out, err := testutil.ExecuteCommand(NewRootCmd(), "doctor", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertNotContains(t, out, "sk-secret-value")
	report, _ = runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "credentials.provider", "", doctorPass)

	report, _ = runDoctorJSON(t, writeDoctorProject(t, "evaluator:\n  provider: null\n"))
	assertDoctorStatus(t, report, "credentials.provider", "", doctorSkip)
}

func TestDoctorGitHubToken(t *testing.T) {
	fakeDoctorHost(t)
	const src = "github://acme/rules@v1.0.0/rules"
	dir := writeDoctorProject(t, "rules:\n  - source: "+src+"\n    scope: global\n")

	report, _ := runDoctorJSON(t, dir)
	c := assertDoctorStatus(t, report, "credentials.github", src, doctorWarn)
	testutil.AssertContains(t, c.Remedy, "GITHUB_TOKEN")

	t.Setenv("GITHUB_TOKEN", "ghp-secret")
	report, _ = runDoctorJSON(t, dir)
	c = assertDoctorStatus(t, report, "credentials.github", src, doctorWarn)
	testutil.AssertContains(t, c.Remedy, "STRSPC_TOKEN_OWNERS")

	t.Setenv("STRSPC_TOKEN_OWNERS", "acme")
	out, _ := testutil.ExecuteCommand(NewRootCmd(), "doctor", dir, "--json")
	testutil.AssertNotContains(t, out, "ghp-secret")
	report, _ = runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "credentials.github", src, doctorPass)

	report, _ = runDoctorJSON(t, writeDoctorProject(t, "rules: []\n"))
	assertDoctorStatus(t, report, "credentials.github", "", doctorSkip)
}

func TestDoctorSources(t *testing.T) {
	fakeDoctorHost(t)
	dir := writeDoctorProject(t, "rules:\n"+
		"  - source: ./rules\n    scope: local\n"+
		"  - source: ./gone\n    scope: local\n"+
		"  - source: github://o/r@v1.0.0/rules\n    scope: global\n"+
		"  - source: github://o/r@v2.0.0/rules\n    scope: global\n"+
		"  - source: https://example.com/rules\n    scope: global\n")
	writeRefsFile(t, dir, "rules/.keep", "")
	writeCachedSource(t, dir, "v1.0.0")

	report, err := runDoctorJSON(t, dir)
	assertExit(t, err, exitViolations)
	assertDoctorStatus(t, report, "sources.state", "./rules", doctorPass)
	assertDoctorStatus(t, report, "sources.state", "./gone", doctorFail)
	assertDoctorStatus(t, report, "sources.state", "github://o/r@v1.0.0/rules", doctorPass)
	c := assertDoctorStatus(t, report, "sources.state", "github://o/r@v2.0.0/rules", doctorWarn)
	if c.Remedy != "strspc sync" {
		t.Errorf("remedy %q", c.Remedy)
	}
	assertDoctorStatus(t, report, "sources.state", "https://example.com/rules", doctorFail)
}

func TestDoctorStaleSource(t *testing.T) {
	fakeDoctorHost(t)
	dir := writeDoctorProject(t, "rules:\n  - source: github://o/r@latest/rules\n    scope: global\ncache:\n  ttl: 1h\n")
	writeCachedSource(t, dir, "v1.0.0")
	writeLatestTag(t, dir, "v1.0.0", time.Now().Add(-2*time.Hour))

	report, _ := runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "sources.state", "github://o/r@latest/rules", doctorWarn)
}

func TestDoctorInstall(t *testing.T) {
	h := fakeDoctorHost(t)
	dir := t.TempDir()

	report, _ := runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "install.version", "", doctorPass)
	assertDoctorStatus(t, report, "install.path", "", doctorPass)
	c := assertDoctorStatus(t, report, "install.platform", "", doctorInfo)
	if c.Message != "linux/amd64, go1.0" {
		t.Errorf("platform %q", c.Message)
	}

	h.version = "dev"
	report, _ = runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "install.version", "", doctorWarn)

	other := filepath.Join(t.TempDir(), "strspc")
	writeRefsFile(t, filepath.Dir(other), "strspc", "other")
	h.paths["strspc"] = other
	report, _ = runDoctorJSON(t, dir)
	c = assertDoctorStatus(t, report, "install.path", "", doctorWarn)
	testutil.AssertContains(t, c.Message, resolvedPath(other))
	testutil.AssertContains(t, c.Message, resolvedPath(h.self))

	delete(h.paths, "strspc")
	report, _ = runDoctorJSON(t, dir)
	c = assertDoctorStatus(t, report, "install.path", "", doctorWarn)
	testutil.AssertContains(t, c.Message, "not on PATH")
}

func TestDoctorInstallPathFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	h := fakeDoctorHost(t)
	link := filepath.Join(t.TempDir(), "strspc")
	if err := os.Symlink(h.self, link); err != nil {
		t.Fatal(err)
	}
	h.paths["strspc"] = link
	report, _ := runDoctorJSON(t, t.TempDir())
	assertDoctorStatus(t, report, "install.path", "", doctorPass)
}

func TestDoctorGitMissing(t *testing.T) {
	h := fakeDoctorHost(t)
	delete(h.paths, "git")

	outside := t.TempDir()
	report, err := runDoctorJSON(t, outside)
	assertExit(t, err, exitOK)
	assertDoctorStatus(t, report, "tools.git", "", doctorWarn)
	assertDoctorStatus(t, report, "project.gitignore", "", doctorSkip)

	repo := t.TempDir()
	writeRefsFile(t, repo, ".git", "gitdir: elsewhere") // a worktree's .git file
	sub := filepath.Join(repo, "sub")
	writeRefsFile(t, sub, ".keep", "")
	report, err = runDoctorJSON(t, sub)
	assertExit(t, err, exitViolations)
	assertDoctorStatus(t, report, "tools.git", "", doctorFail)
}

func TestDoctorGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	fakeDoctorHost(t)
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	report, _ := runDoctorJSON(t, dir)
	c := assertDoctorStatus(t, report, "project.gitignore", "", doctorWarn)
	testutil.AssertContains(t, c.Message, ".strspc/cache/ and .strspc/cache.db")

	writeRefsFile(t, dir, ".strspc/.gitignore", strspcGitignore)
	report, _ = runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "project.gitignore", "", doctorPass)

	report, _ = runDoctorJSON(t, t.TempDir())
	assertDoctorStatus(t, report, "project.gitignore", "", doctorSkip)
}

func TestDoctorCI(t *testing.T) {
	fakeDoctorHost(t)
	dir := t.TempDir()

	report, _ := runDoctorJSON(t, dir, "--strict") // info never fails, even with --strict
	if report.Summary.Info == 0 {
		t.Error("no info result counted")
	}
	c := assertDoctorStatus(t, report, "project.ci", "", doctorInfo)
	testutil.AssertContains(t, c.Message, "strspc ci init")

	writeRefsFile(t, dir, ".github/workflows/rules.yaml", "jobs:\n  check:\n    uses: SteerSpec/.github/.github/workflows/strspc-check.yml@v1\n")
	report, _ = runDoctorJSON(t, dir)
	c = assertDoctorStatus(t, report, "project.ci", "", doctorPass)
	if c.Message != ".github/workflows/rules.yaml" {
		t.Errorf("message %q", c.Message)
	}

	writeRefsFile(t, dir, ".github/workflows/rules.yaml", "steps:\n  - run: strspc check --static-only\n")
	report, _ = runDoctorJSON(t, dir)
	assertDoctorStatus(t, report, "project.ci", "", doctorPass)
}

func TestWorkflowRunsCheck(t *testing.T) {
	cases := []struct {
		name, workflow string
		want           bool
	}{
		{"run", "jobs:\n  a:\n    steps:\n      - run: strspc check --diff origin/main\n", true},
		{"run after cd", "jobs:\n  a:\n    steps:\n      - run: cd app && ./bin/strspc check\n", true},
		{"multi-line run", "jobs:\n  a:\n    steps:\n      - run: |\n          strspc sync\n          strspc check\n", true},
		{"reusable workflow", "jobs:\n  a:\n    uses: SteerSpec/.github/.github/workflows/strspc-check.yml@v1\n", true},
		{"comment", "# runs strspc check\njobs:\n  a:\n    steps:\n      - run: make test\n", false},
		{"echo", "jobs:\n  a:\n    steps:\n      - run: echo \"strspc check\"\n", false},
		{"name only", "name: strspc check\njobs:\n  a:\n    steps:\n      - run: make test\n", false},
		{"not yaml", "jobs: [\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workflowRunsCheck([]byte(tc.workflow)); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// realmProject writes a project whose local source ./rules is a valid realm.
func realmProject(t *testing.T) (project, rules string) {
	t.Helper()
	project = writeDoctorProject(t, "rules:\n  - source: ./rules\n    scope: local\nevaluator:\n  provider: null\n")
	rules = filepath.Join(project, "rules")
	setupValidRealm(t, rules)
	writeEntityFile(t, rules, "TST.json", validEntityJSON)
	return project, rules
}

func TestDoctorRealmValidate(t *testing.T) {
	fakeDoctorHost(t)
	project, rules := realmProject(t)

	requested := cacheRealmSchemas(t)
	report, err := runDoctorJSON(t, project)
	assertExit(t, err, exitOK)
	c := assertDoctorStatus(t, report, "realm.validate", "./rules", doctorSkip)
	testutil.AssertContains(t, c.Message, "not cached")
	testutil.AssertContains(t, c.Remedy, "strspc realm validate ./rules")
	if len(*requested) > 0 {
		t.Errorf("doctor made network requests: %v", *requested)
	}

	requested = cacheRealmSchemas(t, "realm/v1.json", "entity/v1.json", "entity/bootstrap.json")
	report, err = runDoctorJSON(t, project)
	assertExit(t, err, exitOK)
	assertDoctorStatus(t, report, "realm.validate", "./rules", doctorPass)

	writeEntityFile(t, rules, "BAD.json", invalidEntityJSON)
	report, err = runDoctorJSON(t, project)
	assertExit(t, err, exitViolations)
	c = assertDoctorStatus(t, report, "realm.validate", "./rules", doctorFail)
	testutil.AssertContains(t, c.Message, "error(s)")

	if len(*requested) > 0 {
		t.Errorf("doctor made network requests: %v", *requested)
	}
}

func TestDoctorRealmDependencyNotCached(t *testing.T) {
	fakeDoctorHost(t)
	project, rules := realmProject(t)
	writeRealmDeps(t, rules, entity.RealmDep{RealmID: "dev.example.other", Version: "1.0.0", Source: "github://acme/other@v1.0.0/rules"})
	requested := cacheRealmSchemas(t, "realm/v1.json", "entity/v1.json", "entity/bootstrap.json")

	report, err := runDoctorJSON(t, project)
	assertExit(t, err, exitOK)
	c := assertDoctorStatus(t, report, "realm.validate", "./rules", doctorWarn)
	testutil.AssertContains(t, c.Message, "1 warning(s)")
	if len(*requested) > 0 {
		t.Errorf("doctor made network requests: %v", *requested)
	}
}

func TestDoctorRealmCore(t *testing.T) {
	fakeDoctorHost(t)
	cacheRealmSchemas(t)
	project, rules := realmProject(t)

	report, _ := runDoctorJSON(t, project)
	assertDoctorStatus(t, report, "realm.core", "./rules", doctorSkip)

	withStandards := strings.Replace(validEntityJSON, `"title": "Test Entity"`,
		`"title": "Test Entity", "standards": [{"id": "ietf.rfc2119", "relation": "derived_from"}]`, 1)
	writeEntityFile(t, rules, "TST.json", withStandards)
	report, _ = runDoctorJSON(t, project)
	assertDoctorStatus(t, report, "realm.core", "./rules", doctorPass) // no pin

	writeRealmDeps(t, rules, entity.RealmDep{RealmID: coreRealmID, Version: "1.14.0", Source: "github://SteerSpec/strspc-rules@v1.14.0/rules/core"})
	report, err := runDoctorJSON(t, project)
	assertExit(t, err, exitViolations)
	c := assertDoctorStatus(t, report, "realm.core", "./rules", doctorFail)
	testutil.AssertContains(t, c.Message, "TST.json declares standards")
	testutil.AssertContains(t, c.Message, "1.14.0")
	testutil.AssertContains(t, c.Remedy, "strspc realm dep upgrade "+coreRealmID+" --to 1.15.0")

	writeRealmDeps(t, rules, entity.RealmDep{RealmID: coreRealmID, Version: "1.15.0", Source: "github://SteerSpec/strspc-rules@v1.15.0/rules/core"})
	report, _ = runDoctorJSON(t, project)
	assertDoctorStatus(t, report, "realm.core", "./rules", doctorPass)
}

func TestDoctorBadPath(t *testing.T) {
	fakeDoctorHost(t)
	_, err := testutil.ExecuteCommand(NewRootCmd(), "doctor", filepath.Join(t.TempDir(), "nope"))
	assertExit(t, err, exitConfig)

	file := filepath.Join(t.TempDir(), "f")
	writeRefsFile(t, filepath.Dir(file), "f", "")
	_, err = testutil.ExecuteCommand(NewRootCmd(), "doctor", file)
	assertExit(t, err, exitConfig)

	_, err = testutil.ExecuteCommand(NewRootCmd(), "doctor", "--fix")
	assertExit(t, err, exitConfig)
	_, err = testutil.ExecuteCommand(NewRootCmd(), "doctor", "a", "b")
	assertExit(t, err, exitConfig)
}

func TestDoctorOfflineClientRefusesRequests(t *testing.T) {
	_, err := offlineHTTPClient().Get("https://steerspec.dev/schemas/realm/v1.json")
	if !errors.Is(err, errDoctorOffline) {
		t.Errorf("got %v, want errDoctorOffline", err)
	}
}

func TestDoctorSummaryLine(t *testing.T) {
	got := doctorSummaryLine(doctorSummary{Pass: 3, Warn: 2, Fail: 1})
	if want := "1 failed, 2 warning(s), 3 passed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = doctorSummaryLine(doctorSummary{Pass: 1, Skip: 2, Info: 1})
	if want := fmt.Sprintf("0 failed, 0 warning(s), 1 passed, %d skipped, 1 info", 2); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
