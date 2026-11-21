package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

// envSchemaCache overrides the schema cache directory, for CI that caches a
// directory or a sandbox where the user cache can't be written.
const envSchemaCache = "STRSPC_SCHEMA_CACHE"

// Every command reads and writes JSON schemas in one cache:
// $STRSPC_SCHEMA_CACHE, or <user cache>/strspc/schemas. A core release's
// schemas never change and are kept by version, so projects share them
// safely; `strspc schema` fetches, updates and clears the cache.

// schemaCacheDirFrom returns the schema cache directory, reading the
// environment through getenv and locating the user cache with userCacheDir.
func schemaCacheDirFrom(getenv func(string) string, userCacheDir func() (string, error)) (string, error) {
	if d := strings.TrimSpace(getenv(envSchemaCache)); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return "", fmt.Errorf("%s %q: %w", envSchemaCache, d, err)
		}
		return abs, nil
	}
	base, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating the user cache (set %s): %w", envSchemaCache, err)
	}
	return filepath.Join(base, "strspc", "schemas"), nil
}

// schemaCacheDir returns the schema cache directory.
func schemaCacheDir() (string, error) {
	return schemaCacheDirFrom(os.Getenv, os.UserCacheDir)
}

// schemaCacheOptionsFrom selects the schema cache for a fetcher. When the
// directory can't be located it selects none, and the manager falls back to
// its own default.
func schemaCacheOptionsFrom(getenv func(string) string, userCacheDir func() (string, error)) []schema.Option {
	dir, err := schemaCacheDirFrom(getenv, userCacheDir)
	if err != nil {
		return nil
	}
	return []schema.Option{schema.WithCacheDir(dir)}
}

// newCachedSchemaFetcher builds a fetcher on the shared schema cache. Options
// given after the cache, such as a test's WithCacheDir, take precedence.
func newCachedSchemaFetcher(opts ...schema.Option) *schema.Fetcher {
	return schema.New(append(schemaCacheOptionsFrom(os.Getenv, os.UserCacheDir), opts...)...)
}
