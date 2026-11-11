package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// makeSuccessor returns a rule in state that supersedes the rule supersededID.
func makeSuccessor(id, state, supersededID string) entity.Rule {
	r := makeRule(id, state, "Replacement rule", "github:bob")
	r.Supersedes = &supersededID
	return r
}

func TestRuleTerminateSupersededPublished(t *testing.T) {
	for _, successorState := range []string{"P", "I"} {
		t.Run(successorState, func(t *testing.T) {
			dir := t.TempDir()
			setupEntityFile(t, dir, "TST",
				makeRule("TST-001", "P", "Published rule", "github:alice"),
				makeSuccessor("TST-002", successorState, "TST-001"))

			out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "terminate", "TST-001", "--dir", dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			testutil.AssertContains(t, out, "Terminated rule TST-001: Published → Terminated")
			testutil.AssertContains(t, out, "Entity version: 0.1.0 → 0.2.0")

			f := loadEntityFile(t, dir, "TST")
			if f.Rules[0].State != "T" {
				t.Errorf("TST-001 state = %s, want T", f.Rules[0].State)
			}
			if f.Rules[1].State != successorState {
				t.Errorf("TST-002 state = %s, want %s", f.Rules[1].State, successorState)
			}
		})
	}
}

func TestRuleTerminateRetired(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "R", "Retired rule", "github:alice"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "terminate", "TST-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Retired → Terminated")

	if got := loadEntityFile(t, dir, "TST").Rules[0].State; got != "T" {
		t.Errorf("state = %s, want T", got)
	}
}

func TestRuleTerminateRefused(t *testing.T) {
	tests := []struct {
		name  string
		rules []entity.Rule
		want  []string
	}{
		{
			name: "Draft successor",
			rules: []entity.Rule{
				makeRule("TST-001", "P", "Published rule", "github:alice"),
				makeSuccessor("TST-002", "D", "TST-001"),
			},
			want: []string{"no Published rule supersedes it", `TST-002 is in state "D"`, "RULST-012"},
		},
		{
			name: "Abandoned successor",
			rules: []entity.Rule{
				makeRule("TST-001", "P", "Published rule", "github:alice"),
				makeSuccessor("TST-002", "A", "TST-001"),
			},
			want: []string{"no Published rule supersedes it", "RULST-012"},
		},
		{
			name:  "no successor",
			rules: []entity.Rule{makeRule("TST-001", "P", "Published rule", "github:alice")},
			want:  []string{"no Published rule supersedes it", "RULST-012"},
		},
		{
			name: "superseding itself",
			rules: []entity.Rule{
				makeSuccessor("TST-001", "P", "TST-001"),
			},
			want: []string{"no Published rule supersedes it", "RULST-012"},
		},
		{
			name:  "Implemented",
			rules: []entity.Rule{makeRule("TST-001", "I", "Implemented rule", "github:alice")},
			want:  []string{`state "I"`, "can be terminated"},
		},
		{
			name:  "Draft",
			rules: []entity.Rule{makeRule("TST-001", "D", "Draft rule", "github:alice")},
			want:  []string{`state "D"`, "can be terminated"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			setupEntityFile(t, dir, "TST", tt.rules...)
			path := filepath.Join(dir, "TST.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			_, err = testutil.ExecuteCommand(NewRootCmd(), "rule", "terminate", "TST-001", "--dir", dir)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}

			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("entity file was rewritten after a refused terminate")
			}
		})
	}
}

func TestRuleTerminateJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST",
		makeRule("TST-001", "P", "Published rule", "github:alice"),
		makeSuccessor("TST-002", "P", "TST-001"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "terminate", "TST-001", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing JSON %q: %v", out, err)
	}
	want := map[string]string{
		"rule_id":     "TST-001",
		"old_state":   "P",
		"new_state":   "T",
		"old_version": "0.1.0",
		"version":     "0.2.0",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("JSON keys = %v, want %d keys", got, len(want))
	}
}

func TestRuleTerminateSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB",
		makeRule("SUB-001", "P", "Published rule", "github:alice"),
		makeSuccessor("SUB-002", "P", "SUB-001"))

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "terminate", "SUB-001", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := loadEntityFile(t, dir, "TST").SubEntities[0].Rules[0].State; got != "T" {
		t.Errorf("state = %s, want T", got)
	}
}

func TestRuleTerminateUnknownRule(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "R", "Retired rule", "github:alice"))

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "terminate", "TST-009", "--dir", dir); err == nil {
		t.Fatal("expected an error for an unknown rule")
	}
}

func TestRuleTransitionWrapsCause(t *testing.T) {
	cause := errors.New("cause")
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST", makeRule("TST-001", "R", "Retired rule", "github:alice"))

	err := runRuleTransition(io.Discard, dir, "TST-001", "terminating", false,
		func(*entity.File, string) error { return cause })
	if !errors.Is(err, cause) {
		t.Fatalf("error %v does not wrap the cause", err)
	}
	if got, want := err.Error(), "terminating TST-001: cause"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}

	err = runRuleTransition(io.Discard, dir, "BAD", "terminating", false, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "terminating BAD: invalid rule ID") {
		t.Errorf("error = %v, want a wrapped invalid rule ID", err)
	}

	err = runRuleTransition(io.Discard, dir, "NOPE-001", "terminating", false, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "terminating NOPE-001: ") {
		t.Errorf("error = %v, want a wrapped entity lookup error", err)
	}
}
