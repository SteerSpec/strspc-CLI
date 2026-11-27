package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// testReleaseVersion is a core release for the schema cache fixtures, newer
// than any real one so no other cached release outranks it.
const testReleaseVersion = "1.99.0"

// testConfigSchema returns testdata's config.v1 schema. With old set it
// returns a copy that predates the effort values xhigh and max, as a stale
// cached latest copy would.
func testConfigSchema(t *testing.T, old bool) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "config.v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may have CRLF line endings.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if old {
		s := strings.Replace(string(data), `"high",
            "xhigh",
            "max"`, `"high"`, 1)
		if s == string(data) {
			t.Fatal("testdata/config.v1.schema.json no longer lists xhigh and max")
		}
		data = []byte(s)
	}
	return data
}

// writeSchemaCacheFile writes data at key in the schema cache dir, dated
// age ago.
func writeSchemaCacheFile(t *testing.T, dir, key string, data []byte, age time.Duration) {
	t.Helper()
	file := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
}

// offlineConfigFetcher reads the latest config.v1 schema from the schema
// cache dir only, as an offline run does.
func offlineConfigFetcher(dir string) *schema.Fetcher {
	return schema.New(schema.WithCacheDir(dir), schema.WithClient(offlineHTTPClient()), withSchemasOnly())
}

const validTestConfig = "rules:\n  - source: ./rules/\n    scope: local\nevaluator:\n  provider: null\n  effort: xhigh\n"

func TestValidateConfigLatestAccepts(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, false), 0)
	if err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(validTestConfig)); err != nil {
		t.Fatalf("got %v, want the config accepted", err)
	}
}

// Offline, a stale latest copy older than a release cached since must not
// refuse what that release accepts: the newer schema wins.
func TestValidateConfigNewerReleaseWins(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, true), 48*time.Hour)
	if err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(validTestConfig)); err == nil {
		t.Fatal("the stale latest copy alone must refuse effort: xhigh")
	}
	writeSchemaCacheFile(t, dir, releaseCacheKey("v"+testReleaseVersion, configV1SchemaPath), testConfigSchema(t, false), 0)
	if err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(validTestConfig)); err != nil {
		t.Fatalf("got %v, want the config accepted by the newer release's schema", err)
	}
}

// The release copy used is the newest release cached, by semver.
func TestValidateConfigNewestReleaseOnly(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, releaseCacheKey("v1.9.0", configV1SchemaPath), testConfigSchema(t, false), 0)
	writeSchemaCacheFile(t, dir, releaseCacheKey("v1.10.0", configV1SchemaPath), testConfigSchema(t, true), 0)
	err := validateConfig(context.Background(), nil, dir, []byte(validTestConfig))
	var ce *configSchemaError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want a schema mismatch from 1.10.0", err)
	}
	testutil.AssertContains(t, err.Error(), "the config.v1 schema of "+coreRealmID+" 1.10.0")
	testutil.AssertContains(t, err.Error(), "/evaluator/effort")
}

// When both refuse, the violations come from the latest schema.
func TestValidateConfigBothRefuseReportsLatest(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, false), 0)
	writeSchemaCacheFile(t, dir, releaseCacheKey("v"+testReleaseVersion, configV1SchemaPath), testConfigSchema(t, false), 0)
	err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(validTestConfig+"unknown_key: 1\n"))
	var ce *configSchemaError
	if !errors.As(err, &ce) {
		t.Fatalf("got %v, want a schema mismatch", err)
	}
	testutil.AssertContains(t, err.Error(), ".strspc/config.yaml does not match the latest config.v1 schema")
	testutil.AssertContains(t, err.Error(), "unknown_key")
}

// A cold cache offline fails closed and names the remedy.
func TestValidateConfigColdCacheFailsClosed(t *testing.T) {
	dir := t.TempDir()
	err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(validTestConfig))
	if !errors.Is(err, errConfigSchemaUnavailable) {
		t.Fatalf("got %v, want errConfigSchemaUnavailable", err)
	}
	testutil.AssertContains(t, err.Error(), "strspc schema fetch")
	err = validateConfig(context.Background(), nil, "", []byte(validTestConfig))
	if !errors.Is(err, errConfigSchemaUnavailable) {
		t.Fatalf("no fetcher, no cache: got %v, want errConfigSchemaUnavailable", err)
	}
}

