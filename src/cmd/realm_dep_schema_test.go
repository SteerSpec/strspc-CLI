package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// setupSchemaSyncTest adds a _schema/ directory to an upgrade fixture and
// serves coreFiles as the upgraded core release's _schema/.
func setupSchemaSyncTest(t *testing.T, validate func() *result.Result, local, coreFiles map[string]string) (dir string) {
	t.Helper()
	dir = setupUpgradeTest(t, "v9.9.9", validate)
	coreDir := t.TempDir()
	writeFiles(t, filepath.Join(dir, "_schema"), local)
	writeFiles(t, filepath.Join(coreDir, "_schema"), coreFiles)

	orig := fetchUpgradeDep
	t.Cleanup(func() { fetchUpgradeDep = orig })
	fetchUpgradeDep = func(context.Context, string, entity.RealmDep) (string, error) { return coreDir, nil }
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRealmDepUpgradeSyncsCoreSchemas(t *testing.T) {
	dir := setupSchemaSyncTest(t, nil,
		map[string]string{
			"entity.v1.schema.json": `{"v":1}`,
			"realm.v1.schema.json":  `{"v":1}`,
			"custom.json":           `{"mine":true}`, // core lacks it: untouched
			"notes.txt":             `not json`,
		},
		map[string]string{
			"entity.v1.schema.json": `{"v":2}`,
			"realm.v1.schema.json":  "{\n  \"v\": 1\n}\n", // formatting only: untouched
			"extra.json":            `{"core":"only"}`,    // realm lacks it: not added
		})

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "entity.v1.schema.json")

	schema := filepath.Join(dir, "_schema")
	if got := readFile(t, filepath.Join(schema, "entity.v1.schema.json")); got != `{"v":2}` {
		t.Errorf("entity schema not synced: %s", got)
	}
	if got := readFile(t, filepath.Join(schema, "realm.v1.schema.json")); got != `{"v":1}` {
		t.Errorf("formatting-only difference was rewritten: %s", got)
	}
	if got := readFile(t, filepath.Join(schema, "custom.json")); got != `{"mine":true}` {
		t.Errorf("file the core lacks was changed: %s", got)
	}
	if _, err := os.Stat(filepath.Join(schema, "extra.json")); !os.IsNotExist(err) {
		t.Error("core-only file was copied into the realm")
	}
}

func TestRealmDepUpgradeRestoresSchemasOnValidationFailure(t *testing.T) {
	failing := func() *result.Result {
		res := &result.Result{}
		res.Add(result.Diagnostic{Code: "RM099", Severity: result.Error, Message: "breaks"})
		return res
	}
	dir := setupSchemaSyncTest(t, failing,
		map[string]string{"entity.v1.schema.json": `{"v":1}`},
		map[string]string{"entity.v1.schema.json": `{"v":2}`})
	before := readFile(t, filepath.Join(dir, "realm.json"))

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err == nil {
		t.Fatal("expected error when the upgrade breaks the realm")
	}
	if got := readFile(t, filepath.Join(dir, "_schema", "entity.v1.schema.json")); got != `{"v":1}` {
		t.Errorf("schema not restored: %s", got)
	}
	if got := readFile(t, filepath.Join(dir, "realm.json")); got != before {
		t.Error("realm.json not restored")
	}
}

