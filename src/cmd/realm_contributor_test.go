package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// realGitUserEmail is gitUserEmail as built, kept by TestMain before it
// replaces it.
var realGitUserEmail func(string) string

// loadContributors returns the contributors of the realm.json in dir.
func loadContributors(t *testing.T, dir string) []entity.Contributor {
	t.Helper()
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatalf("loading realm.json: %v", err)
	}
	return rf.Realm.Contributors
}

// validateWithSchemas runs realm validate on dir with the schema check (RM002)
// on, using this repo's vendored core schemas, as
// TestRealmValidateAcceptsContributors does.
func validateWithSchemas(t *testing.T, dir string) (string, error) {
	t.Helper()
	orig := newRealmValidator
	t.Cleanup(func() { newRealmValidator = orig })
	newRealmValidator = func(dir string, strict, recursive, _ bool) *realmValidatorSet {
		return buildRealmValidator(dir, strict, recursive, true, nil, fetchRealmDependency)
	}
	for _, name := range []string{"realm.v1.schema.json", "entity.v1.schema.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "rules", "_schema", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_schema", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
}

// TestRealmContributorAdd adds two contributors, one with every field, and
// checks the manifest and that realm validate accepts it with the schema on.
func TestRealmContributorAdd(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "add", "github:alice",
		"--email", "alice@example.com", "--github-id", "42", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	var res struct {
		Realm       string             `json:"realm"`
		Contributor entity.Contributor `json:"contributor"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res.Contributor != testContributors[0] {
		t.Errorf("contributor = %+v, want %+v", res.Contributor, testContributors[0])
	}

	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "add", "github:bob", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Added contributor github:bob")
	testutil.AssertContains(t, out, "gh api users/bob --jq .id")

	if got := loadContributors(t, dir); !slices.Equal(got, testContributors) {
		t.Errorf("contributors = %+v, want %+v", got, testContributors)
	}
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate rejects the manifest: %v\n%s", err, out)
	}
}

// TestRealmContributorAddErrors checks that a bad entry is refused and leaves
// realm.json as it was.
func TestRealmContributorAddErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"not an identifier", []string{"alice"}, "CTRB-001"},
		{"unknown provider", []string{"gitlab:alice"}, "CTRB-002"},
		{"repeated", []string{"github:alice"}, "repeats"},
		{"repeated in another case", []string{"github:Alice"}, "repeats"},
		{"handle as email", []string{"github:carol", "--email", "@carol"}, "CTRB-007"},
		{"not an email", []string{"github:carol", "--email", "carol"}, "CTRB-007"},
		{"identifier as email", []string{"github:carol", "--email", "github:carol"}, "CTRB-007"},
		{"zero GitHub ID", []string{"github:carol", "--github-id", "0"}, "positive"},
		{"negative GitHub ID", []string{"github:carol", "--github-id", "-3"}, "positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			setupValidRealm(t, dir)
			setContributors(t, dir)
			before := readFiles(t, dir, "realm.json")
			args := append([]string{"realm", "contributor", "add"}, tt.args...)
			_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
			if err == nil {
				t.Fatalf("want an error containing %q, got none", tt.wantErr)
			}
			testutil.AssertContains(t, err.Error(), tt.wantErr)
			if !bytes.Equal(readFiles(t, dir, "realm.json")["realm.json"], before["realm.json"]) {
				t.Error("realm.json changed although the command failed")
			}
		})
	}
}

// TestRealmContributorSubRealm checks that a sub-realm's list can't be edited
// (CTRB-006), and that list shows its parent's.
func TestRealmContributorSubRealm(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	setContributors(t, dir)
	sub := filepath.Join(dir, "auth")
	before := readFiles(t, sub, "realm.json")

	for _, args := range [][]string{{"add", "github:carol"}, {"remove", "github:alice"}} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), append(append([]string{"realm", "contributor"}, args...), "--dir", sub)...)
		if err == nil {
			t.Fatalf("%s on a sub-realm: want an error, got none", args[0])
		}
		testutil.AssertContains(t, err.Error(), "CTRB-006")
	}
	if !bytes.Equal(readFiles(t, sub, "realm.json")["realm.json"], before["realm.json"]) {
		t.Error("the sub-realm's realm.json changed")
	}

	// A parent manifest that can't be read stops add and remove before they
	// write, rather than passing for no parent.
	parentManifest := filepath.Join(dir, "realm.json")
	good, err := os.ReadFile(parentManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentManifest, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "github:carol"}, {"remove", "github:alice"}} {
		if _, err := testutil.ExecuteCommand(NewRootCmd(), append(append([]string{"realm", "contributor"}, args...), "--dir", sub)...); err == nil {
			t.Errorf("%s under an unreadable parent manifest: want an error, got none", args[0])
		}
	}
	if !bytes.Equal(readFiles(t, sub, "realm.json")["realm.json"], before["realm.json"]) {
		t.Error("the sub-realm's realm.json changed under an unreadable parent manifest")
	}
	if err := os.WriteFile(parentManifest, good, 0o600); err != nil {
		t.Fatal(err)
	}

	// An old core pin doesn't hide the sub-realm refusal.
	older := semver.New(contributorsMinCore.Major(), contributorsMinCore.Minor()-1, 0, "", "")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", coreRealmID+"@"+older.String(), "--dir", sub); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "add", "github:carol", "--dir", sub); err == nil {
		t.Fatal("add on a sub-realm with an old core pin: want an error, got none")
	} else {
		testutil.AssertContains(t, err.Error(), "CTRB-006")
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "list", "--dir", sub, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res struct {
		InheritedFrom string               `json:"inherited_from"`
		Contributors  []entity.Contributor `json:"contributors"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res.InheritedFrom != "dev.steerspec.test" || !slices.Equal(res.Contributors, testContributors) {
		t.Errorf("list = %+v, want the parent's contributors from dev.steerspec.test", res)
	}
}

// TestRealmContributorAddOldCore checks that add is refused while the core pin
// predates the schema that accepts contributors.
func TestRealmContributorAddOldCore(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	// Built from contributorsMinCore: a literal pin outside the pin files stops
	// strspc-rules' pin_core.py.
	older := semver.New(contributorsMinCore.Major(), contributorsMinCore.Minor()-1, 0, "", "")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", coreRealmID+"@"+older.String(), "--dir", dir); err != nil {
		t.Fatal(err)
	}
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "add", "github:alice", "--dir", dir)
	if err == nil {
		t.Fatal("want an error for a core pin without contributors, got none")
	}
	testutil.AssertContains(t, err.Error(), "reject `contributors`")
	testutil.AssertContains(t, err.Error(), contributorsMinCore.String())
	if got := loadContributors(t, dir); got != nil {
		t.Errorf("contributors = %+v, want none", got)
	}
}

