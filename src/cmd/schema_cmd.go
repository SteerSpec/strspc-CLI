package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// latestSchemaPaths are the schemas used without a core pin: the config
// schema every command validates config.yaml against, and the entity and
// realm schemas of a realm that pins no core version.
var latestSchemaPaths = []string{configV1SchemaPath, schema.EntityV1Path, schema.RealmV1Path}

// releaseSchemaPaths are the schemas `schema fetch` caches for a core
// release: the entity and realm schemas lint and realm validate read for a
// realm pinned to it, and the bootstrap schema realm init also vendors.
var releaseSchemaPaths = []string{schema.EntityV1Path, schema.RealmV1Path, schema.BootstrapPath}

// releaseOptionalPaths are the schemas `schema fetch` also caches for a core
// release when it publishes them: its config schema, which decides when no
// latest copy can be had or only an older, expired one (validateConfig). A release
// without one isn't a failure.
var releaseOptionalPaths = []string{configV1SchemaPath}

// schemaCacheEntries are the top-level names the schema fetcher writes in the
// cache: one directory per latest schema family, and versions/ for releases.
// `schema clear` deletes only the schema files below them (schemaCacheFile).
var schemaCacheEntries = []string{"config", "entity", "realm", "versions"}

// schemaVersion is a core release version, as a realm pins it.
var schemaVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// schemaLatestTTL mirrors the manager's refresh interval for a cached latest
// schema, for the cache listing.
const schemaLatestTTL = time.Hour

// schemaTarget is a set of schemas fetched with the same options.
type schemaTarget struct {
	label    string // "latest" or "dev.steerspec.core X.Y.Z"
	version  string // the core release, or "" for the latest schemas
	opts     []schema.Option
	paths    []string
	optional []string // fetched like paths, but a failure only skips them
}

func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Show, fetch, update or clear the cache of JSON schemas",
		Long: "strspc validates rule files, realm manifests and .strspc/config.yaml against JSON schemas\n" +
			"published on steerspec.dev. Every command keeps them in one cache, shared by all projects:\n" +
			"$" + envSchemaCache + ", or strspc/schemas under the user cache directory. A cached schema\n" +
			"is used offline. A core release's schemas never change; a latest schema is refreshed after\n" +
			"an hour when the network can be reached.\n\n" +
			"Without a subcommand, schema lists the cache. `schema fetch` fills it for a project, so\n" +
			"that validation works offline afterwards. `realm init` also keeps the schemas it vendors\n" +
			"there: offline, it reads a pinned release's schemas from the cache, and without a pin it\n" +
			"uses the newest release cached in full, with a warning.\n\n" +
			"Examples:\n" +
			"  strspc schema\n" +
			"  " + envSchemaCache + "=/path/to/cache strspc schema",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listSchemaCache(cmd.OutOrStdout(), time.Now())
		},
	}
	cmd.AddCommand(newSchemaFetchCmd(), newSchemaUpdateCmd(), newSchemaClearCmd())
	for _, c := range append([]*cobra.Command{cmd}, cmd.Commands()...) {
		c.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return withExitCode(exitConfig, err) })
		validate := c.Args
		c.Args = func(cc *cobra.Command, args []string) error {
			if err := validate(cc, args); err != nil {
				return withExitCode(exitConfig, err)
			}
			return nil
		}
	}
	return cmd
}

