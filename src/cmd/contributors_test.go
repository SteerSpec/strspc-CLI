package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// setContributors writes testContributors into the realm.json in dir.
func setContributors(t *testing.T, dir string) {
	t.Helper()
	editRealmJSON(t, dir, func(m map[string]any) { m["realm"].(map[string]any)["contributors"] = testContributors })
}

// TestAuthoringChecksAddedBy runs each command that writes an added_by
// against a Realm that lists contributors, one that lists none, and a
// sub-realm, which uses its parent's list (CTRB-006).
func TestAuthoringChecksAddedBy(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"rule add", []string{"rule", "add", "TST", "--body", "The system MUST log."}},
		{"note add", []string{"note", "add", "TST-001", "--type", "rationale", "--body", "Why."}},
		{"rule supersede", []string{"rule", "supersede", "TST-001", "--body", "The system MUST audit."}},
	}
	// realm returns the --dir to pass and the directory holding TST.json. A
	// sub-realm is reached through its own --dir: the entity lookup does not
	// descend into sub-realms.
	realms := map[string]func(t *testing.T) (dir, entityDir string){
		"listed": func(t *testing.T) (string, string) {
			dir := t.TempDir()
			setupValidRealm(t, dir)
			setContributors(t, dir)
			return dir, dir
		},
		"unlisted": func(t *testing.T) (string, string) {
			dir := t.TempDir()
			setupValidRealm(t, dir)
			return dir, dir
		},
		"no manifest": func(t *testing.T) (string, string) {
			dir := t.TempDir()
			return dir, dir
		},
		"sub-realm alone": func(t *testing.T) (string, string) {
			dir := t.TempDir()
			setupValidRealmWithSubs(t, dir)
			setContributors(t, dir)
			return filepath.Join(dir, "auth"), filepath.Join(dir, "auth")
		},
	}
	cases := []struct {
		realm, addedBy string
		wantErr        string // "" means accepted
	}{
		{"listed", "github:alice", ""},
		{"listed", "@alice", "CTRB-001"},
		{"listed", "alice@example.com", "CTRB-001"},
		{"listed", "github:BOB", ""},
		{"listed", "github:carol", "CTRB-005"},
		{"listed", "@carol", "CTRB-001"},
		{"listed", "alice", "added_by"},
		{"unlisted", "github:carol", ""},
		{"no manifest", "@anyone", "CTRB-001"},
		{"no manifest", "gitlab:anyone", "CTRB-002"},
		{"sub-realm alone", "github:alice", ""},
		{"sub-realm alone", "github:carol", "CTRB-005"},
	}
	for _, c := range commands {
		for _, tc := range cases {
			t.Run(c.name+"/"+tc.realm+"/"+tc.addedBy, func(t *testing.T) {
				dir, entityDir := realms[tc.realm](t)
				setupEntityFile(t, entityDir, "TST", makeRule("TST-001", "P", "The system MUST validate.", "github:alice"))
				path := filepath.Join(entityDir, "TST.json")
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}

				args := append(append([]string{}, c.args...), "--added-by", tc.addedBy, "--dir", dir)
				_, err = testutil.ExecuteCommand(NewRootCmd(), args...)
				if tc.wantErr == "" {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					return
				}
				if err == nil {
					t.Fatalf("want an error citing %s, got none", tc.wantErr)
				}
				testutil.AssertContains(t, err.Error(), tc.wantErr)
				after, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !bytes.Equal(before, after) {
					t.Error("entity file changed although the command failed")
				}
			})
		}
	}
}

// TestAuthoringBrokenManifest checks that an unreadable realm.json stops an
// authoring command instead of skipping the contributor check.
func TestAuthoringBrokenManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	setupEntityFile(t, dir, "TST")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST", "--body", "b", "--added-by", "github:alice", "--dir", dir)
	if err == nil {
		t.Fatal("want an error for an unreadable realm.json, got none")
	}
	testutil.AssertContains(t, err.Error(), "contributors of the realm")
}

// diffEntity returns an entity file with a rule added by each of addedBy,
// numbered from 001, at the given version. Version 0.1.0 has a fixed
// timestamp and any other the current time, so a bump passes RD009.
func diffEntity(euid, version string, addedBy ...string) []byte {
	ts := "2026-01-01T00:00:00Z"
	if version != "0.1.0" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	var rules []string
	for i, by := range addedBy {
		rules = append(rules, `{"id": "`+euid+`-00`+string(rune('1'+i))+`", "body": "Rule body.", "state": "D", "revision": 0, "added_by": "`+by+`"}`)
	}
	return []byte(`{
  "$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "` + euid + `", "title": "Test Entity"},
  "rule_set": {"version": "` + version + `", "timestamp": "` + ts + `"},
  "rules": [` + strings.Join(rules, ",\n") + `],
  "notes": []
}`)
}

