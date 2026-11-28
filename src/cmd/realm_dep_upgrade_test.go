package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const upgradeRealmJSON = `{
  "$schema": "./_schema/realm.v1.schema.json",
  "realm": {"id": "dev.steerspec.test", "title": "Test Realm", "version": "0.1.0"},
  "dependencies": [
    {"realm_id": "dev.steerspec.core", "version": "1.8.0", "source": "github://SteerSpec/strspc-rules@v1.8.0/rules/core"},
    {"realm_id": "dev.local.sibling", "version": "0.1.0", "source": "../sibling"}
  ],
  "rule_identifier_format": null
}`

// setupUpgradeTest writes a realm with two dependencies, stubs the release
// lookup to return latest, and skips real validation unless valid is overridden.
func setupUpgradeTest(t *testing.T, latest string, validate func() *result.Result) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(upgradeRealmJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	origTag, origValidate := latestReleaseTag, validateUpgradedRealm
	t.Cleanup(func() { latestReleaseTag, validateUpgradedRealm = origTag, origValidate })
	latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) { return latest, nil }
	validateUpgradedRealm = func(context.Context, string) *result.Result {
		if validate != nil {
			return validate()
		}
		return &result.Result{}
	}
	return dir
}

func TestPlanDepUpgradeLimitsTokenToAllowedOwners(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("GITHUB_REPOSITORY_OWNER", "")
	t.Setenv(remote.TokenOwnersEnv, "")

	origTag := latestReleaseTag
	t.Cleanup(func() { latestReleaseTag = origTag })
	var token string
	latestReleaseTag = func(_ context.Context, _ remote.Ref, got string) (string, error) {
		token = got
		return "v1.1.0", nil
	}

	tests := []struct {
		owner string
		want  string
	}{
		{"SteerSpec", "secret"},
		{"acme", ""},
	}
	for _, tt := range tests {
		dep := entity.RealmDep{RealmID: "dev.example.dep", Version: "1.0.0", Source: "github://" + tt.owner + "/specs@v1.0.0"}
		if _, _, err := planDepUpgrade(context.Background(), dep, ""); err != nil {
			t.Fatalf("%s: %v", tt.owner, err)
		}
		if token != tt.want {
			t.Errorf("%s: token = %q, want %q", tt.owner, token, tt.want)
		}
	}
}

func TestRealmDepUpgradeRewritesVersionAndSource(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "1.8.0 -> 9.9.9")
	testutil.AssertContains(t, out, "not a github://")

	deps := readRealmDeps(t, dir)
	if deps[0].Version != "9.9.9" || deps[0].Source != "github://SteerSpec/strspc-rules@v9.9.9/rules/core" {
		t.Errorf("core dep not rewritten: %+v", deps[0])
	}
	if deps[1].Version != "0.1.0" || deps[1].Source != "../sibling" {
		t.Errorf("local dep changed: %+v", deps[1])
	}
}

func TestRealmDepUpgradeCheck(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--dir", dir)
	if err == nil {
		t.Fatal("expected non-zero exit when an upgrade is available")
	}
	testutil.AssertContains(t, err.Error(), "1 dependency upgrade(s) available")
	if got := readRealmDeps(t, dir)[0].Version; got != "1.8.0" {
		t.Errorf("--check wrote realm.json: version %s", got)
	}
}

func TestRealmDepUpgradeUpToDate(t *testing.T) {
	dir := setupUpgradeTest(t, "v1.8.0", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "up to date")
}

func TestRealmDepUpgradeNeverDowngrades(t *testing.T) {
	dir := setupUpgradeTest(t, "v1.7.0", nil)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for older latest release")
	}
	testutil.AssertContains(t, err.Error(), "refusing to downgrade")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "dev.steerspec.core", "--to", "1.2.0", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for --to below current")
	}
}

func TestRealmDepUpgradeTo(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "dev.steerspec.core", "--to", "v1.9.0", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dep := readRealmDeps(t, dir)[0]
	if dep.Version != "1.9.0" || dep.Source != "github://SteerSpec/strspc-rules@v1.9.0/rules/core" {
		t.Errorf("unexpected dep: %+v", dep)
	}
}

