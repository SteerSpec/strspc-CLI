package cmd

import (
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
// schema `strspc check` validates config.yaml against, and the entity and
// realm schemas of a realm that pins no core version.
var latestSchemaPaths = []string{configV1SchemaPath, schema.EntityV1Path, schema.RealmV1Path}

// releaseSchemaPaths are the schemas lint and realm validate read for a realm
// pinned to a core release.
var releaseSchemaPaths = []string{schema.EntityV1Path, schema.RealmV1Path}

// schemaCacheEntries are the top-level names the schema fetcher writes in the
// cache: one directory per latest schema family, and versions/ for releases.
// `schema clear` removes only these, so a misdirected STRSPC_SCHEMA_CACHE
// can't delete anything else.
var schemaCacheEntries = []string{"config", "entity", "realm", "versions"}

// schemaVersion is a core release version, as a realm pins it.
var schemaVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// schemaLatestTTL mirrors the manager's refresh interval for a cached latest
// schema, for the cache listing.
const schemaLatestTTL = time.Hour

// schemaTarget is a set of schemas fetched with the same options.
type schemaTarget struct {
	label string // "latest" or "dev.steerspec.core X.Y.Z"
	opts  []schema.Option
	paths []string
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
			"that validation works offline afterwards.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listSchemaCache(cmd.OutOrStdout(), time.Now())
		},
	}
	cmd.AddCommand(newSchemaFetchCmd(), newSchemaUpdateCmd(), newSchemaClearCmd())
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
		Long: "Fetch the latest config, entity and realm schemas, and the entity and realm schemas of the\n" +
			"core release that each local realm pins: the local sources of .strspc/config.yaml in path\n" +
			"(default: the current directory) that hold a realm.json, their sub-realms, and each --dir.\n" +
			"--core-version adds a release. A schema already cached is not downloaded again. Exits 1\n" +
			"when a schema can't be fetched and 2 when the configuration or a realm can't be read.",
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
			"they are. Exits 1 when a schema can't be fetched.",
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
			"needs again, and fails offline until it can: run `strspc schema fetch` while online.",
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
			label: coreRealmID + " " + v,
			opts:  []schema.Option{schema.WithCoreVersion(v)},
			paths: releaseSchemaPaths,
		})
	}

	realms, err := schemaProjectRealms(cwd)
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "realm.json")); err != nil {
			return nil, fmt.Errorf("--dir %s: no realm.json: %w", d, err)
		}
		realms = append(realms, d)
	}
	for _, dir := range realms {
		versionsOfRealmTree(dir, addVersion)
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
	if _, err := os.Stat(filepath.Join(cwd, ".strspc", "config.yaml")); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	cfg, err := loadStrspcConfig(cwd)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, r := range cfg.Rules {
		if r.Source == "" || strings.HasPrefix(r.Source, remote.Scheme) {
			continue
		}
		dir := filepath.Join(cwd, r.Source)
		if _, err := os.Stat(filepath.Join(dir, "realm.json")); err == nil {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// versionsOfRealmTree calls add with the core version the realm in dir pins,
// and those of its sub-realms. A manifest that can't be read is skipped:
// reporting it is realm validate's job.
func versionsOfRealmTree(dir string, add func(string)) {
	visit := func(rf *entity.RealmFile) {
		if dep := corePin(rf); dep != nil && schemaVersion.MatchString(dep.Version) {
			add(dep.Version)
		}
	}
	var walk func(string, int)
	walk = func(d string, depth int) {
		rf, err := entity.LoadRealm(filepath.Join(d, "realm.json"))
		if err != nil || depth > 16 {
			return
		}
		visit(rf)
		for _, sub := range subRealmsOf(d, rf) {
			walk(sub.dir, depth+1)
		}
	}
	walk(dir, 0)
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
	writeln(w, "Schema cache: "+cacheDir)

	staging := ""
	if refresh {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return fmt.Errorf("creating the schema cache: %w", err)
		}
		staging, err = os.MkdirTemp(cacheDir, ".update-")
		if err != nil {
			return fmt.Errorf("creating a staging directory: %w", err)
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
		for _, p := range t.paths {
			_, err := fetcher.Fetch(cmd.Context(), p)
			if err == nil && refresh {
				err = moveIntoCache(staging, cacheDir, p)
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
		return fmt.Errorf("%d schema(s) could not be fetched; the cached copies, if any, are unchanged", failed)
	}
	return nil
}

// moveIntoCache moves the latest schema p from the staging directory into the
// cache, replacing the cached copy.
func moveIntoCache(staging, cacheDir, p string) error {
	src := filepath.Join(staging, filepath.FromSlash(p))
	dst := filepath.Join(cacheDir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("updating the cache: %w", err)
	}
	return nil
}

// clearSchemaCache removes the schema fetcher's entries from the cache.
func clearSchemaCache(w io.Writer) error {
	cacheDir, err := schemaCacheDir()
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	removed := 0
	names := slices.Clone(schemaCacheEntries)
	if stale, err := filepath.Glob(filepath.Join(cacheDir, ".update-*")); err == nil {
		for _, s := range stale {
			names = append(names, filepath.Base(s))
		}
	}
	for _, name := range names {
		p := filepath.Join(cacheDir, name)
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("clearing the schema cache: %w", err)
		}
		removed++
	}
	if removed == 0 {
		writeln(w, "No schema cached in "+cacheDir)
		return nil
	}
	writeln(w, "Cleared the schema cache in "+cacheDir)
	return nil
}

// listSchemaCache prints each cached schema with its core release and when it
// was fetched.
func listSchemaCache(w io.Writer, now time.Time) error {
	cacheDir, err := schemaCacheDir()
	if err != nil {
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
		writeln(w, fmt.Sprintf("  %-26s %-16s %s", r.label, r.path, r.note))
	}
	return nil
}
