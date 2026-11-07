package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func runRuleShowCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"rule", "show"}, args...)...)
}

// runRuleShowSplit runs rule show with stdout and stderr kept apart.
func runRuleShowSplit(t *testing.T, dir string, o *ruleShowOptions, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = runRuleShow(context.Background(), &out, &errOut, dir, args, o)
	return out.String(), errOut.String(), err
}

func shownIDs(t *testing.T, stdout string) []string {
	t.Helper()
	var rules []shownRule
	if err := json.Unmarshal([]byte(stdout), &rules); err != nil {
		t.Fatalf("output is not a JSON list: %v\n%s", err, stdout)
	}
	ids := make([]string, len(rules))
	for i := range rules {
		ids[i] = rules[i].ID
	}
	return ids
}

func TestRuleShowSelects(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		state []string
		want  string
	}{
		{"rule ID", []string{"APP-001"}, nil, "APP-001"},
		{"rule in a dependency", []string{"ACCT-001"}, nil, "ACCT-001"},
		{"entity", []string{"APP"}, nil, "APP-001 APP-002 APP-003 APP-004"},
		{"entity by state code", []string{"APP"}, []string{"D"}, "APP-004"},
		{"entity by state name", []string{"APP"}, []string{"draft"}, "APP-004"},
		{"entity by state list", []string{"APP"}, []string{"I,R"}, "APP-001 APP-003"},
		{"entity filtered to nothing", []string{"APP"}, []string{"T"}, ""},
		{"rule ID ignores state", []string{"APP-002"}, []string{"I"}, "APP-002"},
		{"qualified rule", []string{"dev.lib:ACCT-001"}, nil, "ACCT-001"},
		{"qualified entity", []string{"dev.app:APP"}, []string{"A"}, "APP-002"},
		{"argument order", []string{"ACCT-001", "APP-003"}, nil, "ACCT-001 APP-003"},
		{"deduplicated", []string{"APP-001", "APP", "APP-001"}, []string{"I"}, "APP-001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			var states []string
			for _, s := range tt.state {
				states = append(states, strings.Split(s, ",")...)
			}
			out, _, err := runRuleShowSplit(t, dir, &ruleShowOptions{jsonOutput: true, states: states}, tt.args...)
			assertExit(t, err, exitOK)
			if got := strings.Join(shownIDs(t, out), " "); got != tt.want {
				t.Errorf("rules = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuleShowHumanOutput(t *testing.T) {
	setupRefsProject(t)
	out, err := runRuleShowCmd(t, "APP-001", "ACCT-001")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "APP-001 [I] dev.app · APP")
	testutil.AssertContains(t, out, "  Rule APP-001.")
	testutil.AssertContains(t, out, "ACCT-001 [P] dev.lib · ACCT")
}

func TestRuleShowUnknown(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"rule", []string{"APP-999", "ACCT-001"}, "APP-999"},
		{"entity", []string{"NOPE"}, "NOPE"},
		{"rule in another realm", []string{"dev.app:ACCT-001"}, "dev.app:ACCT-001"},
		{"realm not configured", []string{"dev.other:APP-001"}, "dev.other:APP-001"},
		{"all named", []string{"APP-999", "NOPE"}, "APP-999, NOPE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			out, _, err := runRuleShowSplit(t, dir, &ruleShowOptions{}, tt.args...)
			assertExit(t, err, exitViolations)
			if out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestRuleShowConfigurationErrors(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		state []string
		want  string
	}{
		{"state", []string{"APP"}, []string{"X"}, "invalid --state state \"X\""},
		{"malformed argument", []string{"APP-1"}, nil, "invalid argument"},
		{"control character", []string{"APP\x1b"}, nil, "control character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			_, _, err := runRuleShowSplit(t, dir, &ruleShowOptions{states: tt.state}, tt.args...)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), tt.want)
			if strings.Contains(err.Error(), ".strspc/config.yaml") {
				t.Errorf("a flag error names the config file: %v", err)
			}
		})
	}

	t.Run("no config", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		_, err := runRuleShowCmd(t, "APP-001")
		assertExit(t, err, exitConfig)
	})

	t.Run("no argument", func(t *testing.T) {
		setupRefsProject(t)
		_, err := runRuleShowCmd(t)
		assertExit(t, err, exitConfig)
	})

	t.Run("unknown flag", func(t *testing.T) {
		setupRefsProject(t)
		_, err := runRuleShowCmd(t, "APP-001", "--nope")
		assertExit(t, err, exitConfig)
	})
}

