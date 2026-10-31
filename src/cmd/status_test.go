package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// statusProject writes a project whose config lists sources, and returns it.
func statusProject(t *testing.T, ttl string, sources ...string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("rules:\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "  - source: %s\n    scope: global\n", s)
	}
	b.WriteString("evaluator:\n  provider: claude\n  model: claude-sonnet-5-5\nfail_on: [implemented, P]\n")
	if ttl != "" {
		fmt.Fprintf(&b, "cache:\n  ttl: %s\n", ttl)
	}
	writeRawCheckConfig(t, dir, b.String())
	return dir
}

// writeCachedSource fills the cache as `strspc sync` would, for
// github://o/r@<ref>/rules.
func writeCachedSource(t *testing.T, project, ref string, rules ...inheritRule) {
	t.Helper()
	sum := sha256.Sum256([]byte("rules"))
	dir := filepath.Join(project, ".strspc", "cache", "github", "o", "r", ref, hex.EncodeToString(sum[:])[:12])
	writeInheritEntity(t, dir, "CORE", "", rules...)
}

func writeLatestTag(t *testing.T, project, tag string, at time.Time) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"tag": tag, "resolved_at": at})
	if err != nil {
		t.Fatal(err)
	}
	writeRefsFile(t, project, ".strspc/cache/github/o/r/.meta/latest.json", string(body))
}

func runStatus(t *testing.T, project string, args ...string) *statusReport {
	t.Helper()
	out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"status", project, "--json"}, args...)...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var report statusReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("output is not one JSON document: %v\n%s", err, out)
	}
	return &report
}

