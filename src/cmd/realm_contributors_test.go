package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

var testContributors = []entity.Contributor{
	{ID: "github:alice", Email: "alice@example.com", GitHubID: 42},
	{ID: "github:bob"},
}

// TestRealmWritesKeepContributors runs every command that rewrites realm.json
// on a manifest with realm.contributors (CTRB-004) and checks they survive.
// Contributors are added last, as raw JSON, so the command under test is the
// first writer that sees them: fixture setup that goes through
// loadModifyWriteRealm can't drop them unnoticed.
func TestRealmWritesKeepContributors(t *testing.T) {
	newRealm := func(t *testing.T) string {
		dir := t.TempDir()
		setupValidRealm(t, dir)
		return dir
	}
	tests := []struct {
		name  string
		setup func(t *testing.T) string
		args  func(dir string) []string
	}{
		{"dep add", newRealm, func(dir string) []string {
			return []string{"realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir}
		}},
		{"dep remove", func(t *testing.T) string {
			dir := newRealm(t)
			writeRealmDeps(t, dir, entity.RealmDep{RealmID: "dev.steerspec.core", Version: "0.1.0"})
			return dir
		}, func(dir string) []string {
			return []string{"realm", "dep", "remove", "dev.steerspec.core", "--dir", dir}
		}},
		{"dep upgrade", func(t *testing.T) string {
			return setupUpgradeTest(t, "v9.9.9", nil)
		}, func(dir string) []string {
			return []string{"realm", "dep", "upgrade", "--dir", dir}
		}},
		{"standards add", newRealm, func(dir string) []string {
			return []string{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--dir", dir}
		}},
		{"standards remove", func(t *testing.T) string {
			dir := newRealm(t)
			setRealmStandards(t, dir, entity.StandardRef{ID: "owasp.asvs", Relation: "implements"})
			return dir
		}, func(dir string) []string {
			return []string{"realm", "standards", "remove", "--id", "owasp.asvs", "--dir", dir}
		}},
		{"standards update", func(t *testing.T) string {
			dir := newRealm(t)
			setRealmStandards(t, dir, entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "4.0.3"})
			return dir
		}, func(dir string) []string {
			return []string{"realm", "standards", "update", "--id", "owasp.asvs", "--version", "5.0.0", "--dir", dir}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
			addContributors(t, dir)
			if _, err := testutil.ExecuteCommand(NewRootCmd(), tt.args(dir)...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
			if err != nil {
				t.Fatalf("loading realm.json: %v", err)
			}
			if !slices.Equal(rf.Realm.Contributors, testContributors) {
				t.Errorf("contributors after %s = %+v, want %+v", tt.name, rf.Realm.Contributors, testContributors)
			}
		})
	}
}

// TestRealmValidateAcceptsContributors runs realm validate, schema check
// (RM002) included, on a manifest with realm.contributors. The schemas are
// this repo's vendored copies, from the pinned core (1.16.0 or later), read
// from the realm's _schema/ so no network is needed. A field the schema
// doesn't know still fails, which shows the check ran.
func TestRealmValidateAcceptsContributors(t *testing.T) {
	orig := newRealmValidator
	t.Cleanup(func() { newRealmValidator = orig })
	newRealmValidator = func(dir string, strict, recursive, _ bool) *realmValidatorSet {
		return buildRealmValidator(dir, strict, recursive, true, nil, fetchRealmDependency)
	}

	validate := func(t *testing.T, edit func(manifest map[string]any)) (string, error) {
		t.Helper()
		dir := t.TempDir()
		setupValidRealm(t, dir)
		for _, name := range []string{"realm.v1.schema.json", "entity.v1.schema.json"} {
			data, err := os.ReadFile(filepath.Join("..", "..", "rules", "_schema", name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "_schema", name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		editRealmJSON(t, dir, edit)
		return testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	}

	out, err := validate(t, func(m map[string]any) { m["realm"].(map[string]any)["contributors"] = testContributors })
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "No errors or warnings")

	out, err = validate(t, func(m map[string]any) { m["realm"].(map[string]any)["unknown_field"] = true })
	if err == nil {
		t.Fatalf("an unknown realm field passed, so the schema wasn't checked:\n%s", out)
	}
	testutil.AssertContains(t, out, "RM002")
}

// TestRealmWriteLeavesContributorsAbsent checks a manifest without
// contributors doesn't gain an empty list, which the schema rejects (minItems 1).
func TestRealmWriteLeavesContributorsAbsent(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "contributors") {
		t.Errorf("an absent contributor list must stay absent:\n%s", data)
	}
}

// addContributors edits realm.json as raw JSON, not through the write path
// under test, so a writer that drops the field can't hide it.
func addContributors(t *testing.T, dir string) {
	t.Helper()
	editRealmJSON(t, dir, func(m map[string]any) { m["realm"].(map[string]any)["contributors"] = testContributors })
}

// editRealmJSON applies edit to realm.json as a generic JSON document.
func editRealmJSON(t *testing.T, dir string, edit func(manifest map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, "realm.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	edit(manifest)
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func setRealmStandards(t *testing.T, dir string, refs ...entity.StandardRef) {
	t.Helper()
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Realm.Standards = refs
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
