package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

const (
	rl003Docs = "https://steerspec.dev/docs/diagnostics/RL003/"
	re003Docs = "https://steerspec.dev/docs/diagnostics/RE003/"
)

func TestExplainCode(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "RM006")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"RM006: Duplicate EUID in the Realm",
		"realm-lint",
		"Error",
		"The same EUID appears in more than one entity or sub-entity of the realm.",
		"Fix: Rename one of the entities so that each EUID appears once in the Realm.",
		"Docs: https://steerspec.dev/docs/diagnostics/RM006/",
	} {
		testutil.AssertContains(t, output, want)
	}
}

func TestExplainAcceptsAnyCase(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "rm006")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "RM006: Duplicate EUID in the Realm")
}

func TestExplainStripsMarkdownFromHumanOutput(t *testing.T) {
	// RD005's fix names `strspc rule abandon` in backticks in the registry.
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "RD005")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "strspc rule abandon")
	testutil.AssertNotContains(t, output, "`")
}

func TestExplainCLIOwnedCode(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "CHK003")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "CHK003: ")
	testutil.AssertContains(t, output, "Docs: https://steerspec.dev/docs/diagnostics/CHK003/")
}

func TestExplainUnknownCode(t *testing.T) {
	_, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "XX999")
	if err == nil {
		t.Fatal("expected error for unknown code, got nil")
	}
	testutil.AssertContains(t, err.Error(), `"XX999"`)
	testutil.AssertContains(t, err.Error(), "--list")
}

func TestExplainRequiresExactlyOneOfCodeAndList(t *testing.T) {
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "explain"); err == nil {
		t.Error("expected error with neither a code nor --list, got nil")
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "RM006", "--list"); err == nil {
		t.Error("expected error with both a code and --list, got nil")
	}
}

func TestExplainList(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "--list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 101 {
		t.Fatalf("listed %d codes, want 101", len(lines))
	}
	// CHK codes sort ahead of the manager's, which start at EC001.
	if !strings.HasPrefix(lines[0], "CHK002") || !strings.HasPrefix(lines[1], "CHK003") {
		t.Errorf("first lines = %q, %q; want CHK002 then CHK003", lines[0], lines[1])
	}
	testutil.AssertContains(t, output, "RM006")
	testutil.AssertContains(t, output, "Duplicate EUID in the Realm")
}

func TestExplainJSON(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "RD005", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if jsonErr := json.Unmarshal([]byte(output), &got); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	want := map[string]string{
		"code":     "RD005",
		"title":    "Deleted content",
		"module":   "rule-diff",
		"severity": "Error",
		"docs":     "https://steerspec.dev/docs/diagnostics/RD005/",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %v, want %q", key, got[key], value)
		}
	}
	// JSON keeps the registry's Markdown for consumers that render it.
	testutil.AssertContains(t, got["fix"].(string), "`strspc rule abandon`")
}

func TestExplainListJSON(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "explain", "--list", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got []map[string]any
	if jsonErr := json.Unmarshal([]byte(output), &got); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	if len(got) != 101 {
		t.Fatalf("listed %d codes, want 101", len(got))
	}
	for _, entry := range got {
		for _, key := range []string{"code", "title", "description", "fix", "docs", "module", "severity"} {
			if entry[key] == "" || entry[key] == nil {
				t.Errorf("%v: %s is empty", entry["code"], key)
			}
		}
	}
}

