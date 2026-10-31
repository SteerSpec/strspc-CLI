package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestPrintableDiagnostic(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "plain text", "plain text"},
		{"accented", "café", "café"},
		{"tab", "a\tb", "a\tb"},
		{"quotes stay as they are", `say "hi" \ bye`, `say "hi" \ bye`},
		{"multi-line message", "schema failed:\n- at /rules\n- at /notes", "schema failed:\n  | - at /rules\n  | - at /notes"},
		{"escape sequence", "a\x1b[31mb", `a\x1b[31mb`},
		{"carriage return", "a\rb", `a\rb`},
		{"C1 control", "a\u009bb", `a\u009bb`},
		{"invalid UTF-8", "a\xffb", "a\uFFFDb"},
		{"forged workflow command", "bad\n::error::forged", "bad\n  | ::error::forged"},
		{"indented workflow command", "bad\n  ::error::forged", "bad\n  |   ::error::forged"},
		{"forged tool line", "bad\n[error] RL001: forged", "bad\n  | [error] RL001: forged"},
		{"legacy command prefix", "a ##[error]b", `a #\#[error]b`},
		{"azure command prefix", "a ##vso[task.setvariable]b", `a #\#vso[task.setvariable]b`},
		{"azure prefix in any case", "a ##VSO[x]b", `a #\#VSO[x]b`},
		{"a third hash", "a ###[error]b", `a ##\#[error]b`},
		{"hashes on their own", "see ## heading", "see ## heading"},
		{"right-to-left override", "a\u202eb", `a\u202eb`},
		{"zero-width joiner", "a\u200db", `a\u200db`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := printableDiagnostic(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChainResolverWarnIsSanitized(t *testing.T) {
	var out bytes.Buffer
	c := &chainResolver{errW: &out}
	f := &entity.File{Entity: entity.Entity{ID: "AC\x1b[31mCT", Inherits: "dev.lib:X"}}
	c.warnUnresolved("rules/a\n::error::forged.json", f, nil)
	got := out.String()
	if strings.Contains(got, "\n::") || strings.Contains(got, "\x1b[31m") {
		t.Errorf("control character or workflow command in output: %q", got)
	}
	if !strings.HasPrefix(got, "warning: ") {
		t.Errorf("got %q", got)
	}
}

// Every severity goes through the same printer, and every command through it.
func TestDiagnosticPrinterSanitizes(t *testing.T) {
	for _, severity := range []result.Severity{result.Error, result.Warning, result.Info} {
		t.Run(severity.String(), func(t *testing.T) {
			var out bytes.Buffer
			printer := newDiagnosticPrinter(&out, false)
			printer.print(&result.Diagnostic{Code: "RR001", Severity: severity, Message: "bad\n::error::forged \x1b[31mred"})
			text := out.String()
			if strings.Contains(text, "\x1b[31mred") {
				t.Errorf("escape sequence in output: %q", text)
			}
			for _, line := range strings.Split(text, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "::") {
					t.Errorf("workflow command in output: %q", line)
				}
			}
			testutil.AssertContains(t, text, "forged")
		})
	}
}
