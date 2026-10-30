package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/evalcache"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/providers"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// newEvalProvider builds the AI provider named in the configuration.
// Package-level so tests can substitute a stub (same pattern as
// newSchemaFetcher in lint.go).
var newEvalProvider = providers.New

// Output formats accepted by --format.
const (
	formatHuman  = "human"
	formatJSON   = "json"
	formatGitHub = "github"
)

// checkOutput is the JSON representation of a check run; it also feeds the
// GitHub step summary.
type checkOutput struct {
	OK          bool                `json:"ok"`
	ExitCode    int                 `json:"exit_code"`
	StaticOnly  bool                `json:"static_only"`
	Provider    string              `json:"provider,omitempty"`
	Model       string              `json:"model,omitempty"`
	RuleCount   int                 `json:"rule_count"`
	SourceCount int                 `json:"source_count"`
	Diff        string              `json:"diff,omitempty"`
	Usage       *ruleeval.Usage     `json:"usage,omitempty"`
	Diagnostics []result.Diagnostic `json:"diagnostics"`
	// Error carries the fatal cause (configuration, resolution, provider) so
	// JSON consumers do not have to parse stderr.
	Error string `json:"error,omitempty"`
}

// checkOptions collects the flags of `strspc check`.
type checkOptions struct {
	diff       diffFlags
	format     string
	provider   string
	model      string
	endpoint   string
	staticOnly bool
	noCache    bool
	verbose    bool
	jsonAlias  bool // hidden --json, kept for scripts written against 1.x
}

func newCheckCmd() *cobra.Command {
	var o checkOptions

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Evaluate code changes against resolved rules",
		Long: "Evaluate a code diff against the rules declared in .strspc/config.yaml.\n\n" +
			"Rules are resolved from the configured sources (local directories or github:// references,\n" +
			"cached under .strspc/cache/), then the diff is sent to the configured AI provider which\n" +
			"returns one verdict per rule. Violations of rules whose state is listed in fail_on fail\n" +
			"the check; other violations are warnings.\n\n" +
			"Diff selection, highest precedence first: --diff-file, --staged, --pr, --base,\n" +
			"$GITHUB_BASE_REF (origin/<ref>, fetched when missing), then the working tree vs HEAD.\n" +
			"--pr and --base diff from the merge base with HEAD. Files under .strspc/ and rules/ are\n" +
			"never evaluated.\n\n" +
			"Exit codes: 0 compliant, 1 violations in fail_on states, 2 configuration or rule\n" +
			"resolution error, 3 AI provider unavailable (no API key, rejected credentials).\n" +
			"--static-only (or evaluator.provider: null) skips AI evaluation and exits 0 after\n" +
			"resolving the rule sources.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd := "."
			if len(args) > 0 {
				cwd = args[0]
			}
			if o.jsonAlias && o.format == formatHuman {
				o.format = formatJSON
			}
			return runCheck(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cwd, &o)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.diff.Base, "base", "", "git ref to diff against (merge base with HEAD → working tree)")
	f.BoolVar(&o.diff.Staged, "staged", false, "evaluate staged changes only (index vs HEAD)")
	f.IntVar(&o.diff.PR, "pr", 0, "GitHub PR number: diff against its base (requires gh CLI)")
	f.StringVar(&o.diff.DiffFile, "diff-file", "", "read a unified diff from this file instead of git")
	f.StringVar(&o.format, "format", formatHuman, "output format: human|json|github")
	f.StringVar(&o.provider, "provider", "", "AI provider (overrides evaluator.provider): claude|null")
	f.StringVar(&o.model, "model", "", "model identifier (overrides evaluator.model)")
	f.StringVar(&o.endpoint, "endpoint", "", "provider endpoint URL; opts in to a non-Anthropic host (also $"+envEvaluatorEndpoint+")")
	f.BoolVar(&o.staticOnly, "static-only", false, "skip AI evaluation; resolve sources and run structural checks only")
	f.BoolVar(&o.noCache, "no-cache", false, "do not read or write the evaluation cache (.strspc/cache.db)")
	f.BoolVar(&o.verbose, "verbose", false, "print resolution, cache and usage details to stderr")
	f.BoolVar(&o.jsonAlias, "json", false, "alias for --format json")
	_ = f.MarkHidden("json")

	return cmd
}

