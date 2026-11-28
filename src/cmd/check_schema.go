package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
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
// schema can be at hand: the latest config.v1, fetched or, offline, the
// cached copy however old; and the newest core release's config.v1 in the
// schema cache. Exactly one of them decides, and the document must match it:
//
//   - a fresh latest copy (downloaded now, or cached within its TTL) decides;
//   - with no latest copy, the release copy decides;
//   - a stale latest copy (served offline from an expired cache) decides,
//     unless the release copy is provably newer: its release is above the
//     x-steerspec-core-version the stale copy carries.
//
// When neither is available, nothing is used unvalidated: the command fails
// and names `strspc schema fetch`.

// errConfigSchemaUnavailable marks a config.v1 schema that is neither cached
// nor reachable.
var errConfigSchemaUnavailable = errors.New("the config.v1 schema is not cached and can't be fetched; " +
	"run `strspc schema fetch` while online")

// errConfigSchemaUnusable marks a latest config.v1 schema that was fetched or
// read from the cache but can't be compiled. It is final: falling back to a
// release copy would let a broken latest copy hand the decision to an older
// schema.
var errConfigSchemaUnusable = errors.New("the latest config.v1 schema can't be used; " +
	"run `strspc schema clear`, then `strspc schema fetch` while online")

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
// core release's copy in cacheDir ("": none), the cache latest also uses.
// One of them decides, as described above. It returns nil when that schema
// accepts raw, a *configSchemaError listing its violations when it doesn't,
// an error wrapping errConfigSchemaUnavailable when neither is available, and
// one wrapping errConfigSchemaUnusable when the latest copy is there but
// doesn't compile.
func validateConfig(ctx context.Context, latest *schema.Fetcher, cacheDir string, raw []byte) error {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parsing config.yaml: %w", err)
	}
	doc, err := jsonValue(doc)
	if err != nil {
		return fmt.Errorf("parsing config.yaml: %w", err)
	}

	var latestC *jsonschema.Schema
	var latestData []byte
	latestErr := errors.New("no schema fetcher")
	if latest != nil {
		latestData, latestErr = latest.Fetch(ctx, configV1SchemaPath)
		if errors.Is(latestErr, errNotSchema) {
			// Downloaded, but not a schema: there, and unusable.
			return fmt.Errorf("can't validate .strspc/config.yaml: %w: %w", errConfigSchemaUnusable, latestErr)
		}
		if latestErr != nil {
			latestErr = fmt.Errorf("fetching %s: %w", configV1SchemaPath, latestErr)
		} else if latestC, latestErr = compileSchemaBytes(latestData); latestErr != nil {
			return fmt.Errorf("can't validate .strspc/config.yaml: %w: %w", errConfigSchemaUnusable, latestErr)
		}
	}
	var releaseC *jsonschema.Schema
	var release *semver.Version
	if cacheDir != "" {
		if tag := newestCachedRelease(cacheDir, []string{configV1SchemaPath}); tag != "" {
			// newestCachedRelease has read it already: a usable schema.
			data, _ := readCachedSchema(cacheDir, releaseCacheKey(tag, configV1SchemaPath))
			v, verr := semver.StrictNewVersion(strings.TrimPrefix(tag, "v"))
			if c, err := compileSchemaBytes(data); err == nil && verr == nil {
				releaseC, release = c, v
			}
		}
	}

	var label string
	var decider *jsonschema.Schema
	switch {
	case latestC != nil && (release == nil || !latestIsStale(cacheDir, latestData) || !releaseIsNewer(release, latestData)):
		label, decider = "the latest config.v1 schema", latestC
	case releaseC != nil:
		label, decider = "the config.v1 schema of "+coreRealmID+" "+release.Original(), releaseC
	default:
		return fmt.Errorf("can't validate .strspc/config.yaml: %w: %w", errConfigSchemaUnavailable, latestErr)
	}
	verr := decider.Validate(doc)
	if verr == nil {
		return nil
	}
	refused := &configSchemaError{label: label}
	for _, msg := range schemaViolations(verr) {
		refused.violations = append(refused.violations, sanitizeLine(msg))
	}
	return refused
}

// latestIsStale reports whether data, the latest config.v1 schema the fetcher
// returned, is the cached copy in cacheDir served after its TTL expired: the
// manager serves that copy when the refresh fails. A copy downloaded now is
// written to the cache, so the file is then fresh; one the cache doesn't hold
// (a local schema, or a write that failed) came from somewhere else than the
// expired copy and is fresh too.
func latestIsStale(cacheDir string, data []byte) bool {
	if cacheDir == "" {
		return false
	}
	cached, ok := readCachedSchema(cacheDir, configV1SchemaPath)
	if !ok || !bytes.Equal(cached, data) {
		return false
	}
	fi, err := os.Lstat(filepath.Join(cacheDir, filepath.FromSlash(configV1SchemaPath)))
	return err == nil && time.Since(fi.ModTime()) >= schemaLatestTTL
}

// releaseIsNewer reports whether release is provably newer than latest, a
// config.v1 schema: above the core release latest names in its
// x-steerspec-core-version. A schema that names none can't be compared, so
// the release isn't newer.
func releaseIsNewer(release *semver.Version, latest []byte) bool {
	var meta struct {
		CoreVersion string `json:"x-steerspec-core-version"`
	}
	if json.Unmarshal(latest, &meta) != nil || !schemaVersion.MatchString(meta.CoreVersion) {
		return false
	}
	named, err := semver.StrictNewVersion(meta.CoreVersion) // downloaded: may not parse, such as a component past uint64
	if err != nil {
		return false
	}
	return release.GreaterThan(named)
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
