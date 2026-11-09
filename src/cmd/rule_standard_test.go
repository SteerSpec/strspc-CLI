package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// standardRealmMeta is the manifest of the Standard Realm the tests author.
func standardRealmMeta(id string, profiles ...string) entity.RealmMeta {
	return entity.RealmMeta{
		ID: id, Title: "ASVS", Version: "0.1.0", Kind: entity.KindStandard, Edition: "5.0.0",
		License: "CC-BY-SA-4.0", Attribution: &entity.Attribution{Text: "OWASP ASVS 5.0.0, by the OWASP Foundation", URL: "https://owasp.org/"},
		Profiles: profiles,
	}
}

// setupStandardRealm writes, in dir, a Standard Realm declaring profiles L1 to
// L3, with this repo's vendored schemas and the entity V01, which has the
// sub-entity V01S01.
func setupStandardRealm(t *testing.T, dir string) {
	t.Helper()
	writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{
		Schema: "./_schema/realm.v1.schema.json", Realm: standardRealmMeta("dev.test.asvs", "L1", "L2", "L3"),
		Dependencies: []entity.RealmDep{},
	})
	copyRepoSchemas(t, dir)
	setupEntityFileWithSub(t, dir, "V01", "V01S01")
}

// copyRepoSchemas copies this repo's vendored schemas into dir/_schema.
func copyRepoSchemas(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"realm.v1.schema.json", "entity.v1.schema.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "rules", "_schema", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_schema", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runRule(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"rule"}, args...)...)
}

