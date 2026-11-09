package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/coverage"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// coverageBaseline is what --baseline reads of a standards coverage --json
// report: what coverage.Compare compares. The pointers tell a report with no
// clause from a document that isn't a report at all.
type coverageBaseline struct {
	Declared   *[]entity.StandardRef `json:"declared"`
	Clauses    *[]coverage.Clause    `json:"clauses"`
	Unresolved []coverage.Mapping    `json:"unresolved"`
}

var coverageStatuses = []coverage.Status{
	coverage.StatusCovered, coverage.StatusMapped, coverage.StatusLapsed,
	coverage.StatusCited, coverage.StatusExcluded, coverage.StatusUnmapped,
}

// loadCoverageBaseline reads the report at path, relative to cwd. Its figures,
// evaluator, warnings and gate are left out: Compare reads none of them. A
// clause without a realm comes from a report written before clauses had one,
// and would read as every clause lost, so it is refused.
func loadCoverageBaseline(cwd, path string) (*coverage.Report, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--baseline: %w", err)
	}
	var b coverageBaseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("--baseline %s: not a JSON report: %w", path, err)
	}
	if b.Declared == nil || b.Clauses == nil {
		return nil, fmt.Errorf("--baseline %s: not a coverage report: want the output of standards coverage --json", path)
	}
	for i := range *b.Clauses {
		c := &(*b.Clauses)[i]
		switch {
		case c.Realm == "":
			return nil, fmt.Errorf("--baseline %s: clause %s:%s in %s has no realm: the report predates realms in coverage, "+
				"so record the baseline again", path, standardLabel(c.Standard, c.Version), c.Clause, c.Entity)
		case !slices.Contains(coverageStatuses, c.Status):
			return nil, fmt.Errorf("--baseline %s: clause %s:%s in %s has the unknown status %q",
				path, standardLabel(c.Standard, c.Version), c.Clause, c.Entity, c.Status)
		}
	}
	return &coverage.Report{Declared: *b.Declared, Clauses: *b.Clauses, Unresolved: b.Unresolved}, nil
}

// coverageReportOf is the head report Compare reads: the clauses as the
// command prints them, so base and head take the same shape.
func coverageReportOf(out *coverageOutput) *coverage.Report {
	r := &coverage.Report{
		Declared:   out.Declared,
		Clauses:    make([]coverage.Clause, 0, len(out.Clauses)),
		Unresolved: managerMappings(out.Unresolved),
	}
	for i := range out.Clauses {
		c := &out.Clauses[i]
		mc := coverage.Clause{
			Standard: c.Standard, Version: c.Version, Clause: c.Clause, Realm: c.Realm, Entity: c.Entity,
			Status: coverage.Status(c.Status), Rules: managerMappings(c.Rules),
		}
		if ex := c.Exclusion; ex != nil {
			mc.Exclusion = &coverage.ExclusionRef{Realm: ex.Realm, Note: ex.Note, Content: ex.Content}
		}
		r.Clauses = append(r.Clauses, mc)
	}
	return r
}

func managerMappings(mappings []coverageMapping) []coverage.Mapping {
	out := make([]coverage.Mapping, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, coverage.Mapping{Rule: m.Rule, State: m.State, Note: m.Note})
	}
	return out
}

// checkRegressions fails each way the report is worse than the baseline. An
// edition upgrade and a clause newly excluded are warnings instead when
// --allow-rebase or --allow-excluded allows them.
func (g *coverageGate) checkRegressions(report *coverageOutput, opts *coverageGateOptions) {
	if opts.baseline == nil {
		return
	}
	regressions := coverage.Compare(opts.baseline, coverageReportOf(report))
	for i := range regressions {
		r := &regressions[i]
		msg := regressionMessage(r)
		if (r.Kind == coverage.KindRebased && opts.allowRebase) || (r.Kind == coverage.KindExcluded && opts.allowExcluded) {
			g.warnings = append(g.warnings, "allowed regression against the baseline: "+msg)
			continue
		}
		g.Failures = append(g.Failures, coverageGateFailure{
			Kind: gateRegression, Regression: string(r.Kind), Message: msg,
			Standard: r.Standard, Version: r.Version, Clause: r.Clause, Realm: r.Realm, Entity: r.Entity,
			Base: string(r.Base), Head: string(r.Head), Rules: coverageMappings(r.Rules),
		})
	}
}

// regressionMessage says what got worse against the baseline.
func regressionMessage(r *coverage.Regression) string {
	label := standardLabel(r.Standard, r.Version)
	switch {
	case r.Kind == coverage.KindUnresolved:
		note := "a `standard` note"
		if len(r.Rules) > 0 {
			note = fmt.Sprintf("the `standard` note %s of %s", r.Rules[0].Note, r.Rules[0].Rule)
		}
		return note + " resolved in the baseline and now resolves to no declared standard (unresolved)"
	case r.Clause == "" && r.Kind == coverage.KindRebased:
		return fmt.Sprintf("%s was declared as %s in the baseline, and only another edition now is (rebased)", label, entity.RelationImplements)
	case r.Clause == "":
		return fmt.Sprintf("%s was declared as %s in the baseline, and no longer is (%s)", label, entity.RelationImplements, r.Kind)
	}
	now := "is no longer in the report"
	switch {
	case r.Kind == coverage.KindRebased:
		now = "and only another edition of the standard is now declared"
	case r.Exclusion != nil:
		now = "is now excluded by " + r.Exclusion.Note
	case r.Head != "":
		now = "is now " + string(r.Head)
	}
	return fmt.Sprintf("%s:%s in %s (%s) was %s, %s (%s)", label, r.Clause, r.Entity, r.Realm, r.Base, now, r.Kind)
}
