package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
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
// ": <JSON path>". Only a colon followed by a space separates them, so a
// Windows drive letter stays in the file.
func firstPathPart(p string) string {
	file, _, _ := strings.Cut(p, ": ")
	return file
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

// TestLintDotFindsOneLetterSubRealm lints "." with a Standard sub-realm whose
// name is one character, as long as "." itself.
func TestLintDotFindsOneLetterSubRealm(t *testing.T) {
	root := t.TempDir()
	writeKindManifest(t, root, "dev.test", false, "s")
	copyRepoSchemas(t, root)
	setupEntityFile(t, root, "ROOT")
	addVerbatimRule(t, root, "ROOT", false)
	s := filepath.Join(root, "s")
	writeKindManifest(t, s, "dev.test.s", true)
	setupEntityFile(t, s, "STD")
	addVerbatimRule(t, s, "STD", true)

	t.Chdir(root)
	got := lintBodyCodes(t, ".")
	if got["RL021 ROOT.json"] == 0 {
		t.Errorf("ROOT.json: %v, want RL021", got)
	}
	if got["RL021 STD.json"] != 0 || got["RL026 STD.json"] != 0 {
		t.Errorf("s/STD.json, in a Standard Realm: %v, want no RL021 or RL026", got)
	}
}

// TestCollectSubRealmLintersReuse builds a linter for a sub-realm only when
// its core pin or kind differs from its parent's.
func TestCollectSubRealmLintersReuse(t *testing.T) {
	root := t.TempDir()
	pin := func(v string) []entity.RealmDep {
		return []entity.RealmDep{{RealmID: coreRealmID, Version: v, Source: "github://SteerSpec/strspc-rules@v" + v + "/rules/core"}}
	}
	write := func(dir, id string, meta entity.RealmMeta, deps []entity.RealmDep, subs ...string) {
		meta.ID = id
		if deps == nil {
			deps = []entity.RealmDep{}
		}
		writeJSONFile(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Realm: meta, Dependencies: deps, SubRealms: subs})
	}
	code := entity.RealmMeta{Title: "t", Version: "0.1.0"}
	write(root, "dev.t", code, pin("1.23.0"), "same", "other", "unpinned", "std")
	write(filepath.Join(root, "same"), "dev.t.same", code, pin("1.23.0"))
	write(filepath.Join(root, "other"), "dev.t.other", code, pin("1.22.0"))
	write(filepath.Join(root, "unpinned"), "dev.t.unpinned", code, nil)
	write(filepath.Join(root, "std"), "dev.t.std", standardRealmMeta("x", "L1"), nil)

	var built []string
	newLinter := func(_ []schema.Option, kind string) *rulelint.Linter {
		built = append(built, kind)
		return rulelint.New(rulelint.WithRealmKind(kind))
	}
	parent := realmLinting{linter: rulelint.New(), pin: corePinKey(root)}
	out := make(map[string]*rulelint.Linter)
	collectSubRealmLinters(root, parent, newLinter, out, 0)

	for name, shared := range map[string]bool{"same": true, "unpinned": true, "other": false, "std": false} {
		if l := out[filepath.Join(root, name)]; (l == parent.linter) != shared {
			t.Errorf("%s: shares the parent's linter = %v, want %v", name, l == parent.linter, shared)
		}
	}
	if len(built) != 2 {
		t.Errorf("built %d linter(s), kinds %q; want 2: the other pin and the Standard Realm", len(built), built)
	}
}
