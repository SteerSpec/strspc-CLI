package render

import (
	"fmt"

	"github.com/SteerSpec/strspc-CLI/src/internal/entity"
)

// RuleIDFormatter formats a rule's display identifier according to RLIFRMT rules.
type RuleIDFormatter struct {
	OpenBracket      string
	CloseBracket     string
	RevisionSplitter string
	StateSplitter    string
}

// DefaultRuleIDFormatter returns a formatter with RLIFRMT default settings.
func DefaultRuleIDFormatter() RuleIDFormatter {
	return RuleIDFormatter{
		OpenBracket:      "[",
		CloseBracket:     "]",
		RevisionSplitter: ".",
		StateSplitter:    "/",
	}
}

// Format returns the rendered rule identifier, e.g. "[ENT-001.0/D]".
func (f RuleIDFormatter) Format(r *entity.Rule) string {
	return fmt.Sprintf("%s%s%s%d%s%s%s",
		f.OpenBracket,
		r.ID,
		f.RevisionSplitter,
		r.Revision,
		f.StateSplitter,
		r.State,
		f.CloseBracket,
	)
}
