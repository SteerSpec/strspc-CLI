package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// makeParentRealm creates a minimal parent realm directory with realm.json and _schema/.
func makeParentRealm(t *testing.T, id string, deps []entity.RealmDep) string {
	t.Helper()
	dir := t.TempDir()

	schemaDir := filepath.Join(dir, "_schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sf := range schemaFiles {
		content := []byte(`{"$id": "test-` + sf.local + `"}`)
		if err := os.WriteFile(filepath.Join(schemaDir, sf.local), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if deps == nil {
		deps = []entity.RealmDep{}
	}
	realm := realmJSON{
		Schema:            "./_schema/realm.v1.schema.json",
		Realm:             realmMeta{ID: id, Title: "Parent Realm", Version: "0.1.0"},
		Dependencies:      deps,
		RuleIdentifierFmt: nil,
	}
	data, err := json.MarshalIndent(realm, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestRealmAddSubrealmHappyPath(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := filepath.Join(t.TempDir(), "child")

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--title", "Child Realm",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "Initialized sub-realm")
	testutil.AssertContains(t, output, "com.test.parent.child")

	// realm.json exists and is correct.
	data, err := os.ReadFile(filepath.Join(childDir, "realm.json"))
	if err != nil {
		t.Fatalf("reading child realm.json: %v", err)
	}

	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatalf("parsing child realm.json: %v", err)
	}

	if realm.Realm.ID != "com.test.parent.child" {
		t.Errorf("realm ID = %q, want %q", realm.Realm.ID, "com.test.parent.child")
	}
	if realm.Realm.Title != "Child Realm" {
		t.Errorf("realm title = %q, want %q", realm.Realm.Title, "Child Realm")
	}
	if realm.Realm.Version != "0.1.0" {
		t.Errorf("realm version = %q, want %q", realm.Realm.Version, "0.1.0")
	}
	if realm.Schema != "./_schema/realm.v1.schema.json" {
		t.Errorf("$schema = %q, want %q", realm.Schema, "./_schema/realm.v1.schema.json")
	}
}

func TestRealmAddSubrealmCopiesSchemas(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := filepath.Join(t.TempDir(), "child")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, sf := range schemaFiles {
		path := filepath.Join(childDir, "_schema", sf.local)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected schema file %s to exist in child", sf.local)
		}
	}
}

func TestRealmAddSubrealmInheritsDeps(t *testing.T) {
	deps := []entity.RealmDep{
		{RealmID: "dev.steerspec.core", Version: "0.1.0", Source: "github://SteerSpec/strspc-rules@latest"},
	}
	parentDir := makeParentRealm(t, "com.test.parent", deps)
	childDir := filepath.Join(t.TempDir(), "child")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(childDir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}

	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatal(err)
	}

	if len(realm.Dependencies) != 1 {
		t.Fatalf("expected 1 inherited dependency, got %d", len(realm.Dependencies))
	}
	if realm.Dependencies[0].RealmID != "dev.steerspec.core" {
		t.Errorf("dep realm_id = %q, want %q", realm.Dependencies[0].RealmID, "dev.steerspec.core")
	}
}

func TestRealmAddSubrealmNoInheritDeps(t *testing.T) {
	deps := []entity.RealmDep{
		{RealmID: "dev.steerspec.core", Version: "0.1.0"},
	}
	parentDir := makeParentRealm(t, "com.test.parent", deps)
	childDir := filepath.Join(t.TempDir(), "child")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
		"--no-inherit-deps",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(childDir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}

	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatal(err)
	}

	if len(realm.Dependencies) != 0 {
		t.Errorf("expected 0 dependencies with --no-inherit-deps, got %d", len(realm.Dependencies))
	}
}

func TestRealmAddSubrealmAlreadyExists(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := t.TempDir()

	// Pre-create realm.json in child.
	if err := os.WriteFile(filepath.Join(childDir, "realm.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err == nil {
		t.Fatal("expected error for existing realm, got nil")
	}
	testutil.AssertContains(t, err.Error(), "realm already exists")
}

func TestRealmAddSubrealmForce(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(childDir, "realm.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
		"--force",
	)
	if err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(childDir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}

	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatal(err)
	}
	if realm.Realm.ID != "com.test.parent.child" {
		t.Errorf("realm ID = %q, want %q", realm.Realm.ID, "com.test.parent.child")
	}
}

func TestRealmAddSubrealmNoParentRealm(t *testing.T) {
	emptyDir := t.TempDir()
	childDir := filepath.Join(t.TempDir(), "child")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", emptyDir,
	)
	if err == nil {
		t.Fatal("expected error for missing parent realm.json, got nil")
	}
	testutil.AssertContains(t, err.Error(), "parent realm")
}

func TestRealmAddSubrealmInvalidID(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := filepath.Join(t.TempDir(), "child")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "INVALID ID!",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err == nil {
		t.Fatal("expected error for invalid realm ID, got nil")
	}
	testutil.AssertContains(t, err.Error(), "invalid realm ID")
}

func TestRealmAddSubrealmFallsBackToFetchWhenNoParentSchema(t *testing.T) {
	setupSchemaServer(t)

	// Parent without _schema/.
	parentDir := t.TempDir()
	realm := realmJSON{
		Schema:            "./_schema/realm.v1.schema.json",
		Realm:             realmMeta{ID: "com.test.parent", Title: "Parent", Version: "0.1.0"},
		Dependencies:      []entity.RealmDep{},
		RuleIdentifierFmt: nil,
	}
	data, err := json.MarshalIndent(realm, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentDir, "realm.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	childDir := filepath.Join(t.TempDir(), "child")
	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err != nil {
		t.Fatalf("unexpected error with fetch fallback: %v", err)
	}

	for _, sf := range schemaFiles {
		path := filepath.Join(childDir, "_schema", sf.local)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected schema file %s to exist after fetch fallback", sf.local)
		}
	}
}
