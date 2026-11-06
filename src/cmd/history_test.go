package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/rulehistory"
)

// histRule is a rule of a history fixture.
type histRule struct {
	id, state, body, addedBy, supersedes string
	rev                                  int
}

func histRulesJSON(rules []histRule) string {
	entries := make([]string, 0, len(rules))
	for _, r := range rules {
		sup := "null"
		if r.supersedes != "" {
			sup = fmt.Sprintf("%q", r.supersedes)
		}
		by := r.addedBy
		if by == "" {
			by = "github:test"
		}
		entries = append(entries, fmt.Sprintf(`{"id": %q, "revision": %d, "state": %q, "body": %q,`+
			` "added_by": %q, "added_at": "2026-01-01", "supersedes": %s}`, r.id, r.rev, r.state, r.body, by, sup))
	}
	return "[" + strings.Join(entries, ", ") + "]"
}

// histEntityJSON writes entity ENT at version with rules, holding the
// sub-entity SUB at the same version with subRules.
func histEntityJSON(version string, rules, subRules []histRule) string {
	return fmt.Sprintf(`{"$schema": "./_schema/entity.v1.schema.json",
  "entity": {"id": "ENT", "title": "Entity", "description": "d"},
  "rule_set": {"version": %q, "timestamp": "2026-01-01T00:00:00Z", "hash": null},
  "rules": %s, "notes": [],
  "sub_entities": [{"entity": {"id": "SUB", "parent": "ENT", "title": "Sub", "description": "d"},
    "rule_set": {"version": %q, "timestamp": "2026-01-01T00:00:00Z", "hash": null},
    "rules": %s, "notes": [], "sub_entities": []}]}
`, version, histRulesJSON(rules), version, histRulesJSON(subRules))
}

// histCommit commits everything in dir with a pinned date.
func histCommit(t *testing.T, dir, date, subject string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_DATE", date)
	t.Setenv("GIT_COMMITTER_DATE", date)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "-m", subject)
}

// newHistoryRepo builds a repository whose history of ENT holds every kind
// of change. ENT starts in rules/legacy/ and is renamed to rules/ENT.json
// before the last change. It returns the repository and its rules directory.
func newHistoryRepo(t *testing.T) (repo, rules string) {
	t.Helper()
	repo = t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	legacy := "rules/legacy/ENT.json"

	writeFile(t, repo, legacy, histEntityJSON("0.1.0",
		[]histRule{
			{id: "ENT-001", state: "D", body: "a"},
			{id: "ENT-002", state: "D", body: "b"},
			{id: "ENT-003", state: "D", body: "c"},
			{id: "ENT-004", state: "D", body: "d"},
		},
		[]histRule{{id: "SUB-001", state: "D", body: "s"}}))
	histCommit(t, repo, "2026-01-01T10:00:00Z", "feat: add ENT (#1)")

	writeFile(t, repo, legacy, histEntityJSON("0.2.0",
		[]histRule{
			{id: "ENT-001", state: "D", body: "a2", rev: 1},
			{id: "ENT-002", state: "P", body: "b"},
			{id: "ENT-003", state: "A", body: "c"},
			{id: "ENT-004", state: "I", body: "d"},
		},
		[]histRule{{id: "SUB-001", state: "P", body: "s"}}))
	histCommit(t, repo, "2026-01-02T10:00:00Z", "feat: edit ENT (#2)")

	writeFile(t, repo, legacy, histEntityJSON("0.3.0",
		[]histRule{
			{id: "ENT-001", state: "D", body: "a2", rev: 1},
			{id: "ENT-002", state: "I", body: "b"},
			{id: "ENT-003", state: "A", body: "c"},
			{id: "ENT-004", state: "R", body: "d"},
			{id: "ENT-005", state: "D", body: "e", supersedes: "ENT-002"},
		},
		[]histRule{{id: "SUB-001", state: "P", body: "s"}}))
	histCommit(t, repo, "2026-01-03T10:00:00Z", "feat: supersede ENT-002 (#3)")

	gitRun(t, repo, "mv", legacy, "rules/ENT.json")
	histCommit(t, repo, "2026-01-04T10:00:00Z", "refactor: move ENT")

	writeFile(t, repo, "rules/ENT.json", histEntityJSON("0.4.0",
		[]histRule{
			{id: "ENT-001", state: "D", body: "a2", rev: 1, addedBy: "github:other"},
			{id: "ENT-002", state: "I", body: "b"},
			{id: "ENT-004", state: "T", body: "d"},
			{id: "ENT-005", state: "D", body: "e", supersedes: "ENT-002"},
		},
		[]histRule{{id: "SUB-001", state: "P", body: "s"}}))
	histCommit(t, repo, "2026-01-05T10:00:00Z", "chore: terminate ENT-004 (#4)")
	return repo, filepath.Join(repo, "rules")
}

