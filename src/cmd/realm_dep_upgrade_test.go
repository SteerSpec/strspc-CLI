package cmd

import (
	"bytes"
	"context"
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

func TestRealmDepUpgradeRewritesVersionAndSource(t *testing.T) {
	dir := setupUpgradeTest(t, "v1.11.0", nil)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "1.8.0 -> 1.11.0")
	testutil.AssertContains(t, out, "not a github://")

	deps := readRealmDeps(t, dir)
	if deps[0].Version != "1.11.0" || deps[0].Source != "github://SteerSpec/strspc-rules@v1.11.0/rules/core" {
		t.Errorf("core dep not rewritten: %+v", deps[0])
	}
	if deps[1].Version != "0.1.0" || deps[1].Source != "../sibling" {
		t.Errorf("local dep changed: %+v", deps[1])
	}
}

func TestRealmDepUpgradeCheck(t *testing.T) {
	dir := setupUpgradeTest(t, "v1.11.0", nil)

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
	dir := setupUpgradeTest(t, "v1.11.0", nil)

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
	dir := setupUpgradeTest(t, "v1.11.0", failing)
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

func TestRealmDepUpgradePreservesSubRealms(t *testing.T) {
	dir := setupUpgradeTest(t, "v1.11.0", nil)
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
		dir := setupUpgradeTest(t, "v1.11.0", nil)
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
