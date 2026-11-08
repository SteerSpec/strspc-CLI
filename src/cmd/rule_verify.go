package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// Verdicts reported by `strspc rule verify`: the provider's three verdicts
// plus unknown, for a rule the provider returned nothing for.
const (
	verifyCompliant     = "compliant"
	verifyViolated      = "violated"
	verifyNotApplicable = "not_applicable"
	verifyUnknown       = "unknown"
)

// verifyOptions collects the flags of `strspc rule verify`.
type verifyOptions struct {
	check    checkOptions // format, provider, model, effort, endpoint, verbose
	project  string
	paths    []string
	excludes []string
	dryRun   bool
}

func newRuleVerifyCmd() *cobra.Command {
	var o verifyOptions

	cmd := &cobra.Command{
		Use:   "verify <rule_id>...",
		Short: "(experimental) Check that the current code complies with rules",
		Long: "Audit the current code against rules, not a diff: every tracked file of the working tree\n" +
			"(or of --path, minus --exclude) is sent to the configured AI provider with numbered lines, and\n" +
			"each rule gets a verdict, compliant, violated, not_applicable or unknown, with an explanation\n" +
			"and the lines it rests on. Something a rule requires that no file has is a violation. As with\n" +
			"check, files under .strspc/ and rules/ are never read, even when --path matches them; nor are\n" +
			"untracked, deleted, symlinked or binary files.\n\n" +
			"This is the check to run before promoting a rule from Published to Implemented.\n\n" +
			"The files read stand for the whole code: --path and --exclude narrow what counts as existing,\n" +
			"so excluding tests makes a rule such as \"every command has a test\" fail. A tree too large\n" +
			"for one request is read in parts that collect what each file holds for each rule, then every\n" +
			"rule is judged on all of it. What each file holds is kept in .strspc/cache.db (unless\n" +
			"--no-cache), so a rerun reads only the files that changed.\n\n" +
			"Experimental (GH #291). Use --dry-run first: it reports the size of the run without calling\n" +
			"the provider or needing an API key.\n\n" +
			"Exit codes: 0 every rule compliant or not applicable, 1 a rule violated or left unknown,\n" +
			"2 configuration or rule resolution error, 3 AI provider unavailable, 4 the JSON report could\n" +
			"not be written. A run that already failed keeps its own exit code even if the report cannot\n" +
			"be written either (CMD-008).",
		Example: "  strspc rule verify CMD-008 --dry-run\n" +
			"  strspc rule verify CMD-008 CMD-009 --path src/cmd --exclude '**/*_test.go' --effort high",
		Args:          cobra.MinimumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args, &o)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.project, "project", ".", "project directory holding .strspc/config.yaml")
	f.StringArrayVar(&o.paths, "path", nil, "only read tracked files matching this glob, relative to the project (repeatable)")
	f.StringArrayVar(&o.excludes, "exclude", nil, "skip tracked files matching this glob, relative to the project (repeatable), e.g. '**/*_test.go'")
	f.StringVar(&o.check.format, "format", formatHuman, "output format: human|json")
	f.StringVar(&o.check.provider, "provider", "", "AI provider (overrides evaluator.provider): claude")
	f.StringVar(&o.check.model, "model", "", "model identifier (overrides evaluator.model)")
	f.StringVar(&o.check.effort, "effort", "", "reasoning effort (overrides evaluator.effort): low|medium|high|xhigh|max")
	f.StringVar(&o.check.endpoint, "endpoint", "", "provider endpoint URL; opts in to a non-Anthropic host (also $"+envEvaluatorEndpoint+")")
	f.BoolVar(&o.check.verbose, "verbose", false, "print resolution and usage details to stderr")
	f.BoolVar(&o.check.noCache, "no-cache", false, "do not read or write the evaluation cache (.strspc/cache.db)")
	f.BoolVar(&o.dryRun, "dry-run", false, "report the size of the run without calling the provider")
	return cmd
}