// checkRun carries the state of one invocation across its phases.
type checkRun struct {
	ctx    context.Context
	out    io.Writer
	errOut io.Writer
	cwd    string
	opts   checkOptions

	cfg        *strspcConfig
	entries    []ruleresolve.SourceEntry
	ttl        time.Duration
	failOn     []string
	evaluateOn []string

	static       bool
	providerName string
	provider     ruleeval.Provider
	model        string

	fetcher  *remote.Fetcher
	resolved []*ruleresolve.ResolvedFile
	notices  []result.Diagnostic // non-fatal resolution and cache diagnostics
	inputs   []ruleeval.RuleInput

	chains       map[string]*realmresolve.Inheritance // resolved chains by inheriting EUID, unique across sources (RSV005)
	sourceRealms map[string]string                    // realm ID by source, for sources with a manifest

	repoDir    string
	diffSource diffSource
	diff       string
}

func runCheck(ctx context.Context, out, errOut io.Writer, cwd string, o *checkOptions) error {
	r := &checkRun{ctx: ctx, out: out, errOut: errOut, cwd: cwd, opts: *o}

	switch o.format {
	case formatHuman, formatJSON, formatGitHub:
	default:
		return r.fail(exitConfig, fmt.Errorf("invalid --format %q: must be human, json or github", o.format))
	}

	if err := r.loadConfig(); err != nil {
		return r.fail(exitConfig, err)
	}
	if code, err := r.buildProvider(); err != nil {
		return r.fail(code, err)
	}
	if err := r.resolveRules(); err != nil {
		return r.fail(exitConfig, err)
	}
	if !r.static {
		if err := r.collectDiff(); err != nil {
			return r.fail(exitConfig, err)
		}
	}
	return r.evaluate()
}

// fail reports a fatal error in the selected format and wraps it with the
// exit code. The diagnostics gathered so far are shown so that, for
// example, a resolution failure lists the offending source.
func (r *checkRun) fail(code int, err error, diags ...result.Diagnostic) error {
	var already *exitError
	if errors.As(err, &already) {
		return err // reported by the phase that produced it
	}
	switch r.opts.format {
	case formatJSON:
		out := r.output(code, diags)
		out.Error = err.Error()
		_ = r.writeJSONOutput(&out)
	case formatGitHub:
		writeGitHubAnnotations(r.out, diags)
		writeln(r.out, githubErrorAnnotation(err))
		out := r.output(code, diags)
		out.Error = err.Error()
		_ = appendGitHubStepSummary(os.Getenv("GITHUB_STEP_SUMMARY"), &out)
	default:
		if len(diags) > 0 {
			outputText(r.out, &result.Result{Diagnostics: withLineInPath(diags)})
		}
	}
	return withExitCode(code, err)
}

// loadConfig reads .strspc/config.yaml and normalises the settings used by
// the check. Structural problems the check cannot work around (no rule
// source, unknown lifecycle state codes) are configuration errors; a
// mismatch against the published config.v1 schema is reported as a warning.
func (r *checkRun) loadConfig() error {
	raw, err := readStrspcConfig(r.cwd)
	if err != nil {
		return err
	}
	cfg, err := parseStrspcConfig(raw)
	if err != nil {
		return err
	}
	r.cfg = cfg

	r.entries, err = buildSourceEntries(cfg)
	if err != nil {
		return err
	}
	if len(r.entries) == 0 {
		// `strspc init` has written a core source since 1.15, so an empty
		// list is a broken configuration, not a fresh project.
		return errors.New("no rule sources configured in .strspc/config.yaml: rules: must list at least one source (core rule SPCFG-004)")
	}

	if cfg.Cache.TTL != "" {
		r.ttl, err = time.ParseDuration(cfg.Cache.TTL)
		if err != nil {
			return fmt.Errorf("parsing cache.ttl %q: %w", cfg.Cache.TTL, err)
		}
	}

	r.failOn = normalizeStateCodes(cfg.FailOn)
	if err := validateStateCodes("fail_on", r.failOn); err != nil {
		return err
	}
	if cfg.EvaluateOn != nil {
		// An explicit `evaluate_on: []` means "evaluate nothing"; only an
		// absent key falls back to the ruleeval default.
		r.evaluateOn = normalizeStateCodes(cfg.EvaluateOn)
		if err := validateStateCodes("evaluate_on", r.evaluateOn); err != nil {
			return err
		}
	}

	r.notices = append(r.notices, r.validateConfigSchema(raw)...)
	return nil
}

