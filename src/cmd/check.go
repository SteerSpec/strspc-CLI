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

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/evalcache"
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
// the check.
func (r *checkRun) loadConfig() error {
	cfg, err := loadStrspcConfig(r.cwd)
	if err != nil {
		return err
	}
	r.cfg = cfg

	r.entries, err = buildSourceEntries(cfg)
	if err != nil {
		return err
	}
	if len(r.entries) == 0 {
		// SPCFG-004 wants at least one source, but a freshly initialised
		// project has none yet: degrade to a warning so `init && check`
		// keeps working (SPEC §8.4).
		r.notices = append(r.notices, result.Diagnostic{
			Module:   "strspc",
			Code:     "CHK001",
			Severity: result.Warning,
			Message:  "no rule sources configured in .strspc/config.yaml (rules: is empty); nothing to check",
		})
	}

	if cfg.Cache.TTL != "" {
		r.ttl, err = time.ParseDuration(cfg.Cache.TTL)
		if err != nil {
			return fmt.Errorf("parsing cache.ttl %q: %w", cfg.Cache.TTL, err)
		}
	}

	r.failOn = normalizeStateCodes(cfg.FailOn)
	if cfg.EvaluateOn != nil {
		// An explicit `evaluate_on: []` means "evaluate nothing"; only an
		// absent key falls back to the ruleeval default.
		r.evaluateOn = normalizeStateCodes(cfg.EvaluateOn)
	}
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
		diags = append(diags, r.resolveRealmDeps(cacheDir)...)
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
func (r *checkRun) resolveRealmDeps(cacheDir string) []result.Diagnostic {
	var diags []result.Diagnostic
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
		if len(rf.Dependencies) == 0 && len(rf.SubRealms) == 0 {
			continue // sub-realms may declare dependencies of their own
		}
		rr := realmresolve.New(
			realmresolve.WithCacheDir(cacheDir),
			realmresolve.WithRemoteFetcher(r.fetcher),
			realmresolve.WithCacheTTL(r.ttl),
		)
		out, res := rr.Resolve(r.ctx, rf, dir)
		diags = append(diags, res.Diagnostics...)
		r.verbosef("realm %s: %d dependency(ies) resolved", rf.Realm.ID, len(out.Dependencies))
	}
	return diags
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
// their own directory and the cache key reflects both.
func (r *checkRun) buildInputs() {
	labelBase := r.repoDir
	if labelBase == "" {
		labelBase = r.cwd
	}
	for _, rf := range r.resolved {
		label := r.sourceLabel(rf, labelBase)
		ins := ruleeval.RuleInputsFromFile(rf.File)
		for i := range ins {
			ins[i].Source = label
			ins[i].Scope = string(rf.Origin.Scope)
		}
		r.inputs = append(r.inputs, ins...)
	}
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
		store, err := evalcache.Open(filepath.Join(r.cwd, ".strspc", "cache.db"), evalcache.WithTTL(r.ttl))
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
		if c < 0x20 || c == 0x7f {
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

// rejectControlChars refuses repository-controlled identifiers that could
// smuggle a line break into stdout, where GitHub parses `::` workflow
// commands.
func rejectControlChars(field, value string) error {
	for _, c := range value {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("%s contains a control character (%q); use a plain identifier", field, value)
		}
	}
	return nil
}

func (o *checkObserver) unavailableErr() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.unavailable
}

func (o *checkObserver) counts() (hits, misses, calls, skipped int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.hits, o.misses, o.calls, o.skipped
}

// normalizeStateCodes converts human-readable state names (e.g. "implemented")
// to the single-letter codes expected by ruleeval (e.g. "I"). Short codes are
// passed through unchanged so both forms are accepted in config.yaml.
func normalizeStateCodes(states []string) []string {
	nameToCode := map[string]string{
		"draft":       "D",
		"abandoned":   "A",
		"published":   "P",
		"implemented": "I",
		"retired":     "R",
		"terminated":  "T",
	}
	out := make([]string, len(states))
	for i, s := range states {
		s = strings.TrimSpace(s)
		if code, ok := nameToCode[strings.ToLower(s)]; ok {
			out[i] = code
		} else if len(s) == 1 {
			out[i] = strings.ToUpper(s)
		} else {
			out[i] = s
		}
	}
	return out
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