// verifyRule is the outcome for one requested rule.
type verifyRule struct {
	ID          string           `json:"id"`
	State       string           `json:"state"`
	Verdict     string           `json:"verdict"`
	Explanation string           `json:"explanation,omitempty"`
	Confidence  float64          `json:"confidence,omitempty"`
	Evidence    []verifyEvidence `json:"evidence,omitempty"`
}

// verifyEvidence is a provider-cited location. LineConfirmed reports that
// the excerpt was found in the file as sent, at Line; otherwise Line is the
// model's own reading of the line numbers it was shown.
type verifyEvidence struct {
	ruleeval.Location
	LineConfirmed bool `json:"line_confirmed"`
	// ReportedLine is the model's line when it was corrected.
	ReportedLine int `json:"reported_line,omitempty"`
}

// verifyEstimate sizes a run before it is sent. EstRequests is an upper
// bound on a cold cache: a tree that fits one request costs one verdict
// request per rule group and at most one detail request per rule; a larger
// one, one request per part and rule group to collect findings, then one
// per rule group to judge them.
type verifyEstimate struct {
	Files           int            `json:"files"`
	Bytes           int            `json:"bytes"`
	BytesByDir      map[string]int `json:"bytes_by_dir"`
	EstInputTokens  int            `json:"est_input_tokens"`
	EstParts        int            `json:"est_parts"`
	RuleGroups      int            `json:"rule_groups"`
	EstRequests     int            `json:"est_requests"`
	PerRequestLimit int            `json:"per_request_input_tokens"`
}

// verifyOutput is the JSON report of a verify run.
type verifyOutput struct {
	OK          bool                  `json:"ok"`
	ExitCode    int                   `json:"exit_code"`
	DryRun      bool                  `json:"dry_run,omitempty"`
	Provider    string                `json:"provider,omitempty"`
	Model       string                `json:"model,omitempty"`
	Effort      string                `json:"effort,omitempty"`
	Scope       string                `json:"scope"`
	Skipped     []skippedFile         `json:"skipped,omitempty"`
	Estimate    verifyEstimate        `json:"estimate"`
	Usage       *ruleeval.Usage       `json:"usage,omitempty"`
	Rules       []verifyRule          `json:"rules"`
	Diagnostics []explainedDiagnostic `json:"diagnostics"`
	Error       string                `json:"error,omitempty"`
}

// verifyRun is a check run that audits a file set instead of a diff.
type verifyRun struct {
	*checkRun
	files   []ruleeval.File
	skipped []skippedFile
}

func runVerify(ctx context.Context, out, errOut io.Writer, ids []string, o *verifyOptions) error {
	r := &verifyRun{checkRun: &checkRun{ctx: ctx, out: out, errOut: errOut, cwd: o.project, opts: o.check}}
	if o.check.format != formatHuman && o.check.format != formatJSON {
		return withExitCode(exitConfig, fmt.Errorf("invalid --format %q: must be human or json", o.check.format))
	}
	// fail() renders JSON for check's report shape; verify reports its own,
	// so phases report through verifyFail instead.
	if err := r.loadConfig(); err != nil {
		return verifyFail(r.checkRun, exitConfig, err)
	}
	if !o.dryRun {
		code, err := r.buildProvider()
		if err != nil {
			return verifyFail(r.checkRun, code, err)
		}
		if r.static {
			return verifyFail(r.checkRun, exitConfig, errors.New("rule verify needs an AI provider: evaluator.provider is null (or --static-only); set it to claude or pass --provider claude"))
		}
	}
	if err := r.resolveSources(); err != nil {
		return verifyFail(r.checkRun, exitConfig, err)
	}
	// The tree first: buildInputs labels sources relative to r.repoDir, the
	// base the file paths use, so local rules stay scoped to their directory.
	if err := r.collectVerifyTree(o.paths, o.excludes); err != nil {
		return verifyFail(r.checkRun, exitConfig, err)
	}
	// verify judges the named rules whatever evaluate_on says, and
	// buildInputs passes evaluate_on to inherit.Effective, which filters an
	// inheriting entity's own rules by it. nil takes inherit's defaults (D,
	// P, I) rather than every state: a Retired override must not keep hiding
	// the inherited rule it replaced.
	r.evaluateOn = nil
	r.buildInputs()
	selected, err := selectVerifyInputs(r.inputs, ids)
	if err != nil {
		return verifyFail(r.checkRun, exitConfig, err)
	}
	r.inputs = oneVerifyGroup(selected)
	est := estimateVerify(r.files, r.inputs)

	if o.dryRun {
		vo := r.verifyOutput(exitOK, est, nil)
		vo.DryRun = true
		return r.writeVerify(&vo, nil)
	}
	return r.runVerifyEvaluation(est)
}

