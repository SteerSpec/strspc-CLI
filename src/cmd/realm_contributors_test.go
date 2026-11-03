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
func TestRealmWritesKeepContributors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string
		args  func(dir string) []string
	}{
		{"dep add", contributorRealm, func(dir string) []string {
			return []string{"realm", "dep", "add", "dev.steerspec.core@0.1.0", "--dir", dir}
		}},
		{"dep remove", func(t *testing.T) string {
			dir := contributorRealm(t)
			writeRealmDeps(t, dir, entity.RealmDep{RealmID: "dev.steerspec.core", Version: "0.1.0"})
			return dir
		}, func(dir string) []string {
			return []string{"realm", "dep", "remove", "dev.steerspec.core", "--dir", dir}
		}},
		{"dep upgrade", func(t *testing.T) string {
			dir := setupUpgradeTest(t, "v9.9.9", nil)
			addContributors(t, dir)
			return dir
		}, func(dir string) []string {
			return []string{"realm", "dep", "upgrade", "--dir", dir}
		}},
		{"standards add", contributorRealm, func(dir string) []string {
			return []string{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--dir", dir}
		}},
		{"standards remove", func(t *testing.T) string {
			dir := contributorRealm(t)
			setRealmStandards(t, dir, entity.StandardRef{ID: "owasp.asvs", Relation: "implements"})
			return dir
		}, func(dir string) []string {
			return []string{"realm", "standards", "remove", "--id", "owasp.asvs", "--dir", dir}
		}},
		{"standards update", func(t *testing.T) string {
			dir := contributorRealm(t)
			setRealmStandards(t, dir, entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "4.0.3"})
			return dir
		}, func(dir string) []string {
			return []string{"realm", "standards", "update", "--id", "owasp.asvs", "--version", "5.0.0", "--dir", dir}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
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

func contributorRealm(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	setupValidRealm(t, dir)
	addContributors(t, dir)
	return dir
}

// addContributors edits realm.json as raw JSON, not through the write path
// under test, so a writer that drops the field can't hide it.
func addContributors(t *testing.T, dir string) {
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
	manifest["realm"].(map[string]any)["contributors"] = testContributors
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