func newSchemaFetchCmd() *cobra.Command {
	var (
		dirs     []string
		versions []string
	)
	cmd := &cobra.Command{
		Use:   "fetch [path]",
		Short: "Download the schemas a project needs into the cache",
		Long: "Fetch the latest config, entity and realm schemas, and the entity, realm, bootstrap and\n" +
			"(when the release has one) config schemas of the core release that each local realm pins:\n" +
			"the local sources of .strspc/config.yaml in path (default: the current directory) that hold\n" +
			"a realm.json, their sub-realms, and each --dir. The config is not validated first, so that\n" +
			"fetch can fill a cold cache. --core-version adds a release, so that `realm init --core-version`\n" +
			"works offline too. A schema already cached is not downloaded again. Exits 1\n" +
			"when a schema can't be fetched and 2 when the configuration or a realm can't be read.\n\n" +
			"Examples:\n" +
			"  strspc schema fetch\n" +
			"  strspc schema fetch --dir ./rules --core-version 1.25.0",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := schemaFetchTargets(argOrDot(args), dirs, versions)
			if err != nil {
				return withExitCode(exitConfig, err)
			}
			return fetchSchemaTargets(cmd, targets, false)
		},
	}
	cmd.Flags().StringArrayVar(&dirs, "dir", nil, "also fetch the schemas of the realm in this directory and its sub-realms (repeatable)")
	cmd.Flags().StringArrayVar(&versions, "core-version", nil, "also fetch the schemas of this core release, such as 1.25.0 (repeatable)")
	return cmd
}

func newSchemaUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download the latest schemas again",
		Long: "Download the latest config, entity and realm schemas again, even when the cached copy is\n" +
			"less than an hour old. A copy is replaced only once its download succeeds, so an update\n" +
			"that fails offline keeps the cache usable. Release schemas never change and are left as\n" +
			"they are. Exits 1 when a schema can't be fetched.\n\n" +
			"Examples:\n" +
			"  strspc schema update",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fetchSchemaTargets(cmd, []schemaTarget{{label: "latest", paths: latestSchemaPaths}}, true)
		},
	}
	return cmd
}

func newSchemaClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Delete the cached schemas",
		Long: "Delete every schema in the cache. The next command that validates downloads the schemas it\n" +
			"needs again, and fails offline until it can: run `strspc schema fetch` while online. Only the\n" +
			"default location, or a directory `schema fetch`, `schema update` or `realm init` has used, is\n" +
			"cleared; any other $" + envSchemaCache + " exits 2.\n\n" +
			"Examples:\n" +
			"  strspc schema clear",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return clearSchemaCache(cmd.OutOrStdout())
		},
	}
	return cmd
}

func argOrDot(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

// schemaFetchTargets lists what `schema fetch` downloads for the project in
// cwd, the realms in dirs and the core releases in versions.
func schemaFetchTargets(cwd string, dirs, versions []string) ([]schemaTarget, error) {
	targets := []schemaTarget{{label: "latest", paths: latestSchemaPaths}}
	seen := map[string]bool{}
	addVersion := func(v string) {
		if seen[v] {
			return
		}
		seen[v] = true
		targets = append(targets, schemaTarget{
			label:    coreRealmID + " " + v,
			version:  v,
			opts:     []schema.Option{schema.WithCoreVersion(v)},
			paths:    releaseSchemaPaths,
			optional: releaseOptionalPaths,
		})
	}

	realms, err := schemaProjectRealms(cwd)
	if err != nil {
		return nil, fmt.Errorf("finding the project's realms: %w", err)
	}
	for _, d := range dirs {
		manifest, err := projectPath(d, "realm.json")
		if err != nil {
			return nil, fmt.Errorf("--dir %s: %w", d, err)
		}
		if _, err := os.Stat(manifest); err != nil {
			return nil, fmt.Errorf("--dir %s: no realm.json: %w", d, err)
		}
		realms = append(realms, d)
	}
	for _, dir := range realms {
		if err := versionsOfRealmTree(dir, addVersion); err != nil {
			return nil, fmt.Errorf("finding the core releases to fetch: %w", err)
		}
	}
	for _, v := range versions {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if !schemaVersion.MatchString(v) {
			return nil, fmt.Errorf("--core-version %q: must be a release version such as 1.25.0", v)
		}
		addVersion(v)
	}
	sort.SliceStable(targets[1:], func(i, j int) bool { return targets[1+i].label < targets[1+j].label })
	return targets, nil
}

// schemaProjectRealms returns the local sources of the project's config that
// hold a realm.json. A project without .strspc/config.yaml has none.
func schemaProjectRealms(cwd string) ([]string, error) {
	// All of this is project content: open only what projectPath returns.
	config, err := projectPath(cwd, filepath.Join(".strspc", "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("the project's configuration: %w", err)
	}
	// Lstat: a dangling link is a config that can't be read, not none at all.
	if _, err := os.Lstat(config); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	// Not validated against config.v1, unlike every other read of the config
	// (SEC-004): fetch is what fills a cold schema cache, so it can't require
	// the schema first. It uses only the local sources, to find the core
	// releases to fetch, and reads nothing else from the config.
	raw, err := readStrspcConfig(filepath.Dir(filepath.Dir(config)))
	if err != nil {
		return nil, fmt.Errorf("reading the project's configuration: %w", err)
	}
	cfg, err := parseStrspcConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("reading the project's configuration: %w", err)
	}
	var dirs []string
	for _, r := range cfg.Rules {
		if r.Source == "" || strings.HasPrefix(r.Source, remote.Scheme) {
			continue
		}
		// --dir is typed by the user, so only what is below it is checked.
		manifest, err := projectPath(cwd, filepath.Join(r.Source, "realm.json"))
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", r.Source, err)
		}
		dir := filepath.Dir(manifest)
		// Lstat: a manifest that is there but can't be read must fail the
		// fetch, as its pin would be missing from the cache.
		switch _, err := os.Lstat(manifest); {
		case err == nil:
			dirs = append(dirs, dir)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("looking for a realm in %s: %w", dir, err)
		}
	}
	return dirs, nil
}