// collectVerifyTree reads the tracked files of the project into r.files.
func (r *verifyRun) collectVerifyTree(paths, excludes []string) error {
	repoDir, err := gitRoot(r.cwd)
	if err != nil {
		return err
	}
	r.repoDir = repoDir
	subDir, err := repoRelative(repoDir, r.cwd)
	if err != nil {
		return err
	}
	specs, err := treePathspecs(subDir, paths, excludes)
	if err != nil {
		return err
	}
	r.diffSource = diffSource{Kind: diffFromTree, Ref: treeScope(paths, excludes)}
	r.files, r.skipped, err = collectTreeFiles(r.ctx, repoDir, subDir, specs)
	if err != nil {
		return err
	}
	for _, s := range r.skipped {
		r.verbosef("tree: skipped %s (%s)", s.Path, s.Reason)
	}
	if len(r.files) == 0 {
		return fmt.Errorf("no tracked text files to verify in %s", r.diffSource)
	}
	r.verbosef("tree: %s (%d file(s), %d skipped)", r.diffSource, len(r.files), len(r.skipped))
	return nil
}

// treeScope describes the files a verify run reads, e.g. "tracked files
// under src excluding **/*_test.go".
func treeScope(paths, excludes []string) string {
	s := "all tracked files"
	if len(paths) > 0 {
		s = "tracked files under " + strings.Join(paths, ", ")
	}
	if len(excludes) > 0 {
		s += " excluding " + strings.Join(excludes, ", ")
	}
	return s
}

