package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// writeGitHubAnnotations renders diagnostics as GitHub Actions workflow
// commands: ::error for Error severity (violations of rules in fail_on
// states), ::warning for Warning (other violations, provider errors) and
// ::notice for Info.
func writeGitHubAnnotations(w io.Writer, diags []result.Diagnostic) {
	for i := range diags {
		writeln(w, githubAnnotation(&diags[i]))
	}
}

// githubAnnotation formats one diagnostic as
// ::<level> file=<path>,line=<n>,title=<code>::<message>.
func githubAnnotation(d *result.Diagnostic) string {
	level := "notice"
	switch d.Severity {
	case result.Error:
		level = "error"
	case result.Warning:
		level = "warning"
	case result.Info:
		level = "notice"
	}

	var props []string
	if isWorkspacePath(d.Path) {
		props = append(props, "file="+escapeGitHubProperty(d.Path))
		if d.Line > 0 {
			props = append(props, fmt.Sprintf("line=%d", d.Line))
		}
	}
	title := d.Code
	if d.Module != "" {
		title = d.Module + " " + d.Code
	}
	props = append(props, "title="+escapeGitHubProperty(title))

	return "::" + level + " " + strings.Join(props, ",") + "::" + escapeGitHubData(d.Message)
}

// isWorkspacePath reports whether p can be attached to an annotation as a
// workspace-relative file: URIs and absolute paths (rule sources, cache
// directories) are left out so GitHub does not try to resolve them.
func isWorkspacePath(p string) bool {
	return p != "" && !strings.Contains(p, "://") && !filepath.IsAbs(p)
}

// githubErrorAnnotation renders a fatal command error as a ::error so CI
// shows it in the run summary even when the step log is collapsed.
func githubErrorAnnotation(err error) string {
	return "::error title=" + escapeGitHubProperty("strspc check") + "::" + escapeGitHubData(err.Error())
}

// escapeGitHubData escapes a workflow command message.
func escapeGitHubData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeGitHubProperty escapes a workflow command property value.
func escapeGitHubProperty(s string) string {
	s = escapeGitHubData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}

// githubStepSummary renders the Markdown appended to $GITHUB_STEP_SUMMARY.
func githubStepSummary(out *checkOutput) string {
	var b strings.Builder
	b.WriteString("## strspc check\n\n")

	status := "passed"
	switch {
	case out.ExitCode == exitViolations:
		status = "failed: violations of rules in fail_on states"
	case out.ExitCode != exitOK:
		status = fmt.Sprintf("failed (exit %d)", out.ExitCode)
	}
	fmt.Fprintf(&b, "**Status:** %s  \n", status)
	fmt.Fprintf(&b, "**Rules:** %d from %d source(s)  \n", out.RuleCount, out.SourceCount)
	switch {
	case out.StaticOnly:
		b.WriteString("**Evaluation:** static-only (AI evaluation skipped)  \n")
	default:
		fmt.Fprintf(&b, "**Evaluation:** %s / %s  \n", out.Provider, out.Model)
	}
	if out.Diff != "" {
		fmt.Fprintf(&b, "**Diff:** %s  \n", out.Diff)
	}
	b.WriteString("\n")

	if len(out.Diagnostics) == 0 {
		b.WriteString("No diagnostics.\n")
		return b.String()
	}

	b.WriteString("| Severity | Code | Message | Location |\n")
	b.WriteString("|----------|------|---------|----------|\n")
	for i := range out.Diagnostics {
		d := &out.Diagnostics[i]
		loc := d.Path
		if loc != "" && d.Line > 0 {
			loc = fmt.Sprintf("%s:%d", d.Path, d.Line)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			d.Severity, d.Code, markdownCell(d.Message), markdownCell(loc))
	}
	return b.String()
}

// markdownCell makes s safe inside a Markdown table cell.
func markdownCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// appendGitHubStepSummary appends the summary to the file named by
// $GITHUB_STEP_SUMMARY (path), if set. Writing is best effort for the
// caller: the error is returned for reporting, never for failing the check.
func appendGitHubStepSummary(path string, out *checkOutput) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(githubStepSummary(out)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
