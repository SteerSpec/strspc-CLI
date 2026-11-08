package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
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

func TestRealmAddSubrealmIDNotChildOfParent(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := filepath.Join(t.TempDir(), "child")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.other.unrelated",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err == nil {
		t.Fatal("expected error for non-child realm ID, got nil")
	}
	testutil.AssertContains(t, err.Error(), "must be a child of parent realm ID")
}

func TestRealmAddSubrealmSameDirAsParent(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", parentDir,
		"--parent-dir", parentDir,
		"--force",
	)
	if err == nil {
		t.Fatal("expected error when target and parent are the same directory, got nil")
	}
	testutil.AssertContains(t, err.Error(), "must differ from parent directory")
}

func TestRealmAddSubrealmErrorWhenParentSchemaIsFile(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)

	// Replace _schema/ directory with a regular file.
	schemaPath := filepath.Join(parentDir, "_schema")
	if err := os.RemoveAll(schemaPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(schemaPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	childDir := filepath.Join(t.TempDir(), "child")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child",
		"--dir", childDir,
		"--parent-dir", parentDir,
	)
	if err == nil {
		t.Fatal("expected error when parent _schema/ is a file, got nil")
	}
	testutil.AssertContains(t, err.Error(), "not a directory")
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

// TestRealmAddSubrealmFetchesMissingSchemasFromParentCorePin: a parent holding
// only one schema supplies it, the other two come from the parent's core pin
// (not the latest release), all from one release, with or without inherited deps.
func TestRealmAddSubrealmFetchesMissingSchemasFromParentCorePin(t *testing.T) {
	for _, extra := range [][]string{nil, {"--no-inherit-deps"}} {
		tags := setupSchemaServer(t)
		parentDir := makeParentRealm(t, "com.test.parent", []entity.RealmDep{{RealmID: coreRealmID, Version: "1.2.3"}})
		kept := schemaFiles[0].local
		for _, sf := range schemaFiles[1:] {
			if err := os.Remove(filepath.Join(parentDir, "_schema", sf.local)); err != nil {
				t.Fatal(err)
			}
		}

		childDir := filepath.Join(t.TempDir(), "child")
		args := append([]string{"realm", "add-subrealm", "--id", "com.test.parent.child", "--dir", childDir, "--parent-dir", parentDir}, extra...)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), args...); err != nil {
			t.Fatalf("%v: %v", extra, err)
		}

		if want := len(schemaFiles) - 1; len(*tags) != want {
			t.Errorf("%v: got %d schema requests (%v), want %d", extra, len(*tags), *tags, want)
		}
		for _, tag := range *tags {
			if tag != "v1.2.3" {
				t.Errorf("%v: fetched from %q, want the parent's pin v1.2.3", extra, tag)
			}
		}
		got, err := os.ReadFile(filepath.Join(childDir, "_schema", kept))
		if err != nil {
			t.Fatal(err)
		}
		if want := `{"$id": "test-` + kept + `"}`; string(got) != want {
			t.Errorf("%v: %s = %q, want the parent's copy %q", extra, kept, got, want)
		}
	}
}

// TestRealmAddSubrealmResolvesLatestOnce: with no core pin, several missing
// files still cost a single latest-release lookup.
func TestRealmAddSubrealmResolvesLatestOnce(t *testing.T) {
	setupSchemaServer(t)
	lookups := 0
	latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) {
		lookups++
		return testLatestCoreTag, nil
	}
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	if err := os.RemoveAll(filepath.Join(parentDir, "_schema")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parentDir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}

	childDir := filepath.Join(t.TempDir(), "child")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm", "--id", "com.test.parent.child", "--dir", childDir, "--parent-dir", parentDir); err != nil {
		t.Fatal(err)
	}
	if lookups != 1 {
		t.Errorf("latest-release lookups = %d, want 1", lookups)
	}
}

// runAddSubrealm runs add-subrealm for com.test.parent.child.
func runAddSubrealm(t *testing.T, parentDir, childDir string, extra ...string) error {
	t.Helper()
	args := append([]string{"realm", "add-subrealm", "--id", "com.test.parent.child", "--dir", childDir, "--parent-dir", parentDir}, extra...)
	_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
	return err
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error containing %q, got none", want)
	}
	testutil.AssertContains(t, err.Error(), want)
}

