package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// setupSchemaServer starts a test HTTP server that serves fake schema files
// and sets schemaBaseURL to point to it, registering cleanup via t.Cleanup.
func setupSchemaServer(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	for _, sf := range schemaFiles {
		contentCopy := fmt.Sprintf(`{"$id": "test-%s"}`, sf.local)
		remoteCopy := sf.remote
		mux.HandleFunc("/"+remoteCopy, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(contentCopy))
		})
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	original := schemaBaseURL
	schemaBaseURL = server.URL
	t.Cleanup(func() { schemaBaseURL = original })
}

func TestRealmInitCreatesRealm(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "rules")

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target, "--id", "com.test.demo", "--title", "Demo Realm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "Initialized Realm")
	testutil.AssertContains(t, output, "com.test.demo")

	// Check realm.json exists and is valid.
	realmPath := filepath.Join(target, "realm.json")
	data, err := os.ReadFile(realmPath)
	if err != nil {
		t.Fatalf("reading realm.json: %v", err)
	}

	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatalf("parsing realm.json: %v", err)
	}

	if realm.Realm.ID != "com.test.demo" {
		t.Errorf("realm ID = %q, want %q", realm.Realm.ID, "com.test.demo")
	}
	if realm.Realm.Title != "Demo Realm" {
		t.Errorf("realm title = %q, want %q", realm.Realm.Title, "Demo Realm")
	}
	if realm.Realm.Version != "0.1.0" {
		t.Errorf("realm version = %q, want %q", realm.Realm.Version, "0.1.0")
	}
	if realm.Schema != "./_schema/realm.v1.schema.json" {
		t.Errorf("$schema = %q, want %q", realm.Schema, "./_schema/realm.v1.schema.json")
	}

	// Check _schema/ files exist.
	for _, sf := range schemaFiles {
		path := filepath.Join(target, "_schema", sf.local)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected schema file %s to exist", sf.local)
		}
	}
}

func TestRealmInitDefaultID(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "myrealm")

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "myrealm")

	data, err := os.ReadFile(filepath.Join(target, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatal(err)
	}
	if realm.Realm.ID != "myrealm" {
		t.Errorf("realm ID = %q, want %q", realm.Realm.ID, "myrealm")
	}
}

func TestRealmInitInvalidID(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "rules")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target, "--id", "INVALID ID!")
	if err == nil {
		t.Fatal("expected error for invalid realm ID, got nil")
	}
	testutil.AssertContains(t, err.Error(), "invalid realm ID")
}

func TestRealmInitAlreadyExists(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "rules")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "realm.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target)
	if err == nil {
		t.Fatal("expected error for existing realm, got nil")
	}
	testutil.AssertContains(t, err.Error(), "realm already initialized")
}

func TestRealmInitForce(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "rules")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "realm.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target, "--id", "com.test.force", "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(target, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm realmJSON
	if err := json.Unmarshal(data, &realm); err != nil {
		t.Fatal(err)
	}
	if realm.Realm.ID != "com.test.force" {
		t.Errorf("realm ID = %q, want %q", realm.Realm.ID, "com.test.force")
	}
}

func TestRealmInitSchemaFetchError(t *testing.T) {
	// Point to a server that returns 404.
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	original := schemaBaseURL
	schemaBaseURL = server.URL
	t.Cleanup(func() { schemaBaseURL = original })

	dir := t.TempDir()
	target := filepath.Join(dir, "rules")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target)
	if err == nil {
		t.Fatal("expected error for schema fetch failure, got nil")
	}
	testutil.AssertContains(t, err.Error(), "fetching schema")
}

func TestRealmInitDetectsConfig(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()

	// Create .strspc/config.yaml without referencing the realm dir.
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte("rules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "rules")

	// Run from inside dir so printConfigSuggestion finds .strspc/config.yaml.
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertContains(t, output, "Tip: Add this source")
}

func TestRealmInitConfigAlreadyReferenced(t *testing.T) {
	setupSchemaServer(t)
	dir := t.TempDir()

	target := filepath.Join(dir, "rules")

	// Create .strspc/config.yaml that already references the realm dir.
	strspcDir := filepath.Join(dir, ".strspc")
	if err := os.MkdirAll(strspcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configContent := fmt.Sprintf("rules:\n  - source: %s\n", target)
	if err := os.WriteFile(filepath.Join(strspcDir, "config.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--dir", target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	testutil.AssertNotContains(t, output, "Tip: Add this source")
}

func TestRealmInitHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--id")
	testutil.AssertContains(t, output, "--title")
	testutil.AssertContains(t, output, "--dir")
	testutil.AssertContains(t, output, "--force")
}
