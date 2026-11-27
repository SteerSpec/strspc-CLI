package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// configV1SchemaPath is the path of the config.v1 schema below schema.BaseURL.
// It is the machine-readable expression of the SPCFG core rules (SPEC §9).
const configV1SchemaPath = "config/v1.json"

// configSchemaTimeout bounds the schema fetch: `strspc check` runs in
// pre-commit hooks, where an offline run must fall back to the cache quickly
// rather than wait for the fetcher's default 30-second timeout.
const configSchemaTimeout = 5 * time.Second

// newConfigSchemaFetcher builds the fetcher of the latest config.v1 schema for
// the project in cwd, on the shared schema cache (schemaCacheDir), which
// `strspc schema fetch` fills. Options given take precedence, such as status's
// client that makes no network call. Package-level so tests can serve the
// schema locally.
var newConfigSchemaFetcher = func(_ string, opts ...schema.Option) *schema.Fetcher {
	return newCachedSchemaFetcher(append([]schema.Option{schema.WithClient(&http.Client{Timeout: configSchemaTimeout})}, opts...)...)
}

// .strspc/config.yaml is validated against the config.v1 schema before its
// content is used (SEC-004): validateConfig takes the bytes read and the
// caller parses those same bytes only once they pass. Two copies of the
// schema can be at hand, and the document is valid when either accepts it:
//
//   - the latest config.v1, fetched or, offline, the cached copy however old;
//   - the newest core release's config.v1 in the schema cache.
//
// The newer schema wins either way: offline, a cached latest copy older than a
// release cached since must not refuse a key that release added. When neither
// is available, nothing is used unvalidated: the command fails and names
// `strspc schema fetch`.

// errConfigSchemaUnavailable marks a config.v1 schema that is neither cached
// nor reachable.
var errConfigSchemaUnavailable = errors.New("the config.v1 schema is not cached and can't be fetched; " +
	"run `strspc schema fetch` while online")

// configSchemaError reports a config.yaml the config.v1 schema refuses. The
// message lists at most maxViolationsShown violations; check reports each one
// as a CHK002 diagnostic.
type configSchemaError struct {
	label      string   // which schema refused it
	violations []string // sanitized
}

func (e *configSchemaError) Error() string {
	shown := e.violations
	if len(shown) > maxViolationsShown {
		shown = shown[:maxViolationsShown]
	}
	more := ""
	if n := len(e.violations) - len(shown); n > 0 {
		more = fmt.Sprintf(" (and %d more; strspc check lists them)", n)
	}
	return fmt.Sprintf(".strspc/config.yaml does not match %s: %s%s", e.label, strings.Join(shown, "; "), more)
}

// diagnostics returns one CHK002 error per violation.
func (e *configSchemaError) diagnostics() []result.Diagnostic {
	diags := make([]result.Diagnostic, 0, len(e.violations))
	for _, msg := range e.violations {
		diags = append(diags, result.Diagnostic{
			Module:   "strspc",
			Code:     "CHK002",
			Severity: result.Error,
			Path:     ".strspc/config.yaml",
			Message:  "config.yaml does not match " + e.label + ": " + msg,
		})
	}
	return diags
}

// configSchemaDiagnostics returns the CHK002 diagnostics err carries, if any.
func configSchemaDiagnostics(err error) []result.Diagnostic {
	var ce *configSchemaError
	if errors.As(err, &ce) {
		return ce.diagnostics()
	}
	return nil
}

// loadStrspcConfig reads .strspc/config.yaml under cwd, validates it against
// the config.v1 schema and parses it. Every command that acts on the config
// reads it through here, checkRun.loadConfig included. opts go to the fetcher
// of the latest schema (newConfigSchemaFetcher).
func loadStrspcConfig(ctx context.Context, cwd string, opts ...schema.Option) (*strspcConfig, error) {
	raw, err := readStrspcConfig(cwd)
	if err != nil {
		return nil, err
	}
	cacheDir, _ := schemaCacheDir() // unlocatable: no release copy, the latest schema only
	if err := validateConfig(ctx, newConfigSchemaFetcher(cwd, opts...), cacheDir, raw); err != nil {
		return nil, err
	}
	return parseStrspcConfig(raw)
}