func TestRuleShowNotesAndSubEntities(t *testing.T) {
	dir := setupRefsProject(t)
	rules := filepath.Join(dir, "rules")
	sub := `[{"entity": {"id": "SUB", "parent": "WEB", "title": "t", "description": "d"},
	  "rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
	  "rules": [{"id": "SUB-001", "revision": 0, "state": "I", "body": "Sub rule.\nSecond line.", "added_by": "github:t", "added_at": "2026-01-01", "supersedes": null}],
	  "notes": [{"id": "SUB-001/01", "rule_ref": "SUB-001", "type": "rationale", "content": "Because.", "added_by": "github:t", "added_at": "2026-01-01", "revision": 0}],
	  "sub_entities": []}]`
	body := inheritEntityJSON("WEB", "", sub, inheritRule{id: "WEB-001", state: "P"}, inheritRule{id: "WEB-002", state: "D"})
	writeInheritFile(t, rules, "WEB", withNote(body, "WEB-001", "example", "Like this."))

	t.Run("json", func(t *testing.T) {
		out, _, err := runRuleShowSplit(t, dir, &ruleShowOptions{jsonOutput: true}, "WEB", "SUB-001")
		assertExit(t, err, exitOK)
		var got []shownRule
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].ID != "WEB-001" || got[1].ID != "WEB-002" || got[2].ID != "SUB-001" {
			t.Fatalf("rules = %+v, want WEB-001, WEB-002, SUB-001 (the parent EUID excludes the sub-entity)", got)
		}
		if len(got[0].Notes) != 1 || got[0].Notes[0].Type != "example" || got[0].Notes[0].Content != "Like this." {
			t.Errorf("WEB-001 notes = %+v", got[0].Notes)
		}
		if got[2].Entity != "SUB" || got[2].Realm != "dev.app" || len(got[2].Notes) != 1 {
			t.Errorf("SUB-001 = %+v", got[2])
		}
		if !strings.Contains(out, `"notes": []`) {
			t.Errorf("a rule without notes should have \"notes\": [], got:\n%s", out)
		}
	})

	t.Run("sub-entity EUID", func(t *testing.T) {
		out, _, err := runRuleShowSplit(t, dir, &ruleShowOptions{jsonOutput: true}, "SUB")
		assertExit(t, err, exitOK)
		if got := strings.Join(shownIDs(t, out), " "); got != "SUB-001" {
			t.Errorf("rules = %q, want SUB-001", got)
		}
	})

	t.Run("human", func(t *testing.T) {
		out, err := runRuleShowCmd(t, "SUB-001")
		assertExit(t, err, exitOK)
		testutil.AssertContains(t, out, "SUB-001 [I] dev.app · SUB")
		testutil.AssertContains(t, out, "  Sub rule.\n  Second line.")
		testutil.AssertContains(t, out, "  SUB-001/01 rationale: Because.")
	})
}

func TestRuleShowEmptyJSON(t *testing.T) {
	dir := setupRefsProject(t)
	out, _, err := runRuleShowSplit(t, dir, &ruleShowOptions{jsonOutput: true, states: []string{"T"}}, "APP")
	assertExit(t, err, exitOK)
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("stdout = %q, want []", out)
	}
}

func TestRuleShowEscapesControlCharacters(t *testing.T) {
	dir := setupRefsProject(t)
	body := inheritEntityJSON("ESC", "", "", inheritRule{id: "ESC-001", state: "I"})
	body = strings.Replace(body, `"body": "Rule ESC-001."`, `"body": "Bad \u001b[31m text."`, 1)
	writeInheritFile(t, filepath.Join(dir, "rules"), "ESC", body)
	out, err := runRuleShowCmd(t, "ESC-001")
	assertExit(t, err, exitOK)
	if strings.Contains(out, "\x1b[31m") {
		t.Errorf("output holds a raw escape sequence:\n%q", out)
	}
	testutil.AssertContains(t, out, `\x1b[31m`)
}

// A JSON document that can't be written is an output error (exit 4).
func TestRuleShowJSONWriteError(t *testing.T) {
	dir := setupRefsProject(t)
	err := runRuleShow(context.Background(), failingWriter{}, &bytes.Buffer{}, dir, []string{"APP-001"}, &ruleShowOptions{jsonOutput: true})
	assertExit(t, err, exitOutput)
}
