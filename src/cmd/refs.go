package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulerefs"
)

// refsDefaultInclude and refsDefaultExclude select the files refs scans unless
// a file is named on the command line. A changelog names retired rules by
// design, and .strspc/ holds downloaded rule sources.
var (
	refsDefaultInclude = []string{"*.md"}
	refsDefaultExclude = []string{".strspc/**", "CHANGELOG.md"}
)

// refsWalkSkipDirs are not descended into outside a git repository, where
// .gitignore can't say so.
var refsWalkSkipDirs = []string{".git", ".strspc", "node_modules"}

// refsBinaryProbe is how many leading bytes are searched for a NUL byte.
const refsBinaryProbe = 8000

type refsOptions struct {
	jsonOutput bool
	format     string
	include    []string
	exclude    []string
	ignore     []string
	allowState []string
	strict     bool
}

func newRefsCmd() *cobra.Command {
	var o refsOptions

	cmd := &cobra.Command{
		Use:   "refs [path...]",
		Short: "Validate rule citations in text files",
		Long: "Scan text files for rule identifiers and validate each one against the rules in\n" +
			".strspc/config.yaml, including the realm dependencies they pin.\n\n" +
			"Markdown files are scanned by default, ignoring what .gitignore ignores, .strspc/ and\n" +
			"CHANGELOG.md. A file named as an argument is scanned whatever its extension.\n\n" +
			"An unknown rule, an Abandoned rule, and a live rule replaced by one in state I, R or T are\n" +
			"errors. A Retired, Terminated or Draft rule, and a live rule that a Published rule will\n" +
			"replace, are warnings, which --strict turns into errors.\n\n" +
			"A line holding \"strspc-refs: ignore\" exempts itself and the next line, in any comment syntax.\n\n" +
			"Exit codes: 0 clean, 1 findings, 2 configuration or resolution error.\n\n" +
			"A pattern with no slash matches a file name in any directory, and dir/** matches everything\n" +
			"under dir.\n\n" +
			"Examples:\n" +
			"  strspc refs\n" +
			"  strspc refs docs .claude --strict\n" +
			"  strspc refs --include '*.yml' --exclude 'docs/archive/**'\n" +
			"  strspc refs --ignore SHA-256 --allow-state R,T\n" +
			"  strspc refs --format github",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return withExitCode(exitConfig, fmt.Errorf("getting working directory: %w", err))
			}
			return runRefs(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cwd, args, &o)
		},
	}

	cmd.Flags().BoolVar(&o.jsonOutput, "json", false, "output diagnostics as JSON")
	cmd.Flags().StringVar(&o.format, "format", formatHuman, "output format: human or github")
	cmd.Flags().StringSliceVar(&o.include, "include", nil, "also scan files matching this glob")
	cmd.Flags().StringSliceVar(&o.exclude, "exclude", nil, "skip files matching this glob")
	cmd.Flags().StringSliceVar(&o.ignore, "ignore", nil, "identifier that isn't a citation, such as SHA-256")
	cmd.Flags().StringSliceVar(&o.allowState, "allow-state", nil, "accept citations of rules in this state: D, A, P, I, R or T")
	cmd.Flags().BoolVar(&o.strict, "strict", false, "treat warnings as errors")
	cmd.MarkFlagsMutuallyExclusive("json", "format")

	return cmd
}

func runRefs(ctx context.Context, out, errOut io.Writer, cwd string, paths []string, o *refsOptions) error {
	if o.format != formatHuman && o.format != formatGitHub {
		return withExitCode(exitConfig, fmt.Errorf("invalid --format %q: must be human or github", o.format))
	}
	states := normalizeStateCodes(o.allowState)
	if err := validateStateCodes("--allow-state", states); err != nil {
		return withExitCode(exitConfig, err)
	}
	files, err := refsFiles(cwd, paths, o)
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	sources, err := refsSources(ctx, errOut, cwd)
	if err != nil {
		return withExitCode(exitConfig, err)
	}
	checker, err := rulerefs.New(sources,
		rulerefs.WithStrict(o.strict),
		rulerefs.WithAllowedStates(states...),
		rulerefs.WithIgnore(o.ignore...),
	)
	if err != nil {
		return withExitCode(exitConfig, err)
	}

	res := &result.Result{}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(f)))
		if err != nil {
			return withExitCode(exitConfig, fmt.Errorf("reading %s: %w", printableText(f), err))
		}
		if bytes.IndexByte(data[:min(len(data), refsBinaryProbe)], 0) >= 0 {
			continue
		}
		res.Diagnostics = append(res.Diagnostics, checker.CheckText(printableText(f), data).Diagnostics...)
	}

	switch {
	case o.jsonOutput:
		if err := writeJSON(out, res); err != nil {
			return err
		}
	case o.format == formatGitHub:
		writeGitHubAnnotations(out, res.Diagnostics)
	default:
		outputText(out, &result.Result{Diagnostics: withLineInPath(res.Diagnostics)})
	}
	if errs := res.Errors(); len(errs) > 0 {
		return fmt.Errorf("refs found %d error(s)", len(errs))
	}
	return nil
}