// buildProvider decides between static-only and AI evaluation and constructs
// the provider. It runs before resolution so that a missing API key is
// reported as "provider unavailable" (exit 3) even when the sources cannot
// be reached.
func (r *checkRun) buildProvider() (int, error) {
	r.providerName = strings.TrimSpace(r.cfg.Evaluator.Provider)
	if r.opts.provider != "" {
		r.providerName = strings.TrimSpace(r.opts.provider)
	}
	r.model = r.cfg.Evaluator.Model
	if r.opts.model != "" {
		r.model = r.opts.model
	}

	if r.opts.staticOnly || isStaticProviderName(r.providerName) {
		// No provider will be built, so a custom endpoint left in the config
		// is irrelevant and must not fail the static modes.
		r.static = true
		return exitOK, nil
	}
	if err := rejectControlChars("evaluator.provider", r.providerName); err != nil {
		return exitConfig, err
	}
	if err := rejectControlChars("evaluator.model", r.model); err != nil {
		return exitConfig, err
	}

	endpoint, err := resolveEndpoint(r.cfg.Evaluator.Endpoint, r.opts.endpoint, os.Getenv(envEvaluatorEndpoint))
	if err != nil {
		return exitConfig, err
	}

	p, err := newEvalProvider(r.providerName, providers.Options{
		Model:    r.model,
		Endpoint: endpoint,
		Effort:   r.cfg.Evaluator.Effort,
	})
	switch {
	case err == nil:
		r.provider = p
		if d, ok := p.(ruleeval.Describer); ok && d.Model() != "" {
			r.model = d.Model()
		}
		if err := rejectControlChars("evaluator.model", r.model); err != nil {
			return exitConfig, err
		}
		return exitOK, nil
	case errors.Is(err, providers.ErrStatic):
		r.static = true
		return exitOK, nil
	case errors.Is(err, ruleeval.ErrUnavailable):
		return exitUnavailable, fmt.Errorf("provider %q unavailable: %w (run with --static-only to skip AI evaluation)", r.providerName, err)
	case errors.Is(err, providers.ErrProviderNotImplemented):
		return exitConfig, fmt.Errorf("evaluator.provider %q is not implemented yet; use claude, or null for static checks: %w", r.providerName, err)
	case errors.Is(err, providers.ErrUnknownProvider):
		return exitConfig, fmt.Errorf("evaluator.provider %q is not a known provider (claude|null): %w", r.providerName, err)
	default:
		return exitConfig, fmt.Errorf("configuring evaluator %q: %w", r.providerName, err)
	}
}

// resolveRules fetches every configured source (github:// ones into
// .strspc/cache/) and validates the realm dependencies of sources that ship
// a realm.json. Any error-severity diagnostic aborts the check.
func (r *checkRun) resolveRules() error {
	cacheDir := filepath.Join(r.cwd, ".strspc", "cache")
	r.fetcher = &remote.Fetcher{Token: remote.TokenFromEnv(), CacheDir: cacheDir, TTL: r.ttl}

	opts := []ruleresolve.Option{
		ruleresolve.WithBaseDir(r.cwd),
		ruleresolve.WithCacheDir(cacheDir),
		ruleresolve.WithRemoteFetcher(r.fetcher),
	}
	if r.ttl > 0 {
		opts = append(opts, ruleresolve.WithCacheTTL(r.ttl))
	}
	resolver, err := ruleresolve.New(r.entries, opts...)
	if err != nil {
		return fmt.Errorf("initializing resolver: %w", err)
	}

	resolved, res := resolver.Resolve(r.ctx)
	diags := append([]result.Diagnostic(nil), res.Diagnostics...)
	if res.OK() {
		// Only once every source is in place: a failed download must not be
		// retried here (and time out twice).
		diags = append(diags, r.resolveRealmDeps(cacheDir, resolved)...)
	}

	var fatal int
	for _, d := range diags {
		if d.Severity == result.Error {
			fatal++
		} else {
			r.notices = append(r.notices, d)
		}
	}
	if fatal > 0 {
		return r.fail(exitConfig, fmt.Errorf("rule resolution failed with %d error(s)", fatal), diags...)
	}
	r.resolved = resolved
	r.verbosef("sources: %d resolved, %d entity file(s)", len(r.entries), len(resolved))
	return nil
}

