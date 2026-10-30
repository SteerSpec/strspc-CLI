package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

const (
	allowSchema  = `{}`
	rejectSchema = `{"not": {}}`
)

// serveSchemas points newSchemaFetcher at a server holding the given paths and
// an empty cache, and records which paths were requested. Anything else is a 404.
func serveSchemas(t *testing.T, files map[string]string) *[]string {
	t.Helper()
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	original := newSchemaFetcher
	newSchemaFetcher = func(opts ...schema.Option) *schema.Fetcher {
		return schema.New(append([]schema.Option{
			schema.WithBaseURL(srv.URL + "/schemas"),
			schema.WithCacheDir(cacheDir),
		}, opts...)...)
	}
	t.Cleanup(func() { newSchemaFetcher = original })
	return &requested
}

// pinnedRealm writes a realm pinning core at version, holding one valid entity.
// A local source keeps RM016 from reaching the network.
func pinnedRealm(t *testing.T, dir, version string) {
	t.Helper()
	setupValidRealm(t, dir)
	if version != "" {
		writeRealmDeps(t, dir, entity.RealmDep{RealmID: schema.CoreRealmID, Version: version, Source: "../core"})
	}
	writeEntityFile(t, dir, "TST.json", validEntityJSON)
}

// Paths of the core 1.10.0 release's schemas and of the latest ones.
const (
	release110Entity = "/rules/v1.10.0/_schema/entity.v1.schema.json"
	release110Realm  = "/rules/v1.10.0/_schema/realm.v1.schema.json"
	latestEntity     = "/schemas/entity/v1.json"
	latestRealm      = "/schemas/realm/v1.json"
)

func TestLintUsesThePinnedCoreSchemas(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "1.10.0")
	serveSchemas(t, map[string]string{release110Entity: rejectSchema, latestEntity: allowSchema})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err == nil {
		t.Fatalf("expected RL002 from the pinned schema, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RL002")
}

func TestLintFileUsesItsRealmsPin(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "1.10.0")
	serveSchemas(t, map[string]string{release110Entity: rejectSchema, latestEntity: allowSchema})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", filepath.Join(dir, "TST.json"))
	if err == nil {
		t.Fatalf("expected RL002 from the pinned schema, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RL002")
}

func TestLintWithoutPinUsesLatest(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "")
	requested := serveSchemas(t, map[string]string{latestEntity: allowSchema})

	if output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, output)
	}
	if got := strings.Join(*requested, " "); got != latestEntity {
		t.Errorf("requested %q, want only %q", got, latestEntity)
	}
}

func TestLintIgnoresARealmsOwnSchemaUnlessCore(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "1.10.0")
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(allowSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	serveSchemas(t, map[string]string{release110Entity: rejectSchema})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	if err == nil {
		t.Fatalf("a realm's own schema decided validation:\n%s", output)
	}
	testutil.AssertContains(t, output, "RL002")

	if output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--core"); err != nil {
		t.Fatalf("--core should use the local schema: %v\n%s", err, output)
	}
}