// refsSources resolves the configured sources the way check does, then turns
// them and the realms they depend on into what the citations are judged
// against. The sources come first, so a project's own rules win. Resolution
// problems go to errOut through writeRefsNotices, which keeps the output a single
// JSON document; check's own report of them is discarded.
func refsSources(ctx context.Context, errOut io.Writer, cwd string) ([]rulerefs.Source, error) {
	r := &checkRun{ctx: ctx, out: io.Discard, errOut: errOut, cwd: cwd, opts: checkOptions{format: formatHuman}}
	if err := r.loadConfig(); err != nil {
		return nil, err
	}
	if err := r.resolveRules(); err != nil {
		writeRefsNotices(errOut, r.resolutionDiags)
		return nil, err
	}
	writeRefsNotices(errOut, r.notices)

	byOrigin := make(map[string][]*entity.File)
	for _, rf := range r.resolved {
		byOrigin[rf.Origin.Source] = append(byOrigin[rf.Origin.Source], rf.File)
	}
	sources := make([]rulerefs.Source, 0, len(r.entries))
	for _, e := range r.entries {
		s := rulerefs.Source{Entities: byOrigin[e.Source]}
		if t := r.realmTrees[e.Source]; t != nil {
			s.RealmID = t.manifest.Realm.ID
			s.Format = t.manifest.RuleIdentifierFormat
			s.Entities = withoutSubRealms(s.Entities, t.resolved.SubRealms)
		}
		sources = append(sources, s)
	}

	seen := make(map[string]bool)
	add := func(realm *realmresolve.ResolvedRealm, format *entity.RuleIdentifierFormat) {
		if id := realm.Realm.Realm.ID; !seen[id] {
			seen[id] = true
			sources = append(sources, resolvedRealmSources(realm, format)...)
		}
	}
	// A sub-realm can pin another version of a dependency, so the project's own
	// dependencies go first.
	trees := make([]*realmTree, 0, len(r.entries))
	for _, e := range r.entries {
		if t := r.realmTrees[e.Source]; t != nil {
			trees = append(trees, t)
		}
	}
	for _, t := range trees {
		for _, sub := range t.resolved.SubRealms {
			add(sub, t.manifest.RuleIdentifierFormat) // a sub-realm renders like its parent (RLM-015)
		}
	}
	for _, t := range trees {
		for _, dep := range t.resolved.Dependencies {
			add(dep, dep.Realm.RuleIdentifierFormat)
		}
	}
	for _, t := range trees {
		for _, sub := range t.resolved.SubRealms {
			for _, dep := range sub.Dependencies {
				add(dep, dep.Realm.RuleIdentifierFormat)
			}
		}
	}
	return sources, nil
}

// writeRefsNotices writes resolution diagnostics. Their text can come
// from a remote dependency, so one line of it stays one printable line: a CI
// runner reads stderr for workflow commands too.
func writeRefsNotices(w io.Writer, notices []result.Diagnostic) {
	for _, d := range withLineInPath(notices) {
		writeln(w, descStyle.Render(printableText(d.String())))
	}
}

// withoutSubRealms drops the entities of subs: a source is walked recursively,
// but a sub-realm's rules belong to the sub-realm's own realm ID.
func withoutSubRealms(entities []*entity.File, subs []*realmresolve.ResolvedRealm) []*entity.File {
	return slices.DeleteFunc(slices.Clone(entities), func(ef *entity.File) bool {
		return slices.ContainsFunc(subs, func(sub *realmresolve.ResolvedRealm) bool {
			_, ok := sub.Entities[ef.Entity.ID]
			return ok
		})
	})
}

// resolvedRealmSources converts a resolved Realm, in a stable order. A dependency
// is walked recursively, so its entities include those of its sub-realms, which
// become sources of their own under their own realm IDs.
func resolvedRealmSources(realm *realmresolve.ResolvedRealm, format *entity.RuleIdentifierFormat) []rulerefs.Source {
	id := realm.Realm.Realm.ID
	subs := refsSubRealmDirs(realm)
	euids := make([]string, 0, len(realm.Entities))
	for euid := range realm.Entities {
		euids = append(euids, euid)
	}
	slices.Sort(euids)

	byRealm := make(map[string][]*entity.File)
	for _, euid := range euids {
		owner := id
		for _, sub := range subs {
			if strings.HasPrefix(realm.EUIDs[euid], sub.dir+string(filepath.Separator)) {
				owner = sub.realmID
			}
		}
		byRealm[owner] = append(byRealm[owner], realm.Entities[euid])
	}

	sources := []rulerefs.Source{{RealmID: id, Format: format, Entities: byRealm[id]}}
	for _, sub := range subs {
		sources = append(sources, rulerefs.Source{RealmID: sub.realmID, Format: format, Entities: byRealm[sub.realmID]})
	}
	return sources
}

