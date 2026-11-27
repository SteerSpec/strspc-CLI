package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

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
// given after the cache, such as a test's WithCacheDir, take precedence. It
// caches only schemas (schemasOnly), whatever client the options give it.
func newCachedSchemaFetcher(opts ...schema.Option) *schema.Fetcher {
	all := append(schemaCacheOptionsFrom(os.Getenv, os.UserCacheDir), opts...)
	return schema.New(append(all, withSchemasOnly())...)
}

// releaseCacheKey returns the cache key of the schema p of the core release
// tag, below versions/ as the manager's fetcher keeps it, or "" when tag isn't
// a release version (a prerelease is never cached).
func releaseCacheKey(tag, p string) string {
	v := strings.TrimPrefix(tag, "v")
	if !schemaVersion.MatchString(v) {
		return ""
	}
	return "versions/" + v + "/" + p
}

// readCachedSchema returns the cached schema at key when it is a regular file
// below the cache, no larger than a download may be, that holds a usable
// schema.
func readCachedSchema(cacheDir, key string) ([]byte, bool) {
	if refuseSymlinkedParents(cacheDir, key) != nil {
		return nil, false
	}
	file := filepath.Join(cacheDir, filepath.FromSlash(key))
	fi, err := os.Lstat(file)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxSchemaDownload {
		return nil, false
	}
	data, err := os.ReadFile(file)
	if err != nil || checkSchemaDoc(data) != nil {
		return nil, false
	}
	return data, true
}

// writeCachedSchema stores data, a downloaded schema, in the cache at key,
// through a temporary file renamed into place, and marks the cache. Data that
// isn't a usable schema, or that readCachedSchema would refuse as too large,
// is not stored.
func writeCachedSchema(cacheDir, key string, data []byte) error {
	if len(data) > maxSchemaDownload {
		return fmt.Errorf("schema larger than %d bytes", maxSchemaDownload)
	}
	if err := checkSchemaDoc(data); err != nil {
		return err
	}
	if err := prepareSchemaCache(cacheDir); err != nil {
		return err
	}
	if err := refuseSymlinkedParents(cacheDir, key); err != nil {
		return err
	}
	dir := filepath.Dir(filepath.Join(cacheDir, filepath.FromSlash(key)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".schema-cache-*")
	if err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(cacheDir, filepath.FromSlash(key)))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("updating the cache: %w", err)
	}
	return nil
}

// newestCachedRelease returns the tag of the newest core release whose
// schemas at paths are all in the cache, or "" when there is none.
func newestCachedRelease(cacheDir string, paths []string) string {
	entries, err := os.ReadDir(filepath.Join(cacheDir, "versions"))
	if err != nil {
		return ""
	}
	var versions []*semver.Version
	for _, e := range entries {
		v, err := semver.StrictNewVersion(e.Name())
		if err == nil && schemaVersion.MatchString(e.Name()) {
			versions = append(versions, v)
		}
	}
	sort.Sort(sort.Reverse(semver.Collection(versions)))
	for _, v := range versions {
		tag := "v" + v.Original()
		if !slices.ContainsFunc(paths, func(p string) bool {
			_, ok := readCachedSchema(cacheDir, releaseCacheKey(tag, p))
			return !ok
		}) {
			return tag
		}
	}
	return ""
}