// resolveRealmDeps validates realm.json dependencies of every source that
// has a manifest. Dependencies are read-only references for the realm's
// own authoring (SPEC §2.8); their rules are not evaluated here, only
// resolved so that a broken or colliding dependency surfaces in the check.
// The exception is an entity's inheritance chain, kept for buildInputs: the
// rules it inherits are that entity's rules (ENT-009).
func (r *checkRun) resolveRealmDeps(cacheDir string, resolved []*ruleresolve.ResolvedFile) []result.Diagnostic {
	var diags []result.Diagnostic
	r.chains = make(map[string]*realmresolve.Inheritance)
	r.sourceRealms = make(map[string]string)
	for _, e := range r.entries {
		dir, err := r.sourceDir(e)
		if err != nil || dir == "" {
			continue // unreachable remote sources were already reported by the resolver
		}
		manifest := filepath.Join(dir, "realm.json")
		if _, err := os.Stat(manifest); err != nil {
			continue
		}
		rf, err := entity.LoadRealm(manifest)
		if err != nil {
			diags = append(diags, result.Diagnostic{
				Module:   "realm-resolve",
				Code:     "RR008",
				Severity: result.Error,
				Message:  fmt.Sprintf("cannot load realm.json: %v", err),
				Path:     manifest,
			})
			continue
		}
		r.sourceRealms[e.Source] = rf.Realm.ID
		if len(rf.Dependencies) == 0 && len(rf.SubRealms) == 0 && !sourceUsesInheritance(resolved, e.Source) {
			continue // sub-realms may declare dependencies of their own
		}
		rrOpts := []realmresolve.Option{
			realmresolve.WithCacheDir(cacheDir),
			realmresolve.WithRemoteFetcher(r.fetcher),
			realmresolve.WithCacheTTL(r.ttl),
		}
		if strings.HasPrefix(e.Source, remote.Scheme) {
			// The cache holds only this source's subtree; the origin lets
			// realmresolve rebase relative dependencies (`../core/`, also
			// those of sub-realms) onto the repository at the same ref.
			if ref, err := remote.ParseGitHub(e.Source); err == nil {
				rrOpts = append(rrOpts, realmresolve.WithOrigin(ref))
			}
		}
		rr := realmresolve.New(rrOpts...)
		out, res := rr.Resolve(r.ctx, rf, dir)
		diags = append(diags, res.Diagnostics...)
		for _, c := range out.Inheritance {
			r.chains[c.Entity.Entity.Entity.ID] = c
		}
		r.verbosef("realm %s: %d dependency(ies) resolved, %d inheritance chain(s)", rf.Realm.ID, len(out.Dependencies), len(out.Inheritance))
	}
	return diags
}

// sourceUsesInheritance reports whether an entity resolved from source
// declares `inherits` or `overrides`, which need realm-resolve even without
// dependencies.
func sourceUsesInheritance(resolved []*ruleresolve.ResolvedFile, source string) bool {
	for _, rf := range resolved {
		if rf.Origin.Source == source && (usesInherits(rf.File) || usesOverrides(rf.File)) {
			return true
		}
	}
	return false
}

// sourceDir returns the local directory holding a source: the path itself
// for local sources, the cache directory for github:// ones (already
// downloaded by the resolver, so this never hits the network).
func (r *checkRun) sourceDir(e ruleresolve.SourceEntry) (string, error) {
	if strings.HasPrefix(e.Source, remote.Scheme) {
		ref, err := remote.ParseGitHub(e.Source)
		if err != nil {
			return "", err
		}
		return r.fetcher.EnsureDir(r.ctx, ref)
	}
	if strings.Contains(e.Source, "://") {
		return "", nil
	}
	p := e.Source
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.cwd, p)
	}
	return filepath.Abs(p)
}

// collectDiff locates the git repository and produces the diff to evaluate.
func (r *checkRun) collectDiff() error {
	r.diffSource = resolveDiffSource(r.opts.diff, os.Getenv)

	repoDir, err := gitRoot(r.cwd)
	if err != nil {
		if r.diffSource.Kind == diffFromFile {
			repoDir = "" // a diff file needs no repository
		} else {
			return err
		}
	}
	r.repoDir = repoDir

	subDir := "."
	if repoDir != "" {
		subDir, err = repoRelative(repoDir, r.cwd)
		if err != nil {
			return err
		}
	}

	r.diff, err = collectDiff(r.ctx, repoDir, subDir, r.diffSource)
	if err != nil {
		return err
	}
	r.verbosef("diff: %s (%d bytes, %d file(s))", r.diffSource, len(r.diff), strings.Count(r.diff, "\ndiff --git ")+boolInt(strings.HasPrefix(r.diff, "diff --git ")))
	return nil
}

