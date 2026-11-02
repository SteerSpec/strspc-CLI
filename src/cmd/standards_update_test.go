package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

var asvs4 = entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "4.0.3", URL: "https://owasp.org/asvs"}

// setupStandardsTree writes MYE declaring asvs4 with a note mapping it, and a
// sub-entity SUB with a note of its own. When subDeclares, SUB declares the id
// too, so its note resolves to SUB rather than MYE.
func setupStandardsTree(t *testing.T, dir string, subDeclares bool) {
	t.Helper()
	setupEntityFileWithSub(t, dir, "MYE", "SUB", makeRule("SUB-001", "D", "MUST y.", "@test"))
	editEntityFile(t, dir, "MYE", func(f *entity.File) {
		f.Rules = []entity.Rule{makeRule("MYE-001", "D", "MUST x.", "@test")}
		f.Entity.Standards = []entity.StandardRef{asvs4}
		f.Notes = []entity.Note{standardNote("MYE-001/01", "MYE-001", entity.NoteTypeStandard, "owasp.asvs:V2.1")}
		sub := &f.SubEntities[0]
		sub.Notes = []entity.Note{standardNote("SUB-001/01", "SUB-001", entity.NoteTypeStandard, "owasp.asvs:V3.1")}
		if subDeclares {
			sub.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
		}
	})
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "MYE", "--dir", dir); err != nil {
		t.Fatal(err)
	}
}

func TestEntityStandardsUpdateVersion(t *testing.T) {
	for _, tc := range []struct {
		name        string
		subDeclares bool
		want        []string
	}{
		{"notes in the subtree", false, []string{"MYE-001/01", "SUB-001/01"}},
		{"sub-entity's own declaration", true, []string{"MYE-001/01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			setupStandardsTree(t, dir, tc.subDeclares)
			before := loadEntityFile(t, dir, "MYE").RuleSet.Version

			out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "update", "MYE",
				"--id", "owasp.asvs", "--version", "5.0.0", "--dir", dir, "--json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var u standardUpdate
			if err := json.Unmarshal([]byte(out), &u); err != nil {
				t.Fatalf("parsing %q: %v", out, err)
			}
			if u.OldStandard != asvs4 || u.Standard.Version != "5.0.0" || u.Standard.URL != asvs4.URL {
				t.Errorf("standards = %+v → %+v", u.OldStandard, u.Standard)
			}
			if !slices.Equal(u.Notes, tc.want) {
				t.Errorf("notes = %v, want %v", u.Notes, tc.want)
			}
			f := loadEntityFile(t, dir, "MYE")
			if got := f.Entity.Standards; len(got) != 1 || got[0].Version != "5.0.0" {
				t.Errorf("file standards = %+v", got)
			}
			if f.RuleSet.Version == before || u.OldVersion != before || u.Version != f.RuleSet.Version {
				t.Errorf("version %s → %s, output %s → %s", before, f.RuleSet.Version, u.OldVersion, u.Version)
			}
			assertFileHashFresh(t, dir, "MYE")
		})
	}
}

// The notes listed are those whose nearest declaration is the edited one, even
// when an outer scope declares the same id: the Realm's declaration is farther.
func TestEntityStandardsUpdateVersionOuterDeclaration(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Realm.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	setupStandardsTree(t, dir, false)

	for _, tc := range []struct {
		euid string
		want []string
	}{
		{"MYE", []string{"MYE-001/01", "SUB-001/01"}},
		{"SUB", []string{"SUB-001/01"}},
	} {
		if tc.euid == "SUB" {
			editEntityFile(t, dir, "MYE", func(f *entity.File) {
				f.SubEntities[0].Entity.Standards = []entity.StandardRef{asvs4}
			})
		}
		out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "update", tc.euid,
			"--id", "owasp.asvs", "--version", "9.9.9", "--dir", dir, "--json")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.euid, err)
		}
		var u standardUpdate
		if err := json.Unmarshal([]byte(out), &u); err != nil {
			t.Fatalf("parsing %q: %v", out, err)
		}
		if !slices.Equal(u.Notes, tc.want) {
			t.Errorf("%s: notes = %v, want %v", tc.euid, u.Notes, tc.want)
		}
	}
}

