package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	RunsOnImplemented bool   `json:"runs_on_implemented"`
}

// coverageMapping is one `standard` note on one rule.
type coverageMapping struct {
	Rule  string `json:"rule"`
	State string `json:"state"`
	Note  string `json:"note"`
}

// coverageClause is one clause of a standard as it stands in one entity.
type coverageClause struct {
	Standard string            `json:"standard"`
	Version  string            `json:"version,omitempty"`
	Clause   string            `json:"clause"`
	Entity   string            `json:"entity"`
	Status   string            `json:"status"`
	Rules    []coverageMapping `json:"rules"`
}

// coverageOutput is the report, in the shape --json prints.
type coverageOutput struct {
	Evaluator  coverageEvaluator    `json:"evaluator"`
	Declared   []entity.StandardRef `json:"declared"`
	Clauses    []coverageClause     `json:"clauses"`
	Unresolved []coverageMapping    `json:"unresolved"`
	Warnings   []string             `json:"warnings"`
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
	)

	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Show how a project's rules map to the clauses of external standards",
		Long: "Show each clause the rules map, per standard: covered when an Implemented rule maps it, mapped\n" +
			"when only a rule in another state does, lapsed when the rule mapping it was retired or replaced,\n" +
			"and cited when the standard is only a source. Each entity is measured over its own rules and\n" +
			"those it inherits, less those replaced.\n\n" +
			"Implemented is a rule's state: the report shows the evaluator setting, which decides whether\n" +
			"anything checks those rules. SteerSpec holds no clause lists, so a clause no rule maps isn't\n" +
			"shown.\n\n" +
			"The report covers the local sources in .strspc/config.yaml that have a realm.json, with their\n" +
			"sub-realms. --all-sources adds the remote ones.\n\n" +
			"Exit codes: 0 when the report is printed; 2 when the configuration can't be read or a source\n" +
			"or dependency doesn't resolve, as a report would then understate the coverage.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStandardsCoverage(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), ".", allSources, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&allSources, "all-sources", false, "include the remote sources in .strspc/config.yaml")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func runStandardsCoverage(ctx context.Context, out, errOut io.Writer, cwd string, allSources, jsonOutput bool) error {
	format := formatHuman
	if jsonOutput {
		format = formatJSON
	}
	r := &checkRun{ctx: ctx, out: out, errOut: errOut, cwd: cwd, opts: checkOptions{format: format}}
	if err := r.loadConfig(); err != nil {
		return r.fail(exitConfig, err)
	}
	r.entries = coverageEntries(r.entries, allSources)
	if len(r.entries) == 0 {
		return r.fail(exitConfig, errors.New("no local rule source in .strspc/config.yaml: pass --all-sources to report on the remote ones"))
	}
	if err := r.resolveRules(); err != nil {
		return r.fail(exitConfig, err)
	}

	report := r.coverageReport()
	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling report: %w", err)
		}
		writeln(out, string(data))
		return nil
	}
	for _, w := range report.Warnings {
		writeWarning(errOut, "%s", w)
	}
	writeCoverage(out, report)
	return nil
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
// counts once, also when a source and its dependency are both configured.
func (r *checkRun) coverageReport() *coverageOutput {
	var entities []*realmresolve.Inheritance
	seen := make(map[string]bool)
	for _, e := range r.entries {
		tree := r.realmTrees[e.Source]
		if tree == nil {
			continue
		}
		for _, in := range tree.resolved.Entities {
			if id := in.Entity.Entity.Entity.ID; !seen[id] {
				seen[id] = true
				entities = append(entities, in)
			}
		}
	}

	report := coverage.Compute(entities)
	out := &coverageOutput{
		Evaluator:  r.coverageEvaluator(),
		Declared:   append([]entity.StandardRef{}, report.Declared...),
		Clauses:    make([]coverageClause, 0, len(report.Clauses)),
		Unresolved: coverageMappings(report.Unresolved),
		Warnings:   r.coverageWarnings(),
	}
	for i := range report.Clauses {
		c := &report.Clauses[i]
		out.Clauses = append(out.Clauses, coverageClause{
			Standard: c.Standard,
			Version:  c.Version,
			Clause:   c.Clause,
			Entity:   c.Entity,
			Status:   string(c.Status),
			Rules:    coverageMappings(c.Rules),
		})
	}
	return out
}

func coverageMappings(mappings []coverage.Mapping) []coverageMapping {
	out := make([]coverageMapping, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, coverageMapping{Rule: m.Rule, State: m.State, Note: m.Note})
	}
	return out
}

func (r *checkRun) coverageEvaluator() coverageEvaluator {
	name := strings.TrimSpace(r.cfg.Evaluator.Provider)
	if isStaticProviderName(name) {
		return coverageEvaluator{Provider: "null"}
	}
	states := r.evaluateOn
	if states == nil {
		states = ruleeval.DefaultEvaluateStates
	}
	return coverageEvaluator{Provider: name, RunsOnImplemented: slices.Contains(states, entity.StateImplemented)}
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
	for _, rf := range r.resolved {
		if _, ok := r.sourceRealms[rf.Origin.Source]; !ok {
			notes[rf.Origin.Source] += countStandardNotes(rf.File)
		}
	}
	for _, e := range r.entries {
		if n := notes[e.Source]; n > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: %d `standard` note(s) left out: the source has no realm.json", e.Source, n))
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
	return groups
}

func writeCoverage(w io.Writer, report *coverageOutput) {
	writeln(w, coverageEvaluatorLine(report.Evaluator))
	for _, g := range coverageGroups(report) {
		writeln(w, "")
		heading := g.id
		if g.version != "" {
			heading += " " + g.version
		}
		writeln(w, sanitizeLine(fmt.Sprintf("%s (%s)", heading, strings.Join(g.relations, ", "))))
		for _, u := range g.urls {
			writeln(w, sanitizeLine("  "+u))
		}
		if len(g.clauses) == 0 {
			writeln(w, "  no clause mapped")
			continue
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for i := range g.clauses {
			c := &g.clauses[i]
			rules := make([]string, 0, len(c.Rules))
			for _, m := range c.Rules {
				rules = append(rules, fmt.Sprintf("%s (%s)", m.Rule, stateLabel(m.State)))
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", sanitizeLine(c.Clause), c.Entity, c.Status, sanitizeLine(strings.Join(rules, ", ")))
		}
		_ = tw.Flush()
	}
	if len(report.Unresolved) > 0 {
		writeln(w, "\nNotes that resolve to no declared standard, not counted:")
		for _, m := range report.Unresolved {
			writeln(w, sanitizeLine(fmt.Sprintf("  %s (note %s)", m.Rule, m.Note)))
		}
	}
	writeln(w, "\ncovered: an Implemented rule maps the clause. mapped: only a rule in another state does.\n"+
		"lapsed: the rule mapping it was retired or replaced. cited: the standard is a source, with no claim.\n"+
		"SteerSpec holds no clause lists, so a clause no rule maps isn't shown.")
}

func coverageEvaluatorLine(e coverageEvaluator) string {
	switch {
	case e.Provider == "null":
		return "Evaluator: null, so nothing evaluates Implemented rules"
	case e.RunsOnImplemented:
		return sanitizeLine(fmt.Sprintf("Evaluator: %s, runs on Implemented rules", e.Provider))
	default:
		return sanitizeLine(fmt.Sprintf("Evaluator: %s, not run on Implemented rules (evaluate_on)", e.Provider))
	}
}