// An unusable release copy is no schema at all.
func TestValidateConfigIgnoresUnusableRelease(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, releaseCacheKey("v"+testReleaseVersion, configV1SchemaPath), []byte(`{"type": 7}`), 0)
	err := validateConfig(context.Background(), nil, dir, []byte(validTestConfig))
	if !errors.Is(err, errConfigSchemaUnavailable) {
		t.Fatalf("got %v, want errConfigSchemaUnavailable", err)
	}
}

// The gate parses what the config parser parses: a document it can't judge
// is an error, never a pass.
func TestValidateConfigRejectsWhatItCantJudge(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, false), 0)
	for name, doc := range map[string]string{
		"not YAML":       "rules: [\n",
		"non-string key": validTestConfig + "1: one\n",
		"timestamp":      "rules:\n  - source: ./rules/\n    scope: local\ncache:\n  ttl: 2001-12-14\n",
		"duplicate key":  validTestConfig + "rules: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(doc))
			if err == nil {
				t.Fatal("got nil, want an error")
			}
		})
	}
}

// An empty config.yaml is the YAML null, which isn't the object config.v1
// requires.
func TestValidateConfigEmptyIsInvalid(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, false), 0)
	for _, doc := range []string{"", "# only a comment\n"} {
		err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(doc))
		var ce *configSchemaError
		if !errors.As(err, &ce) {
			t.Errorf("%q: got %v, want a schema mismatch", doc, err)
		}
	}
}

func TestConfigSchemaErrorListsAtMostThree(t *testing.T) {
	e := &configSchemaError{label: "the latest config.v1 schema", violations: []string{"a", "b", "c", "d", "e"}}
	testutil.AssertContains(t, e.Error(), "a; b; c (and 2 more; strspc check lists them)")
	diags := e.diagnostics()
	if len(diags) != 5 || diags[0].Code != "CHK002" || diags[0].Severity != result.Error {
		t.Errorf("diagnostics = %+v, want five CHK002 errors", diags)
	}
	if configSchemaDiagnostics(errors.New("other")) != nil {
		t.Error("an unrelated error carries no diagnostics")
	}
}

// Violations quote the document, so control characters are removed.
func TestValidateConfigSanitizesViolations(t *testing.T) {
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, false), 0)
	err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(validTestConfig+"\"bad\\u001b[31mkey\": 1\n"))
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) {
		t.Fatalf("got %q, want a mismatch without the escape character", err)
	}
}

func TestJSONValueScalars(t *testing.T) {
	for _, v := range []any{nil, true, "s", 1, int64(2), uint64(3), 1.5} {
		if got, err := jsonValue(v); err != nil || got != v {
			t.Errorf("jsonValue(%v) = %v, %v", v, got, err)
		}
	}
	if _, err := jsonValue([]any{map[string]any{"k": time.Time{}}}); err == nil {
		t.Error("a nested timestamp must be an error")
	}
}

func TestCompileConfigSchemaRejectsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	t.Cleanup(srv.Close)
	fetcher := schema.New(schema.WithBaseURL(srv.URL), schema.WithCacheDir(t.TempDir()))
	_, err := compileConfigSchema(context.Background(), fetcher)
	if err == nil {
		t.Fatal("expected an error for a schema that isn't JSON")
	}
	testutil.AssertContains(t, err.Error(), "parsing "+configV1SchemaPath)
}

// An error without per-location lines is kept whole, on one line.
func TestSchemaViolationsWithoutLocations(t *testing.T) {
	got := schemaViolations(errors.New("schema  failed\n  to compile"))
	if want := []string{"schema failed to compile"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The example config of docs/configuration.md matches config.v1.
func TestConfigurationDocExampleIsValid(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may have CRLF line endings.
	text := strings.ReplaceAll(string(doc), "\r\n", "\n")
	_, rest, ok := strings.Cut(text, "```yaml\n")
	example, _, ok2 := strings.Cut(rest, "```")
	if !ok || !ok2 || !strings.Contains(example, "rules:") {
		t.Fatal("no YAML example in docs/configuration.md")
	}
	dir := t.TempDir()
	writeSchemaCacheFile(t, dir, configV1SchemaPath, testConfigSchema(t, false), 0)
	if err := validateConfig(context.Background(), offlineConfigFetcher(dir), dir, []byte(example)); err != nil {
		t.Errorf("the example doesn't match config.v1: %v", err)
	}
}