// TestRuleAddStandard transcribes clauses into a Standard Realm through rule
// add only, and the Realm then passes realm validate and lint, strictly.
func TestRuleAddStandard(t *testing.T) {
	dir := t.TempDir()
	setupStandardRealm(t, dir)

	out, err := runRule(t, "add", "V01S01", "--body", "Verify that the application MUST do one thing.",
		"--clause", "V1.1.1", "--profiles", "L1,L2", "--profiles", "L3", "--added-by", "github:alice", "--dir", dir)
	if err != nil {
		t.Fatalf("rule add: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "Added rule V01S01-001")
	testutil.AssertContains(t, out, "Clause: V1.1.1 · Profiles: L1, L2, L3")

	out, err = runRule(t, "add", "V01S01", "--body", "Verify that it MUST do another.",
		"--clause", "V1.1.1", "--profiles", "L1,L2,L3", "--added-by", "github:alice", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("rule add --json: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["rule_id"] != "V01S01-002" || got["clause"] != "V1.1.1" || len(got["profiles"].([]any)) != 3 {
		t.Errorf("JSON = %v", got)
	}

	sub := findSubEntity(loadEntityFile(t, dir, "V01"), "V01S01")
	if len(sub.Rules) != 2 {
		t.Fatalf("rules = %+v", sub.Rules)
	}
	for _, r := range sub.Rules {
		if r.Clause != "V1.1.1" || !slices.Equal(r.Profiles, []string{"L1", "L2", "L3"}) {
			t.Errorf("rule %s: clause %q, profiles %q", r.ID, r.Clause, r.Profiles)
		}
	}
	assertFileHashFresh(t, dir, "V01")

	for _, args := range [][]string{{"realm", "validate", dir, "--strict"}, {"lint", dir, "--strict"}} {
		if out, err := testutil.ExecuteCommand(NewRootCmd(), args...); err != nil {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
}

// TestRuleAddStandardRefusals passes manager's refusals through, each citing
// the rule it enforces.
func TestRuleAddStandardRefusals(t *testing.T) {
	tests := []struct {
		name    string
		code    bool // a code Realm instead of a Standard Realm
		args    []string
		wantErr string
	}{
		{name: "clause in a code realm", code: true, args: []string{"--clause", "V1"}, wantErr: "RUL-017"},
		{name: "profiles in a code realm", code: true, args: []string{"--profiles", "L1"}, wantErr: "RUL-017"},
		{name: "empty clause in a code realm", code: true, args: []string{"--clause="}, wantErr: "RUL-017"},
		{name: "empty profiles in a code realm", code: true, args: []string{"--profiles="}, wantErr: "RUL-017"},
		{name: "empty profiles", args: []string{"--clause", "V1.1.1", "--profiles="}, wantErr: "--profiles: want one or more profile names (RUL-016)"},
		{name: "empty clause", args: []string{"--clause=", "--profiles", "L1"}, wantErr: "STDRLM-005"},
		{name: "no clause", args: []string{"--profiles", "L1"}, wantErr: "STDRLM-005"},
		{name: "no profiles", args: []string{"--clause", "V1.1.1"}, wantErr: "STDRLM-018"},
		{name: "undeclared profile", args: []string{"--clause", "V1.1.1", "--profiles", "L4"}, wantErr: "STDRLM-008"},
		{name: "repeated profile", args: []string{"--clause", "V1.1.1", "--profiles", "L1,L1"}, wantErr: "RUL-016"},
		{name: "spaced clause", args: []string{"--clause", " V1.1.1", "--profiles", "L1"}, wantErr: "RUL-015"},
		{name: "other profiles for the clause", args: []string{"--clause", "V1.1.2", "--profiles", "L1"}, wantErr: "STDRLM-009"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.code {
				setupValidRealm(t, dir)
				setupEntityFile(t, dir, "V01S01")
			} else {
				setupStandardRealm(t, dir)
				if _, err := runRule(t, "add", "V01S01", "--body", "B.", "--clause", "V1.1.2", "--profiles", "L2",
					"--added-by", "github:alice", "--dir", dir); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(filepath.Join(dir, "V01S01.json"))
			if tt.code && err != nil {
				t.Fatal(err)
			}
			args := append([]string{"add", "V01S01", "--body", "B.", "--added-by", "github:alice", "--dir", dir}, tt.args...)
			_, err = runRule(t, args...)
			if err == nil {
				t.Fatal("rule add succeeded")
			}
			testutil.AssertContains(t, err.Error(), tt.wantErr)
			if tt.code {
				after, _ := os.ReadFile(filepath.Join(dir, "V01S01.json"))
				if !bytes.Equal(after, before) {
					t.Error("a refused rule add changed the entity file")
				}
			}
		})
	}
}

// TestRuleAddCodeRealmUnchanged adds a rule with no clause or profiles in a
// code Realm, as before.
func TestRuleAddCodeRealmUnchanged(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	setupEntityFile(t, dir, "TST")
	out, err := runRule(t, "add", "TST", "--body", "The system MUST work.", "--added-by", "github:alice", "--dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertNotContains(t, out, "clause")
	testutil.AssertNotContains(t, out, "profiles")
	r := loadEntityFile(t, dir, "TST").Rules[0]
	if r.Clause != "" || r.Profiles != nil {
		t.Errorf("rule = %+v", r)
	}
}

// TestRuleAddStandardSubRealm reads the kind of the sub-realm holding the
// entity file, not its parent's, also when --dir is the parent's and the
// target is a sub-entity found below it.
func TestRuleAddStandardSubRealm(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	sub := filepath.Join(dir, "auth")
	writeJSONFile(t, filepath.Join(sub, "realm.json"), &entity.RealmFile{
		Realm: standardRealmMeta("dev.steerspec.test.auth", "L1"), Dependencies: []entity.RealmDep{},
	})
	setupEntityFileWithSub(t, sub, "V02", "V02S01")
	if out, err := runRule(t, "add", "V02S01", "--body", "B.", "--clause", "V2.1.1", "--profiles", "L1",
		"--added-by", "github:alice", "--dir", dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if r := findSubEntity(loadEntityFile(t, sub, "V02"), "V02S01").Rules[0]; r.Clause != "V2.1.1" {
		t.Errorf("rule = %+v", r)
	}

	// And a code sub-realm of a Standard Realm refuses them.
	dir = t.TempDir()
	setupStandardRealm(t, dir)
	code := filepath.Join(dir, "code")
	writeJSONFile(t, filepath.Join(code, "realm.json"), &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test.asvs.code", Title: "c", Version: "0.1.0"}, Dependencies: []entity.RealmDep{},
	})
	setupEntityFileWithSub(t, code, "CODE", "CODESUB")
	_, err := runRule(t, "add", "CODESUB", "--body", "B.", "--clause", "V1", "--added-by", "github:alice", "--dir", dir)
	if err == nil {
		t.Fatal("rule add succeeded in a code sub-realm")
	}
	testutil.AssertContains(t, err.Error(), "RUL-017")
}

// TestRuleAddStandardOldCore refuses a clause in a Realm pinning a core whose
// schemas have no clause.
func TestRuleAddStandardOldCore(t *testing.T) {
	dir := t.TempDir()
	setupStandardRealm(t, dir)
	writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{
		Schema: "./_schema/realm.v1.schema.json", Realm: standardRealmMeta("dev.test.asvs", "L1"),
		Dependencies: []entity.RealmDep{{RealmID: coreRealmID, Version: "1.20.0"}},
	})
	_, err := runRule(t, "add", "V01S01", "--body", "B.", "--clause", "V1", "--profiles", "L1",
		"--added-by", "github:alice", "--dir", dir)
	if err == nil {
		t.Fatal("rule add succeeded")
	}
	testutil.AssertContains(t, err.Error(), "whose schemas reject `clause` (it needs "+manifestMinCore.String()+" or later)")

	// A code Realm refuses the flags as such (RUL-017), whatever its core pin.
	dir = t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{
		Realm:        entity.RealmMeta{ID: "dev.test.code", Title: "c", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{{RealmID: coreRealmID, Version: "1.20.0"}},
	})
	setupEntityFile(t, dir, "CODE")
	_, err = runRule(t, "add", "CODE", "--body", "B.", "--clause", "V1", "--added-by", "github:alice", "--dir", dir)
	if err == nil {
		t.Fatal("rule add succeeded in a code realm")
	}
	testutil.AssertContains(t, err.Error(), "RUL-017")
	testutil.AssertNotContains(t, err.Error(), "schemas reject")
}

// TestRuleSupersedeStandard keeps the clause and profiles of the superseded
// rule (STDRLM-012), and rule show prints them.
func TestRuleSupersedeStandard(t *testing.T) {
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		setupStandardRealm(t, dir)
		clauseRule := makeRule("V01-001", entity.StatePublished, "Verify that X MUST hold.", "github:alice")
		clauseRule.Clause, clauseRule.Profiles = "V1.2.3", []string{"L2", "L3"}
		setupEntityFile(t, dir, "V01", clauseRule)
		return dir
	}

	out, err := runRule(t, "supersede", "V01-001", "--body", "Verify again.", "--added-by", "github:alice", "--dir", setup(t))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "Clause: V1.2.3 · Profiles: L2, L3")

	dir := setup(t)
	out, err = runRule(t, "supersede", "V01-001", "--body", "Verify that X MUST always hold.", "--added-by", "github:alice", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["rule_id"] != "V01-002" || got["clause"] != "V1.2.3" || len(got["profiles"].([]any)) != 2 {
		t.Errorf("JSON = %v", got)
	}
	r := loadEntityFile(t, dir, "V01").Rules[1]
	if r.Clause != "V1.2.3" || !slices.Equal(r.Profiles, []string{"L2", "L3"}) {
		t.Errorf("new rule = %+v", r)
	}
}

// TestRuleShowStandardFields shows a rule's clause and profiles, and nothing
// for a rule without them.
func TestRuleShowStandardFields(t *testing.T) {
	r := shownRule{ID: "V01-001", State: "P", Entity: "V01", Body: "Verify.", Clause: "V1.2.3", Profiles: []string{"L2", "L3"}, Notes: []shownNote{}}
	plain := shownRule{ID: "MOD-001", State: "I", Entity: "MOD", Body: "Do.", Notes: []shownNote{}}

	var b bytes.Buffer
	writeShownRules(&b, []*shownRule{&r, &plain})
	testutil.AssertContains(t, b.String(), "V01-001 [P] V01\n  Clause: V1.2.3 · Profiles: L2, L3\n  Verify.")
	testutil.AssertContains(t, b.String(), "MOD-001 [I] MOD\n  Do.")

	data, err := json.Marshal([]*shownRule{&r, &plain})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, string(data), `"clause":"V1.2.3","profiles":["L2","L3"]`)
	if n := strings.Count(string(data), `"clause"`); n != 1 {
		t.Errorf("clause appears %d times in %s, want once", n, data)
	}
}

// TestRuleAddStandardNestedSubRealmPin follows the core pin of a sub-realm
// nested two deep, not only of the first sub-realm below --dir.
func TestRuleAddStandardNestedSubRealmPin(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "t", Version: "0.1.0"}, SubRealms: []string{"a"},
		Dependencies: []entity.RealmDep{{RealmID: coreRealmID, Version: "1.23.0"}},
	})
	a := filepath.Join(dir, "a")
	writeJSONFile(t, filepath.Join(a, "realm.json"), &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test.a", Title: "a", Version: "0.1.0"}, SubRealms: []string{"leaf"},
		Dependencies: []entity.RealmDep{},
	})
	leaf := filepath.Join(a, "leaf")
	writeJSONFile(t, filepath.Join(leaf, "realm.json"), &entity.RealmFile{
		Realm: standardRealmMeta("dev.test.a.leaf", "L1"), Dependencies: []entity.RealmDep{{RealmID: coreRealmID, Version: "1.20.0"}},
	})
	setupEntityFileWithSub(t, leaf, "V03", "V03S01")

	_, err := runRule(t, "add", "V03S01", "--body", "B.", "--clause", "V3.1.1", "--profiles", "L1",
		"--added-by", "github:alice", "--dir", dir)
	if err == nil {
		t.Fatal("rule add succeeded against a 1.20.0 pin")
	}
	testutil.AssertContains(t, err.Error(), "realm dev.test.a.leaf pins "+coreRealmID+" 1.20.0, whose schemas reject `clause`")
}