// TestRealmAddSubrealmFilesystemErrors covers the places where the sub-realm
// can't be laid out, each reported with what failed.
func TestRealmAddSubrealmFilesystemErrors(t *testing.T) {
	t.Run("malformed parent realm.json", func(t *testing.T) {
		parentDir := makeParentRealm(t, "com.test.parent", nil)
		if err := os.WriteFile(filepath.Join(parentDir, "realm.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, runAddSubrealm(t, parentDir, filepath.Join(t.TempDir(), "child")), "loading parent realm.json")
	})

	t.Run("target under a regular file", func(t *testing.T) {
		parentDir := makeParentRealm(t, "com.test.parent", nil)
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, runAddSubrealm(t, parentDir, filepath.Join(file, "child")), "creating directory")
	})

	t.Run("target _schema is a file", func(t *testing.T) {
		parentDir := makeParentRealm(t, "com.test.parent", nil)
		childDir := filepath.Join(t.TempDir(), "child")
		if err := os.MkdirAll(childDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(childDir, "_schema"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, runAddSubrealm(t, parentDir, childDir), "creating _schema/")
	})

	t.Run("parent schema is a directory", func(t *testing.T) {
		parentDir := makeParentRealm(t, "com.test.parent", nil)
		schema := filepath.Join(parentDir, "_schema", schemaFiles[0].local)
		if err := os.Remove(schema); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(schema, 0o755); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, runAddSubrealm(t, parentDir, filepath.Join(t.TempDir(), "child")), "reading schema "+schemaFiles[0].local)
	})

	t.Run("target schema is a directory", func(t *testing.T) {
		parentDir := makeParentRealm(t, "com.test.parent", nil)
		childDir := filepath.Join(t.TempDir(), "child")
		if err := os.MkdirAll(filepath.Join(childDir, "_schema", schemaFiles[0].local), 0o755); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, runAddSubrealm(t, parentDir, childDir), "writing schema "+schemaFiles[0].local)
	})

	t.Run("target realm.json is a directory, with --force", func(t *testing.T) {
		parentDir := makeParentRealm(t, "com.test.parent", nil)
		childDir := filepath.Join(t.TempDir(), "child")
		if err := os.MkdirAll(filepath.Join(childDir, "realm.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, runAddSubrealm(t, parentDir, childDir, "--force"), "writing realm.json")
	})
}

// TestRealmAddSubrealmFetchErrors covers a schema the parent lacks that can't
// be fetched: the core release can't be resolved, or the file isn't served.
func TestRealmAddSubrealmFetchErrors(t *testing.T) {
	failLatest := func(t *testing.T) {
		t.Helper()
		latest := latestReleaseTag
		latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) {
			return "", errors.New("no release")
		}
		t.Cleanup(func() { latestReleaseTag = latest })
	}
	serveNothing := func(t *testing.T) {
		t.Helper()
		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)
		base := schemaBaseURL
		schemaBaseURL = server.URL
		t.Cleanup(func() { schemaBaseURL = base })
		latest := latestReleaseTag
		latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) { return testLatestCoreTag, nil }
		t.Cleanup(func() { latestReleaseTag = latest })
	}
	dropOne := func(t *testing.T, parentDir string) {
		t.Helper()
		if err := os.Remove(filepath.Join(parentDir, "_schema", schemaFiles[1].local)); err != nil {
			t.Fatal(err)
		}
	}
	dropAll := func(t *testing.T, parentDir string) {
		t.Helper()
		if err := os.RemoveAll(filepath.Join(parentDir, "_schema")); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name   string
		remove func(*testing.T, string)
		fail   func(*testing.T)
		want   string
	}{
		{"one schema missing, no release", dropOne, failLatest, "no release"},
		{"one schema missing, not served", dropOne, serveNothing, schemaFiles[1].local},
		{"no parent _schema, no release", dropAll, failLatest, "no release"},
		{"no parent _schema, not served", dropAll, serveNothing, schemaFiles[0].local},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fail(t)
			parentDir := makeParentRealm(t, "com.test.parent", nil)
			tt.remove(t, parentDir)
			childDir := filepath.Join(t.TempDir(), "child")
			assertErrContains(t, runAddSubrealm(t, parentDir, childDir), tt.want)
			if _, err := os.Stat(filepath.Join(childDir, "realm.json")); err == nil {
				t.Error("realm.json was written although the schemas failed")
			}
		})
	}
}

// TestRealmAddSubrealmForceReplacesLink replaces a target realm.json that is a
// symbolic link with a regular file, and leaves the link's target alone.
func TestRealmAddSubrealmForceReplacesLink(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	childDir := t.TempDir()
	link := filepath.Join(childDir, "realm.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child", "--dir", childDir, "--parent-dir", parentDir, "--force"); err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	if isLink, err := isLinkPath(link); err != nil || isLink {
		t.Errorf("realm.json is still a link (err %v)", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "{}" {
		t.Errorf("the link's target was written: %q (err %v)", data, err)
	}
}
