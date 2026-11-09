package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/coverage"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// coverageEvaluator is the evaluator setting, which says what "Implemented"
// amounts to in this project: a rule in that state may get no evaluator run.
type coverageEvaluator struct {
	Provider          string `json:"provider"`
	Usable            bool   `json:"usable"`
	RunsOnImplemented bool   `json:"runs_on_implemented"`
}

// coverageMapping is one `standard` note on one rule.
type coverageMapping struct {
	Rule  string `json:"rule"`
	State string `json:"state"`
	Note  string `json:"note"`
}

// coverageExclusion is the `exclusion` Realm Note behind an excluded clause
// (STDREF-022): the manifest holding it, its ID and its justification.
type coverageExclusion struct {
	Realm   string `json:"realm"`
	Note    string `json:"note"`
	Content string `json:"content"`
}

// coverageClause is one clause of a standard as it stands in one entity. Realm
// is the ID of the Realm tree holding the entity, which a --baseline needs to
// match the clause.
type coverageClause struct {
	Standard  string             `json:"standard"`
	Version   string             `json:"version,omitempty"`
	Clause    string             `json:"clause"`
	Realm     string             `json:"realm"`
	Entity    string             `json:"entity"`
	Status    string             `json:"status"`
	Rules     []coverageMapping  `json:"rules"`
	Exclusion *coverageExclusion `json:"exclusion,omitempty"`
}

// coverageFigure is the coverage figure of a Standard Reference that links a
// Standard Realm (STDREF-024, STDREF-025): Covered of the clauses of the
// linked list, narrowed to the profile, less those excluded.
type coverageFigure struct {
	Tree string `json:"tree"`
	// Entity is the entity measured, empty for the Realm figure, which counts
	// a clause covered in any entity of the tree.
	Entity   string `json:"entity,omitempty"`
	Standard string `json:"standard"`
	Version  string `json:"version,omitempty"`
	Realm    string `json:"realm"`
	Profile  string `json:"profile,omitempty"`
	// DeclaredBy is the realm_id of the manifest declaring the reference,
	// empty when an entity declares it.
	DeclaredBy string `json:"declared_by,omitempty"`
	// Total is the number of clauses of the list, before exclusions. The
	// others count the clauses not excluded, and add up to Total less
	// Excluded.
	Total    int `json:"total"`
	Excluded int `json:"excluded"`
	Covered  int `json:"covered"`
	Mapped   int `json:"mapped"`
	Lapsed   int `json:"lapsed"`
	Unmapped int `json:"unmapped"`
	// Figure is Covered over Total less Excluded, null when that is 0
	// (STDREF-028), so an undefined figure never reads as 0 or 1.
	Figure *float64 `json:"figure"`
}

// coverageOutput is the report, in the shape --json prints.
type coverageOutput struct {
	Evaluator  coverageEvaluator    `json:"evaluator"`
	Declared   []entity.StandardRef `json:"declared"`
	Clauses    []coverageClause     `json:"clauses"`
	Figures    []coverageFigure     `json:"figures"`
	Unresolved []coverageMapping    `json:"unresolved"`
	Warnings   []string             `json:"warnings"`
	// Gate is the verdict of the gate flags, absent when none is set.
	Gate *coverageGate `json:"gate,omitempty"`
}

func newStandardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "standards",
		Short: "Report on the external standards a project's rules map to",
		Long:  "Commands that report on the external standards a project's Realms declare and its rules map to.",
	}
	cmd.AddCommand(newStandardsCoverageCmd())
	return cmd
}