// gitRepoWith commits base, then writes head to the working tree. Names may
// hold directories.
func gitRepoWith(t *testing.T, base, head map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %v failed: %v\n%s", args, err, out)
		}
	}
	write := func(files map[string][]byte) {
		for name, data := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "Test")
	write(base)
	run("git", "add", ".")
	run("git", "commit", "-m", "initial")
	write(head)
	return dir
}

// manifestJSON returns a realm.json with the given ID, contributors (nil
// leaves the key out) and sub-realms.
func manifestJSON(t *testing.T, id string, contributors []entity.Contributor, subRealms ...string) []byte {
	t.Helper()
	realm := map[string]any{"id": id, "title": "Test Realm", "version": "0.1.0"}
	if contributors != nil {
		realm["contributors"] = contributors
	}
	m := map[string]any{"realm": realm, "dependencies": []any{}}
	if subRealms != nil {
		m["sub_realms"] = subRealms
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestDiffContributors checks that diff reports RD023, an error, for a new
// entry naming none of the Realm's contributors, resolving the Realm of each
// file.
func TestDiffContributors(t *testing.T) {
	tests := []struct {
		name       string
		base, head map[string][]byte
		target     string // relative to the repo; "" is the repo itself
		want       int    // RD023 count
	}{
		{
			"unlisted new rule",
			map[string][]byte{"realm.json": manifestJSON(t, "dev.test", testContributors), "ENT.json": diffEntity("ENT", "0.1.0", "github:alice")},
			map[string][]byte{"ENT.json": diffEntity("ENT", "0.2.0", "github:alice", "github:carol")},
			"ENT.json", 1,
		},
		{
			"listed new rule, login in another case",
			map[string][]byte{"realm.json": manifestJSON(t, "dev.test", testContributors), "ENT.json": diffEntity("ENT", "0.1.0", "github:alice")},
			map[string][]byte{"ENT.json": diffEntity("ENT", "0.2.0", "github:alice", "github:Bob")},
			"ENT.json", 0,
		},
		{
			"realm without contributors",
			map[string][]byte{"realm.json": manifestJSON(t, "dev.test", nil), "ENT.json": diffEntity("ENT", "0.1.0", "github:alice")},
			map[string][]byte{"ENT.json": diffEntity("ENT", "0.2.0", "github:alice", "github:carol")},
			"ENT.json", 0,
		},
		{
			"old entry by an unlisted contributor",
			map[string][]byte{"realm.json": manifestJSON(t, "dev.test", testContributors), "ENT.json": diffEntity("ENT", "0.1.0", "github:carol")},
			map[string][]byte{"ENT.json": diffEntity("ENT", "0.2.0", "github:carol", "github:alice")},
			"ENT.json", 0,
		},
		{
			"new file in a sub-realm, from the repo root",
			map[string][]byte{
				"rules/realm.json":      manifestJSON(t, "dev.test", testContributors, "auth"),
				"rules/auth/realm.json": manifestJSON(t, "dev.test.auth", nil),
				"README":                []byte("x"),
			},
			map[string][]byte{
				"rules/auth/AUT.json": diffEntity("AUT", "0.1.0", "github:carol"),
				"rules/ENT.json":      diffEntity("ENT", "0.1.0", "github:alice"),
			},
			"", 1,
		},
		{
			"separate realms in one run",
			map[string][]byte{
				"a/realm.json": manifestJSON(t, "dev.a", testContributors),
				"b/realm.json": manifestJSON(t, "dev.b", []entity.Contributor{{ID: "github:carol"}}),
			},
			map[string][]byte{
				"a/AAA.json": diffEntity("AAA", "0.1.0", "github:carol"),
				"b/BBB.json": diffEntity("BBB", "0.1.0", "github:carol"),
			},
			"", 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := gitRepoWith(t, tt.base, tt.head)
			out, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(repo, tt.target))
			if tt.want == 0 && err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, out)
			}
			if tt.want > 0 && err == nil {
				t.Fatalf("want diff to fail on RD023, got no error:\n%s", out)
			}
			if got := strings.Count(out, "RD023"); got != tt.want {
				t.Errorf("RD023 reported %d times, want %d:\n%s", got, tt.want, out)
			}
		})
	}
}

