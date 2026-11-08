package cmd

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/coverage"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// Kinds of coverage gate failure, the `kind` of each failure in --json.
const (
	gateLapsed      = string(coverage.StatusLapsed)
	gateMapped      = string(coverage.StatusMapped)
	gateUnresolved  = "unresolved"
	gateRequired    = "required"
	gateEvaluator   = "evaluator"
	gateMinCoverage = "min_coverage"
)

// coverageGateFlags holds the gate flags of standards coverage as given.
type coverageGateFlags struct {
	failOn           []string
	require          []string
	requireEvaluator bool
	minCoverage      []string
}

// standardSelector names a standard, and an edition of it unless version is
// empty, which matches any edition.
type standardSelector struct {
	id, version string
}

func (s standardSelector) matches(id, version string) bool {
	return s.id == id && (s.version == "" || s.version == version)
}

func (s standardSelector) String() string {
	if s.version == "" {
		return s.id
	}
	return s.id + "@" + s.version
}

// coverageThreshold is one --min-coverage: a minimum Realm figure in tenths of
// a percent, for the standards standard selects, or every one when it is nil.
type coverageThreshold struct {
	standard *standardSelector
	tenths   int
}

// coverageGateOptions is what the gate flags ask for, once parsed.
type coverageGateOptions struct {
	failOn           []string
	require          []standardSelector
	requireEvaluator bool
	minCoverage      []coverageThreshold
}

func (o *coverageGateOptions) enabled() bool {
	return len(o.failOn) > 0 || len(o.require) > 0 || o.requireEvaluator || len(o.minCoverage) > 0
}

// coverageGate is the gate's verdict, the `gate` object of --json.
type coverageGate struct {
	Passed   bool                  `json:"passed"`
	Failures []coverageGateFailure `json:"failures"`
	// warnings are the thresholds left unchecked, which join the report's.
	warnings []string
}

// coverageGateFailure is one reason the gate failed. Kind is one of the gate*
// constants; the other fields say what failed, where they apply.
type coverageGateFailure struct {
	Kind      string            `json:"kind"`
	Message   string            `json:"message"`
	Standard  string            `json:"standard,omitempty"`
	Version   string            `json:"version,omitempty"`
	Clause    string            `json:"clause,omitempty"`
	Entity    string            `json:"entity,omitempty"`
	Tree      string            `json:"tree,omitempty"`
	Rules     []coverageMapping `json:"rules,omitempty"`
	Figure    *float64          `json:"figure,omitempty"`
	Threshold *float64          `json:"threshold,omitempty"`
}

// parseCoverageGate checks the gate flags and parses them.
func parseCoverageGate(f *coverageGateFlags) (*coverageGateOptions, error) {
	opts := &coverageGateOptions{requireEvaluator: f.requireEvaluator}
	var err error
	if opts.failOn, err = parseFailOnStatuses(f.failOn); err != nil {
		return nil, err
	}
	for _, v := range f.require {
		sel, err := parseStandardSelector("--require", v)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(opts.require, sel) {
			opts.require = append(opts.require, sel)
		}
	}
	if opts.minCoverage, err = parseMinCoverage(f.minCoverage); err != nil {
		return nil, err
	}
	return opts, nil
}