func TestRealmDepUpgradeFlagErrors(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)

	for name, args := range map[string][]string{
		"to without id":      {"--to", "1.9.0"},
		"to with check":      {"dev.steerspec.core", "--to", "1.9.0", "--check"},
		"unknown dependency": {"dev.nope"},
	} {
		if _, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "dep", "upgrade", "--dir", dir}, args...)...); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestRealmDepUpgradeRestoresOnValidationFailure(t *testing.T) {
	failing := func() *result.Result {
		res := &result.Result{}
		res.Add(result.Diagnostic{Code: "RM099", Severity: result.Error, Message: "breaks"})
		return res
	}
	dir := setupUpgradeTest(t, "v9.9.9", failing)
	before, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err == nil {
		t.Fatal("expected error when the upgrade breaks the realm")
	}
	testutil.AssertContains(t, err.Error(), "left unchanged")

	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(after, before) {
		t.Errorf("realm.json was modified:\n%s", after)
	}
}

func TestFetchLatestReleaseTag(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	orig := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = orig })

	tag, err := fetchLatestReleaseTag(context.Background(), remote.Ref{Owner: "o", Repo: "r"}, "tok")
	if err != nil || tag != "v2.0.0" {
		t.Fatalf("got %q, %v", tag, err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization header %q", gotAuth)
	}
	if _, err := fetchLatestReleaseTag(context.Background(), remote.Ref{Owner: "o", Repo: "missing"}, ""); err == nil {
		t.Error("expected error for 404")
	}
}

func TestFetchLatestReleaseTagErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/broken/releases/latest":
			_, _ = w.Write([]byte(`not json`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	orig := githubAPIBase
	t.Cleanup(func() { githubAPIBase = orig })
	githubAPIBase = srv.URL

	_, err := fetchLatestReleaseTag(context.Background(), remote.Ref{Owner: "o", Repo: "broken"}, "")
	if err == nil || !strings.Contains(err.Error(), "decoding latest release of o/broken") {
		t.Errorf("invalid body: error = %v, want a decoding error", err)
	}
	_, err = fetchLatestReleaseTag(context.Background(), remote.Ref{Owner: "o", Repo: "down"}, "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("server error: error = %v, want HTTP 500", err)
	}

	githubAPIBase = "http://[::1"
	if _, err := fetchLatestReleaseTag(context.Background(), remote.Ref{Owner: "o", Repo: "r"}, ""); err == nil {
		t.Error("invalid API base: want an error, got none")
	}
}

func TestPlanDepUpgradeEdgeCases(t *testing.T) {
	origTag := latestReleaseTag
	t.Cleanup(func() { latestReleaseTag = origTag })
	pinned := entity.RealmDep{RealmID: "dev.example.dep", Version: "1.0.0", Source: "github://acme/specs@v1.0.0"}

	latest := pinned
	latest.Source = "github://acme/specs@latest"
	up, note, err := planDepUpgrade(context.Background(), latest, "")
	if err != nil || up != nil || note != "skipped: source already tracks @latest" {
		t.Errorf("@latest: got %+v, %q, %v", up, note, err)
	}

	tests := []struct {
		name string
		tag  string
		err  error
		want string
	}{
		{"lookup fails", "", errors.New("rate limited"), "dev.example.dep: rate limited"},
		{"tag is not semver", "release-two", nil, `release tag "release-two" is not semver`},
		{"latest is a prerelease", "v2.0.0-rc.1", nil, "latest release v2.0.0-rc.1 is a prerelease"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) { return tt.tag, tt.err }
			_, _, err := planDepUpgrade(context.Background(), pinned, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRealmDepUpgradePreservesSubRealms(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	withSubs := strings.Replace(upgradeRealmJSON, `"rule_identifier_format"`, `"sub_realms": ["auth"],
  "rule_identifier_format"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(withSubs), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.SubRealms) != 1 || rf.SubRealms[0] != "auth" {
		t.Errorf("sub_realms lost: %v", rf.SubRealms)
	}
}

func TestRealmDepUpgradeSkipsNonTagRefs(t *testing.T) {
	for _, ref := range []string{strings.Repeat("a", 40), "abc1234", strings.Repeat("A", 40), "main", "v1"} {
		dir := setupUpgradeTest(t, "v9.9.9", nil)
		pinned := strings.Replace(upgradeRealmJSON, "@v1.8.0/", "@"+ref+"/", 1)
		if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(pinned), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", ref, err)
		}
		testutil.AssertContains(t, out, "not a release tag")
		if got := readRealmDeps(t, dir)[0].Source; !strings.Contains(got, "@"+ref+"/") {
			t.Errorf("%s: pin rewritten: %s", ref, got)
		}
	}
}

func TestRealmDepUpgradeNeutralisesRealmValues(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	writeRealmDeps(t, dir,
		// The version stays SemVer: the schema check (SEC-004) refuses a manifest
		// whose version holds control characters before upgrade reads it.
		entity.RealmDep{RealmID: "::stop-commands::x\x1b[2J", Version: "1.0.0", Source: "../sibling"},
		entity.RealmDep{RealmID: "::add-mask::z", Version: "1.8.0", Source: "github://SteerSpec/strspc-rules@v1.8.0/rules/core"},
	)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("output contains a raw escape:\n%q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "::") {
			t.Errorf("line starts a workflow command: %q", line)
		}
	}
	testutil.AssertContains(t, out, `"::stop-commands::x\x1b[2J"@1.0.0`)
	testutil.AssertContains(t, out, `"::add-mask::z": 1.8.0 -> 9.9.9`)
}

func TestRealmDepUpgradeErrorsNeutraliseRealmValues(t *testing.T) {
	core := "github://SteerSpec/strspc-rules@v1.8.0/rules/core"
	for name, tt := range map[string]struct {
		dep  entity.RealmDep
		want string
	}{
		"version": {entity.RealmDep{RealmID: "::stop-commands::x", Version: "nope\x1b[2J", Source: core}, "not semver"},
		"source": {
			entity.RealmDep{RealmID: "::stop-commands::x", Version: "1.8.0", Source: "github://Steer\x1b[2JSpec/strspc-rules@v1.8.0"},
			"control character",
		},
		"no ref": {entity.RealmDep{RealmID: "::stop-commands::x", Version: "1.8.0", Source: "github://SteerSpec/strspc-rules"}, "missing @ref"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := setupUpgradeTest(t, "v9.9.9", nil)
			writeRealmDeps(t, dir, tt.dep)

			// --check plans without validating; an upgrade refuses such a
			// manifest against its schema before it plans (SEC-004).
			_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--dir", dir)
			if err == nil {
				t.Fatal("expected an error")
			}
			testutil.AssertContains(t, err.Error(), tt.want)
			if strings.Contains(err.Error(), "\x1b") {
				t.Errorf("error contains a raw escape: %q", err)
			}
			if !strings.HasPrefix(err.Error(), `"::stop-commands::x"`) {
				t.Errorf("error doesn't quote the ID: %q", err)
			}
		})
	}
}

func decodeUpgradeReport(t *testing.T, out string) depUpgradeReport {
	t.Helper()
	var r depUpgradeReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, out)
	}
	return r
}

func TestRealmDepUpgradeJSONApplied(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := decodeUpgradeReport(t, out)
	if !r.Applied || r.Check || len(r.Upgrades) != 1 || len(r.Skipped) != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	if u := r.Upgrades[0]; u.RealmID != "dev.steerspec.core" || u.From != "1.8.0" || u.To != "9.9.9" ||
		u.Source != "github://SteerSpec/strspc-rules@v9.9.9/rules/core" {
		t.Errorf("unexpected upgrade: %+v", u)
	}
	if r.Errors == nil || r.Synced == nil {
		t.Errorf("empty lists must be [] not null: %+v", r)
	}
}

func TestRealmDepUpgradeJSONCheckKeepsExitCode(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--json", "--dir", dir)
	if err == nil {
		t.Fatal("expected non-zero exit when an upgrade is available")
	}
	r := decodeUpgradeReport(t, out)
	if !r.Check || r.Applied || len(r.Upgrades) != 1 {
		t.Errorf("unexpected report: %+v", r)
	}
	if got := readRealmDeps(t, dir)[0].Version; got != "1.8.0" {
		t.Errorf("--check wrote realm.json: version %s", got)
	}
}

func TestRealmDepUpgradeJSONUpToDate(t *testing.T) {
	dir := setupUpgradeTest(t, "v1.8.0", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--json", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := decodeUpgradeReport(t, out)
	if r.Applied || len(r.Upgrades) != 0 || len(r.Skipped) != 2 {
		t.Errorf("unexpected report: %+v", r)
	}
}

func TestRealmDepUpgradeJSONReportsValidationFailure(t *testing.T) {
	failing := func() *result.Result {
		res := &result.Result{}
		res.Add(result.Diagnostic{Code: "RM099", Severity: result.Error, Message: "breaks"})
		return res
	}
	dir := setupUpgradeTest(t, "v9.9.9", failing)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err == nil {
		t.Fatal("expected error when the upgrade breaks the realm")
	}
	r := decodeUpgradeReport(t, out)
	if r.Applied || len(r.Errors) != 1 || r.Errors[0] != "breaks" {
		t.Errorf("unexpected report: %+v", r)
	}
	if got := readRealmDeps(t, dir)[0].Version; got != "1.8.0" {
		t.Errorf("realm.json not restored: version %s", got)
	}
}

func TestRealmDepUpgradeJSONOutputFailureStillRollsBack(t *testing.T) {
	failing := func() *result.Result {
		res := &result.Result{}
		res.Add(result.Diagnostic{Code: "RM099", Severity: result.Error, Message: "breaks"})
		return res
	}
	dir := setupUpgradeTest(t, "v9.9.9", failing)
	before, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd()
	root.SetOut(failingWriter{})
	root.SetArgs([]string{"realm", "dep", "upgrade", "--json", "--dir", dir})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error")
	}

	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(after, before) {
		t.Errorf("realm.json not restored after output failure:\n%s", after)
	}
}

func TestRealmDepUpgradeJSONReportsSyncFailure(t *testing.T) {
	dir := setupSchemaSyncTest(t, nil, map[string]string{"entity.v1.schema.json": `{"v":1}`}, nil)
	before, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	fetchUpgradeDep = func(context.Context, string, entity.RealmDep) (string, error) {
		return "", errors.New("fetch failed")
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error when the schema sync fails")
	}
	r := decodeUpgradeReport(t, out)
	if r.Applied || len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "fetch failed") {
		t.Errorf("unexpected report: %+v", r)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(after, before) {
		t.Errorf("realm.json not restored:\n%s", after)
	}
}

func TestRealmDepUpgradeJSONReportsPlanningFailure(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) {
		return "", errors.New("lookup failed")
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error when planning fails")
	}
	r := decodeUpgradeReport(t, out)
	if r.Applied || len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "lookup failed") {
		t.Errorf("unexpected report: %+v", r)
	}
	if got := readRealmDeps(t, dir)[0].Version; got != "1.8.0" {
		t.Errorf("realm.json was modified: version %s", got)
	}
}

const upgradeConfigYAML = `# keep this comment
rules:
  - source: github://SteerSpec/strspc-rules@v1.8.0/rules/core   # core
    scope: global
  - source: ./rules/
    scope: global
`

// withRepoConfig turns dir into a repository root holding a .strspc/config.yaml.
func withRepoConfig(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ".strspc", "config.yaml")
	if err := os.WriteFile(p, []byte(upgradeConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRealmDepUpgradeUpdatesConfigPin(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := decodeUpgradeReport(t, out)
	if len(r.Config) != 1 || r.Config[0] != cfg {
		t.Errorf("config = %v, want [%s]", r.Config, cfg)
	}
	want := `# keep this comment
rules:
  - source: github://SteerSpec/strspc-rules@v9.9.9/rules/core   # core
    scope: global
  - source: ./rules/
    scope: global
`
	if got := readFile(t, cfg); got != want {
		t.Errorf("config.yaml = %q, want %q", got, want)
	}
}

func TestRealmDepUpgradeCheckReportsConfigPinWithoutWriting(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--json", "--dir", dir)
	if err == nil {
		t.Fatal("expected non-zero exit when an upgrade is available")
	}
	if r := decodeUpgradeReport(t, out); len(r.Config) != 1 {
		t.Errorf("config = %v, want one entry", r.Config)
	}
	if got := readFile(t, cfg); got != upgradeConfigYAML {
		t.Errorf("--check wrote config.yaml: %q", got)
	}
}

func TestRealmDepUpgradeRestoresConfigPinOnValidationFailure(t *testing.T) {
	failing := func() *result.Result {
		res := &result.Result{}
		res.Add(result.Diagnostic{Code: "RM099", Severity: result.Error, Message: "breaks"})
		return res
	}
	dir := setupUpgradeTest(t, "v9.9.9", failing)
	cfg := withRepoConfig(t, dir)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err == nil {
		t.Fatal("expected error when the upgrade breaks the realm")
	}
	if got := readFile(t, cfg); got != upgradeConfigYAML {
		t.Errorf("config.yaml not restored: %q", got)
	}
}

func TestRealmDepUpgradeLeavesUnrelatedConfigSources(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)
	other := "rules:\n  - source: github://acme/specs@v1.8.0/rules/core\n    scope: global\n"
	if err := os.WriteFile(cfg, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r := decodeUpgradeReport(t, out); len(r.Config) != 0 {
		t.Errorf("config = %v, want none", r.Config)
	}
	if got := readFile(t, cfg); got != other {
		t.Errorf("unrelated config changed: %q", got)
	}
}

func TestRealmDepUpgradeIgnoresConfigBehindSymlink(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	outside := t.TempDir()
	victim := filepath.Join(outside, "config.yaml")
	if err := os.WriteFile(victim, []byte(upgradeConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".strspc")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, victim); got != upgradeConfigYAML {
		t.Errorf("file behind the .strspc symlink was rewritten: %q", got)
	}
}

func TestRealmDepUpgradeIgnoresConfigInUnrelatedParent(t *testing.T) {
	parent := t.TempDir() // no .git anywhere above the realm
	cfg := withRepoConfigNoGit(t, parent)
	dir := filepath.Join(parent, "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(upgradeRealmJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	setupUpgradeStubs(t, "v9.9.9")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, cfg); got != upgradeConfigYAML {
		t.Errorf("config outside any repository was rewritten: %q", got)
	}
}

func withRepoConfigNoGit(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, ".strspc", "config.yaml")
	if err := os.WriteFile(p, []byte(upgradeConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// setupUpgradeStubs stubs the release lookup and validation without creating a realm.
func setupUpgradeStubs(t *testing.T, latest string) {
	t.Helper()
	origTag, origValidate := latestReleaseTag, validateUpgradedRealm
	t.Cleanup(func() { latestReleaseTag, validateUpgradedRealm = origTag, origValidate })
	latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) { return latest, nil }
	validateUpgradedRealm = func(context.Context, string) *result.Result { return &result.Result{} }
}

func TestRealmDepUpgradeStopsAtBlockedNearestConfig(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := withRepoConfigNoGit(t, parent) // a regular config in the repository root
	dir := filepath.Join(parent, "rules")
	if err := os.MkdirAll(filepath.Join(dir, ".strspc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(upgradeRealmJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cfg, filepath.Join(dir, ".strspc", "config.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	setupUpgradeStubs(t, "v9.9.9")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, cfg); got != upgradeConfigYAML {
		t.Errorf("an ancestor's config was rewritten past a blocked nearest one: %q", got)
	}
}

func TestRealmDepUpgradeRewritesFlowStyleConfigPin(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)
	flow := "rules: [{source: github://SteerSpec/strspc-rules@v1.8.0/rules/core, scope: global}, {source: ./rules/, scope: global}]\n"
	if err := os.WriteFile(cfg, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "rules: [{source: github://SteerSpec/strspc-rules@v9.9.9/rules/core, scope: global}, {source: ./rules/, scope: global}]\n"
	if got := readFile(t, cfg); got != want {
		t.Errorf("config.yaml = %q, want %q", got, want)
	}

	// Quoted, in a block entry, with a trailing comment.
	quoted := "rules:\n  - source: \"github://SteerSpec/strspc-rules@v9.9.9/rules/core\" # core\n    scope: global\n"
	dir2 := setupUpgradeTest(t, "v9.9.9", nil)
	cfg2 := withRepoConfig(t, dir2)
	in := strings.Replace(quoted, "v9.9.9", "v1.8.0", 1)
	if err := os.WriteFile(cfg2, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, cfg2); got != quoted {
		t.Errorf("config.yaml = %q, want %q", got, quoted)
	}
}

func TestRealmDepUpgradeFailsOnConfigLayoutItCannotRewrite(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)
	folded := "rules:\n  - source: >-\n      github://SteerSpec/strspc-rules@v1.8.0/rules/core\n    scope: global\n"
	if err := os.WriteFile(cfg, []byte(folded), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(dir, "realm.json"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error: the config pin cannot be rewritten")
	}
	testutil.AssertContains(t, err.Error(), "update it by hand")
	if got := readFile(t, filepath.Join(dir, "realm.json")); got != before {
		t.Errorf("realm.json not restored:\n%s", got)
	}
	if got := readFile(t, cfg); got != folded {
		t.Errorf("config.yaml changed: %q", got)
	}
}

func TestRealmDepUpgradeLeavesCommentedConfigSources(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)
	in := "# source: github://SteerSpec/strspc-rules@v1.8.0/rules/core\nrules:\n  - source: ./rules/   # was source: github://SteerSpec/strspc-rules@v1.8.0/rules/core\n    scope: global\n"
	if err := os.WriteFile(cfg, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--json", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r := decodeUpgradeReport(t, out); len(r.Config) != 0 {
		t.Errorf("config = %v, want none", r.Config)
	}
	if got := readFile(t, cfg); got != in {
		t.Errorf("commented sources were rewritten: %q", got)
	}
}

func TestRealmDepUpgradeCheckJSONReportsUnrewritableConfig(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)
	folded := "rules:\n  - source: >-\n      github://SteerSpec/strspc-rules@v1.8.0/rules/core\n    scope: global\n"
	if err := os.WriteFile(cfg, []byte(folded), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--check", "--json", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error")
	}
	r := decodeUpgradeReport(t, out)
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "update it by hand") {
		t.Errorf("unexpected report: %+v", r)
	}
}

// SEC-004: the upgrade validates the repository's config against config.v1
// before it reads the pins in it. A config that doesn't match stops the
// upgrade before any file is written, with --check too.
func TestRealmDepUpgradeRefusesInvalidConfig(t *testing.T) {
	for _, args := range [][]string{nil, {"--check"}} {
		t.Run(strings.Join(append([]string{"upgrade"}, args...), " "), func(t *testing.T) {
			dir := setupUpgradeTest(t, "v9.9.9", nil)
			cfg := withRepoConfig(t, dir)
			invalid := upgradeConfigYAML + "unknown_key: 1\n"
			if err := os.WriteFile(cfg, []byte(invalid), 0o644); err != nil {
				t.Fatal(err)
			}
			manifest := readFile(t, filepath.Join(dir, "realm.json"))

			_, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "dep", "upgrade", "--dir", dir}, args...)...)
			if err == nil {
				t.Fatal("expected the invalid config to stop the upgrade")
			}
			testutil.AssertContains(t, err.Error(), "does not match the latest config.v1 schema")
			testutil.AssertContains(t, err.Error(), "unknown_key")
			if got := readFile(t, cfg); got != invalid {
				t.Errorf("config.yaml changed: %q", got)
			}
			if got := readFile(t, filepath.Join(dir, "realm.json")); got != manifest {
				t.Errorf("realm.json changed:\n%s", got)
			}
			if _, err := os.Stat(filepath.Join(dir, "_schema")); err == nil {
				t.Error("schemas were synced")
			}
		})
	}
}

// Offline with no config.v1 schema cached, the upgrade fails closed and
// names the remedy.
func TestRealmDepUpgradeColdSchemaCacheFailsClosed(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	cfg := withRepoConfig(t, dir)
	useConfigSchemaCache(t)
	manifest := readFile(t, filepath.Join(dir, "realm.json"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err == nil {
		t.Fatal("expected the upgrade to fail without a config schema")
	}
	testutil.AssertContains(t, err.Error(), "strspc schema fetch")
	if got := readFile(t, cfg); got != upgradeConfigYAML {
		t.Errorf("config.yaml changed: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "realm.json")); got != manifest {
		t.Errorf("realm.json changed:\n%s", got)
	}
}