func TestEntityStandardsUpdateFields(t *testing.T) {
	dir := t.TempDir()
	setupStandardsTree(t, dir, false)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "update", "MYE",
		"--id", "owasp.asvs", "--relation", "derived_from", "--clear-version", "--clear-url", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Updated standard owasp.asvs (derived_from)")
	testutil.AssertContains(t, out, "was owasp.asvs 4.0.3 (implements)")
	testutil.AssertContains(t, out, "now map clauses of owasp.asvs no version instead of 4.0.3")
	got := loadEntityFile(t, dir, "MYE").Entity.Standards
	if want := (entity.StandardRef{ID: "owasp.asvs", Relation: "derived_from"}); len(got) != 1 || got[0] != want {
		t.Errorf("standards = %+v, want %+v", got, want)
	}
	assertFileHashFresh(t, dir, "MYE")
}

func TestEntityStandardsUpdateValidation(t *testing.T) {
	dir := t.TempDir()
	setupStandardsTree(t, dir, false)
	path := filepath.Join(dir, "MYE.json")
	before, _ := os.ReadFile(path)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no id", []string{"--version", "5.0.0"}},
		{"nothing to change", []string{"--id", "owasp.asvs"}},
		{"same value", []string{"--id", "owasp.asvs", "--version", "4.0.3"}},
		{"unknown id", []string{"--id", "iso.27001", "--version", "1"}},
		{"empty version", []string{"--id", "owasp.asvs", "--version", ""}},
		{"empty url", []string{"--id", "owasp.asvs", "--url", ""}},
		{"version and clear-version", []string{"--id", "owasp.asvs", "--version", "5", "--clear-version"}},
		{"url and clear-url", []string{"--id", "owasp.asvs", "--url", "https://x.org", "--clear-url"}},
		{"bad relation", []string{"--id", "owasp.asvs", "--relation", "nope"}},
		{"bad url", []string{"--id", "owasp.asvs", "--url", "ftp://x.org"}},
		{"control char", []string{"--id", "owasp.asvs", "--version", "5\x1b"}},
		{"control char in id", []string{"--id", "owasp.asvs\n", "--version", "5"}},
	} {
		args := append([]string{"entity", "standards", "update", "MYE", "--dir", dir}, tc.args...)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), args...); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("entity file changed on error")
	}
}

func TestRealmStandardsUpdate(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Realm.Standards = []entity.StandardRef{asvs4}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// AAA's note resolves to the Realm; BBB declares the id, so its note doesn't.
	setupEntityFile(t, dir, "AAA", makeRule("AAA-001", "D", "MUST x.", "@test"))
	editEntityFile(t, dir, "AAA", func(f *entity.File) {
		f.Notes = []entity.Note{standardNote("AAA-001/01", "AAA-001", entity.NoteTypeStandard, "owasp.asvs:V2.1")}
	})
	setupEntityFile(t, dir, "BBB", makeRule("BBB-001", "D", "MUST x.", "@test"))
	editEntityFile(t, dir, "BBB", func(f *entity.File) {
		f.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
		f.Notes = []entity.Note{standardNote("BBB-001/01", "BBB-001", entity.NoteTypeStandard, "owasp.asvs:V2.1")}
	})

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "update",
		"--id", "owasp.asvs", "--version", "5.0.0", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var u standardUpdate
	if err := json.Unmarshal([]byte(out), &u); err != nil {
		t.Fatalf("parsing %q: %v", out, err)
	}
	if u.Realm != "dev.steerspec.test" || u.Standard.Version != "5.0.0" || !slices.Equal(u.Notes, []string{"AAA-001/01"}) {
		t.Errorf("output = %+v", u)
	}
	if got := readRealmStandards(t, dir); len(got) != 1 || got[0].Version != "5.0.0" {
		t.Errorf("realm standards = %+v", got)
	}

	// A relation change lists no notes and leaves the version alone.
	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "update",
		"--id", "owasp.asvs", "--relation", "derived_from", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Updated standard owasp.asvs 5.0.0 (derived_from) on Realm")
	if got := readRealmStandards(t, dir); got[0].Relation != "derived_from" || got[0].Version != "5.0.0" {
		t.Errorf("realm standards = %+v", got)
	}

	before, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "update", "--id", "iso.27001", "--version", "1", "--dir", dir); err == nil {
		t.Error("unknown id: expected an error")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(before, after) {
		t.Error("realm.json changed on error")
	}
}