// selectVerifyInputs keeps the inputs whose rule ID is in ids (matched
// case-insensitively). Every requested ID must resolve.
func selectVerifyInputs(inputs []ruleeval.RuleInput, ids []string) ([]ruleeval.RuleInput, error) {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[strings.ToUpper(strings.TrimSpace(id))] = true
	}
	var out []ruleeval.RuleInput
	found := make(map[string]bool, len(ids))
	for i := range inputs {
		id := strings.ToUpper(inputs[i].Rule.ID)
		if want[id] && !found[id] {
			found[id] = true
			out = append(out, inputs[i])
		}
	}
	var missing []string
	for id := range want {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("rule(s) not found in the configured sources: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// verifyGroupID is the EntityID every verified rule is grouped under.
const verifyGroupID = "rule-verify"

// oneVerifyGroup puts every input in a single evaluator group, so each part
// of the tree is sent once with all the rules instead of once per entity:
// groups run concurrently and would each write the same part to the prompt
// cache. The owning entity moves to Owner, which the prompt prints in its
// place ("entity: X"), so the model sees the same rule header.
func oneVerifyGroup(inputs []ruleeval.RuleInput) []ruleeval.RuleInput {
	out := make([]ruleeval.RuleInput, len(inputs))
	for i := range inputs {
		out[i] = inputs[i]
		if out[i].Owner == "" {
			out[i].Owner = out[i].EntityID
		}
		out[i].EntityID = verifyGroupID
	}
	return out
}

// estimateVerify sizes a run the way the Claude provider splits an audit:
// the files, rendered with numbered lines, are cut into parts that fit the
// per-request input budget left after the system prompt and the rules, and
// each entity's rules (up to the per-request rule limit) form a group.
func estimateVerify(files []ruleeval.File, inputs []ruleeval.RuleInput) verifyEstimate {
	est := verifyEstimate{Files: len(files), BytesByDir: map[string]int{}, PerRequestLimit: claude.DefaultMaxInputTokens}
	var rendered strings.Builder
	for _, f := range files {
		est.Bytes += len(f.Content)
		top, _, found := strings.Cut(f.Path, "/")
		if !found {
			top = "."
		}
		est.BytesByDir[top] += len(f.Content)
		lines := contentLines(f.Content)
		rendered.WriteString(prompt.FileSlice(f.Path, 1, len(lines), lines))
	}
	est.EstInputTokens = claude.EstimateTokens(rendered.String())

	perEntity := map[string]int{}
	var rules strings.Builder
	for i := range inputs {
		perEntity[inputs[i].EntityID]++
		rules.WriteString(inputs[i].Rule.Body)
		for _, n := range inputs[i].Notes {
			rules.WriteString(n.Content)
		}
	}
	for _, n := range perEntity {
		est.RuleGroups += (n + claude.DefaultMaxRulesPerRequest - 1) / claude.DefaultMaxRulesPerRequest
	}
	budget := claude.DefaultMaxInputTokens - 256 - claude.EstimateTokens(prompt.AuditSystem()) - claude.EstimateTokens(rules.String())
	budget = max(budget, claude.DefaultMaxInputTokens/4)
	est.EstParts = max(1, (est.EstInputTokens+budget-1)/budget)
	if est.EstParts == 1 {
		est.EstRequests = est.RuleGroups + len(inputs)
	} else {
		est.EstRequests = est.EstParts*est.RuleGroups + est.RuleGroups
	}
	return est
}

// contentLines splits a file's content into its lines, as the provider
// numbers them: a final newline ends the last line rather than opening an
// empty one.
func contentLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// runVerifyEvaluation audits the files and reports one verdict per rule.
func (r *verifyRun) runVerifyEvaluation(est verifyEstimate) error {
	rec := newVerdictRecorder()
	obs := &checkObserver{}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	obs.cancel = cancel

	// Every requested rule is judged whatever its state, and a violation is
	// always reported: verify answers "does the code comply", not "may this
	// change merge". A compliant verdict is detailed too, since it is the
	// evidence a promotion rests on.
	opts := []ruleeval.Option{
		ruleeval.WithFailOn(allStateCodes),
		ruleeval.WithEvaluateStates(allStateCodes),
		ruleeval.WithDetail(ruleeval.DetailCompliant),
		ruleeval.WithConcurrency(3),
		ruleeval.WithObserver(obs),
	}
	if r.opts.noCache {
		r.verbosef("cache: disabled")
	} else if store, closeStore := r.openEvalCache(ctx); store != nil {
		defer closeStore()
		opts = append(opts, ruleeval.WithCache(auditStore{blobs: store}))
	}
	evaluator, err := ruleeval.New(wrapRecording(r.provider, rec), opts...)
	if err != nil {
		return verifyFail(r.checkRun, exitConfig, fmt.Errorf("initializing evaluator: %w", err))
	}
	r.verbosef("evaluator: %s model=%s effort=%s, %d rule(s), ~%d input tokens in ~%d part(s)",
		r.providerName, r.model, orDefault(r.effort, "default"), len(r.inputs), est.EstInputTokens, est.EstParts)

	res, err := evaluator.Audit(ctx, r.inputs, r.files)
	if errors.Is(err, ruleeval.ErrAuditUnsupported) {
		return verifyFail(r.checkRun, exitConfig, fmt.Errorf("provider %q cannot audit code as it stands, which rule verify needs", r.providerName))
	}
	if err != nil {
		return verifyFail(r.checkRun, exitConfig, err)
	}
	usage := evaluator.Usage()
	r.verbosef("usage: %d request(s), %d input / %d output tokens, cache read %d / write %d",
		usage.Requests, usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens)

	if unavailable := obs.unavailableErr(); unavailable != nil {
		return verifyFail(r.checkRun, exitUnavailable, fmt.Errorf("provider %q unavailable: %w", r.providerName, unavailable))
	}
	if calls, failed, first := obs.failures(); calls > 0 && failed == calls {
		return verifyFail(r.checkRun, exitUnavailable, fmt.Errorf("provider %q failed every request (%d of %d): %w", r.providerName, failed, calls, first))
	}

	// RE001 carries nothing the per-rule report lacks; keep the rest.
	var diags []result.Diagnostic
	diags = append(diags, r.notices...)
	for _, d := range res.Diagnostics {
		if d.Code != ruleeval.CodeViolated {
			diags = append(diags, d)
		}
	}

	rules := rec.report(r.inputs)
	relocateEvidence(rules, r.files)
	code := exitOK
	for _, vr := range rules {
		if vr.Verdict == verifyViolated || vr.Verdict == verifyUnknown {
			code = exitViolations
		}
	}
	vo := r.verifyOutput(code, est, diags)
	vo.Rules = rules
	if usage.Requests > 0 {
		vo.Usage = &usage
	}
	return r.writeVerify(&vo, nil)
}

func (r *verifyRun) verifyOutput(code int, est verifyEstimate, diags []result.Diagnostic) verifyOutput {
	vo := verifyOutput{
		OK:          code == exitOK,
		ExitCode:    code,
		Provider:    r.providerName,
		Model:       r.model,
		Effort:      r.effort,
		Scope:       r.diffSource.String(),
		Skipped:     r.skipped,
		Estimate:    est,
		Rules:       []verifyRule{},
		Diagnostics: explainDiagnostics(diags, false),
	}
	if r.provider == nil {
		vo.Provider, vo.Model, vo.Effort = "", "", "" // dry run: nothing was configured
	}
	return vo
}

// writeVerify prints the report and returns the error matching its exit
// code. cause is the fatal error, if any.
func (r *checkRun) writeVerify(vo *verifyOutput, cause error) error {
	if r.opts.format == formatJSON {
		data, err := marshalIndentPrintable(vo)
		if err == nil {
			_, err = r.out.Write(data)
		}
		if err != nil && cause == nil {
			return withExitCode(exitOutput, fmt.Errorf("writing JSON output: %w", err))
		}
	} else {
		r.printVerify(vo)
	}
	switch {
	case cause != nil:
		return withExitCode(vo.ExitCode, cause)
	case vo.ExitCode == exitViolations:
		return withExitCode(exitViolations, errors.New("rule verify: a rule is violated or could not be judged"))
	}
	return nil
}

func marshalIndentPrintable(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(printableJSON(data), '\n'), nil
}

// diagsOf unwraps explained diagnostics for the text renderer.
func diagsOf(in []explainedDiagnostic) []result.Diagnostic {
	out := make([]result.Diagnostic, len(in))
	for i := range in {
		out[i] = in[i].Diagnostic
	}
	return out
}

func (r *checkRun) printVerify(vo *verifyOutput) {
	w := r.out
	est := vo.Estimate
	if vo.DryRun || r.opts.verbose {
		// The scope echoes --path and --exclude: neutralise control characters.
		writeln(w, sanitizeLine(fmt.Sprintf("Scope: %s: %d file(s), %d bytes, ~%d input tokens", vo.Scope, est.Files, est.Bytes, est.EstInputTokens)))
		dirs := make([]string, 0, len(est.BytesByDir))
		for d := range est.BytesByDir {
			dirs = append(dirs, d)
		}
		sort.Slice(dirs, func(i, j int) bool { return est.BytesByDir[dirs[i]] > est.BytesByDir[dirs[j]] })
		for _, d := range dirs {
			writeln(w, fmt.Sprintf("  %-24s %9d bytes", sanitizeLine(path.Clean(d)), est.BytesByDir[d]))
		}
		if len(vo.Skipped) > 0 {
			writeln(w, fmt.Sprintf("Skipped: %d tracked file(s) not read (deleted, symlinked or binary; see --verbose or the JSON report)", len(vo.Skipped)))
		}
		writeln(w, fmt.Sprintf("Estimate: ~%d part(s) of ≤%d input tokens, %d rule group(s): at most ~%d request(s) on a cold cache",
			est.EstParts, est.PerRequestLimit, est.RuleGroups, est.EstRequests))
	}
	if vo.DryRun {
		writeln(w, brandStyle.Render("Dry run: nothing was sent. A rerun reads only the files that changed since the cached run."))
		return
	}
	if len(vo.Diagnostics) > 0 {
		outputDiagnostics(w, &result.Result{Diagnostics: withLineInPath(diagsOf(vo.Diagnostics))}, false)
	}
	counts := map[string]int{}
	for _, vr := range vo.Rules {
		counts[vr.Verdict]++
		line := fmt.Sprintf("%-12s %-15s", vr.ID, vr.Verdict)
		if len(vr.Evidence) > 0 {
			ev := vr.Evidence[0]
			line += fmt.Sprintf(" %s:%d", ev.File, ev.Line)
			if !ev.LineConfirmed {
				line += " (line approximate)"
			}
		}
		if vr.Explanation != "" {
			line += "  " + vr.Explanation
		}
		writeln(w, sanitizeLine(line))
	}
	summary := fmt.Sprintf("Verified %d rule(s) against %s with %s/%s effort=%s: %d compliant, %d violated, %d not applicable, %d unknown",
		len(vo.Rules), vo.Scope, vo.Provider, vo.Model, orDefault(vo.Effort, "default"),
		counts[verifyCompliant], counts[verifyViolated], counts[verifyNotApplicable], counts[verifyUnknown])
	writeln(w, brandStyle.Render(sanitizeLine(summary)))
}

// verifyFail reports a fatal error in the selected format.
func verifyFail(r *checkRun, code int, err error) error {
	var already *exitError
	if errors.As(err, &already) {
		return err
	}
	if r.opts.format == formatJSON {
		vo := verifyOutput{
			ExitCode: code, Rules: []verifyRule{}, Error: err.Error(),
			Diagnostics: explainDiagnostics(r.resolutionDiags, false),
		}
		if r.diffSource.Kind != "" {
			vo.Scope = r.diffSource.String()
		}
		return r.writeVerify(&vo, err)
	}
	if len(r.resolutionDiags) > 0 {
		outputDiagnostics(r.out, &result.Result{Diagnostics: withLineInPath(r.resolutionDiags)}, false)
	}
	return withExitCode(code, err)
}

// relocateEvidence checks evidence line numbers against the files sent. The
// lines are numbered in the prompt, so the model's line should hold its
// excerpt; this confirms it, and catches a miscount. The line is confirmed
// when the reported line holds the excerpt, or corrected when the excerpt
// occurs exactly once; with several other occurrences the model's line is
// kept, unconfirmed, since picking one would be a guess. Only files that
// were sent are consulted, so a path the model invents reads nothing.
func relocateEvidence(rules []verifyRule, files []ruleeval.File) {
	var lines map[string][]string // built on first use
	for i := range rules {
		for j := range rules[i].Evidence {
			ev := &rules[i].Evidence[j]
			needle := excerptNeedle(ev.Excerpt)
			if needle == "" || ev.File == "" {
				continue
			}
			if lines == nil {
				lines = make(map[string][]string, len(files))
				for _, f := range files {
					lines[f.Path] = contentLines(f.Content)
				}
			}
			line, ok := locateExcerpt(lines[path.Clean(ev.File)], needle, ev.Line)
			if !ok {
				continue
			}
			if line != ev.Line {
				ev.ReportedLine = ev.Line
				ev.Line = line
			}
			ev.LineConfirmed = true
		}
	}
}

// minNeedle is the shortest excerpt fragment worth matching: anything
// shorter (a lone brace, `if err`) says nothing about where it was taken.
// Longer but common lines (`return nil`) are left to locateExcerpt, which
// refuses to pick among several matches.
const minNeedle = 8

// excerptNeedle reduces an excerpt to the text to search for: its first
// non-blank line, without the line number and tab the prompt shows it with
// should the model have copied them, cut at an ellipsis the model may use
// to shorten a quote, trimmed. Too short a fragment gives "".
func excerptNeedle(excerpt string) string {
	for _, l := range strings.Split(excerpt, "\n") {
		l = strings.TrimSpace(l)
		if num, rest, found := strings.Cut(l, "\t"); found && num != "" && strings.Trim(num, "0123456789") == "" {
			l = strings.TrimSpace(rest)
		}
		if l == "" {
			continue
		}
		for _, ell := range []string{"...", "…"} {
			if before, _, found := strings.Cut(l, ell); found {
				l = strings.TrimSpace(before)
			}
		}
		if len(l) < minNeedle {
			return ""
		}
		return collapseSpace(l)
	}
	return ""
}

// collapseSpace turns every run of whitespace in s into one space. Models
// quote code with alignment padding (gofmt's) shrunk, so excerpts and lines
// are compared collapsed.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// locateExcerpt returns the 1-based line holding needle (whitespace
// collapsed, as excerptNeedle returns it): want itself when it does, else
// the only line that does. ok is false when no line does, or when several
// do and want is not one of them.
func locateExcerpt(lines []string, needle string, want int) (line int, ok bool) {
	if want >= 1 && want <= len(lines) && strings.Contains(collapseSpace(lines[want-1]), needle) {
		return want, true
	}
	for i, l := range lines {
		if !strings.Contains(collapseSpace(l), needle) {
			continue
		}
		if line != 0 {
			return 0, false // ambiguous
		}
		line = i + 1
	}
	return line, line != 0
}

// verdictRecorder keeps the provider's verdict for each rule: the evaluator
// only turns violations into diagnostics, and verify needs to tell compliant
// from not applicable.
type verdictRecorder struct {
	mu       sync.Mutex
	verdicts map[string]*ruleeval.EvalResponse
	// incomplete lists rules whose batch left part of the tree unjudged
	// (BatchResponse.Errors: a hunk over the input budget, say).
	incomplete map[string]bool
}

func newVerdictRecorder() *verdictRecorder {
	return &verdictRecorder{verdicts: map[string]*ruleeval.EvalResponse{}, incomplete: map[string]bool{}}
}

// put records resp for id. The Claude provider already merges the parts of
// a split tree into one verdict per call, but a provider may report a rule
// more than once, so responses are merged rather than overwritten: violated
// outranks compliant, which outranks not_applicable (the provider's own
// precedence), and violations pool their evidence.
func (v *verdictRecorder) put(id string, resp *ruleeval.EvalResponse) {
	if resp == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	cur, ok := v.verdicts[id]
	switch {
	case !ok || verdictRank(resp.Verdict) > verdictRank(cur.Verdict):
		c := *resp
		c.Evidence = append([]ruleeval.Location(nil), resp.Evidence...)
		v.verdicts[id] = &c
	case resp.Verdict == ruleeval.Violated && cur.Verdict == ruleeval.Violated:
		cur.Evidence = append(cur.Evidence, resp.Evidence...)
		if resp.Confidence > cur.Confidence {
			cur.Confidence = resp.Confidence
		}
		if cur.Explanation == "" {
			cur.Explanation = resp.Explanation
		}
	}
}

// markIncomplete records that ids were not judged against the whole tree.
func (v *verdictRecorder) markIncomplete(ids ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, id := range ids {
		v.incomplete[id] = true
	}
}

func verdictRank(v ruleeval.Verdict) int {
	switch v {
	case ruleeval.Violated:
		return 3
	case ruleeval.Compliant:
		return 2
	case ruleeval.NotApplicable:
		return 1
	}
	return 0
}

func (v *verdictRecorder) report(inputs []ruleeval.RuleInput) []verifyRule {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]verifyRule, 0, len(inputs))
	for i := range inputs {
		rule := inputs[i].Rule
		vr := verifyRule{ID: rule.ID, State: rule.State, Verdict: verifyUnknown}
		if resp, ok := v.verdicts[rule.ID]; ok {
			switch resp.Verdict {
			case ruleeval.Compliant:
				vr.Verdict = verifyCompliant
			case ruleeval.Violated:
				vr.Verdict = verifyViolated
			case ruleeval.NotApplicable:
				vr.Verdict = verifyNotApplicable
			}
			vr.Explanation = resp.Explanation
			vr.Confidence = resp.Confidence
			for _, loc := range resp.Evidence {
				vr.Evidence = append(vr.Evidence, verifyEvidence{Location: loc})
			}
		}
		// A violation found in the parts that were judged stands; anything
		// else says nothing about the parts that were not.
		if v.incomplete[rule.ID] && vr.Verdict != verifyViolated {
			if vr.Verdict != verifyUnknown {
				vr.Explanation = strings.TrimSpace("Only part of the tree was judged (see RE002); the rest could be " +
					"in breach. Partial verdict: " + vr.Verdict + ". " + vr.Explanation)
			}
			vr.Verdict = verifyUnknown
		}
		out = append(out, vr)
	}
	return out
}