// versionsOfRealmTree calls add with the core version the realm in dir pins,
// and those of its sub-realms, as deep as realm validate follows them
// (maxSubRealmDepth). Every manifest that is there must be readable, or its
// pin would be silently left out; a listed sub-realm without one pins nothing.
func versionsOfRealmTree(dir string, add func(string)) error {
	var walk func(string, int) error
	walk = func(d string, depth int) error {
		manifest, err := projectPath(d, "realm.json")
		if err != nil {
			return fmt.Errorf("the realm in %s: %w", d, err)
		}
		rf, err := entity.LoadRealm(manifest)
		if err != nil {
			return fmt.Errorf("reading the realm in %s: %w", d, err)
		}
		if dep := corePin(rf); dep != nil && schemaVersion.MatchString(dep.Version) {
			add(dep.Version)
		}
		if depth >= maxSubRealmDepth {
			return nil
		}
		for _, name := range rf.SubRealms {
			if !isSubRealmName(name) {
				continue // RM008
			}
			subManifest, err := projectPath(d, filepath.Join(name, "realm.json"))
			if err != nil {
				return fmt.Errorf("in sub-realm %s: %w", name, err)
			}
			if _, err := os.Lstat(subManifest); errors.Is(err, fs.ErrNotExist) {
				continue // RM009
			}
			if err := walk(filepath.Dir(subManifest), depth+1); err != nil {
				return fmt.Errorf("in sub-realm %s: %w", name, err)
			}
		}
		return nil
	}
	return walk(dir, 0)
}

