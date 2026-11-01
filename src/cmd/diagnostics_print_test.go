package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

// A directory render skips a file it can't read and names it in a warning, in
// both output modes.
func TestRenderSkipWarningIsSanitized(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names can't hold control characters")
	}
	for _, flags := range [][]string{nil, {"--json"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			dir := t.TempDir()
			name := "a\n::error::forged\x1b[31m.json"
			if err := os.WriteFile(filepath.Join(dir, name), []byte("not json"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"render", dir, "-o", t.TempDir()}, flags...)
			out, _ := testutil.ExecuteCommand(NewRootCmd(), args...)
			testutil.AssertContains(t, out, "skipping")
			if strings.Contains(out, "\n::") || strings.Contains(out, "\x1b[31m") {
				t.Errorf("control character or workflow command in output: %q", out)
			}
		})
	}
}

// Cobra would print a root-level error itself, unsanitized and before Execute.
func TestRootLeavesErrorsToExecute(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "--bad\x1b[31m")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if strings.Contains(out, "\x1b[31m") {
		t.Errorf("escape sequence in output: %q", out)
	}
	if got := printableDiagnostic("Error: " + err.Error()); strings.Contains(got, "\x1b[31m") {
		t.Errorf("escape sequence in the error: %q", got)
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

func TestPrintableJSON(t *testing.T) {
	values := []string{
		"plain",
		"a ##[error]b",
		"a ##vso[task.setvariable]b",
		"a ##VSO[x]b",
		"a ###[error]b",
		`back\slash ##[x]`,
		"see ## heading",
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			data, err := json.Marshal(map[string]string{"k": value})
			if err != nil {
				t.Fatal(err)
			}
			got := printableJSON(data)
			if runnerCommandRe.Match(got) {
				t.Errorf("runner command in %s", got)
			}
			var back map[string]string
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("%s: %v", got, err)
			}
			if back["k"] != value {
				t.Errorf("got %q, want %q", back["k"], value)
			}
		})
	}
}

// Every JSON writer a CI step could print goes through printableJSON.
func TestJSONWritersEscapeRunnerCommands(t *testing.T) {
	report := &statusReport{Sources: []statusSource{{Source: "##vso[task.setvariable]x"}}}
	diags := &result.Result{Diagnostics: []result.Diagnostic{{Code: "RF001", Message: "##[error]x"}}}
	out := checkOutput{Error: "##[error]x"}

	var buf bytes.Buffer
	writers := map[string]func() error{
		"status": func() error { return writeStatusJSON(&buf, report) },
		"lint":   func() error { return writeJSON(&buf, diags, false) },
		"check":  func() error { r := &checkRun{out: &buf}; return r.writeJSONOutput(&out) },
		"rule":   func() error { return writeJSONOutput(&buf, map[string]string{"id": "##vso[x]"}) },
		"explain": func() error {
			return writeIndentedJSON(&buf, map[string]string{"code": "##[error]x"})
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			buf.Reset()
			if err := write(); err != nil {
				t.Fatal(err)
			}
			if runnerCommandRe.Match(buf.Bytes()) {
				t.Errorf("runner command in %s", buf.String())
			}
			if !json.Valid(buf.Bytes()) {
				t.Errorf("not JSON: %s", buf.String())
			}
		})
	}
}

func TestSyncJSONEscapesRunnerCommands(t *testing.T) {
	dir := statusProject(t, "", "./##vso[task.setvariable]x/")
	out, _ := testutil.ExecuteCommand(NewRootCmd(), "sync", dir, "--json")
	testutil.AssertContains(t, out, "vso")
	if runnerCommandRe.MatchString(out) {
		t.Errorf("runner command in output: %s", out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("not JSON: %s", out)
	}
}

func TestRenderJSONEscapesRunnerCommands(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(inheritEntityJSON("APP", "", "", inheritRule{id: "APP-001", state: "I"}), "Rule APP-001.", "##vso[task.setvariable]x", 1)
	writeInheritFile(t, dir, "APP", body)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(dir, "APP.json"), "--json")
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, out, "task.setvariable")
	if runnerCommandRe.MatchString(out) {
		t.Errorf("runner command in output: %s", out)
	}

	// A file keeps the bytes of the rule.
	outDir := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(dir, "APP.json"), "--json", "-o", outDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "APP.json"))
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, string(data), "##vso[task.setvariable]x")
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
