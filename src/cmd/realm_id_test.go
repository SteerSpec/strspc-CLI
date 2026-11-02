package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// realmIDSamples holds IDs on both sides of the realm.v1 grammar.
var realmIDSamples = []string{
	"acme", "com.acme", "com.acme.prod2", "dev.steerspec.core", "a", "a1.b2",
	"acme-prod", "com.acme-prod", "1acme", "com.1acme", "Acme", "com..acme",
	".acme", "acme.", "acme_prod", "acme prod", "",
}

// realmIDPattern must accept exactly what realm.v1.schema.json's realm.id
// pattern accepts, so realm init never scaffolds a realm that realm validate
// rejects (strspc-cli-cnn).
func TestRealmIDPatternMatchesTheSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "rules", "_schema", "realm.v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties struct {
			Realm struct {
				Properties struct {
					ID struct {
						Pattern string `json:"pattern"`
					} `json:"id"`
				} `json:"properties"`
			} `json:"realm"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	pattern := s.Properties.Realm.Properties.ID.Pattern
	if pattern == "" {
		t.Fatal("realm.id pattern not found in realm.v1.schema.json")
	}
	schemaRe := regexp.MustCompile(pattern)
	for _, id := range realmIDSamples {
		if got, want := realmIDPattern.MatchString(id), schemaRe.MatchString(id); got != want {
			t.Errorf("realmIDPattern(%q) = %v, schema says %v", id, got, want)
		}
	}
}

func TestRealmInitRejectsIDsOutsideTheGrammar(t *testing.T) {
	setupSchemaServer(t)
	for _, id := range []string{"acme-prod", "1acme", "com..acme", "Acme"} {
		target := filepath.Join(t.TempDir(), "rules")
		_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target, "--id", id)
		if err == nil || !strings.Contains(err.Error(), "invalid realm ID") {
			t.Errorf("--id %s: error = %v, want invalid realm ID", id, err)
		}
		if _, statErr := os.Stat(filepath.Join(target, "realm.json")); statErr == nil {
			t.Errorf("--id %s: realm.json was written", id)
		}
	}
}

func TestRealmInitDefaultIDOutsideTheGrammar(t *testing.T) {
	setupSchemaServer(t)
	target := filepath.Join(t.TempDir(), "my-project")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target)
	if err == nil || !strings.Contains(err.Error(), `directory name "my-project" is not a valid realm ID`) || !strings.Contains(err.Error(), "--id") {
		t.Fatalf("error = %v, want a refusal suggesting --id", err)
	}
	if _, statErr := os.Stat(filepath.Join(target, "realm.json")); statErr == nil {
		t.Error("realm.json was written")
	}

	// The same directory works with an explicit ID.
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target, "--id", "com.acme.myproject"); err != nil {
		t.Fatalf("with --id: %v", err)
	}
}

func TestDependencyRealmIDOutsideTheGrammar(t *testing.T) {
	for _, spec := range []string{"acme-prod@1.0.0", "1acme@1.0.0"} {
		if _, err := parseDependency(spec); err == nil || !strings.Contains(err.Error(), "invalid realm ID") {
			t.Errorf("%s: error = %v, want invalid realm ID", spec, err)
		}
	}
}

// An entry that predates the grammar must still be removable.
func TestRealmDepRemoveAcceptsAnIDOutsideTheGrammar(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Dependencies = []entity.RealmDep{{RealmID: "acme-prod", Version: "1.0.0"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "remove", "acme-prod", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.Dependencies) != 0 {
		t.Errorf("dependencies = %+v, want none", rf.Dependencies)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "remove", "acme\x1b", "--dir", dir); err == nil {
		t.Error("expected a control character to be rejected")
	}
}