// fetchSchemaTargets downloads each target's schemas into the cache. With
// refresh, a schema is downloaded into a staging directory and moved into the
// cache only once that succeeds.
func fetchSchemaTargets(cmd *cobra.Command, targets []schemaTarget, refresh bool) error {
	w := cmd.OutOrStdout()
	cacheDir, err := schemaCacheDir()
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	if err := prepareSchemaCache(cacheDir); err != nil {
		return withExitCode(exitConfig, err)
	}
	writeln(w, "Schema cache: "+cacheDir)

	staging := ""
	if refresh {
		staging, err = os.MkdirTemp(cacheDir, ".update-")
		if err != nil {
			return withExitCode(exitConfig, fmt.Errorf("creating a staging directory: %w", err))
		}
		defer func() { _ = os.RemoveAll(staging) }()
	}

	var failed int
	for _, t := range targets {
		opts := slices.Clone(t.opts)
		if refresh {
			opts = append(opts, schema.WithCacheDir(staging))
		}
		fetcher := newSchemaFetcher(opts...)
		for i, p := range slices.Concat(t.paths, t.optional) {
			data, err := fetcher.Fetch(cmd.Context(), p)
			if err == nil {
				// Downloads are checked already: this is a copy from the cache.
				if bad := checkSchemaDoc(data); bad != nil {
					err = fmt.Errorf("the cached copy can't be used, run `strspc schema clear` and fetch again: %w", bad)
				}
			}
			switch {
			case err == nil && refresh:
				err = moveIntoCache(staging, cacheDir, p)
			case err == nil:
				err = checkCached(cacheDir, t.version, p, data)
			}
			if err != nil && i >= len(t.paths) {
				writeln(w, fmt.Sprintf("  skipped %-26s %s: %v", t.label, p, err))
				continue
			}
			if err != nil {
				failed++
				writeln(w, fmt.Sprintf("  failed  %-26s %s: %v", t.label, p, err))
				continue
			}
			writeln(w, fmt.Sprintf("  ok      %-26s %s", t.label, p))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d schema(s) could not be fetched; for each of them, the cached copy, if any, was kept", failed)
	}
	return nil
}

// moveIntoCache moves the latest schema p from the staging directory into the
// cache, replacing the cached copy.
func moveIntoCache(staging, cacheDir, p string) error {
	src := filepath.Join(staging, filepath.FromSlash(p))
	dst := filepath.Join(cacheDir, filepath.FromSlash(p))
	if err := refuseSymlinkedParents(cacheDir, p); err != nil {
		return fmt.Errorf("moving %s: %w", p, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	// Rename replaces a file on every platform, Windows included, and leaves
	// it as it was on failure. So the cached copy is never removed first.
	renameErr := os.Rename(src, dst)
	if renameErr == nil {
		return nil
	}
	if fi, err := os.Lstat(dst); err != nil || !fi.IsDir() {
		return fmt.Errorf("updating the cache: %w", renameErr)
	}
	// No platform renames a file onto a directory. An empty one is replaced;
	// one that holds files isn't a cached schema, so it stays.
	if err := os.Remove(dst); err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	return nil
}

// checkCached reports an error unless data, the schema p of the core release
// version ("" for the latest schemas) a fetch returned, is what the cache now
// holds. The manager's fetcher ignores a failed cache write, which can leave
// an older or unusable copy there, and serves the latest schema for a release
// the server doesn't have. Neither leaves what was fetched usable offline, so
// the latest copy doesn't stand in for a release here.
func checkCached(cacheDir, version, p string, data []byte) error {
	key, why := p, "it couldn't be written"
	if version != "" {
		key = "versions/" + version + "/" + p
		why = "it couldn't be written, or that release doesn't publish it"
	}
	file := filepath.Join(cacheDir, filepath.FromSlash(key))
	if fi, err := os.Lstat(file); err == nil && fi.Mode().IsRegular() {
		if cached, err := os.ReadFile(file); err == nil && bytes.Equal(cached, data) {
			return nil
		}
	}
	return fmt.Errorf("not in the cache as %s: %s", key, why)
}

// refuseSymlinkedParents reports an error when a directory between cacheDir
// and the cache entry p is a symbolic link, which a rename would follow out
// of the cache. The manager's fetcher refuses such entries the same way.
func refuseSymlinkedParents(cacheDir, p string) error {
	dir := cacheDir
	parts := strings.Split(p, "/")
	for _, part := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, part)
		fi, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("updating the cache: %w", err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
			return fmt.Errorf("updating the cache: %s is not a plain directory of the schema cache", dir)
		}
	}
	return nil
}

// checkSchemaCacheDir reports an error when cacheDir exists but isn't a
// directory. A cache that doesn't exist yet is fine.
func checkSchemaCacheDir(cacheDir string) error {
	fi, err := os.Stat(cacheDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("schema cache %s: %w", cacheDir, err)
	case !fi.IsDir():
		return fmt.Errorf("schema cache %s is not a directory (check %s)", cacheDir, envSchemaCache)
	}
	return nil
}

// schemaStagingDir matches the staging directories `schema update` creates
// with os.MkdirTemp(cacheDir, ".update-").
var schemaStagingDir = regexp.MustCompile(`^\.update-\d+$`)

// schemaCacheFile matches exactly the cache entries strspc's fetches write:
// the latest config, entity and realm v1 schemas, a release's config, entity
// and realm v1 schemas below versions/X.Y.Z/, the bootstrap schema next to
// the entity schema, and the temporary files next to them.
var schemaCacheFile = regexp.MustCompile(`^(?:` +
	`(config|entity|realm|versions/\d+\.\d+\.\d+/(config|entity|realm))/(v1\.json|\.schema-cache-[^/]+)` +
	`|(versions/\d+\.\d+\.\d+/)?entity/bootstrap\.json` +
	`)$`)

// schemaCacheMarker marks a directory `schema fetch`, `schema update` or `realm init` has
// used as the schema cache, so that `schema clear` deletes only in a real
// schema cache.
const schemaCacheMarker = ".strspc-schema-cache"

// prepareSchemaCache creates the cache directory if needed, checks that files
// can be written in it, and marks it as a schema cache. The manager's fetcher
// treats a failed cache write as best-effort, so without this a fetch into an
// unusable cache would report success with nothing cached.
func prepareSchemaCache(cacheDir string) error {
	if err := checkSchemaCacheDir(cacheDir); err != nil {
		return fmt.Errorf("preparing the cache: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("creating the schema cache %s: %w", cacheDir, err)
	}
	// Prove that new entries can be created, as the fetcher needs to.
	probe, err := os.CreateTemp(cacheDir, ".probe-")
	if err != nil {
		return fmt.Errorf("schema cache %s can't be written: %w", cacheDir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return markSchemaCache(cacheDir)
}

// markSchemaCache writes the marker without following a link: an existing
// marker must be a regular file, and a new one is created exclusively, which
// fails on a symbolic link.
func markSchemaCache(cacheDir string) error {
	marker := filepath.Join(cacheDir, schemaCacheMarker)
	fi, err := os.Lstat(marker)
	switch {
	case err == nil && fi.Mode().IsRegular():
		return nil
	case err == nil:
		return fmt.Errorf("schema cache %s: %s is not a regular file", cacheDir, schemaCacheMarker)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("schema cache %s: %w", cacheDir, err)
	}
	f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Another strspc marked it meanwhile; a link still doesn't count.
			if fi, lerr := os.Lstat(marker); lerr == nil && fi.Mode().IsRegular() {
				return nil
			}
		}
		return fmt.Errorf("marking the schema cache %s: %w", cacheDir, err)
	}
	_, werr := f.WriteString("strspc schema cache\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("marking the schema cache %s: %w", cacheDir, werr)
	}
	return nil
}

// clearableSchemaCache reports whether `schema clear` may delete in cacheDir:
// the default location, which strspc owns, or a directory that carries the
// schema cache marker.
func clearableSchemaCache(cacheDir string) bool {
	if def, err := schemaCacheDirFrom(func(string) string { return "" }, os.UserCacheDir); err == nil && def == cacheDir {
		return true
	}
	fi, err := os.Lstat(filepath.Join(cacheDir, schemaCacheMarker))
	return err == nil && fi.Mode().IsRegular()
}

// clearSchemaCache removes the schema fetcher's entries from the cache.
func clearSchemaCache(w io.Writer) error {
	cacheDir, err := schemaCacheDir()
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	if err := checkSchemaCacheDir(cacheDir); err != nil {
		return withExitCode(exitConfig, err)
	}
	if _, err := os.Stat(cacheDir); errors.Is(err, fs.ErrNotExist) {
		writeln(w, "No schema cached in "+cacheDir)
		return nil
	}
	if !clearableSchemaCache(cacheDir) {
		return withExitCode(exitConfig, fmt.Errorf("%s is not marked as a strspc schema cache (no %s): "+
			"check %s, or delete the directory yourself", cacheDir, schemaCacheMarker, envSchemaCache))
	}
	removed, err := removeSchemaFiles(cacheDir)
	if err != nil {
		return fmt.Errorf("clearing the schema cache: %w", err)
	}
	if removed == 0 {
		writeln(w, "No schema cached in "+cacheDir)
		return nil
	}
	writeln(w, "Cleared the schema cache in "+cacheDir)
	return nil
}

// removeSchemaFiles deletes the schema files in cacheDir and in the staging
// directories an interrupted update left there, which have the same layout,
// then the directories that leaves empty. It returns how many files it
// removed. Anything else, and symbolic links, are left alone, whatever the
// directory holding them is called.
func removeSchemaFiles(cacheDir string) (int, error) {
	bases := []string{cacheDir}
	// Not Glob: the cache's own path may hold pattern characters, and would
	// then match other directories.
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", cacheDir, err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && schemaStagingDir.MatchString(e.Name()) {
			s := filepath.Join(cacheDir, e.Name())
			bases = append(bases, s)
			dirs = append(dirs, s)
		}
	}
	removed := 0
	for _, base := range bases {
		for _, name := range schemaCacheEntries {
			root := filepath.Join(base, name)
			fi, err := os.Lstat(root)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return removed, fmt.Errorf("looking at %s: %w", root, err)
			}
			if !fi.IsDir() {
				continue
			}
			err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return fmt.Errorf("reading %s: %w", p, err)
				}
				if d.IsDir() {
					dirs = append(dirs, p)
					return nil
				}
				rel, err := filepath.Rel(base, p)
				if err != nil || !d.Type().IsRegular() || !schemaCacheFile.MatchString(filepath.ToSlash(rel)) {
					return nil //nolint:nilerr // not a schema file: left alone
				}
				if err := os.Remove(p); err != nil {
					return fmt.Errorf("deleting %s: %w", p, err)
				}
				removed++
				return nil
			})
			if err != nil {
				return removed, fmt.Errorf("walking %s: %w", root, err)
			}
		}
	}
	// Deepest first, so a parent is empty once its children are gone.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.Remove(d) // fails, as it should, on a directory still holding other files
	}
	return removed, nil
}