// buildInputs flattens the resolved files into rule inputs, tagging each
// with its source and scope so the provider can restrict local rules to
// their own directory and the cache key reflects both. An entity with an
// inheritance chain contributes its effective rule set, and a rule of a
// configured source that inheriting entities replace is annotated as such.
func (r *checkRun) buildInputs() {
	labelBase := r.repoDir
	if labelBase == "" {
		labelBase = r.cwd
	}
	effective := make(map[string][]inherit.Rule, len(r.chains))
	sets := make([][]inherit.Rule, 0, len(r.chains))
	for id, c := range r.chains {
		effective[id] = inherit.Effective(c.Entity, c.Ancestors, r.evaluateOn)
		sets = append(sets, effective[id])
	}
	overriddenBy := inherit.OverriddenBy(sets...)

	for _, rf := range r.resolved {
		label := r.sourceLabel(rf, labelBase)
		ins := r.entityInputs(rf.File, rf.Path, effective)
		for i := range ins {
			ins[i].Source = label
			ins[i].Scope = string(rf.Origin.Scope)
		}
		if realmID, ok := r.sourceRealms[rf.Origin.Source]; ok {
			ruleeval.AnnotateOverriddenBy(ins, realmID, overriddenBy)
		}
		r.inputs = append(r.inputs, ins...)
	}
}

// entityInputs returns the rule inputs of f and its sub-entities. An entity
// that declares `inherits` without a resolved chain is outside any realm
// that could resolve it: its own rules are kept and CHK003 reports the rest
// as not evaluated.
func (r *checkRun) entityInputs(f *entity.File, path string, effective map[string][]inherit.Rule) []ruleeval.RuleInput {
	var ins []ruleeval.RuleInput
	if rules, ok := effective[f.Entity.ID]; ok {
		ins = ruleeval.RuleInputsFromEffective(f.Entity.ID, rules)
	} else {
		own := *f
		own.SubEntities = nil
		ins = ruleeval.RuleInputsFromFile(&own)
		if f.Entity.Inherits != "" {
			r.notices = append(r.notices, result.Diagnostic{
				Module:   "strspc",
				Code:     "CHK003",
				Severity: result.Warning,
				Path:     path,
				Message:  fmt.Sprintf("entity %s inherits %q but its source has no realm.json to resolve it from; the inherited rules are not evaluated", f.Entity.ID, f.Entity.Inherits),
			})
		}
	}
	for i := range f.SubEntities {
		ins = append(ins, r.entityInputs(&f.SubEntities[i], path, effective)...)
	}
	return ins
}

// sourceLabel names a source the way paths appear in the diff: repository
// relative for local directories, the normalised URI for remote ones.
func (r *checkRun) sourceLabel(rf *ruleresolve.ResolvedFile, base string) string {
	if strings.Contains(rf.ResolvedSource, "://") {
		return rf.ResolvedSource
	}
	rel, err := repoRelative(base, rf.ResolvedSource)
	if err != nil {
		return rf.Origin.Source
	}
	if rel == "." {
		return "./"
	}
	return rel + "/"
}