// rewriteFixture writes a Realm whose TST entity holds @handle, email and
// already migrated added_by values, sub-entity included, and whose OTH entity
// needs no change. It returns the Realm directory.
func rewriteFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	setupValidRealm(t, dir)
	hash := "blake3:0000000000000000000000000000000000000000000000000000000000000000"
	f := entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "TST", Title: "Test Entity"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z", Hash: &hash},
		Rules: []entity.Rule{
			makeRule("TST-001", "P", "The system MUST validate.", "@alice"),
			makeRule("TST-002", "D", "The system MUST log.", "alice@example.com"),
			makeRule("TST-003", "D", "The system MUST audit.", "github:bob"),
		},
		Notes: []entity.Note{{ID: "TST-001/01", RuleRef: "TST-001", Type: "rationale", Content: "Why.", AddedBy: "@bob", AddedAt: "2026-01-01"}},
		SubEntities: []entity.File{{
			Schema:  "./_schema/entity.v1.schema.json",
			Entity:  entity.Entity{ID: "TSTSUB", Title: "Sub Entity", Parent: "TST"},
			RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
			Rules:   []entity.Rule{makeRule("TSTSUB-001", "D", "The sub MUST work.", "@alice")},
			Notes:   []entity.Note{},
		}},
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TST.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	setupEntityFile(t, dir, "OTH", makeRule("OTH-001", "D", "Other.", "github:carol"))
	return dir
}

// readFiles returns the content of each named file in dir.
func readFiles(t *testing.T, dir string, names ...string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = data
	}
	return out
}

// allAddedBy lists ID=added_by for every rule and note of f, sub-entities
// included, in file order.
func allAddedBy(f *entity.File) []string {
	var out []string
	for _, r := range f.Rules {
		out = append(out, r.ID+"="+r.AddedBy)
	}
	for _, n := range f.Notes {
		out = append(out, n.ID+"="+n.AddedBy)
	}
	for i := range f.SubEntities {
		out = append(out, allAddedBy(&f.SubEntities[i])...)
	}
	return out
}

// TestRealmRewriteAddedBy rewrites handles and an email across rules, notes
// and a sub-entity, and checks counts, the patch bump, the hash, and that a
// file with nothing to replace is left alone.
func TestRealmRewriteAddedBy(t *testing.T) {
	dir := rewriteFixture(t)
	oth := readFiles(t, dir, "OTH.json")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by",
		"--github-handles", "--map", "alice@example.com=github:alice", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res addedByResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res.DryRun || len(res.Updated) != 1 || res.Updated[0].Entity != "TST" || res.Updated[0].Replaced != 4 {
		t.Fatalf("result = %+v, want TST with 4 replaced", res)
	}
	if res.Updated[0].OldVersion != "0.1.0" || res.Updated[0].Version != "0.1.1" {
		t.Errorf("version %s → %s, want 0.1.0 → 0.1.1", res.Updated[0].OldVersion, res.Updated[0].Version)
	}
	if res.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1 (OTH)", res.Unchanged)
	}

	got := strings.Join(allAddedBy(loadEntityFile(t, dir, "TST")), " ")
	want := "TST-001=github:alice TST-002=github:alice TST-003=github:bob TST-001/01=github:bob TSTSUB-001=github:alice"
	if got != want {
		t.Errorf("added_by = %s\nwant      %s", got, want)
	}
	if !bytes.Equal(readFiles(t, dir, "OTH.json")["OTH.json"], oth["OTH.json"]) {
		t.Error("OTH.json was rewritten although nothing in it changed")
	}

	// The rewritten file passes lint's hash check (RL011). OTH keeps the
	// fixture's placeholder hash, so only TST is checked.
	lintOut, _ := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	for _, line := range strings.Split(lintOut, "\n") {
		if strings.Contains(line, "RL011") && strings.Contains(line, "TST.json") {
			t.Errorf("lint reports a stale hash after the rewrite: %s", line)
		}
	}
}

// TestRealmRewriteAddedByMapWins checks that --map overrides --github-handles
// for the same @handle.
func TestRealmRewriteAddedByMapWins(t *testing.T) {
	dir := rewriteFixture(t)
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by",
		"--github-handles", "--map", "@alice=github:alice-gh", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Join(allAddedBy(loadEntityFile(t, dir, "TST")), " ")
	want := "TST-001=github:alice-gh TST-002=alice@example.com TST-003=github:bob TST-001/01=github:bob TSTSUB-001=github:alice-gh"
	if got != want {
		t.Errorf("added_by = %s\nwant      %s", got, want)
	}
}

// TestRealmRewriteAddedByMapWinsAnyCase checks that a --map for an @handle
// also wins over --github-handles for the same login in another case, as
// GitHub logins ignore case.
func TestRealmRewriteAddedByMapWinsAnyCase(t *testing.T) {
	dir := rewriteFixture(t)
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by",
		"--github-handles", "--map", "@Alice=github:alice-gh", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Join(allAddedBy(loadEntityFile(t, dir, "TST")), " ")
	want := "TST-001=github:alice-gh TST-002=alice@example.com TST-003=github:bob TST-001/01=github:bob TSTSUB-001=github:alice-gh"
	if got != want {
		t.Errorf("added_by = %s\nwant      %s", got, want)
	}
}

