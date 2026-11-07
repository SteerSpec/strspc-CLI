package cmd

import (
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestGitHubAnnotationInfoIsANotice(t *testing.T) {
	got := githubAnnotation(&result.Diagnostic{Severity: result.Info, Code: "RE003", Message: "skipped"})
	if want := "::notice title=RE003::skipped"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGitHubStepSummaryStates(t *testing.T) {
	tests := []struct {
		name string
		out  checkOutput
		want []string
	}{
		{
			name: "provider unavailable, no diagnostics",
			out:  checkOutput{ExitCode: exitUnavailable, Provider: "claude", Model: "claude-x", RuleCount: 3, SourceCount: 1},
			want: []string{"**Status:** failed (exit 3)", "**Evaluation:** claude / claude-x", "No diagnostics."},
		},
		{
			name: "static run with a diff",
			out:  checkOutput{ExitCode: exitOK, StaticOnly: true, Diff: "file change.diff"},
			want: []string{"**Status:** passed", "static-only", "**Diff:** file change.diff"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := githubStepSummary(&tt.out)
			for _, w := range tt.want {
				testutil.AssertContains(t, got, w)
			}
		})
	}
}