func newStandardsCoverageCmd() *cobra.Command {
	var (
		allSources bool
		jsonOutput bool
		gate       coverageGateFlags
	)

	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Show how a project's rules map to the clauses of external standards",
		Long: "Show each clause the rules map, per standard: covered when an Implemented rule maps it, mapped\n" +
			"when only a rule in another state does, lapsed when the rule mapping it was retired or replaced,\n" +
			"and cited when the standard is only a source. Each entity is measured over its own rules and\n" +
			"those it inherits, less those replaced.\n\n" +
			"Implemented is a rule's state: the report shows the evaluator setting, which decides whether\n" +
			"anything checks those rules.\n\n" +
			"A reference linked to a Standard Realm with `realm` is measured against that Realm's clause list,\n" +
			"narrowed to its `profile` (STDREF-020). A clause of the list no rule maps is unmapped, and one an\n" +
			"exclusion Realm Note names is excluded (STDREF-022, STDREF-023). Each entity, and the Realm as a\n" +
			"whole, gets a figure: the clauses covered over those of the list less the excluded (STDREF-024,\n" +
			"STDREF-025), undefined when none is left (STDREF-028). An implements reference only:\n" +
			"derived_from gets none (STDREF-026). Without a linked Standard Realm, a clause no rule maps\n" +
			"isn't shown: SteerSpec holds no clause lists.\n\n" +
			"The report covers the local sources in .strspc/config.yaml that have a realm.json, with their\n" +
			"sub-realms. --all-sources adds the remote ones.\n\n" +
			"The gate flags make the command fail a build when coverage weakens. --fail-on fails on each\n" +
			"clause lapsed or only mapped, and on each `standard` note resolving to no declared standard\n" +
			"(unresolved); a cited clause never fails. --require fails unless the standard, or that edition\n" +
			"of it, is declared as implements with a covered clause. --require-evaluator fails when clauses\n" +
			"are covered but nothing evaluates Implemented rules. --min-coverage fails a Realm figure under\n" +
			"the percentage, given for every standard or as <id>[@<version>]=<percent> for one, and a\n" +
			"threshold with no figure to check; an undefined figure is only a warning.\n\n" +
			"--baseline fails on each regression against an earlier --json report, such as one the base\n" +
			"branch recorded with the same sources: a clause covered there and mapped or lapsed here\n" +
			"(downgraded), a covered or mapped clause no longer claimed (lost) or now excluded (excluded),\n" +
			"an implements standard no longer declared (dropped) or moved to another edition (rebased), and\n" +
			"a note that resolved and no longer does (unresolved). --allow-rebase and --allow-excluded make\n" +
			"those two kinds warnings. A report written before clauses had a realm can't be a baseline:\n" +
			"record it again. When only one of several Realm trees upgrades an edition, its clauses show as\n" +
			"lost, not rebased.\n\n" +
			"The report then ends with the gate's verdict, the `gate` object of --json. The STDREF rules\n" +
			"defining the statuses and figures are Draft in dev.steerspec.core.\n\n" +
			"Exit codes: 0 when the report is printed and any gate passes; 1 when the gate fails; 2 when the\n" +
			"configuration, a gate flag or the baseline can't be read or a source or dependency doesn't\n" +
			"resolve, as a report would then understate the coverage; 4 when the JSON report can't be\n" +
			"written.\n\n" +
			"Examples:\n" +
			"  strspc standards coverage --fail-on lapsed,mapped,unresolved\n" +
			"  strspc standards coverage --require owasp.asvs@5.0.0 --require-evaluator\n" +
			"  strspc standards coverage --min-coverage 60 --min-coverage owasp.asvs=80 --json\n" +
			"  strspc standards coverage --baseline baseline/coverage.json --allow-rebase",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gate.baselineSet = cmd.Flags().Changed("baseline")
			return runStandardsCoverage(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), ".", allSources, jsonOutput, &gate)
		},
	}
	cmd.Flags().BoolVar(&allSources, "all-sources", false, "include the remote sources in .strspc/config.yaml")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().StringSliceVar(&gate.failOn, "fail-on", nil, "fail on clauses or notes in these states: lapsed, mapped, unresolved")
	cmd.Flags().StringArrayVar(&gate.require, "require", nil, "fail unless this standard, <id>[@<version>], is implemented with a covered clause (repeatable)")
	cmd.Flags().BoolVar(&gate.requireEvaluator, "require-evaluator", false, "fail when clauses are covered but nothing evaluates Implemented rules")
	cmd.Flags().StringArrayVar(&gate.minCoverage, "min-coverage", nil, "fail a Realm figure under this percentage: <percent>, or <id>[@<version>]=<percent> for one standard (repeatable)")
	cmd.Flags().StringVar(&gate.baseline, "baseline", "", "fail on each regression against this earlier standards coverage --json report")
	cmd.Flags().BoolVar(&gate.allowRebase, "allow-rebase", false, "with --baseline, warn instead of failing when a standard moved to another edition")
	cmd.Flags().BoolVar(&gate.allowExcluded, "allow-excluded", false, "with --baseline, warn instead of failing when an exclusion note now excludes a clause")
	return cmd
}