// TestRealmContributorRemove removes by id in any case, refuses an unknown id,
// and drops the key with the last entry, as the schema requires one entry.
func TestRealmContributorRemove(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	setContributors(t, dir)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "remove", "github:carol", "--dir", dir); err == nil {
		t.Fatal("removing an unlisted contributor: want an error, got none")
	}
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "remove", "github:ALICE", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Removed contributor github:alice")
	if got := loadContributors(t, dir); !slices.Equal(got, testContributors[1:]) {
		t.Errorf("contributors = %+v, want %+v", got, testContributors[1:])
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "remove", "github:bob", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(readFiles(t, dir, "realm.json")["realm.json"]), "contributors") {
		t.Error("an empty list must leave no contributors key: the schema needs at least one entry")
	}
}

// TestRealmContributorList checks the human and JSON output, with and without
// a list.
func TestRealmContributorList(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "list", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, `"contributors": []`)
	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "checked for its format only")

	setContributors(t, dir)
	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "contributor", "list", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "github:alice (alice@example.com, GitHub ID 42)")
	testutil.AssertContains(t, out, "github:bob")
}

// explicitEmptyFlag stands, in TestAddedByDefault, for --added-by= given empty.
const explicitEmptyFlag = "<empty>"

// TestAddedByDefault covers where rule add, note add and rule supersede take
// added_by from when --added-by is not given.
func TestAddedByDefault(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"rule add", []string{"rule", "add", "TST", "--body", "The system MUST log."}},
		{"note add", []string{"note", "add", "TST-001", "--type", "rationale", "--body", "Why."}},
		{"rule supersede", []string{"rule", "supersede", "TST-001", "--body", "The system MUST audit."}},
	}
	cases := []struct {
		name     string
		listed   bool
		flag     string
		env      string
		gitEmail string
		want     string // the added_by written; "" when refused
		wantText string // in the output when accepted, in the error when refused
	}{
		{"flag wins over env", true, "github:bob", "github:alice", "", "github:bob", ""},
		{"empty flag doesn't fall through", true, explicitEmptyFlag, "github:alice", "alice@example.com", "", "--added-by must not be empty"},
		{"env", true, "", "github:alice", "", "github:alice", "Added by: github:alice (from STRSPC_ADDED_BY)"},
		{"env unlisted", true, "", "github:carol", "", "", "CTRB-005"},
		{"env without a list", false, "", "github:carol", "", "github:carol", "(from STRSPC_ADDED_BY)"},
		{"git email", true, "", "", "Alice@Example.com", "github:alice", "Added by: github:alice (from git user.email)"},
		{"git email unlisted", true, "", "", "carol@example.com", "", "--added-by is required"},
		{"git email without a list", false, "", "", "alice@example.com", "", "--added-by is required"},
		{"nothing", true, "", "", "", "", "STRSPC_ADDED_BY"},
	}
	for _, c := range commands {
		for _, tc := range cases {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				setupValidRealm(t, dir)
				if tc.listed {
					setContributors(t, dir)
				}
				setupEntityFile(t, dir, "TST", makeRule("TST-001", "P", "The system MUST validate.", "github:alice"))
				before := readFiles(t, dir, "TST.json")
				t.Setenv(addedByEnv, tc.env)
				orig := gitUserEmail
				t.Cleanup(func() { gitUserEmail = orig })
				gitUserEmail = func(string) string { return tc.gitEmail }

				args := append([]string{}, c.args...)
				switch tc.flag {
				case "":
				case explicitEmptyFlag:
					args = append(args, "--added-by=")
				default:
					args = append(args, "--added-by", tc.flag)
				}
				out, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
				if tc.want == "" {
					if err == nil {
						t.Fatalf("want an error containing %q, got none", tc.wantText)
					}
					testutil.AssertContains(t, err.Error(), tc.wantText)
					if !bytes.Equal(readFiles(t, dir, "TST.json")["TST.json"], before["TST.json"]) {
						t.Error("entity file changed although the command failed")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.wantText != "" {
					testutil.AssertContains(t, out, tc.wantText)
				} else if strings.Contains(out, "Added by:") {
					t.Errorf("an explicit --added-by is not echoed:\n%s", out)
				}
				if got := newestAddedBy(t, dir); got != tc.want {
					t.Errorf("added_by = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// newestAddedBy returns the added_by of the last rule or note of TST, which
// the command under test wrote.
func newestAddedBy(t *testing.T, dir string) string {
	t.Helper()
	f := loadEntityFile(t, dir, "TST")
	if len(f.Notes) > 0 {
		return f.Notes[len(f.Notes)-1].AddedBy
	}
	return f.Rules[len(f.Rules)-1].AddedBy
}

// TestGitUserEmail runs the real lookup in a repository with a local email,
// and outside any repository, where a global email must not be used.
func TestGitUserEmail(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\temail = global@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	outside := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(outside))
	if got := realGitUserEmail(outside); got != "" {
		t.Errorf("outside a repository: git user.email = %q, want none", got)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", "")
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "alice@example.com"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if got := realGitUserEmail(dir); got != "alice@example.com" {
		t.Errorf("git user.email = %q, want alice@example.com", got)
	}
}
