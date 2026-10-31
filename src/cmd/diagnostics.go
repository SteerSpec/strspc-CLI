package cmd

import (
	"cmp"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// diagnosticsDocsURL is the base of the published per-code pages. CFG-002
// applies: a code printed here needs a live page on steerspec.dev.
const diagnosticsDocsURL = "https://steerspec.dev/docs/diagnostics/"

// cliCodes describes the codes the CLI emits itself. Every other code belongs
// to the manager's registry.
var cliCodes = map[string]result.CodeInfo{
	"CHK002": {
		Code:     "CHK002",
		Module:   "strspc",
		Severity: "Warning",
		Title:    "Config does not match its schema",
		Summary:  "`.strspc/config.yaml` does not match the published `config.v1` schema. The check continues.",
		Fix:      "Correct the key or value the message names in `.strspc/config.yaml`.",
	},
	"CHK003": {
		Code:     "CHK003",
		Module:   "strspc",
		Severity: "Warning",
		Title:    "Inherited rules not evaluated",
		Summary:  "An entity declares `inherits` in a source with no `realm.json`, so its inherited rules are not evaluated.",
		Fix:      "Add a `realm.json` to the rule source so that the `inherits` target resolves, or remove `inherits`.",
	},
}

// explanation is what --explain adds to a diagnostic. Description and Fix keep
// the registry's inline Markdown; plainText removes it for the terminal.
type explanation struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Fix         string `json:"fix"`
	Docs        string `json:"docs"`
}

// explainedDiagnostic is the JSON form of a diagnostic. Embedding keeps the
// keys of result.Diagnostic, so output without --explain does not change.
type explainedDiagnostic struct {
	result.Diagnostic
	Explanation *explanation `json:"explanation,omitempty"`
}

// lookupDiagnostic returns the description of a code, which must be in its
// canonical upper case.
func lookupDiagnostic(code string) (result.CodeInfo, bool) {
	if info, ok := result.LookupCode(code); ok {
		return info, true
	}
	info, ok := cliCodes[code]
	return info, ok
}

// allDiagnosticCodes returns every known code, sorted.
func allDiagnosticCodes() []result.CodeInfo {
	codes := result.Codes()
	for code := range cliCodes {
		codes = append(codes, cliCodes[code])
	}
	slices.SortFunc(codes, func(a, b result.CodeInfo) int { return cmp.Compare(a.Code, b.Code) })
	return codes
}

func newExplanation(info *result.CodeInfo) explanation {
	return explanation{
		Code:        info.Code,
		Title:       info.Title,
		Description: info.Summary,
		Fix:         info.Fix,
		Docs:        diagnosticsDocsURL + info.Code + "/",
	}
}

// explainDiagnostics converts diags for JSON output and, with explain, attaches
// the explanation of each known code. The result is never nil, so an empty run
// encodes as [].
func explainDiagnostics(diags []result.Diagnostic, explain bool) []explainedDiagnostic {
	out := make([]explainedDiagnostic, len(diags))
	for i := range diags {
		out[i].Diagnostic = diags[i]
		if !explain {
			continue
		}
		if info, ok := lookupDiagnostic(diags[i].Code); ok {
			ex := newExplanation(&info)
			out[i].Explanation = &ex
		}
	}
	return out
}

// plainText removes the registry's inline Markdown. Backticks are the only
// syntax it uses.
func plainText(s string) string {
	return strings.ReplaceAll(s, "`", "")
}

// printableDiagnostic makes a diagnostic safe to print: its text can come from a
// remote dependency. It keeps line breaks, so a multi-line message such as a
// schema error stays readable, escapes every other control character, and marks
// a line that a CI runner would otherwise read as a workflow command.
func printableDiagnostic(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		var b strings.Builder
		for _, r := range line {
			if r != '\t' && unicode.IsControl(r) {
				quoted := strconv.QuoteRune(r)
				b.WriteString(quoted[1 : len(quoted)-1])
			} else {
				b.WriteRune(r)
			}
		}
		lines[i] = b.String()
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "::") {
			lines[i] = "> " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// diagnosticPrinter writes diagnostics as text and counts them. With explain,
// each code is explained under its first occurrence only, so a run with many
// identical findings stays readable.
type diagnosticPrinter struct {
	w        io.Writer
	explain  bool
	seen     map[string]bool
	errors   int
	warnings int
}

func newDiagnosticPrinter(w io.Writer, explain bool) *diagnosticPrinter {
	return &diagnosticPrinter{w: w, explain: explain, seen: make(map[string]bool)}
}

func (p *diagnosticPrinter) print(d *result.Diagnostic) {
	switch d.Severity {
	case result.Error:
		p.errors++
		writeln(p.w, brandStyle.Render(printableDiagnostic(d.String())))
	case result.Warning:
		p.warnings++
		writeln(p.w, descStyle.Render(printableDiagnostic(d.String())))
	case result.Info:
		writeln(p.w, descStyle.Render(printableDiagnostic(d.String())))
	}

	if !p.explain || p.seen[d.Code] {
		return
	}
	p.seen[d.Code] = true
	if info, ok := lookupDiagnostic(d.Code); ok {
		p.writeExplanation(&info)
	}
}

func (p *diagnosticPrinter) writeExplanation(info *result.CodeInfo) {
	ex := newExplanation(info)
	writeln(p.w)
	writeln(p.w, "  "+brandStyle.Render(ex.Code+": "+ex.Title))
	writeln(p.w, "  "+descStyle.Render(plainText(ex.Description)))
	writeln(p.w)
	writeln(p.w, "  "+descStyle.Render("Fix: "+plainText(ex.Fix)))
	writeln(p.w, "  "+descStyle.Render("Docs: "+ex.Docs))
	writeln(p.w)
}