func runStandardsCoverage(ctx context.Context, out, errOut io.Writer, cwd string, allSources, jsonOutput bool, gateFlags *coverageGateFlags) error {
	format := formatHuman
	if jsonOutput {
		format = formatJSON
	}
	r := &checkRun{ctx: ctx, out: out, errOut: errOut, cwd: cwd, opts: checkOptions{format: format}}
	gate, err := parseCoverageGate(gateFlags)
	if err != nil {
		return r.fail(exitConfig, err)
	}
	if gate.baselinePath != "" {
		if gate.baseline, err = loadCoverageBaseline(cwd, gate.baselinePath); err != nil {
			return r.fail(exitConfig, err)
		}
	}
	if err := r.loadConfig(); err != nil {
		return r.fail(exitConfig, err)
	}
	evaluator := r.coverageEvaluator()
	r.entries = coverageEntries(r.entries, allSources)
	if len(r.entries) == 0 {
		return r.fail(exitConfig, errors.New("no local rule source in .strspc/config.yaml: pass --all-sources to report on the remote ones"))
	}
	if err := r.resolveRules(); err != nil {
		return r.fail(exitConfig, err)
	}

	report := r.coverageReport(evaluator)
	if gate.enabled() {
		report.Gate = evaluateCoverageGate(report, gate)
		report.Warnings = append(report.Warnings, report.Gate.warnings...)
	}
	if jsonOutput {
		if err := writeJSONOutput(out, report); err != nil {
			return err
		}
		return coverageGateError(report.Gate)
	}
	for _, w := range report.Warnings {
		writeWarning(errOut, "%s", w)
	}
	writeCoverage(out, report)
	if report.Gate != nil {
		writeCoverageGate(out, report.Gate)
	}
	return coverageGateError(report.Gate)
}

// coverageGateError is the error of a failed gate, exit 1, or nil.
func coverageGateError(g *coverageGate) error {
	if g == nil || g.Passed {
		return nil
	}
	return withExitCode(exitViolations, fmt.Errorf("standards coverage: the gate failed with %d failure(s)", len(g.Failures)))
}

// coverageEntries selects the sources to report on: the local ones, so a
// consumer of the core rules doesn't see their mappings as its own, or all.
func coverageEntries(entries []ruleresolve.SourceEntry, all bool) []ruleresolve.SourceEntry {
	if all {
		return entries
	}
	return slices.DeleteFunc(slices.Clone(entries), func(e ruleresolve.SourceEntry) bool {
		return strings.Contains(e.Source, "://")
	})
}