// parseFailOnStatuses checks the values of --fail-on: lapsed, mapped and
// unresolved. A blank value is ignored.
func parseFailOnStatuses(values []string) ([]string, error) {
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		switch v {
		case "":
			continue
		case gateLapsed, gateMapped, gateUnresolved:
		case string(coverage.StatusCited):
			return nil, errors.New("--fail-on cited: a cited clause claims no coverage (STDREF-006), so it never fails the gate")
		case string(coverage.StatusUnmapped):
			return nil, errors.New("--fail-on unmapped: a clause is unmapped in each entity that doesn't map it, " +
				"though another may cover it; gate a linked Standard Realm's figure with --min-coverage")
		default:
			return nil, fmt.Errorf("--fail-on %q: must be one of %s, %s, %s", v, gateLapsed, gateMapped, gateUnresolved)
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out, nil
}

// parseStandardSelector parses <id>[@<version>].
func parseStandardSelector(flag, v string) (standardSelector, error) {
	if err := rejectControlChars(flag, v); err != nil {
		return standardSelector{}, err
	}
	id, version, hasVersion := strings.Cut(v, "@")
	if id == "" || (hasVersion && version == "") || strings.ContainsAny(v, " \t") {
		return standardSelector{}, fmt.Errorf("%s %q: want <id> or <id>@<version>", flag, v)
	}
	return standardSelector{id: id, version: version}, nil
}

var percentPattern = regexp.MustCompile(`^(\d{1,3})(?:\.(\d))?$`)

// parsePercent parses a percentage from 0 to 100, with at most one decimal and
// an optional %, into tenths of a percent.
func parsePercent(s string) (int, bool) {
	m := percentPattern.FindStringSubmatch(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%")))
	if m == nil {
		return 0, false
	}
	whole, _ := strconv.Atoi(m[1])
	tenths := whole * 10
	if m[2] != "" {
		tenths += int(m[2][0] - '0')
	}
	return tenths, tenths <= 1000
}

// parseMinCoverage parses the values of --min-coverage, each a bare
// <percent> or <id>[@<version>]=<percent>. A standard, or the bare value, can
// be given once.
func parseMinCoverage(values []string) ([]coverageThreshold, error) {
	var out []coverageThreshold
	for _, v := range values {
		t := coverageThreshold{}
		pct := v
		if name, p, ok := strings.Cut(v, "="); ok {
			sel, err := parseStandardSelector("--min-coverage", name)
			if err != nil {
				return nil, err
			}
			t.standard, pct = &sel, p
		}
		tenths, ok := parsePercent(pct)
		if !ok {
			return nil, fmt.Errorf("--min-coverage %q: want a percentage from 0 to 100 with at most one decimal, "+
				"as <percent> or <id>[@<version>]=<percent>", v)
		}
		t.tenths = tenths
		if slices.ContainsFunc(out, func(o coverageThreshold) bool { return sameSelector(o.standard, t.standard) }) {
			return nil, fmt.Errorf("--min-coverage %q: a minimum for %s is already given", v, thresholdScope(t.standard))
		}
		out = append(out, t)
	}
	return out, nil
}

func sameSelector(a, b *standardSelector) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func thresholdScope(s *standardSelector) string {
	if s == nil {
		return "every standard"
	}
	return s.String()
}

func tenthsText(tenths int) string {
	return fmt.Sprintf("%d.%d %%", tenths/10, tenths%10)
}

// evaluateCoverageGate checks report against opts: the evaluator, the required
// standards, the clauses and notes --fail-on names, then the figures.
func evaluateCoverageGate(report *coverageOutput, opts *coverageGateOptions) *coverageGate {
	g := &coverageGate{Failures: []coverageGateFailure{}}
	if opts.requireEvaluator {
		g.checkEvaluator(report)
	}
	for _, sel := range opts.require {
		g.checkRequired(report, sel)
	}
	for i := range report.Clauses {
		c := &report.Clauses[i]
		if slices.Contains(opts.failOn, c.Status) {
			g.Failures = append(g.Failures, coverageGateFailure{
				Kind: c.Status, Message: clauseFailureMessage(c),
				Standard: c.Standard, Version: c.Version, Clause: c.Clause, Entity: c.Entity, Rules: c.Rules,
			})
		}
	}
	if slices.Contains(opts.failOn, gateUnresolved) {
		for _, m := range report.Unresolved {
			g.Failures = append(g.Failures, coverageGateFailure{
				Kind:    gateUnresolved,
				Message: fmt.Sprintf("the `standard` note %s of %s resolves to no declared standard", m.Note, m.Rule),
				Rules:   []coverageMapping{m},
			})
		}
	}
	g.checkMinCoverage(report, opts.minCoverage)
	g.Passed = len(g.Failures) == 0
	return g
}

func standardLabel(id, version string) string {
	if version == "" {
		return id
	}
	return id + "@" + version
}

func clauseFailureMessage(c *coverageClause) string {
	why := "only a rule not yet Implemented maps it"
	if c.Status == gateLapsed {
		why = "the rule mapping it was retired or replaced"
	}
	return fmt.Sprintf("%s:%s is %s in %s: %s", standardLabel(c.Standard, c.Version), c.Clause, c.Status, c.Entity, why)
}

// checkEvaluator fails covered clauses that nothing evaluates.
func (g *coverageGate) checkEvaluator(report *coverageOutput) {
	var covered int
	for i := range report.Clauses {
		if report.Clauses[i].Status == string(coverage.StatusCovered) {
			covered++
		}
	}
	e := report.Evaluator
	var why string
	switch {
	case covered == 0 || (e.Usable && e.RunsOnImplemented):
		return
	case e.Provider == "null":
		why = "the evaluator is null, so nothing evaluates Implemented rules"
	case !e.Usable:
		why = fmt.Sprintf("strspc can't run the evaluator %s", e.Provider)
	default:
		why = fmt.Sprintf("the evaluator %s isn't run on Implemented rules (evaluate_on)", e.Provider)
	}
	g.Failures = append(g.Failures, coverageGateFailure{
		Kind:    gateEvaluator,
		Message: fmt.Sprintf("%d covered clause(s), but %s", covered, why),
	})
}

// checkRequired fails unless a reference matching sel is declared as
// implements and one of its clauses is covered.
func (g *coverageGate) checkRequired(report *coverageOutput, sel standardSelector) {
	var editions, implemented []string
	var declared bool
	for _, ref := range report.Declared {
		if ref.ID != sel.id {
			continue
		}
		if !slices.Contains(editions, ref.Version) {
			editions = append(editions, ref.Version)
		}
		if !sel.matches(ref.ID, ref.Version) {
			continue
		}
		declared = true
		if ref.Relation == entity.RelationImplements && !slices.Contains(implemented, ref.Version) {
			implemented = append(implemented, ref.Version)
		}
	}
	for i := range report.Clauses {
		c := &report.Clauses[i]
		if c.Standard == sel.id && c.Status == string(coverage.StatusCovered) && slices.Contains(implemented, c.Version) {
			return
		}
	}
	var why string
	switch {
	case len(editions) == 0:
		why = "it isn't declared"
	case !declared:
		labels := make([]string, 0, len(editions))
		for _, v := range editions {
			labels = append(labels, standardLabel(sel.id, v))
		}
		why = "it is declared only as " + strings.Join(labels, ", ")
	case len(implemented) == 0:
		why = "it is declared only as " + entity.RelationDerivedFrom
	default:
		why = "it is declared as " + entity.RelationImplements + ", but no clause of it is covered"
	}
	g.Failures = append(g.Failures, coverageGateFailure{
		Kind: gateRequired, Message: fmt.Sprintf("required standard %s: %s", sel, why),
		Standard: sel.id, Version: sel.version,
	})
}

// thresholdFor returns the threshold that applies to a figure: that of its
// edition, else of its standard, else the bare one.
func thresholdFor(thresholds []coverageThreshold, f *coverageFigure) (coverageThreshold, bool) {
	best, rank := coverageThreshold{}, 0
	for _, t := range thresholds {
		r := 1
		switch {
		case t.standard == nil:
		case !t.standard.matches(f.Standard, f.Version):
			continue
		case t.standard.version == "":
			r = 2
		default:
			r = 3
		}
		if r > rank {
			best, rank = t, r
		}
	}
	return best, rank > 0
}

// checkMinCoverage fails each Realm figure under its threshold, and each
// threshold no figure is there to check. An undefined figure is not checked
// (STDREF-028): it is a warning.
func (g *coverageGate) checkMinCoverage(report *coverageOutput, thresholds []coverageThreshold) {
	if len(thresholds) == 0 {
		return
	}
	matched := make([]bool, len(thresholds))
	var realmFigures int
	for i := range report.Figures {
		f := &report.Figures[i]
		if f.Entity != "" {
			continue
		}
		realmFigures++
		for j, t := range thresholds {
			if t.standard != nil && t.standard.matches(f.Standard, f.Version) {
				matched[j] = true
			}
		}
		t, ok := thresholdFor(thresholds, f)
		if !ok {
			continue
		}
		label := fmt.Sprintf("Realm %s: %s", f.Tree, standardLabel(f.Standard, f.Version))
		counted := f.Total - f.Excluded
		if counted <= 0 {
			g.warnings = append(g.warnings, label+": the figure is undefined (STDREF-028), so --min-coverage isn't checked")
			continue
		}
		if f.Covered*1000 >= t.tenths*counted {
			continue
		}
		threshold := float64(t.tenths) / 10
		g.Failures = append(g.Failures, coverageGateFailure{
			Kind: gateMinCoverage,
			Message: fmt.Sprintf("%s covers %d of %d clause(s) (%s), under the minimum of %s",
				label, f.Covered, counted, coveragePercent(f.Covered, counted), tenthsText(t.tenths)),
			Standard: f.Standard, Version: f.Version, Tree: f.Tree, Figure: f.Figure, Threshold: &threshold,
		})
	}
	for j, t := range thresholds {
		switch {
		case t.standard != nil && !matched[j]:
			g.Failures = append(g.Failures, coverageGateFailure{
				Kind: gateMinCoverage,
				Message: fmt.Sprintf("--min-coverage %s: no coverage figure for it: a figure needs an %s reference linked to a Standard Realm",
					t.standard, entity.RelationImplements),
				Standard: t.standard.id, Version: t.standard.version,
			})
		case t.standard == nil && realmFigures == 0:
			g.Failures = append(g.Failures, coverageGateFailure{
				Kind: gateMinCoverage,
				Message: fmt.Sprintf("--min-coverage: the report has no coverage figure: a figure needs an %s reference linked to a Standard Realm",
					entity.RelationImplements),
			})
		}
	}
}

// writeCoverageGate ends the human report with the gate's verdict.
func writeCoverageGate(w io.Writer, g *coverageGate) {
	if g.Passed {
		writeln(w, "\nGate: passed")
		return
	}
	writeln(w, fmt.Sprintf("\nGate: %d failure(s)", len(g.Failures)))
	for i := range g.Failures {
		writeln(w, "  "+coverageCell(g.Failures[i].Message))
	}
}