// TestRealmRewriteAddedBySkipsLinks checks that an entity file under --dir
// that is a symbolic link is listed and not followed, so a link to a file
// outside --dir can't make the migration rewrite it.
func TestRealmRewriteAddedBySkipsLinks(t *testing.T) {
	outside := rewriteFixture(t)
	before := readFiles(t, outside, "TST.json")
	dir := rewriteFixture(t)
	link := filepath.Join(dir, "LNK.json")
	if err := os.Symlink(filepath.Join(outside, "TST.json"), link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res addedByResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(res.Links) != 1 || res.Links[0] != link {
		t.Errorf("links = %v, want [%s]", res.Links, link)
	}
	if len(res.Updated) != 1 || res.Updated[0].Path != filepath.Join(dir, "TST.json") {
		t.Errorf("updated = %+v, want only %s", res.Updated, filepath.Join(dir, "TST.json"))
	}
	if !bytes.Equal(readFiles(t, outside, "TST.json")["TST.json"], before["TST.json"]) {
		t.Error("the file outside --dir was rewritten through the link")
	}
}

// TestRealmDirOfDanglingManifest checks that a realm.json linking to nothing
// is reported, rather than skipped for a parent Realm's manifest.
func TestRealmDirOfDanglingManifest(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	sub := filepath.Join(dir, "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sub, "missing.json"), filepath.Join(sub, "realm.json")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	_, err := realmDirOf(filepath.Join(sub, "TST.json"), dir)
	if err == nil {
		t.Fatal("want an error for a realm.json that links to nothing, got none")
	}
	testutil.AssertContains(t, err.Error(), "realm.json")
}

// TestRealmRewriteAddedByDryRun checks that --dry-run reports the changes and
// writes nothing.
func TestRealmRewriteAddedByDryRun(t *testing.T) {
	dir := rewriteFixture(t)
	before := readFiles(t, dir, "TST.json", "OTH.json")
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dry-run", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Would rewrite 3 added_by in TST")
	testutil.AssertContains(t, out, "to rewrite (dry run, nothing written)")
	for name, data := range readFiles(t, dir, "TST.json", "OTH.json") {
		if !bytes.Equal(data, before[name]) {
			t.Errorf("%s changed during a dry run", name)
		}
	}
}

// TestRealmRewriteAddedByErrors checks that bad flags fail before any file is
// written.
func TestRealmRewriteAddedByErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no mapping", nil, "--map OLD=NEW or --github-handles"},
		{"not a contributor id", []string{"--map", "@alice=alice"}, "CTRB-001"},
		{"unknown provider", []string{"--map", "@alice=gitlab:alice"}, "CTRB-002"},
		{"no equals sign", []string{"--map", "@alice"}, "want OLD=NEW"},
		{"empty side", []string{"--map", "=github:alice"}, "want OLD=NEW"},
		{"conflicting", []string{"--map", "@alice=github:alice", "--map", "@alice=github:alice2"}, "both"},
		{"conflicting case", []string{"--map", "@alice=github:alice", "--map", "@Alice=github:alice2"}, "same GitHub handle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := rewriteFixture(t)
			before := readFiles(t, dir, "TST.json")
			args := append([]string{"realm", "rewrite-added-by", "--dir", dir}, tt.args...)
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil {
				t.Fatalf("want an error containing %q, got none", tt.wantErr)
			}
			testutil.AssertContains(t, err.Error(), tt.wantErr)
			if !bytes.Equal(readFiles(t, dir, "TST.json")["TST.json"], before["TST.json"]) {
				t.Error("TST.json changed although the command failed")
			}
		})
	}
}

// TestRealmDirOfWalksUp finds the manifest of a directory above the file, and
// none past stop.
func TestRealmDirOfWalksUp(t *testing.T) {
	root := t.TempDir()
	setupValidRealm(t, root)
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := realmDirOf(filepath.Join(nested, "TST.json"), root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != resolvedPath(root) {
		t.Errorf("realm dir = %q, want %q", got, resolvedPath(root))
	}
	got, err = realmDirOf(filepath.Join(nested, "TST.json"), filepath.Join(root, "a"))
	if err != nil || got != "" {
		t.Errorf("stopping below the manifest: got %q, %v; want none", got, err)
	}
}

// In a repository with no user.email set, there is no git default.
func TestGitUserEmailUnset(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	if got := realGitUserEmail(dir); got != "" {
		t.Errorf("git user.email = %q, want none", got)
	}
}