// evaluate runs the evaluator (static or AI) and reports the outcome.
func (r *checkRun) evaluate() error {
	r.buildInputs()

	evalOpts := []ruleeval.Option{
		ruleeval.WithStaticOnly(r.static),
		ruleeval.WithFailOn(r.failOn),
		ruleeval.WithConcurrency(3),
	}
	if r.evaluateOn != nil {
		evalOpts = append(evalOpts, ruleeval.WithEvaluateStates(r.evaluateOn))
	}

	obs := &checkObserver{}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	obs.cancel = cancel
	evalOpts = append(evalOpts, ruleeval.WithObserver(obs))

	if !r.static && !r.opts.noCache {
		store, err := evalcache.Open(ctx, filepath.Join(r.cwd, ".strspc", "cache.db"), evalcache.WithTTL(r.ttl))
		switch {
		case err == nil:
			defer func() {
				if cerr := store.Close(); cerr != nil {
					r.verbosef("cache: close: %v", cerr)
				}
			}()
			r.notices = append(r.notices, store.Diagnostics().Diagnostics...)
			// Get treats expired entries as misses but keeps them; prune so
			// cache.db does not grow with every distinct diff.
			if pruned, perr := store.Prune(); perr != nil {
				r.verbosef("cache: prune: %v", perr)
			} else if pruned > 0 {
				r.verbosef("cache: pruned %d expired entry(ies)", pruned)
			}
			evalOpts = append(evalOpts, ruleeval.WithCache(store))
			r.verbosef("cache: %s (ttl %s)", filepath.Join(r.cwd, ".strspc", "cache.db"), r.ttl)
		case errors.Is(err, evalcache.ErrLocked):
			_, _ = fmt.Fprintf(r.errOut, "warning: evaluation cache is locked by another process; continuing without cache\n")
		default:
			_, _ = fmt.Fprintf(r.errOut, "warning: evaluation cache unavailable (%v); continuing without cache\n", err)
		}
	} else {
		r.verbosef("cache: disabled")
	}

	evaluator, err := ruleeval.New(r.provider, evalOpts...)
	if err != nil {
		return r.fail(exitConfig, fmt.Errorf("initializing evaluator: %w", err))
	}

	if !r.static {
		evaluateOn := fmt.Sprint(ruleeval.DefaultEvaluateStates) + " (default)"
		if r.evaluateOn != nil {
			evaluateOn = fmt.Sprint(r.evaluateOn)
		}
		r.verbosef("evaluator: %s model=%s effort=%s fail_on=%v evaluate_on=%s",
			r.providerName, r.model, orDefault(r.cfg.Evaluator.Effort, "default"), r.failOn, evaluateOn)
	}

	res := evaluator.Evaluate(ctx, r.inputs, r.diff)
	usage := evaluator.Usage()

	if unavailable := obs.unavailableErr(); unavailable != nil {
		return r.fail(exitUnavailable, fmt.Errorf("provider %q unavailable: %w", r.providerName, unavailable))
	}
	// A provider that answered nothing at all is as unavailable as one that
	// refused the connection: the per-rule RE002 warnings would otherwise
	// leave the check green with no rule evaluated.
	if calls, failed, first := obs.failures(); calls > 0 && failed == calls {
		return r.fail(exitUnavailable, fmt.Errorf("provider %q failed every request (%d of %d): %w", r.providerName, failed, calls, first))
	}

	hits, misses, calls, skipped := obs.counts()
	if !r.static {
		r.verbosef("rules: %d loaded, %d skipped by evaluate_on, cache hits %d, misses %d, provider calls %d",
			len(r.inputs), skipped, hits, misses, calls)
		r.verbosef("usage: %d request(s), %d input / %d output tokens, cache read %d / write %d",
			usage.Requests, usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
	}

	diags := append(append([]result.Diagnostic(nil), r.notices...), res.Diagnostics...)
	code := exitOK
	violations := len(res.Errors())
	if violations > 0 {
		code = exitViolations
	}
	out := r.output(code, diags)
	if !r.static && usage.Requests > 0 {
		out.Usage = &usage
	}

	switch r.opts.format {
	case formatJSON:
		if err := r.writeJSONOutput(&out); err != nil {
			return err
		}
	case formatGitHub:
		writeGitHubAnnotations(r.out, diags)
		writeln(r.out, r.summaryLine(&out))
		if err := appendGitHubStepSummary(os.Getenv("GITHUB_STEP_SUMMARY"), &out); err != nil {
			_, _ = fmt.Fprintf(r.errOut, "warning: writing GITHUB_STEP_SUMMARY: %v\n", err)
		}
	default:
		outputText(r.out, &result.Result{Diagnostics: withLineInPath(diags)})
		writeln(r.out, brandStyle.Render(r.summaryLine(&out)))
	}

	if violations > 0 {
		return withExitCode(exitViolations, fmt.Errorf("check found %d violation(s) of rules in fail_on states", violations))
	}
	return nil
}

func (r *checkRun) output(code int, diags []result.Diagnostic) checkOutput {
	if diags == nil {
		diags = []result.Diagnostic{}
	}
	out := checkOutput{
		OK:          code == exitOK,
		ExitCode:    code,
		StaticOnly:  r.static,
		RuleCount:   len(r.inputs),
		SourceCount: len(r.entries),
		Diagnostics: diags,
	}
	if !r.static {
		out.Provider = r.providerName
		out.Model = r.model
		if r.diffSource.Kind != "" {
			out.Diff = r.diffSource.String()
		}
	}
	return out
}

func (r *checkRun) summaryLine(out *checkOutput) string {
	s := fmt.Sprintf("Checked %d rule(s) from %d source(s)", out.RuleCount, out.SourceCount)
	if out.StaticOnly {
		return s + " (static-only)"
	}
	return s + fmt.Sprintf(" with %s/%s against %s", out.Provider, out.Model, out.Diff)
}

func (r *checkRun) writeJSONOutput(out *checkOutput) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	if _, err := r.out.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing JSON output: %w", err)
	}
	return nil
}