// runHistorySplit runs a history command with stdout and stderr kept apart.
func runHistorySplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

// historyEvents runs history with --json and returns one line per event.
func historyEvents(t *testing.T, args ...string) []string {
	t.Helper()
	out, _, err := runHistorySplit(t, append(args, "--json")...)
	assertExit(t, err, exitOK)
	var doc historyOutput
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	lines := make([]string, 0, len(doc.Events))
	for i := range doc.Events {
		lines = append(lines, eventLine(&doc.Events[i]))
	}
	return lines
}

func eventLine(ev *rulehistory.Event) string {
	s := fmt.Sprintf("%s %s %s %s>%s #%d", ev.RuleSetVersion, ev.RuleID, ev.Kind, ev.FromState, ev.ToState, ev.PR)
	if ev.Supersedes != "" {
		s += " supersedes " + ev.Supersedes
	}
	if ev.SupersededBy != "" {
		s += " by " + ev.SupersededBy
	}
	return s
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("events:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

var (
	histCommit1 = []string{
		"0.1.0 ENT-001 added >D #1",
		"0.1.0 ENT-002 added >D #1",
		"0.1.0 ENT-003 added >D #1",
		"0.1.0 ENT-004 added >D #1",
		"0.1.0 SUB-001 added >D #1",
	}
	histCommit2 = []string{
		"0.2.0 ENT-001 body_changed >D #2",
		"0.2.0 ENT-002 promoted D>P #2",
		"0.2.0 ENT-003 abandoned D>A #2",
		"0.2.0 ENT-004 state_changed D>I #2",
		"0.2.0 SUB-001 promoted D>P #2",
	}
	histCommit3 = []string{
		"0.3.0 ENT-002 promoted P>I #3",
		"0.3.0 ENT-002 superseded >I #3 by ENT-005",
		"0.3.0 ENT-004 retired I>R #3",
		"0.3.0 ENT-005 added >D #3 supersedes ENT-002",
	}
	histCommit4 = []string{
		"0.4.0 ENT-001 metadata_changed >D #4",
		"0.4.0 ENT-004 terminated R>T #4",
		"0.3.0 ENT-003 removed A> #4", // a removed rule keeps the version before the commit
	}
)

func concat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestHistoryEveryKindAcrossARename(t *testing.T) {
	_, rules := newHistoryRepo(t)
	got := historyEvents(t, "history", "ENT", "--dir", rules)
	assertLines(t, got, concat(histCommit1, histCommit2, histCommit3, histCommit4))

	out, _, err := runHistorySplit(t, "history", "ENT", "--dir", rules, "--json")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, `"path": "rules/ENT.json"`)
	testutil.AssertContains(t, out, `"warnings": []`)
}

func TestHistoryTable(t *testing.T) {
	_, rules := newHistoryRepo(t)
	out, _, err := runHistorySplit(t, "history", "ENT", "--dir", rules)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "ENT (rules/ENT.json), oldest first")
	for _, want := range []string{
		"DATE        VERSION  RULE     REV  CHANGE",
		"2026-01-01  0.1.0    ENT-001  0    added",
		"2026-01-02  0.2.0    ENT-002  0    promoted",
		"superseded by ENT-005",
		"added (supersedes ENT-002)",
		"D→P",
		"#4",
	} {
		testutil.AssertContains(t, out, want)
	}
}