// coverageReport builds the report from what resolveRules left. Every entity
// counts once, also when a source is listed twice, and so does each Realm tree
// added to the links. Two sources can't share an EUID: resolution refuses it
// (RSV005).
func (r *checkRun) coverageReport(evaluator coverageEvaluator) *coverageOutput {
	var entities []*realmresolve.Inheritance
	seen := make(map[string]bool)
	links := coverage.NewLinks()
	added := make(map[string]bool)
	warnings := r.coverageWarnings()
	for _, e := range r.entries {
		tree := r.realmTrees[e.Source]
		if tree == nil {
			continue
		}
		treeKey := tree.dir
		if abs, err := filepath.Abs(tree.dir); err == nil {
			treeKey = abs
		}
		if !added[treeKey] {
			added[treeKey] = true
			if err := links.AddTree(tree.dir, tree.manifest, tree.resolved); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: no coverage figure: %v", e.Source, err))
			}
		}
		for _, in := range tree.resolved.Entities {
			if id := in.Entity.Entity.Entity.ID; !seen[id] {
				seen[id] = true
				entities = append(entities, in)
			}
		}
	}

	report := coverage.Compute(entities, coverage.WithLinks(links))
	out := &coverageOutput{
		Evaluator:  evaluator,
		Declared:   append([]entity.StandardRef{}, report.Declared...),
		Clauses:    make([]coverageClause, 0, len(report.Clauses)),
		Figures:    coverageFigures(report.Figures),
		Unresolved: coverageMappings(report.Unresolved),
		Warnings:   append(warnings, report.Warnings...),
	}
	for i := range report.Clauses {
		c := &report.Clauses[i]
		out.Clauses = append(out.Clauses, coverageClause{
			Standard: c.Standard,
			Version:  c.Version,
			Clause:   c.Clause,
			Realm:    c.Realm,
			Entity:   c.Entity,
			Status:   string(c.Status),
			Rules:    coverageMappings(c.Rules),
		})
		if ex := c.Exclusion; ex != nil {
			out.Clauses[len(out.Clauses)-1].Exclusion = &coverageExclusion{Realm: ex.Realm, Note: ex.Note, Content: ex.Content}
		}
	}
	return out
}

func coverageFigures(figs []coverage.Figure) []coverageFigure {
	out := make([]coverageFigure, 0, len(figs))
	for i := range figs {
		f := &figs[i]
		cf := coverageFigure{
			Tree: f.Tree, Entity: f.Entity, Standard: f.Standard, Version: f.Version, Realm: f.Realm, Profile: f.Profile,
			DeclaredBy: f.DeclaredBy, Total: f.Total + f.Excluded, Excluded: f.Excluded,
			Covered: f.Covered, Mapped: f.Mapped, Lapsed: f.Lapsed, Unmapped: f.Unmapped,
		}
		if f.Defined {
			v := float64(f.Covered) / float64(f.Total)
			cf.Figure = &v
		}
		out = append(out, cf)
	}
	return out
}

// coveragePercent renders covered of total as a percentage, truncated to one
// decimal, never rounded, so 100 % shows only when every clause is covered.
// It reads "undefined" when total is 0 (STDREF-028).
func coveragePercent(covered, total int) string {
	if total <= 0 {
		return "undefined"
	}
	tenths := covered * 1000 / total
	return fmt.Sprintf("%d.%d %%", tenths/10, tenths%10)
}

func coverageMappings(mappings []coverage.Mapping) []coverageMapping {
	out := make([]coverageMapping, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, coverageMapping{Rule: m.Rule, State: m.State, Note: m.Note})
	}
	return out
}

// coverageEvaluator reads the evaluator setting through check's own path. A
// provider that lacks its API key here counts as usable: CI holds the key, not
// this machine.
func (r *checkRun) coverageEvaluator() coverageEvaluator {
	code, err := r.buildProvider()
	switch {
	case r.static:
		return coverageEvaluator{Provider: "null"}
	case err != nil && code != exitUnavailable:
		return coverageEvaluator{Provider: r.providerName}
	}
	states := r.evaluateOn
	if states == nil {
		states = ruleeval.DefaultEvaluateStates
	}
	return coverageEvaluator{Provider: r.providerName, Usable: true, RunsOnImplemented: slices.Contains(states, entity.StateImplemented)}
}