// verbosef writes a diagnostic line to stderr. Values may come from the
// repository config (effort, fail_on, model…) and the runner parses stderr
// for `::` workflow commands too, so control characters are neutralised.
func (r *checkRun) verbosef(format string, args ...any) {
	if r.opts.verbose {
		_, _ = fmt.Fprintf(r.errOut, "strspc check: %s\n", sanitizeLine(fmt.Sprintf(format, args...)))
	}
}

// sanitizeLine replaces control characters (including CR/LF) with spaces so
// the result stays a single log line.
func sanitizeLine(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return ' '
		}
		return c
	}, s)
}

// withLineInPath copies diags, folding Line into Path ("file:line") so the
// shared text renderer shows evidence locations.
func withLineInPath(diags []result.Diagnostic) []result.Diagnostic {
	out := make([]result.Diagnostic, len(diags))
	for i, d := range diags {
		if d.Path != "" && d.Line > 0 {
			d.Path = fmt.Sprintf("%s:%d", d.Path, d.Line)
		}
		out[i] = d
	}
	return out
}

// checkObserver counts evaluator events and detects a provider that became
// unavailable mid-run (the evaluator itself only reports RE002 warnings).
// Groups are evaluated concurrently, so every field is guarded.
type checkObserver struct {
	mu          sync.Mutex
	hits        int
	misses      int
	calls       int
	failed      int   // provider calls that returned an error (cancellation excluded)
	firstErr    error // the first such error, for the exit message
	skipped     int
	unavailable error
	cancel      context.CancelFunc
}

func (o *checkObserver) Observe(ev ruleeval.Event) { //nolint:gocritic // signature fixed by ruleeval.Observer
	o.mu.Lock()
	defer o.mu.Unlock()
	switch ev.Kind {
	case ruleeval.EventCacheHit:
		o.hits++
	case ruleeval.EventCacheMiss:
		o.misses++
	case ruleeval.EventSkipped:
		o.skipped++
	case ruleeval.EventProviderCall:
		o.calls++
		// Only a cancellation is excluded: the observer itself cancels the
		// run after a first unavailable error. A deadline is a real failure,
		// since a per-request timeout can expire while the run continues.
		if ev.Err != nil && !errors.Is(ev.Err, context.Canceled) {
			o.failed++
			if o.firstErr == nil {
				o.firstErr = ev.Err
			}
		}
		if ev.Err != nil && isUnavailable(ev.Err) && o.unavailable == nil {
			o.unavailable = ev.Err
			if o.cancel != nil {
				o.cancel() // no point sending the remaining groups
			}
		}
	case ruleeval.EventCacheError:
		// surfaced through verbose counts only; the evaluator continues
	}
}

// isUnavailable reports whether a provider error means the endpoint cannot
// be used at all: an explicit ruleeval.ErrUnavailable, or a transport
// failure (connection refused, DNS, timeout) that the Claude provider wraps
// without that sentinel. Cancellations are excluded: the observer itself
// cancels the context once a first failure is recorded.
func isUnavailable(err error) bool {
	if errors.Is(err, ruleeval.ErrUnavailable) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var urlErr *url.Error
	var netErr net.Error
	return errors.As(err, &urlErr) || errors.As(err, &netErr)
}

// isStaticProviderName mirrors the aliases providers.New maps to ErrStatic.
func isStaticProviderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "null", "static", "static-only":
		return true
	}
	return false
}