func TestStatusNoConfig(t *testing.T) {
	dir := t.TempDir()
	out, err := testutil.ExecuteCommand(NewRootCmd(), "status", dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Not initialized")
	testutil.AssertContains(t, out, "strspc init")

	if report := runStatus(t, dir); report.Initialized || report.Config != nil {
		t.Errorf("got %+v", report)
	}
}

func TestStatusConfigurationErrors(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"not YAML", "rules: [", "parsing config.yaml"},
		{"bad TTL", "rules: []\ncache:\n  ttl: soon\n", "parsing cache.ttl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRawCheckConfig(t, dir, tt.config)
			_, err := testutil.ExecuteCommand(NewRootCmd(), "status", dir)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestStatusCountsEveryState(t *testing.T) {
	dir := statusProject(t, "", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "",
		inheritRule{id: "APP-001", state: "D"},
		inheritRule{id: "APP-002", state: "P"},
		inheritRule{id: "APP-003", state: "I"},
		inheritRule{id: "APP-004", state: "A"},
		inheritRule{id: "APP-005", state: "R"},
		inheritRule{id: "APP-006", state: "T"},
		inheritRule{id: "APP-007", state: "I"},
		inheritRule{id: "APP-008", state: "X"},
	)
	got := runStatus(t, dir).Rules
	want := ruleCounts{Total: 8, Draft: 1, Published: 1, Implemented: 2, Abandoned: 1, Retired: 1, Terminated: 1, Unknown: 1}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStatusCountsSubEntitiesAndSkipsOtherFiles(t *testing.T) {
	dir := statusProject(t, "", "./rules/")
	rules := filepath.Join(dir, "rules")
	sub := `[{"entity": {"id": "SUB", "parent": "APP", "title": "t", "description": "d"},
	  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
	  "rules": [{"id": "SUB-001", "revision": 0, "state": "I", "body": "b", "added_by": "@t", "added_at": "2026-01-01", "supersedes": null}],
	  "notes": [], "sub_entities": []}]`
	writeInheritFile(t, rules, "APP", inheritEntityJSON("APP", "", sub, inheritRule{id: "APP-001", state: "P"}))
	writeRefsFile(t, dir, "rules/package.json", `{"name": "not an entity"}`)
	writeRefsFile(t, dir, "rules/broken.json", `not json`)

	got := runStatus(t, dir).Rules
	want := ruleCounts{Total: 2, Published: 1, Implemented: 1, Unreadable: 1}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStatusSourceStates(t *testing.T) {
	rule := inheritRule{id: "CORE-001", state: "I"}
	fresh := time.Now().Add(-10 * time.Minute)
	stale := time.Now().Add(-3 * time.Hour)
	tests := []struct {
		name   string
		source string
		setup  func(t *testing.T, project string)
		state  string
		ref    string
		rules  int
		text   string
	}{
		{name: "local", source: "./rules/", setup: func(t *testing.T, p string) {
			writeInheritEntity(t, filepath.Join(p, "rules"), "APP", "", inheritRule{id: "APP-001", state: "I"})
		}, state: sourceLocal, rules: 1, text: ": local, 1 rule(s)"},
		{name: "local directory missing", source: "./gone/", state: sourceMissing, text: "directory not found"},
		{name: "pinned, never synced", source: "github://o/r@v1.0.0/rules", state: sourceNotSynced, ref: "v1.0.0", text: "not synced, run strspc sync"},
		{name: "pinned, cached", source: "github://o/r@v1.0.0/rules", setup: func(t *testing.T, p string) {
			writeCachedSource(t, p, "v1.0.0", rule)
		}, state: sourcePinned, ref: "v1.0.0", rules: 1, text: "pinned, cached, 1 rule(s)"},
		{name: "latest, never resolved", source: "github://o/r@latest/rules", state: sourceNotSynced, text: "not synced"},
		{name: "latest, resolved, not cached", source: "github://o/r@latest/rules", setup: func(t *testing.T, p string) {
			writeLatestTag(t, p, "v2.0.0", fresh)
		}, state: sourceNotSynced, ref: "v2.0.0", text: "not synced"},
		{name: "latest, fresh", source: "github://o/r@latest/rules", setup: func(t *testing.T, p string) {
			writeLatestTag(t, p, "v2.0.0", fresh)
			writeCachedSource(t, p, "v2.0.0", rule)
		}, state: sourceFresh, ref: "v2.0.0", rules: 1, text: "latest is v2.0.0, resolved 10m ago, 1 rule(s)"},
		{name: "latest, stale", source: "github://o/r@latest/rules", setup: func(t *testing.T, p string) {
			writeLatestTag(t, p, "v2.0.0", stale)
			writeCachedSource(t, p, "v2.0.0", rule)
		}, state: sourceStale, ref: "v2.0.0", rules: 1, text: "latest is v2.0.0, resolved 3h ago (stale), 1 rule(s)"},
		{name: "malformed", source: "github://nothing", state: sourceInvalid, text: "invalid:"},
		{name: "unsupported scheme", source: "https://example.com/rules", state: sourceUnsupported, text: "unsupported scheme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := statusProject(t, "1h", tt.source)
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			s := runStatus(t, dir).Sources[0]
			if s.State != tt.state || s.Ref != tt.ref || s.Rules.Total != tt.rules {
				t.Errorf("got %+v", s)
			}
			out, err := testutil.ExecuteCommand(NewRootCmd(), "status", dir)
			assertExit(t, err, exitOK)
			testutil.AssertContains(t, out, tt.text)
		})
	}
}

// The configured TTL decides staleness, not the default.
func TestStatusHonoursConfiguredTTL(t *testing.T) {
	dir := statusProject(t, "48h", "github://o/r@latest/rules")
	writeLatestTag(t, dir, "v2.0.0", time.Now().Add(-3*time.Hour))
	writeCachedSource(t, dir, "v2.0.0", inheritRule{id: "CORE-001", state: "I"})
	report := runStatus(t, dir)
	if got := report.Sources[0].State; got != sourceFresh {
		t.Errorf("state = %s", got)
	}
	if got := report.Config.CacheTTL; got != "48h0m0s" {
		t.Errorf("cache_ttl = %s", got)
	}
}

func TestStatusAddsUpSources(t *testing.T) {
	dir := statusProject(t, "", "github://o/r@v1.0.0/rules", "./rules/")
	writeCachedSource(t, dir, "v1.0.0", inheritRule{id: "CORE-001", state: "I"}, inheritRule{id: "CORE-002", state: "R"})
	writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "", inheritRule{id: "APP-001", state: "D"})
	got := runStatus(t, dir).Rules
	if got.Total != 3 || got.Implemented != 1 || got.Retired != 1 || got.Draft != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestStatusConfig(t *testing.T) {
	report := runStatus(t, statusProject(t, "", "./rules/"))
	cfg := report.Config
	if cfg.Evaluator != "claude" || cfg.Model != "claude-sonnet-5-5" || cfg.CacheTTL != "24h0m0s" {
		t.Errorf("got %+v", cfg)
	}
	if !slices.Equal(cfg.FailOn, []string{"I", "P"}) {
		t.Errorf("fail_on = %v", cfg.FailOn)
	}
}

func TestStatusRealm(t *testing.T) {
	dir := statusProject(t, "", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeRefsFile(t, dir, "rules/realm.json",
		`{"realm": {"id": "dev.app", "title": "t", "version": "1.2.3"}, "sub_realms": ["sub"]}`)
	writeInheritEntity(t, rules, "APP", "", inheritRule{id: "APP-001", state: "I"})
	writeInheritEntity(t, rules, "ADMIN", "", inheritRule{id: "ADMIN-001", state: "I"})
	writeRealmManifest(t, filepath.Join(rules, "sub"), "dev.app.sub")
	writeInheritEntity(t, filepath.Join(rules, "sub"), "SUBE", "", inheritRule{id: "SUBE-001", state: "I"})
	writeRefsFile(t, dir, "rules/package.json", `{"name": "not an entity"}`)

	report := runStatus(t, dir)
	realm := report.Realm
	if realm == nil || realm.ID != "dev.app" || realm.Version != "1.2.3" {
		t.Fatalf("got %+v", realm)
	}
	if !slices.Equal(realm.Entities, []string{"ADMIN", "APP"}) {
		t.Errorf("entities = %v", realm.Entities)
	}
	if !slices.Equal(realm.SubRealms, []string{"sub"}) {
		t.Errorf("sub_realms = %v", realm.SubRealms)
	}
	if report.Rules.Total != 3 {
		t.Errorf("rules of the sub-realm belong to the source: %+v", report.Rules)
	}
	out, err := testutil.ExecuteCommand(NewRootCmd(), "status", dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "Realm: dev.app 1.2.3")
	testutil.AssertContains(t, out, "Entities: ADMIN, APP")
}

func TestStatusNoRealm(t *testing.T) {
	dir := statusProject(t, "", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "APP", "", inheritRule{id: "APP-001", state: "I"})
	if realm := runStatus(t, dir).Realm; realm != nil {
		t.Errorf("got %+v", realm)
	}
}

func listStatusTree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		paths = append(paths, p)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// Nothing may be fetched or written, even for a stale latest and a source that
// was never synced, which is what sync and check would act on.
func TestStatusOnlyReads(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	t.Setenv("HTTPS_PROXY", srv.URL)
	t.Setenv("HTTP_PROXY", srv.URL)

	dir := statusProject(t, "1h", "github://o/r@latest/rules", "github://o/r@v9.9.9/rules", "./rules/")
	writeLatestTag(t, dir, "v2.0.0", time.Now().Add(-48*time.Hour))
	writeCachedSource(t, dir, "v2.0.0", inheritRule{id: "CORE-001", state: "I"})
	before := listStatusTree(t, dir)

	for _, args := range [][]string{{"status", dir}, {"status", dir, "--json"}} {
		if _, err := testutil.ExecuteCommand(NewRootCmd(), args...); err != nil {
			t.Fatal(err)
		}
	}
	report := runStatus(t, dir)
	if got := []string{report.Sources[0].State, report.Sources[1].State}; !slices.Equal(got, []string{sourceStale, sourceNotSynced}) {
		t.Errorf("states = %v: a fetch would have changed them", got)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("%d network calls", n)
	}
	if after := listStatusTree(t, dir); !slices.Equal(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// Sources and realm IDs come from files the project may only have cloned, and
// a value must never decide how a line of output begins.
func TestStatusOutputIsPrintable(t *testing.T) {
	sources := map[string]string{
		"multi-line":        `"./ru\x1b[31mles\n::error::forged/"`,
		"at the line start": `"::error::forged"`,
		"azure command":     `"##vso[task.setvariable]x"`,
		"legacy command":    `"a##[error]x"`,
		"bidi override":     `"a\u202eb"`,
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			dir := statusProject(t, "", source)
			writeRefsFile(t, dir, "ok/realm.json", `{"realm": {"id": "::error::a\u001b[31m", "title": "t", "version": "1.0.0"}}`)
			report, err := collectStatus(dir)
			if err != nil {
				t.Fatal(err)
			}
			report.Realm = realmStatus(filepath.Join(dir, "ok"))
			report.Realm.Entities = []string{"::error::entity"}

			var out bytes.Buffer
			writeStatus(&out, report, time.Now())
			got := out.String()
			for _, bad := range []string{"\x1b[31m", "\u202e", "##vso[", "##["} {
				if strings.Contains(got, bad) {
					t.Errorf("%q in output: %q", bad, got)
				}
			}
			for _, line := range strings.Split(got, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "::") {
					t.Errorf("workflow command in output: %q", line)
				}
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{time.Minute, "1m ago"},
		{59 * time.Minute, "59m ago"},
		{time.Hour, "1h ago"},
		{23 * time.Hour, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{72 * time.Hour, "3d ago"},
	}
	for _, tt := range tests {
		if got := formatAge(tt.d); got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestStatusDefaultsToCurrentDirectory(t *testing.T) {
	dir := statusProject(t, "", "./rules/")
	t.Chdir(dir)
	out, err := testutil.ExecuteCommand(NewRootCmd(), "status")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, ".strspc/config.yaml")
	if _, statErr := os.Stat(".strspc"); statErr != nil {
		t.Fatal(statErr)
	}
}