// coverageWarnings lists the resolution notices, and each source whose
// `standard` notes the report can't reach: without a manifest a source has no
// Realm to declare or resolve a standard in.
func (r *checkRun) coverageWarnings() []string {
	warnings := []string{}
	for _, d := range r.notices {
		warnings = append(warnings, d.Code+": "+d.Message)
	}
	notes := make(map[string]int)
	seen := make(map[string]bool)
	for _, rf := range r.resolved {
		if _, ok := r.sourceRealms[rf.Origin.Source]; !ok && !seen[rf.Path] {
			seen[rf.Path] = true
			notes[rf.Origin.Source] += countStandardNotes(rf.File)
		}
	}
	for _, e := range r.entries {
		if n := notes[e.Source]; n > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: %d `standard` note(s) left out: the source has no realm.json", e.Source, n))
			delete(notes, e.Source)
		}
	}
	return warnings
}

func countStandardNotes(f *entity.File) int {
	var n int
	for i := range f.Notes {
		if f.Notes[i].Type == entity.NoteTypeStandard {
			n++
		}
	}
	for i := range f.SubEntities {
		n += countStandardNotes(&f.SubEntities[i])
	}
	return n
}

// coverageGroup is what the report prints under one standard and edition.
type coverageGroup struct {
	id, version string
	relations   []string
	urls        []string
	clauses     []coverageClause
	figures     []coverageFigure
}

// coverageGroups groups the report by standard and edition, in the order of
// report.Declared. A clause's identity holds no relation, so references that
// differ only in relation or url share a group.
func coverageGroups(report *coverageOutput) []*coverageGroup {
	var groups []*coverageGroup
	find := func(id, version string) *coverageGroup {
		i := slices.IndexFunc(groups, func(g *coverageGroup) bool { return g.id == id && g.version == version })
		if i >= 0 {
			return groups[i]
		}
		g := &coverageGroup{id: id, version: version}
		groups = append(groups, g)
		return g
	}
	for _, ref := range report.Declared {
		g := find(ref.ID, ref.Version)
		if !slices.Contains(g.relations, ref.Relation) {
			g.relations = append(g.relations, ref.Relation)
		}
		if ref.URL != "" && !slices.Contains(g.urls, ref.URL) {
			g.urls = append(g.urls, ref.URL)
		}
	}
	for i := range report.Clauses {
		c := &report.Clauses[i]
		g := find(c.Standard, c.Version)
		g.clauses = append(g.clauses, *c)
	}
	for i := range report.Figures {
		f := &report.Figures[i]
		g := find(f.Standard, f.Version)
		g.figures = append(g.figures, *f)
	}
	return groups
}