// listSchemaCache prints each cached schema with its core release and when it
// was fetched.
func listSchemaCache(w io.Writer, now time.Time) error {
	cacheDir, err := schemaCacheDir()
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	if err := checkSchemaCacheDir(cacheDir); err != nil {
		return withExitCode(exitConfig, err)
	}
	writeln(w, "Schema cache: "+cacheDir)
	type row struct{ label, path, note string }
	var rows []row
	for _, name := range schemaCacheEntries {
		root := filepath.Join(cacheDir, name)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
				return nil //nolint:nilerr // an unreadable entry is left out of the listing
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil //nolint:nilerr // as above
			}
			rel, err := filepath.Rel(cacheDir, p)
			if err != nil {
				return nil //nolint:nilerr // as above
			}
			rel = filepath.ToSlash(rel)
			if !schemaCacheFile.MatchString(rel) {
				return nil // not a schema: another file in the same directories
			}
			r := row{label: "latest", path: rel, note: "fetched " + info.ModTime().UTC().Format("2006-01-02 15:04 UTC")}
			if v, rest, ok := strings.Cut(strings.TrimPrefix(rel, "versions/"), "/"); ok && strings.HasPrefix(rel, "versions/") {
				r.label, r.path = coreRealmID+" "+v, rest
			} else if now.Sub(info.ModTime()) >= schemaLatestTTL {
				r.note += ", refreshed on next use when online"
			}
			rows = append(rows, r)
			return nil
		})
	}
	if len(rows) == 0 {
		writeln(w, "  no schema cached: run `strspc schema fetch` while online")
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].label != rows[j].label {
			return rows[i].label < rows[j].label
		}
		return rows[i].path < rows[j].path
	})
	for _, r := range rows {
		writeln(w, fmt.Sprintf("  %-26s %-21s %s", r.label, r.path, r.note))
	}
	return nil
}
