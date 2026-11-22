package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

const (
	// maxSchemaDownload and schemaDownloadTimeout mirror the manager's limit
	// and its default client's timeout.
	maxSchemaDownload     = 1 << 20
	schemaDownloadTimeout = 30 * time.Second
)

var errNotSchema = errors.New("not a JSON schema")

// checkSchemaDoc reports an error when data isn't a usable JSON schema. JSON
// Schema accepts any object, so {} or {"error": "down"} would compile and
// then accept every document: one of the keywords every published schema has
// must be there too.
func checkSchemaDoc(data []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: %w", errNotSchema, err)
	}
	obj, ok := doc.(map[string]any)
	if !ok || !slices.ContainsFunc([]string{"$schema", "$id", "$ref", "type", "properties"}, func(k string) bool {
		_, has := obj[k]
		return has
	}) {
		return errNotSchema
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.SchemeURLLoader{}) // the schema must not reach beyond itself
	if err := c.AddResource("schema.json", doc); err != nil {
		return fmt.Errorf("%w: %w", errNotSchema, err)
	}
	if _, err := c.Compile("schema.json"); err != nil {
		return fmt.Errorf("%w: %w", errNotSchema, err)
	}
	return nil
}

// schemasOnly fails a successful download that isn't a schema. The manager
// caches whatever a 200 returns, over a usable copy too; a failed download
// instead leaves that copy in place and serves it, as when offline.
type schemasOnly struct{ next http.RoundTripper }

func (s schemasOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := s.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("downloading a schema: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	body := resp.Body // resp.Body is replaced below
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, maxSchemaDownload+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", req.URL.Redacted(), err)
	}
	// An oversized body is passed on for the manager to refuse.
	if len(data) <= maxSchemaDownload {
		if err := checkSchemaDoc(data); err != nil {
			return nil, fmt.Errorf("%s returned what can't be used: %w", req.URL.Redacted(), err)
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	return resp, nil
}

// withSchemasOnly wraps the fetcher's client in schemasOnly. It must be the
// last option, as WithClient replaces the client.
func withSchemasOnly() schema.Option {
	return func(c *schema.Config) {
		client := &http.Client{Timeout: schemaDownloadTimeout}
		if c.Client != nil {
			copied := *c.Client
			client = &copied
		}
		next := client.Transport
		if next == nil {
			next = http.DefaultTransport
		}
		client.Transport = schemasOnly{next}
		c.Client = client
	}
}