func TestRealmDepUpgradeSchemaFetchFailureRestores(t *testing.T) {
	dir := setupSchemaSyncTest(t, nil,
		map[string]string{"entity.v1.schema.json": `{"v":1}`}, nil)
	fetchUpgradeDep = func(context.Context, string, entity.RealmDep) (string, error) {
		return "", os.ErrPermission
	}
	before, _ := os.ReadFile(filepath.Join(dir, "realm.json"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
	if err == nil {
		t.Fatal("expected error when the core release cannot be fetched")
	}
	testutil.AssertContains(t, err.Error(), "left unchanged")
	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(after, before) {
		t.Error("realm.json not restored")
	}
}

// addSubRealm declares a sub-realm of dir holding the given _schema/ files.
func addSubRealm(t *testing.T, dir, name, manifest string, schemas map[string]string) {
	t.Helper()
	withSubs := strings.Replace(upgradeRealmJSON, `"rule_identifier_format"`, `"sub_realms": ["`+name+`"],
  "rule_identifier_format"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(withSubs), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, filepath.Join(dir, name), map[string]string{"realm.json": manifest})
	writeFiles(t, filepath.Join(dir, name, "_schema"), schemas)
}

func TestRealmDepUpgradeSyncsSubRealmSchemas(t *testing.T) {
	const v1, v2 = `{"v":1}`, `{"v":2}`
	schemas := map[string]string{"entity.v1.schema.json": v1}
	core := map[string]string{"entity.v1.schema.json": v2}
	subSchema := func(dir string) string {
		return readFile(t, filepath.Join(dir, "auth", "_schema", "entity.v1.schema.json"))
	}

	t.Run("inherits the core pin", func(t *testing.T) {
		dir := setupSchemaSyncTest(t, nil, schemas, core)
		addSubRealm(t, dir, "auth", `{"realm": {"id": "dev.steerspec.test.auth"}}`, schemas)
		out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
		if err != nil {
			t.Fatalf("upgrade failed: %v\n%s", err, out)
		}
		if got := subSchema(dir); got != v2 {
			t.Errorf("sub-realm schema not synced: %s", got)
		}
		testutil.AssertContains(t, out, filepath.Join("auth", "_schema"))
	})

	t.Run("restored when validation fails", func(t *testing.T) {
		failing := func() *result.Result {
			res := &result.Result{}
			res.Add(result.Diagnostic{Code: "RM099", Severity: result.Error, Message: "breaks"})
			return res
		}
		dir := setupSchemaSyncTest(t, failing, schemas, core)
		addSubRealm(t, dir, "auth", `{"realm": {"id": "dev.steerspec.test.auth"}}`, schemas)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err == nil {
			t.Fatal("expected error when the upgrade breaks the realm")
		}
		if got := subSchema(dir); got != v1 {
			t.Errorf("sub-realm schema not restored: %s", got)
		}
	})

	t.Run("pins its own core", func(t *testing.T) {
		dir := setupSchemaSyncTest(t, nil, schemas, core)
		addSubRealm(t, dir, "auth", `{"dependencies": [{"realm_id": "`+coreRealmID+`", "version": "1.8.0"}]}`, schemas)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
			t.Fatal(err)
		}
		if got := subSchema(dir); got != v1 {
			t.Errorf("a sub-realm with its own pin was synced: %s", got)
		}
	})
}

func TestSyncCoreSchemasRecordsAFileWhoseWriteFails(t *testing.T) {
	const v1 = `{"v":1}`
	dir := setupSchemaSyncTest(t, nil,
		map[string]string{"entity.v1.schema.json": v1},
		map[string]string{"entity.v1.schema.json": `{"v":2}`})
	path := filepath.Join(dir, "_schema", "entity.v1.schema.json")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	synced, err := syncCoreSchemas(context.Background(), dir, depUpgrade{realmID: coreRealmID}, nil)
	if err == nil {
		t.Skip("a read-only file was writable, as it is for root")
	}
	if len(synced) != 1 || string(synced[0].original) != v1 {
		t.Errorf("a failed write can truncate its file, so it must be restored: %+v", synced)
	}
}

func TestRealmDepUpgradeNoSchemaDirIsFine(t *testing.T) {
	dir := setupUpgradeTest(t, "v9.9.9", nil)
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
		t.Fatalf("a realm without _schema/ should upgrade: %v", err)
	}
}

func TestRealmDepUpgradeSchemaSyncIgnoresSymlinks(t *testing.T) {
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.json")
	if err := os.WriteFile(victim, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("entry", func(t *testing.T) {
		dir := setupSchemaSyncTest(t, nil, nil, map[string]string{"entity.v1.schema.json": `{"v":2}`})
		if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, filepath.Join(dir, "_schema", "entity.v1.schema.json")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, victim); got != `{"v":1}` {
			t.Errorf("file behind the symlink was overwritten: %s", got)
		}
	})

	t.Run("directory", func(t *testing.T) {
		dir := setupSchemaSyncTest(t, nil, nil, map[string]string{"victim.json": `{"v":2}`})
		if err := os.Remove(filepath.Join(dir, "_schema")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "_schema")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, victim); got != `{"v":1}` {
			t.Errorf("file behind the _schema symlink was overwritten: %s", got)
		}
	})
}

func TestSyncCoreSchemasErrors(t *testing.T) {
	t.Run("missing realm directory", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		_, err := syncCoreSchemas(context.Background(), missing, depUpgrade{realmID: coreRealmID}, nil)
		if err == nil || !strings.Contains(err.Error(), "opening "+missing) {
			t.Errorf("error = %v, want opening %s", err, missing)
		}
	})

	t.Run("core schema that can't be read", func(t *testing.T) {
		dir := setupSchemaSyncTest(t, nil, map[string]string{"entity.v1.schema.json": `{"v":1}`}, nil)
		coreDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(coreDir, "_schema", "entity.v1.schema.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		fetchUpgradeDep = func(context.Context, string, entity.RealmDep) (string, error) { return coreDir, nil }
		synced, err := syncCoreSchemas(context.Background(), dir, depUpgrade{realmID: coreRealmID}, nil)
		if err == nil || !strings.Contains(err.Error(), "reading core schema entity.v1.schema.json") {
			t.Errorf("error = %v, want reading core schema", err)
		}
		if len(synced) != 0 {
			t.Errorf("synced = %+v, want nothing", synced)
		}
	})

	t.Run("another dependency needs no sync", func(t *testing.T) {
		synced, err := syncCoreSchemas(context.Background(), t.TempDir(), depUpgrade{realmID: "dev.other"}, nil)
		if err != nil || synced != nil {
			t.Errorf("got %+v, %v; want nothing", synced, err)
		}
	})
}

// A declared sub-realm without a readable manifest isn't taken to inherit the
// core pin, so its schemas are left alone.
func TestLocalSchemaFilesSkipsSubRealmWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, filepath.Join(dir, "_schema"), map[string]string{"entity.v1.schema.json": `{}`})
	writeFiles(t, filepath.Join(dir, "auth", "_schema"), map[string]string{"entity.v1.schema.json": `{}`})
	writeFiles(t, filepath.Join(dir, "bad", "_schema"), map[string]string{"entity.v1.schema.json": `{}`})
	writeFiles(t, filepath.Join(dir, "bad"), map[string]string{"realm.json": `{`})
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	got := localSchemaFiles(root, []string{"auth", "bad"})
	if len(got) != 1 || got[0] != filepath.Join("_schema", "entity.v1.schema.json") {
		t.Errorf("files = %v, want only the realm's own schema", got)
	}
}

func TestRestoreSchemasErrors(t *testing.T) {
	if err := restoreSchemas(t.TempDir(), nil); err != nil {
		t.Errorf("nothing to restore: %v", err)
	}
	if err := restoreSchemas(filepath.Join(t.TempDir(), "missing"), []schemaSync{{path: "x.json"}}); err == nil {
		t.Error("missing realm directory: want an error, got none")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "_schema", "x.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := restoreSchemas(dir, []schemaSync{{path: filepath.Join("_schema", "x.json"), original: []byte(`{}`)}})
	if err == nil || !strings.Contains(err.Error(), "restoring "+filepath.Join("_schema", "x.json")) {
		t.Errorf("error = %v, want restoring _schema/x.json", err)
	}
}

func TestSameJSONBytes(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{`{"v":1}`, `{"v":1}`, true},
		{`{"v":1}`, "{\n  \"v\": 1\n}", true},
		{`{"v":1}`, `{"v":2}`, false},
		{`{"v":1}`, `not json`, false},
		{`not json`, `{"v":1}`, false},
	} {
		if got := sameJSONBytes([]byte(tc.a), []byte(tc.b)); got != tc.want {
			t.Errorf("sameJSONBytes(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