// verdictProvider forwards to a provider and records its verdicts. It
// forwards the optional Describer and UsageReporter capabilities too.
type verdictProvider struct {
	inner ruleeval.Provider
	rec   *verdictRecorder
}

func (p *verdictProvider) Evaluate(ctx context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	resp, err := p.inner.Evaluate(ctx, req)
	if err == nil {
		p.rec.put(req.Rule.ID, resp)
	}
	return resp, err
}

func (p *verdictProvider) Model() string {
	if d, ok := p.inner.(ruleeval.Describer); ok {
		return d.Model()
	}
	return ""
}

func (p *verdictProvider) PromptVersion() string {
	if d, ok := p.inner.(ruleeval.Describer); ok {
		return d.PromptVersion()
	}
	return ""
}

// AuditPromptVersion forwards ruleeval.Auditor: "" when the wrapped
// provider cannot audit, which makes Evaluator.Audit refuse it.
func (p *verdictProvider) AuditPromptVersion() string {
	if a, ok := p.inner.(ruleeval.Auditor); ok {
		return a.AuditPromptVersion()
	}
	return ""
}

func (p *verdictProvider) Usage() ruleeval.Usage {
	if u, ok := p.inner.(ruleeval.UsageReporter); ok {
		return u.Usage()
	}
	return ruleeval.Usage{}
}