// rejectControlChars refuses repository-controlled values that could smuggle
// a line break into stdout, where GitHub parses `::` workflow commands, or an
// escape sequence into a terminal. Invalid UTF-8 is refused too: a lone 0x9b
// byte decodes to U+FFFD, not a control character, but a terminal may still
// treat it as one.
func rejectControlChars(field, value string) error {
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("%s contains a control character or invalid UTF-8 (%q)", field, value)
	}
	return nil
}

func (o *checkObserver) unavailableErr() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.unavailable
}

// failures reports how many provider calls were made, how many returned an
// error, and the first error.
func (o *checkObserver) failures() (calls, failed int, first error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls, o.failed, o.firstErr
}

func (o *checkObserver) counts() (hits, misses, calls, skipped int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.hits, o.misses, o.calls, o.skipped
}

// stateNameToCode maps the long lifecycle state names accepted in fail_on
// and evaluate_on to their single-letter codes (entity.State*).
var stateNameToCode = map[string]string{
	"draft":       entity.StateDraft,
	"abandoned":   entity.StateAbandoned,
	"published":   entity.StatePublished,
	"implemented": entity.StateImplemented,
	"retired":     entity.StateRetired,
	"terminated":  entity.StateTerminated,
}

// allStateCodes and allStateNames list the accepted spellings, in lifecycle
// order, for error messages.
var (
	allStateCodes = []string{entity.StateDraft, entity.StateAbandoned, entity.StatePublished, entity.StateImplemented, entity.StateRetired, entity.StateTerminated}
	allStateNames = []string{"draft", "abandoned", "published", "implemented", "retired", "terminated"}
)

// normalizeStateCodes converts human-readable state names (e.g. "implemented")
// and lower-case codes to the canonical single-letter codes. Values it does
// not recognise are passed through unchanged for validateStateCodes to reject.
func normalizeStateCodes(states []string) []string {
	out := make([]string, len(states))
	for i, s := range states {
		s = strings.TrimSpace(s)
		if code, ok := stateNameToCode[strings.ToLower(s)]; ok {
			out[i] = code
		} else if len(s) == 1 {
			out[i] = strings.ToUpper(s)
		} else {
			out[i] = s
		}
	}
	return out
}

// validateStateCodes rejects lifecycle state codes that entity does not
// recognise, so a misspelt fail_on or evaluate_on entry is a configuration
// error (SPCFG-007) rather than a condition that silently never fails.
func validateStateCodes(field string, codes []string) error {
	for _, c := range codes {
		if !entity.IsValidState(c) {
			return fmt.Errorf("invalid %s state %q in .strspc/config.yaml: allowed codes are %s (or %s)",
				field, c, strings.Join(allStateCodes, ", "), strings.Join(allStateNames, ", "))
		}
	}
	return nil
}

// envEvaluatorEndpoint is the environment variable that opts in to a custom
// provider endpoint. Precedence: env > --endpoint > evaluator.endpoint.
const envEvaluatorEndpoint = "STRSPC_EVALUATOR_ENDPOINT"

// anthropicHostSuffix bounds the endpoints a repository config may set on
// its own.
const anthropicHostSuffix = "anthropic.com"

// resolveEndpoint picks the provider endpoint. The repository config is
// untrusted input (a PR can edit .strspc/config.yaml), and the API key is
// sent to whatever host the endpoint names, so a config endpoint is
// accepted only on an Anthropic host; anything else needs an explicit
// opt-in from outside the repo: the environment variable or the flag.
func resolveEndpoint(fromConfig, fromFlag, fromEnv string) (string, error) {
	if e := strings.TrimSpace(fromEnv); e != "" {
		return e, nil
	}
	if e := strings.TrimSpace(fromFlag); e != "" {
		return e, nil
	}
	cfg := strings.TrimSpace(fromConfig)
	if cfg == "" || strings.EqualFold(cfg, "null") {
		return "", nil
	}
	u, err := url.Parse(cfg)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return "", fmt.Errorf("evaluator.endpoint %q is not an https URL", cfg)
	}
	host := strings.ToLower(u.Hostname())
	if host == anthropicHostSuffix || strings.HasSuffix(host, "."+anthropicHostSuffix) {
		return cfg, nil
	}
	return "", fmt.Errorf("evaluator.endpoint %q points outside %s: the API key would be sent to that host; "+
		"opt in explicitly from outside the repository with %s=<url> or --endpoint <url>",
		cfg, anthropicHostSuffix, envEvaluatorEndpoint)
}

func orDefault(s, def string) string {
	if s == "" || s == "[]" {
		return def
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
