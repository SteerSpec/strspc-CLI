package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

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

// A document that isn't YAML has no schema violations: parsing reports it.
func TestConfigSchemaViolationsIgnoresInvalidYAML(t *testing.T) {
	if got := configSchemaViolations(nil, []byte("rules: [\n")); got != nil {
		t.Errorf("got %v, want none", got)
	}
}

// An error without per-location lines is kept whole, on one line.
func TestSchemaViolationsWithoutLocations(t *testing.T) {
	got := schemaViolations(errors.New("schema  failed\n  to compile"))
	if want := []string{"schema failed to compile"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