func TestHistoryFilters(t *testing.T) {
	_, rules := newHistoryRepo(t)
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"sub-entity", []string{"history", "SUB"}, []string{"0.1.0 SUB-001 added >D #1", "0.2.0 SUB-001 promoted D>P #2"}},
		{"rule", []string{"history", "ENT", "--rule", "ENT-004"}, []string{
			"0.1.0 ENT-004 added >D #1", "0.2.0 ENT-004 state_changed D>I #2",
			"0.3.0 ENT-004 retired I>R #3", "0.4.0 ENT-004 terminated R>T #4",
		}},
		{"rule history includes the superseder", []string{"rule", "history", "ENT-002"}, []string{
			"0.1.0 ENT-002 added >D #1", "0.2.0 ENT-002 promoted D>P #2", "0.3.0 ENT-002 promoted P>I #3",
			"0.3.0 ENT-002 superseded >I #3 by ENT-005", "0.3.0 ENT-005 added >D #3 supersedes ENT-002",
		}},
		{"since", []string{"history", "ENT", "--since", "0.2.0"}, concat(histCommit3, histCommit4)},
		{"between", []string{"history", "ENT", "--between", "0.1.0..0.2.0"}, histCommit2},
		{"between with no lower bound", []string{"history", "ENT", "--between", "..0.1.0"}, histCommit1},
		{"nothing matches", []string{"history", "ENT", "--since", "9.0.0"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := historyEvents(t, append(tt.args, "--dir", rules)...)
			assertLines(t, got, tt.want)
		})
	}
}

func TestHistoryNoChangesTable(t *testing.T) {
	_, rules := newHistoryRepo(t)
	out, _, err := runHistorySplit(t, "history", "ENT", "--dir", rules, "--since", "9.0.0")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "No changes.")
}

func TestHistoryErrors(t *testing.T) {
	repo, rules := newHistoryRepo(t)
	writeFile(t, repo, "rules/NEW.json", strings.Replace(histEntityJSON("0.1.0", nil, nil), `"id": "ENT"`, `"id": "NEW"`, 1))

	plain := t.TempDir()
	writeFile(t, plain, "rules/ENT.json", histEntityJSON("0.1.0", nil, nil))

	shallow := filepath.Join(t.TempDir(), "shallow")
	gitRun(t, repo, "clone", "-q", "--depth", "1", "file://"+filepath.ToSlash(repo), shallow)

	tests := []struct {
		name string
		args []string
		exit int
		want string
	}{
		{"not a git checkout", []string{"history", "ENT", "--dir", filepath.Join(plain, "rules")}, exitConfig, "needs a git checkout"},
		{"shallow clone", []string{"history", "ENT", "--dir", filepath.Join(shallow, "rules")}, exitConfig, "git fetch --unshallow"},
		{"untracked file", []string{"history", "NEW", "--dir", rules}, exitConfig, "no committed history"},
		{"unknown entity", []string{"history", "NOPE", "--dir", rules}, exitViolations, "not found"},
		{"malformed rule ID", []string{"rule", "history", "ENT-1", "--dir", rules}, exitViolations, "invalid rule ID"},
		{"malformed --rule", []string{"history", "ENT", "--rule", "ENT", "--dir", rules}, exitViolations, "invalid rule ID"},
		{"bad version", []string{"history", "ENT", "--since", "x", "--dir", rules}, exitConfig, "not a semantic version"},
		{"bad range", []string{"history", "ENT", "--between", "0.1.0", "--dir", rules}, exitConfig, "V1..V2"},
		{"since and between", []string{"history", "ENT", "--since", "0.1.0", "--between", "0.1.0..0.2.0", "--dir", rules}, exitConfig, "together"},
		{"no argument", []string{"history"}, exitConfig, "arg"},
		{"unknown flag", []string{"history", "ENT", "--nope"}, exitConfig, "unknown flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := runHistorySplit(t, tt.args...)
			assertExit(t, err, tt.exit)
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestHistoryUncommittedChanges(t *testing.T) {
	repo, rules := newHistoryRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "rules", "ENT.json"), []byte(histEntityJSON("0.5.0", nil, nil)), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err := runHistory(context.Background(), &out, &errOut, "ENT", &historyOptions{dir: rules, jsonOutput: true})
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, errOut.String(), "rules/ENT.json has uncommitted changes")
	var doc historyOutput
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out.String())
	}
	if len(doc.Events) != len(concat(histCommit1, histCommit2, histCommit3, histCommit4)) {
		t.Errorf("got %d events, want the committed ones only", len(doc.Events))
	}
}