// coverageCell makes repository text safe in one cell of the report: on one
// line, with no tab to break a column, and unable to drive a terminal or a CI
// runner. A runner ignores leading spaces, so no cell can start a command.
func coverageCell(s string) string {
	s = printableDiagnostic(sanitizeLine(s))
	if strings.HasPrefix(strings.TrimSpace(s), "::") {
		return `\` + s
	}
	return s
}

func writeCoverage(w io.Writer, report *coverageOutput) {
	writeln(w, coverageEvaluatorLine(report.Evaluator))
	for _, g := range coverageGroups(report) {
		writeln(w, "")
		heading := g.id
		if g.version != "" {
			heading += " " + g.version
		}
		writeln(w, coverageCell(fmt.Sprintf("%s (%s)", heading, strings.Join(g.relations, ", "))))
		for _, u := range g.urls {
			writeln(w, "  "+coverageCell(u))
		}
		writeCoverageFigures(w, g.figures)
		if len(g.clauses) == 0 {
			writeln(w, "  no clause mapped")
			continue
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for i := range g.clauses {
			c := &g.clauses[i]
			rules := make([]string, 0, len(c.Rules)+1)
			if ex := c.Exclusion; ex != nil {
				rules = append(rules, fmt.Sprintf("excluded by %s (%s)", ex.Note, ex.Realm))
			}
			for _, m := range c.Rules {
				rules = append(rules, fmt.Sprintf("%s (%s)", m.Rule, stateLabel(m.State)))
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", coverageCell(c.Clause), coverageCell(c.Entity), c.Status, coverageCell(strings.Join(rules, ", ")))
		}
		_ = tw.Flush()
		writeCoverageExclusions(w, g)
	}
	if len(report.Unresolved) > 0 {
		writeln(w, "\nNotes that resolve to no declared standard, not counted:")
		for _, m := range report.Unresolved {
			writeln(w, fmt.Sprintf("  %s (note %s)", coverageCell(m.Rule), coverageCell(m.Note)))
		}
	}
	legend := "\ncovered: an Implemented rule maps the clause. mapped: only a rule in another state does.\n" +
		"lapsed: the rule mapping it was retired or replaced. cited: the standard is a source, with no claim.\n"
	if len(report.Figures) == 0 {
		writeln(w, legend+"SteerSpec holds no clause lists, so a clause no rule maps isn't shown.")
		return
	}
	writeln(w, legend+
		"excluded: a Realm Note excludes the clause. unmapped: no rule maps a clause of the linked list.\n"+
		"A figure is covered of the linked list less the excluded clauses, undefined when none is left.\n"+
		"Without a linked Standard Realm, a clause no rule maps isn't shown.")
}

// writeCoverageFigures prints the figures of one standard: for each linked
// Standard Realm, its clause list and the Realm figure, then each entity's.
func writeCoverageFigures(w io.Writer, figs []coverageFigure) {
	for i := range figs {
		f := &figs[i]
		if f.Entity != "" {
			writeln(w, "    "+coverageCell(f.Entity)+": "+figureText(f))
			continue
		}
		list := "Standard Realm " + f.Realm
		if f.Profile != "" {
			list += ", profile " + f.Profile
		}
		list += fmt.Sprintf(": %d clause(s)", f.Total)
		if f.Excluded > 0 {
			list += fmt.Sprintf(", %d excluded", f.Excluded)
		}
		writeln(w, "  "+coverageCell(list))
		realm := "Realm " + f.Tree
		if f.DeclaredBy != "" && f.DeclaredBy != f.Tree {
			realm += " (declared by " + f.DeclaredBy + ")"
		}
		writeln(w, "  "+coverageCell(realm)+": "+figureText(f))
	}
}

// writeCoverageExclusions lists, once each, the exclusions behind the excluded
// clauses of a group, with their justification.
func writeCoverageExclusions(w io.Writer, g *coverageGroup) {
	type key struct{ realm, note string }
	seen := make(map[key]bool)
	var lines []string
	for i := range g.clauses {
		c := &g.clauses[i]
		ex := c.Exclusion
		if ex == nil || seen[key{ex.Realm, ex.Note}] {
			continue
		}
		seen[key{ex.Realm, ex.Note}] = true
		lines = append(lines, "    "+coverageCell(fmt.Sprintf("%s:%s  %s (%s): %s", c.Standard, c.Clause, ex.Note, ex.Realm, ex.Content)))
	}
	if len(lines) == 0 {
		return
	}
	writeln(w, "  Exclusions:")
	for _, l := range lines {
		writeln(w, l)
	}
}

// figureText renders a figure: covered of the clauses counted, or undefined
// when none is counted (STDREF-028), never 0 % or 100 %: every clause is
// excluded, or the profile lists none.
func figureText(f *coverageFigure) string {
	counted := f.Total - f.Excluded
	switch {
	case counted > 0:
	case f.Total == 0:
		return "undefined, the clause list is empty"
	default:
		return "undefined, every clause is excluded"
	}
	return fmt.Sprintf("%d of %d covered (%s)", f.Covered, counted, coveragePercent(f.Covered, counted))
}

func coverageEvaluatorLine(e coverageEvaluator) string {
	switch {
	case e.Provider == "null":
		return "Evaluator: null, so nothing evaluates Implemented rules"
	case !e.Usable:
		return coverageCell(fmt.Sprintf("Evaluator: %s, which strspc can't run, so nothing evaluates Implemented rules", e.Provider))
	case e.RunsOnImplemented:
		return coverageCell(fmt.Sprintf("Evaluator: %s, runs on Implemented rules", e.Provider))
	default:
		return coverageCell(fmt.Sprintf("Evaluator: %s, not run on Implemented rules (evaluate_on)", e.Provider))
	}
}