// validateConfig validates raw, the content of .strspc/config.yaml, against
// the latest config.v1 schema from latest (nil: none) and against the newest
// core release's copy in cacheDir ("": none). It returns nil when either
// accepts it, a *configSchemaError listing the violations of the latest
// schema (or of the release copy when only that is available) when neither
// does, and an error wrapping errConfigSchemaUnavailable when neither is
// available.
func validateConfig(ctx context.Context, latest *schema.Fetcher, cacheDir string, raw []byte) error {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing config.yaml: %w", err)
	}
	doc, err := jsonValue(doc)
	if err != nil {
		return fmt.Errorf("parsing config.yaml: %w", err)
	}

	type candidate struct {
		label    string
		compiled *jsonschema.Schema
	}
	var candidates []candidate
	var latestErr error
	if latest == nil {
		latestErr = errors.New("no schema fetcher")
	} else if c, err := compileConfigSchema(ctx, latest); err != nil {
		latestErr = err
	} else {
		candidates = append(candidates, candidate{"the latest config.v1 schema", c})
	}
	if cacheDir != "" {
		if tag := newestCachedRelease(cacheDir, []string{configV1SchemaPath}); tag != "" {
			// newestCachedRelease has read it already: a usable schema.
			data, _ := readCachedSchema(cacheDir, releaseCacheKey(tag, configV1SchemaPath))
			if c, err := compileSchemaBytes(data); err == nil {
				label := "the config.v1 schema of " + coreRealmID + " " + strings.TrimPrefix(tag, "v")
				candidates = append(candidates, candidate{label, c})
			}
		}
	}
	if len(candidates) == 0 {
		return fmt.Errorf("can't validate .strspc/config.yaml: %w: %w", errConfigSchemaUnavailable, latestErr)
	}

	var refused *configSchemaError
	for _, c := range candidates {
		err := c.compiled.Validate(doc)
		if err == nil {
			return nil
		}
		if refused == nil {
			refused = &configSchemaError{label: c.label}
			for _, msg := range schemaViolations(err) {
				refused.violations = append(refused.violations, sanitizeLine(msg))
			}
		}
	}
	return refused
}

// jsonValue converts a decoded YAML document to the values a JSON document
// decodes to, so the schema sees what JSON would give it. A mapping key that
// isn't a string, or a scalar with no JSON equivalent such as a timestamp,
// is an error: the schema can't judge it.
func jsonValue(v any) (any, error) {
	switch x := v.(type) {
	case nil, bool, string, int, int64, uint64, float64:
		return x, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			j, err := jsonValue(e)
			if err != nil {
				return nil, err
			}
			out[k] = j
		}
		return out, nil
	case map[any]any:
		return nil, errors.New("a mapping key is not a string")
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			j, err := jsonValue(e)
			if err != nil {
				return nil, err
			}
			out[i] = j
		}
		return out, nil
	default:
		return nil, fmt.Errorf("a value of type %T has no JSON equivalent; quote it", x)
	}
}

// compileConfigSchema fetches and compiles the config.v1 schema.
func compileConfigSchema(ctx context.Context, fetcher *schema.Fetcher) (*jsonschema.Schema, error) {
	data, err := fetcher.Fetch(ctx, configV1SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", configV1SchemaPath, err)
	}
	return compileSchemaBytes(data)
}

// compileSchemaBytes compiles data, a config.v1 schema, without letting it
// load anything beyond itself.
func compileSchemaBytes(data []byte) (*jsonschema.Schema, error) {
	sch, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", configV1SchemaPath, err)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.SchemeURLLoader{}) // the schema must not reach beyond itself
	if err := c.AddResource("config.schema.json", sch); err != nil {
		return nil, fmt.Errorf("compiling %s: %w", configV1SchemaPath, err)
	}
	return c.Compile("config.schema.json")
}

// schemaViolations turns a jsonschema validation error into one message per
// reported location. The library renders one "- at '/path': cause" line per
// violation under a header naming the schema; the header carries no
// information for the user and is dropped.
func schemaViolations(err error) []string {
	var out []string
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			out = append(out, rest)
		}
	}
	if len(out) == 0 {
		out = append(out, strings.Join(strings.Fields(err.Error()), " "))
	}
	return out
}