// refsSubRealmDir is a declared sub-realm of a resolved Realm.
type refsSubRealmDir struct {
	dir     string
	realmID string
}

// refsSubRealmDirs reads the sub-realms realm declares. A dependency's manifest
// isn't this project's, so a name that leaves the Realm is ignored. So is one
// with no readable manifest: its rules stay with the Realm, where an unqualified
// citation still finds them.
func refsSubRealmDirs(realm *realmresolve.ResolvedRealm) []refsSubRealmDir {
	var subs []refsSubRealmDir
	for _, name := range realm.Realm.SubRealms {
		if !filepath.IsLocal(name) {
			continue
		}
		dir := filepath.Join(realm.Dir, name)
		if rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json")); err == nil {
			subs = append(subs, refsSubRealmDir{dir: dir, realmID: rf.Realm.ID})
		}
	}
	return subs
}

// refsFiles returns the files to scan, relative to cwd with forward slashes,
// sorted. A file named in paths skips the include patterns; the exclude patterns
// apply to every file.
func refsFiles(cwd string, paths []string, o *refsOptions) ([]string, error) {
	include := slices.Concat(refsDefaultInclude, o.include)
	exclude := slices.Concat(refsDefaultExclude, o.exclude)
	for _, p := range slices.Concat(include, exclude) {
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid glob %q: %w", p, err)
		}
	}

	var named, roots []string
	for _, p := range paths {
		rel, err := refsRelPath(cwd, p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(filepath.Join(cwd, rel))
		if err != nil {
			return nil, fmt.Errorf("accessing %s: %w", printableText(p), err)
		}
		if err := refsCheckInside(cwd, rel); err != nil {
			return nil, err
		}
		if info.IsDir() {
			roots = append(roots, rel)
		} else {
			named = append(named, filepath.ToSlash(rel))
		}
	}
	if len(paths) == 0 {
		roots = []string{"."}
	}

	var listed []string
	if len(roots) > 0 {
		var err error
		if listed, err = refsListFiles(cwd, roots); err != nil {
			return nil, err
		}
	}
	files := make([]string, 0, len(named)+len(listed))
	files = append(files, named...)
	for _, f := range listed {
		if refsGlobMatch(include, f) {
			files = append(files, f)
		}
	}
	files = slices.DeleteFunc(files, func(f string) bool {
		if refsGlobMatch(exclude, f) {
			return true
		}
		info, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(f)))
		return err != nil || !info.Mode().IsRegular() // deleted, a link or a submodule
	})
	slices.Sort(files)
	return slices.Compact(files), nil
}

// refsRelPath returns p relative to cwd. Files outside cwd are rejected: their
// paths could not be matched against the patterns or annotated.
func refsRelPath(cwd, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, p)
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || refsEscapes(rel) {
		return "", fmt.Errorf("path %s is outside the project", printableText(p))
	}
	return rel, nil
}

// refsCheckInside rejects a path that leaves cwd through a symbolic link.
func refsCheckInside(cwd, rel string) error {
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", printableText(cwd), err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(cwd, rel))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", printableText(rel), err)
	}
	inside, err := filepath.Rel(root, target)
	if err != nil || refsEscapes(inside) {
		return fmt.Errorf("path %s is outside the project", printableText(rel))
	}
	return nil
}

// refsEscapes reports whether a relative path climbs out of its base.
func refsEscapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// refsListFiles lists the files under roots. In a git repository it asks git, so
// that ignored files and nested worktrees stay out.
func refsListFiles(cwd string, roots []string) ([]string, error) {
	if _, err := gitRoot(cwd); err != nil {
		return refsWalkFiles(cwd, roots)
	}
	args := append([]string{
		"--literal-pathspecs", "-C", cwd, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--",
	}, roots...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("listing files: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("listing files: %w", err)
	}
	var files []string
	for f := range strings.SplitSeq(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// refsWalkFiles lists the files under roots outside a git repository.
func refsWalkFiles(cwd string, roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(cwd, root), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if slices.Contains(refsWalkSkipDirs, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(cwd, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("listing files: %w", err)
		}
	}
	return files, nil
}

// refsGlobMatch reports whether file, a slash path, matches any pattern. A
// pattern with no slash matches the file name in any directory, and dir/**
// matches everything under dir. Patterns were validated by refsFiles.
func refsGlobMatch(patterns []string, file string) bool {
	for _, p := range patterns {
		if dir, ok := strings.CutSuffix(p, "/**"); ok {
			if strings.HasPrefix(file, dir+"/") {
				return true
			}
			continue
		}
		name := file
		if !strings.Contains(p, "/") {
			name = path.Base(file)
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// printableText quotes text that would otherwise put control characters on the
// terminal or in an annotation.
func printableText(s string) string {
	if rejectControlChars("text", s) == nil {
		return s
	}
	return strings.Trim(strconv.Quote(s), `"`)
}
