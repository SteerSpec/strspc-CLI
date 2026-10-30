package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// renderedInherited is the part of `render --json` these tests read.
type renderedInherited struct {
	Inherited *[]inheritedRule `json:"inherited"`
}

// setupRenderInherit writes dev.app in rules/ with SAVACCT inheriting
// dev.lib:ACCT from lib/ and overriding ACCT-001, and returns SAVACCT's path.
func setupRenderInherit(t *testing.T) string {
	t.Helper()
	dir, _ := setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCT",
		inheritRule{id: "SAVACCT-001", state: "P", overrides: "dev.lib:ACCT-001"})
	return filepath.Join(rules, "SAVACCT.json")
}

func TestRenderListsInheritedRules(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", setupRenderInherit(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Inherited rules")
	testutil.AssertContains(t, out, "dev.lib:ACCT-001")
	testutil.AssertContains(t, out, "dev.lib:ACCT-002")
}

func TestRenderJSONAddsInheritedArray(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", setupRenderInherit(t), "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got renderedInherited
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Inherited == nil || len(*got.Inherited) != 2 {
		t.Fatalf("inherited = %+v, want 2 rows", got.Inherited)
	}
	first := (*got.Inherited)[0]
	want := inheritedRule{
		ID: "dev.lib:ACCT-001", Body: "Rule ACCT-001.", State: "P", Owner: "ACCT", OwnerRealm: "dev.lib",
		InheritedBy: "SAVACCT", OverriddenBy: "SAVACCT-001",
	}
	if first != want {
		t.Errorf("first row = %+v, want %+v", first, want)
	}
	testutil.AssertContains(t, out, `"entity"`)
}

func TestRenderJSONWithoutInheritanceHasNoInheritedKey(t *testing.T) {
	dir := t.TempDir()
	writeRealmManifest(t, dir, "dev.app")
	writeInheritEntity(t, dir, "ACCT", "", inheritRule{id: "ACCT-001", state: "P"})

	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(dir, "ACCT.json"), "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, `"inherited"`) {
		t.Errorf("unexpected inherited key:\n%s", out)
	}
}

func TestRenderJSONListsRulesInheritedBySubEntity(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	sub := inheritEntityJSON("SAVSUB", "dev.lib:ACCT", "", inheritRule{id: "SAVSUB-001", state: "P"})
	writeInheritFile(t, rules, "SAVACCT", inheritEntityJSON("SAVACCT", "", "["+sub+"]", inheritRule{id: "SAVACCT-001", state: "P"}))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(rules, "SAVACCT.json"), "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got renderedInherited
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Inherited == nil || len(*got.Inherited) != 2 || (*got.Inherited)[0].InheritedBy != "SAVSUB" {
		t.Fatalf("inherited = %+v, want 2 rows inherited by SAVSUB", got.Inherited)
	}
}

func TestRenderJSONKeepsEmptyInheritedArrayOfResolvedChain(t *testing.T) {
	draftOnly := inheritRule{id: "ACCT-001", state: "D"}
	own := inheritRule{id: "SAVACCT-001", state: "P"}
	tests := []struct {
		name, body string
	}{
		{"entity", inheritEntityJSON("SAVACCT", "ACCT", "", own)},
		{"sub-entity", inheritEntityJSON("TOP", "", "["+inheritEntityJSON("SAVACCT", "ACCT", "", own)+"]")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRealmManifest(t, dir, "dev.app")
			writeInheritEntity(t, dir, "ACCT", "", draftOnly)
			writeInheritFile(t, dir, "FILE", tt.body)

			out, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(dir, "FILE.json"), "--json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var got renderedInherited
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, out)
			}
			if got.Inherited == nil || len(*got.Inherited) != 0 {
				t.Errorf("inherited = %v, want an empty array", got.Inherited)
			}
		})
	}
}

func TestRenderDirectoryKeepsChainsOfEachRealm(t *testing.T) {
	dir := t.TempDir()
	for _, realm := range []string{"one", "two"} {
		root := filepath.Join(dir, realm)
		writeRealmManifest(t, root, "dev."+realm)
		writeInheritEntity(t, root, "ACCT", "", inheritRule{id: "ACCT-001", state: "P"})
		writeInheritEntity(t, root, "SAVACCT", "ACCT", inheritRule{id: "SAVACCT-001", state: "P"})
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "render", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, realm := range []string{"one", "two"} {
		data, err := os.ReadFile(filepath.Join(dir, realm, "SAVACCT.md"))
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertContains(t, string(data), "dev."+realm+":ACCT-001")
	}
}

func TestRenderDirectoryJSONAddsInheritedArray(t *testing.T) {
	file := setupRenderInherit(t)
	out := t.TempDir()
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Dir(file), "--json", "-o", out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "SAVACCT.json"))
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, string(data), `"inherited"`)
}

func TestRenderWarnsWhenChainCannotBeResolved(t *testing.T) {
	dir, _ := setupInheritProject(t, "null", "./rules/")
	rules := filepath.Join(dir, "rules")
	writeInheritEntity(t, rules, "SAVACCT", "dev.lib:ACCNT", inheritRule{id: "SAVACCT-001", state: "P"})

	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(rules, "SAVACCT.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "warning:")
	testutil.AssertContains(t, out, "RR013")
	testutil.AssertContains(t, out, "SAVACCT-001")
	if strings.Contains(out, "Inherited rules") {
		t.Errorf("unexpected Inherited rules section:\n%s", out)
	}
}

func TestRenderWarnsWhenInheritsHasNoRealm(t *testing.T) {
	dir := t.TempDir()
	writeInheritEntity(t, dir, "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P"})

	out, err := testutil.ExecuteCommand(NewRootCmd(), "render", filepath.Join(dir, "SAVACCT.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "warning:")
	testutil.AssertContains(t, out, "SAVACCT-001")
}