// verdictBatchProvider is verdictProvider for a BatchProvider, so the
// evaluator keeps its grouped request path.
type verdictBatchProvider struct {
	verdictProvider
	batch ruleeval.BatchProvider
}

func (p *verdictBatchProvider) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) { //nolint:gocritic // ruleeval.BatchProvider signature
	resp, err := p.batch.EvaluateBatch(ctx, req)
	if resp != nil {
		for id, v := range resp.Verdicts {
			p.rec.put(id, v)
		}
		if len(resp.Errors) > 0 {
			ids := make([]string, len(req.Inputs))
			for i := range req.Inputs {
				ids[i] = req.Inputs[i].Rule.ID
			}
			p.rec.markIncomplete(ids...)
		}
	}
	return resp, err
}

// auditStore is the evaluation cache as rule verify uses it: only what the
// provider keeps itself (ruleeval.BlobStore), never whole verdicts. A
// verdict served from the cache never reaches the provider, nor so the
// recorder that tells compliant from not applicable, whereas what an audit
// in parts keeps (each file's findings per rule, and each judgement) comes
// back through the provider's response. An audit that fits one request is
// therefore not cached.
type auditStore struct{ blobs ruleeval.BlobStore }

func (auditStore) Get(string) (*ruleeval.EvalResponse, bool, error) { return nil, false, nil }

func (auditStore) Put(string, *ruleeval.EvalResponse) error { return nil }

func (s auditStore) GetBlob(key string) (value []byte, ok bool, err error) {
	return s.blobs.GetBlob(key)
}

func (s auditStore) PutBlobs(values map[string][]byte) error { return s.blobs.PutBlobs(values) }

// wrapRecording wraps p, exposing EvaluateBatch only when p has it.
func wrapRecording(p ruleeval.Provider, rec *verdictRecorder) ruleeval.Provider {
	base := verdictProvider{inner: p, rec: rec}
	if b, ok := p.(ruleeval.BatchProvider); ok {
		return &verdictBatchProvider{verdictProvider: base, batch: b}
	}
	return &base
}