func TestRealmValidateUsesThePinnedCoreSchemas(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "1.10.0")
	serveSchemas(t, map[string]string{
		release110Realm:  rejectSchema,
		release110Entity: allowSchema,
		latestRealm:      allowSchema,
		latestEntity:     allowSchema,
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir)
	if err == nil {
		t.Fatalf("expected RM002 from the pinned schema, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RM002")
}

func TestRealmValidateRecursiveUsesThePinForEntities(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "1.10.0")
	serveSchemas(t, map[string]string{
		release110Realm:  allowSchema,
		release110Entity: rejectSchema,
		latestRealm:      allowSchema,
		latestEntity:     allowSchema,
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	if err == nil {
		t.Fatalf("expected RL002 from the pinned schema, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RL002")
}

func TestRealmDepUpgradeValidatesAgainstTheNewPin(t *testing.T) {
	const (
		pinned = "/rules/v1.8.0/_schema/realm.v1.schema.json"
		next   = "/rules/v1.11.0/_schema/realm.v1.schema.json"
	)
	for name, tt := range map[string]struct {
		nextSchema  string
		wantErr     bool
		wantVersion string
	}{
		"accepted": {nextSchema: allowSchema, wantVersion: "1.11.0"},
		"rejected": {nextSchema: rejectSchema, wantErr: true, wantVersion: "1.8.0"},
	} {
		t.Run(name, func(t *testing.T) {
			originalTag := latestReleaseTag
			t.Cleanup(func() { latestReleaseTag = originalTag })
			latestReleaseTag = func(context.Context, remote.Ref, string) (string, error) { return "v1.11.0", nil }

			dir := t.TempDir()
			setupValidRealm(t, dir)
			writeRealmDeps(t, dir, entity.RealmDep{
				RealmID: schema.CoreRealmID,
				Version: "1.8.0",
				Source:  "github://SteerSpec/strspc-rules@v1.8.0/rules/core",
			})
			serveSchemas(t, map[string]string{pinned: allowSchema, next: tt.nextSchema, latestEntity: allowSchema})

			// The real validator would fetch the dependency, so this one keeps
			// only what matters here: reading the pin when it's built.
			original := newRealmValidator
			t.Cleanup(func() { newRealmValidator = original })
			newRealmValidator = func(dir string, strict, _, core bool) *realmValidatorSet {
				fetcher := newSchemaFetcher(realmSchemaOptions(dir, core)...)
				return &realmValidatorSet{
					realmLinter: realmlint.New(realmlint.WithSchemaFetcher(fetcher), realmlint.WithStrict(strict)),
					ruleLinter:  rulelint.New(rulelint.WithSchemaFetcher(fetcher), rulelint.WithStrict(strict)),
				}
			}

			output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "upgrade", "--dir", dir)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected the new pin's schema to reject the realm, got:\n%s", output)
				}
				testutil.AssertContains(t, output, "RM002")
			} else if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, output)
			}
			if got := readRealmDeps(t, dir)[0].Version; got != tt.wantVersion {
				t.Errorf("version = %s, want %s", got, tt.wantVersion)
			}
		})
	}
}

func TestRealmValidateCoreUsesItsOwnSchemas(t *testing.T) {
	dir := t.TempDir()
	pinnedRealm(t, dir, "")
	for _, name := range []string{"realm.v1.schema.json", "entity.v1.schema.json", "bootstrap.schema.json"} {
		if err := os.WriteFile(filepath.Join(dir, "_schema", name), []byte(rejectSchema), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	serveSchemas(t, map[string]string{latestRealm: allowSchema, latestEntity: allowSchema})

	if output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive"); err != nil {
		t.Fatalf("without --core a realm's schema decided validation: %v\n%s", err, output)
	}
	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive", "--core")
	if err == nil {
		t.Fatalf("--core should use the local schema, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RM002")
}

func TestRealmValidateSubRealmUsesItsOwnPin(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	sub := filepath.Join(dir, "auth")
	writeRealmDeps(t, sub, entity.RealmDep{RealmID: schema.CoreRealmID, Version: "1.10.0", Source: "../../core"})
	serveSchemas(t, map[string]string{
		release110Realm: rejectSchema,
		latestRealm:     allowSchema,
		latestEntity:    allowSchema,
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	if err == nil {
		t.Fatalf("expected RM002 from the sub-realm's pin, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RM002")
	testutil.AssertContains(t, output, filepath.Join("auth", "realm.json"))
}

func TestRealmValidateRecursiveLintsSubRealmEntitiesAgainstItsPin(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	sub := filepath.Join(dir, "auth")
	writeRealmDeps(t, sub, entity.RealmDep{RealmID: schema.CoreRealmID, Version: "1.10.0", Source: "../../core"})
	writeEntityFile(t, sub, "TST.json", validEntityJSON)
	serveSchemas(t, map[string]string{
		release110Realm:  allowSchema,
		release110Entity: rejectSchema,
		latestRealm:      allowSchema,
		latestEntity:     allowSchema,
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive")
	if err == nil {
		t.Fatalf("expected RL002 from the sub-realm's pin, got:\n%s", output)
	}
	testutil.AssertContains(t, output, "RL002")
}

func TestRealmValidateRecursiveReportsSubRealmEntitiesOnce(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	sub := filepath.Join(dir, "auth")
	writeRealmDeps(t, sub, entity.RealmDep{RealmID: schema.CoreRealmID, Version: "1.10.0", Source: "../../core"})
	writeEntityFile(t, sub, "TST.json", invalidEntityJSON)
	serveSchemas(t, map[string]string{
		release110Realm:  allowSchema,
		release110Entity: allowSchema,
		latestRealm:      allowSchema,
		latestEntity:     allowSchema,
	})

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive", "--json")
	if err == nil {
		t.Fatalf("expected an error for the invalid entity, got:\n%s", output)
	}
	if got := strings.Count(output, `"Code": "RL003"`); got != 1 {
		t.Errorf("RL003 reported %d times:\n%s", got, output)
	}
}
