package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
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
// pre-commit hooks, where an offline first run must degrade quickly rather
// than wait for the fetcher's default 30-second timeout.
const configSchemaTimeout = 5 * time.Second

// newConfigSchemaFetcher builds the fetcher used to validate config.yaml.
// Package-level so tests can serve the schema from a local server or disable
// the validation by returning nil. Schemas are cached under .strspc/cache/
// because the check never writes outside .strspc/.
var newConfigSchemaFetcher = func(cwd string) *schema.Fetcher {
	return schema.New(
		schema.WithCacheDir(filepath.Join(cwd, ".strspc", "cache", "schemas")),
		schema.WithClient(&http.Client{Timeout: configSchemaTimeout}),
	)
}

// validateConfigSchema checks the raw config.yaml against the published
// config.v1 JSON schema and returns one CHK002 warning per violation. The
// check is advisory: a schema that cannot be fetched or compiled (offline
// run, moved schema) is only mentioned in verbose output, and a document
// that does not match the schema never fails the check on its own.
func (r *checkRun) validateConfigSchema(raw []byte) []result.Diagnostic {
	fetcher := newConfigSchemaFetcher(r.cwd)
	if fetcher == nil {
		return nil
	}

	compiled, err := compileConfigSchema(r, fetcher)
	if err != nil {
		r.verbosef("config schema: validation skipped: %v", err)
		return nil
	}

	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil // parseStrspcConfig already rejected the document
	}
	if err := compiled.Validate(doc); err != nil {
		var diags []result.Diagnostic
		for _, msg := range schemaViolations(err) {
			diags = append(diags, configSchemaWarning(msg))
		}
		return diags
	}
	return nil
}

// compileConfigSchema fetches and compiles the config.v1 schema.
func compileConfigSchema(r *checkRun, fetcher *schema.Fetcher) (*jsonschema.Schema, error) {
	data, err := fetcher.Fetch(r.ctx, configV1SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", configV1SchemaPath, err)
	}
	sch, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", configV1SchemaPath, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("config.schema.json", sch); err != nil {
		return nil, fmt.Errorf("compiling %s: %w", configV1SchemaPath, err)
	}
	return c.Compile("config.schema.json")
}

// configSchemaWarning builds the CHK002 diagnostic for one schema violation.
func configSchemaWarning(msg string) result.Diagnostic {
	return result.Diagnostic{
		Module:   "strspc",
		Code:     "CHK002",
		Severity: result.Warning,
		Path:     ".strspc/config.yaml",
		Message:  "config.yaml does not match the config.v1 schema: " + msg,
	}
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