// TestEveryCodeTheCLIEmitsIsExplained catches a new Code literal in src/cmd
// that neither the manager registry nor cliCodes describes.
func TestEveryCodeTheCLIEmitsIsExplained(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	codeRe := regexp.MustCompile(`Code:\s+"([A-Z]+\d{3})"`)
	seen := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, m := range codeRe.FindAllSubmatch(src, -1) {
			seen++
			if _, ok := lookupDiagnostic(string(m[1])); !ok {
				t.Errorf("%s emits %s, which has no explanation", file, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no Code literals; the pattern is broken")
	}
}

func TestLintExplainOncePerCode(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "BAD1.json", invalidEntityJSON)
	writeEntityFile(t, dir, "BAD2.json", invalidEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--explain")
	if err == nil {
		t.Fatal("expected error for invalid entities, got nil")
	}
	if got := strings.Count(output, "RL003:"); got < 3 {
		t.Errorf("RL003 appears %d times, want both diagnostics and one heading:\n%s", got, output)
	}
	if got := strings.Count(output, rl003Docs); got != 1 {
		t.Errorf("RL003 explained %d times, want 1:\n%s", got, output)
	}
	testutil.AssertContains(t, output, "RL003: Malformed EUID")
	testutil.AssertContains(t, output, "Fix: ")
}

func TestLintWithoutExplainOmitsExplanation(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "BAD.json", invalidEntityJSON)

	output, _ := testutil.ExecuteCommand(NewRootCmd(), "lint", dir)
	testutil.AssertNotContains(t, output, "Fix: ")
	testutil.AssertNotContains(t, output, rl003Docs)

	output, _ = testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--json")
	testutil.AssertNotContains(t, output, `"explanation"`)
}

func TestLintExplainJSONOnEveryDiagnostic(t *testing.T) {
	setupLintTest(t)
	dir := t.TempDir()
	writeEntityFile(t, dir, "BAD1.json", invalidEntityJSON)
	writeEntityFile(t, dir, "BAD2.json", invalidEntityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "lint", dir, "--explain", "--json")
	if err == nil {
		t.Fatal("expected error for invalid entities, got nil")
	}
	var diags []struct {
		Code        string
		Explanation *struct {
			Code        string `json:"code"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Fix         string `json:"fix"`
			Docs        string `json:"docs"`
		} `json:"explanation"`
	}
	if jsonErr := json.Unmarshal([]byte(output), &diags); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	explained := 0
	for _, d := range diags {
		if d.Explanation == nil {
			t.Fatalf("%s has no explanation:\n%s", d.Code, output)
		}
		if d.Code == "RL003" {
			explained++
			if d.Explanation.Docs != rl003Docs || d.Explanation.Title != "Malformed EUID" || d.Explanation.Fix == "" {
				t.Errorf("explanation = %+v", *d.Explanation)
			}
		}
	}
	if explained != 2 {
		t.Errorf("explained %d RL003 diagnostics, want 2", explained)
	}
}

func TestDiffExplain(t *testing.T) {
	dir := makeGitRepo(t, "ENT.json", entityV2Promote("ENT"), entityV2InvalidEdit("ENT"))

	output, err := testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--explain")
	if err == nil {
		t.Fatal("expected error for illegal body edit, got nil")
	}
	testutil.AssertContains(t, output, "https://steerspec.dev/docs/diagnostics/RD002/")

	output, _ = testutil.ExecuteCommand(NewRootCmd(), "diff", filepath.Join(dir, "ENT.json"), "--explain", "--json")
	testutil.AssertContains(t, output, `"explanation"`)
}

func TestRealmValidateExplain(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)
	entityJSON := `{
  "entity": {"id": "DUP", "title": "Duplicate"},
  "rule_set": {"version": "1.0.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": [],
  "notes": []
}`
	writeEntityFile(t, dir, "DUP1.json", entityJSON)
	writeEntityFile(t, dir, "DUP2.json", entityJSON)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--explain")
	if err == nil {
		t.Fatal("expected error for duplicate EUID, got nil")
	}
	testutil.AssertContains(t, output, "https://steerspec.dev/docs/diagnostics/RM006/")

	output, _ = testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--explain", "--json")
	testutil.AssertContains(t, output, `"explanation"`)
}

func TestRealmValidateRecursiveExplainOncePerCodeAcrossGroups(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	writeEntityFile(t, dir, "BADROOT.json", invalidEntityJSON)
	writeEntityFile(t, filepath.Join(dir, "auth"), "BADSUB.json", strings.ReplaceAll(invalidEntityJSON, `"AB"`, `"CD"`))

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--recursive", "--explain")
	if err == nil {
		t.Fatal("expected error for invalid entities, got nil")
	}
	testutil.AssertContains(t, output, `Sub-realm "auth"`)
	if got := strings.Count(output, rl003Docs); got != 1 {
		t.Errorf("RL003 explained %d times, want 1:\n%s", got, output)
	}
}

func TestCheckExplainHuman(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})

	output, err := runCheckCmd(t, dir, "--explain")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "RE003: AI evaluation skipped")
	testutil.AssertContains(t, output, re003Docs)
}

func TestCheckExplainJSON(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})

	output, err := runCheckCmd(t, dir, "--format", "json", "--explain")
	assertExit(t, err, exitOK)

	var out checkOutput
	if jsonErr := json.Unmarshal([]byte(output), &out); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != "RE003" {
		t.Fatalf("diagnostics = %+v", out.Diagnostics)
	}
	if ex := out.Diagnostics[0].Explanation; ex == nil || ex.Docs != re003Docs {
		t.Errorf("explanation = %+v", ex)
	}
}

func TestCheckJSONWithoutExplainOmitsExplanation(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})

	output, err := runCheckCmd(t, dir, "--format", "json")
	assertExit(t, err, exitOK)
	testutil.AssertNotContains(t, output, `"explanation"`)
}

func TestCheckExplainRejectedWithGitHubFormat(t *testing.T) {
	dir, _ := setupCheckProject(t, "null", []string{"I"})

	_, err := runCheckCmd(t, dir, "--format", "github", "--explain")
	assertExit(t, err, exitConfig)
	var exitErr *exitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %T, want *exitError", err)
	}
	testutil.AssertContains(t, err.Error(), "--explain")
}
