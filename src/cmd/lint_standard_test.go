package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// verbatimBody is worded as a standard words it: no key word (RL021) and an
// example (RL026), which a Standard Realm's rules may keep.
const verbatimBody = "Verify that tokens, such as session tokens, are random."

// addVerbatimRule adds a Draft rule with verbatimBody to euid in dir, with a
// clause and profile when standard.
func addVerbatimRule(t *testing.T, dir, euid string, standard bool) {
	t.Helper()
	args := []string{"add", euid, "--body", verbatimBody, "--added-by", "github:alice", "--dir", dir}
	if standard {
		args = append(args, "--clause", "V1.1.1", "--profiles", "L1")
	}
	if out, err := runRule(t, args...); err != nil {
		t.Fatalf("rule add %s: %v\n%s", euid, err, out)
	}
}

// writeKindManifest writes dir/realm.json for a code Realm, or a Standard
// Realm declaring profile L1, listing subRealms.
func writeKindManifest(t *testing.T, dir, id string, standard bool, subRealms ...string) {
	t.Helper()
	meta := entity.RealmMeta{ID: id, Title: "t", Version: "0.1.0"}
	if standard {
		meta = standardRealmMeta(id, "L1")
	}
	writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{
		Schema: "./_schema/realm.v1.schema.json", Realm: meta, SubRealms: subRealms, Dependencies: []entity.RealmDep{},
	})
}

// lintBodyCodes runs lint with args and counts its RL021 and RL026
// diagnostics by the entity file they name.
func lintBodyCodes(t *testing.T, args ...string) map[string]int {
	t.Helper()
	out, _ := testutil.ExecuteCommand(NewRootCmd(), append(append([]string{"lint"}, args...), "--json")...)
	var diags []struct {
		Code string `json:"code"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &diags); err != nil {
		t.Fatalf("lint --json: %v\n%s", err, out)
	}
	got := make(map[string]int)
	for _, d := range diags {
		if d.Code == "RL021" || d.Code == "RL026" {
			got[d.Code+" "+filepath.Base(firstPathPart(d.Path))]++
		}
	}
	return got
}

// firstPathPart is the file of a diagnostic path, which may go on with
// ": <JSON path>".
func firstPathPart(p string) string {
	for i := range len(p) {
		if p[i] == ':' {
			return p[:i]
		}
	}
	return p
}

// TestLintStandardRealmBodies skips RL021 and RL026 for the rules of a
// Standard Realm, in a directory, a single file and --cross-ref, and keeps
// them for a code Realm.
func TestLintStandardRealmBodies(t *testing.T) {
	std := t.TempDir()
	setupStandardRealm(t, std)
	addVerbatimRule(t, std, "V01S01", true)

	code := t.TempDir()
	writeKindManifest(t, code, "dev.test.code", false)
	copyRepoSchemas(t, code)
	setupEntityFile(t, code, "COD")
	addVerbatimRule(t, code, "COD", false)

	for _, args := range [][]string{{std}, {filepath.Join(std, "V01.json")}, {std, "--cross-ref"}} {
		if got := lintBodyCodes(t, args...); len(got) != 0 {
			t.Errorf("lint %v in a Standard Realm: %v, want no RL021 or RL026", args, got)
		}
	}
	for _, args := range [][]string{{code}, {filepath.Join(code, "COD.json")}, {code, "--cross-ref"}} {
		if got := lintBodyCodes(t, args...); got["RL021 COD.json"] == 0 || got["RL026 COD.json"] == 0 {
			t.Errorf("lint %v in a code Realm: %v, want RL021 and RL026", args, got)
		}
	}
}

// TestLintSubRealmKinds lints each sub-realm with its own kind, at any depth:
// a Standard Realm two levels below a code Realm, and a code Realm below a
// Standard Realm.
func TestLintSubRealmKinds(t *testing.T) {
	root := t.TempDir()
	writeKindManifest(t, root, "dev.test", false, "a")
	copyRepoSchemas(t, root)
	setupEntityFile(t, root, "ROOT")
	addVerbatimRule(t, root, "ROOT", false)

	a := filepath.Join(root, "a")
	writeKindManifest(t, a, "dev.test.a", false, "leaf")
	setupEntityFile(t, a, "MID")
	addVerbatimRule(t, a, "MID", false)

	leaf := filepath.Join(a, "leaf")
	writeKindManifest(t, leaf, "dev.test.a.leaf", true, "code")
	setupEntityFile(t, leaf, "STD")
	addVerbatimRule(t, leaf, "STD", true)

	code := filepath.Join(leaf, "code")
	writeKindManifest(t, code, "dev.test.a.leaf.code", false)
	setupEntityFile(t, code, "LOW")
	addVerbatimRule(t, code, "LOW", false)

	got := lintBodyCodes(t, root)
	for _, f := range []string{"ROOT.json", "MID.json", "LOW.json"} {
		if got["RL021 "+f] == 0 || got["RL026 "+f] == 0 {
			t.Errorf("%s, in a code Realm: %v, want RL021 and RL026", f, got)
		}
	}
	if got["RL021 STD.json"] != 0 || got["RL026 STD.json"] != 0 {
		t.Errorf("STD.json, in a Standard Realm: %v, want no RL021 or RL026", got)
	}
}
