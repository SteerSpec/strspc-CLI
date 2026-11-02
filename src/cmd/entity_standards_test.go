package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// editEntityFile applies edit to the entity file and saves it, as a hand edit
// would: the hash is left as it was.
func editEntityFile(t *testing.T, dir, entityID string, edit func(*entity.File)) {
	t.Helper()
	f := loadEntityFile(t, dir, entityID)
	edit(f)
	if err := saveEntity(filepath.Join(dir, entityID+".json"), f); err != nil {
		t.Fatal(err)
	}
}

func standardNote(id, ruleRef, noteType, content string) entity.Note {
	return entity.Note{ID: id, RuleRef: ruleRef, Type: noteType, Content: content, AddedBy: "@test", AddedAt: "2026-01-01"}
}

func TestEntityStandardsAdd(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "MYE")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "add", "MYE",
		"--id", "owasp.asvs", "--relation", "implements", "--version", "4.0.3", "--url", "https://owasp.org/asvs", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "owasp.asvs 4.0.3 (implements)")

	f := loadEntityFile(t, dir, "MYE")
	want := entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "4.0.3", URL: "https://owasp.org/asvs"}
	if len(f.Entity.Standards) != 1 || f.Entity.Standards[0] != want {
		t.Fatalf("standards = %+v, want [%+v]", f.Entity.Standards, want)
	}
	if f.RuleSet.Version != "0.1.1" {
		t.Errorf("version %q, want 0.1.1", f.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "MYE")

	out, err = testutil.ExecuteCommand(NewRootCmd(), "entity", "rehash", "--all", "--dir", dir)
	if err != nil {
		t.Fatalf("rehash: %v", err)
	}
	testutil.AssertContains(t, out, "0 entity file(s) updated")
}

func TestEntityStandardsAddJSONNoVersion(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "MYE")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "add", "MYE",
		"--id", "ietf.rfc2119", "--relation", "derived_from", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got entityStandardChange
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing JSON %q: %v", out, err)
	}
	if got.Entity != "MYE" || got.Standard.ID != "ietf.rfc2119" || got.OldVersion != "0.1.0" || got.Version != "0.1.1" {
		t.Errorf("unexpected output: %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "MYE.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"version": ""`) || strings.Contains(string(data), `"url"`) {
		t.Errorf("empty optional fields written:\n%s", data)
	}
}

func TestEntityStandardsAddSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "PARENT", "CHILD")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "add", "CHILD",
		"--id", "iso.27001", "--relation", "implements", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "PARENT")
	if len(f.Entity.Standards) != 0 {
		t.Errorf("parent got standards: %+v", f.Entity.Standards)
	}
	sub := findSubEntity(f, "CHILD")
	if len(sub.Entity.Standards) != 1 || sub.Entity.Standards[0].ID != "iso.27001" {
		t.Fatalf("sub-entity standards = %+v", sub.Entity.Standards)
	}
	if sub.RuleSet.Version != "0.1.1" || f.RuleSet.Version != "0.1.1" {
		t.Errorf("versions: sub %q, parent %q, want 0.1.1", sub.RuleSet.Version, f.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "PARENT")
}

func TestEntityStandardsAddValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing id", []string{"--relation", "implements"}, "--id is required"},
		{"missing relation", []string{"--id", "iso.27001"}, "--relation is required"},
		{"bad id", []string{"--id", "ISO27001", "--relation", "implements"}, "STDREF-002"},
		{"bad relation", []string{"--id", "iso.27001", "--relation", "covers"}, "STDREF-004"},
		{"empty version", []string{"--id", "iso.27001", "--relation", "implements", "--version", ""}, "STDREF-007"},
		{"relative url", []string{"--id", "iso.27001", "--relation", "implements", "--url", "iso.org/27001"}, "STDREF-008"},
		{"control char", []string{"--id", "iso.27001", "--relation", "implements", "--version", "1\x1b[0m"}, "control character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			setupEntityFile(t, dir, "MYE")
			before, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))

			args := append([]string{"entity", "standards", "add", "MYE", "--dir", dir}, tt.args...)
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
			after, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))
			if !bytes.Equal(before, after) {
				t.Error("entity file changed on error")
			}
		})
	}
}

func TestEntityStandardsAddDuplicateID(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "MYE")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "add", "MYE",
		"--id", "owasp.asvs", "--relation", "implements", "--version", "4.0.3", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))

	// Same id with another relation and edition is still a duplicate.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "add", "MYE",
		"--id", "owasp.asvs", "--relation", "derived_from", "--version", "5.0", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "STDREF-003") {
		t.Fatalf("error = %v, want STDREF-003", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))
	if !bytes.Equal(before, after) {
		t.Error("entity file changed on duplicate")
	}
}

// An EUID that isn't one can't reach a file outside --dir.
func TestEntityStandardsRemove(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "MYE")
	editEntityFile(t, dir, "MYE", func(f *entity.File) {
		f.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
	})

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "MYE", "--id", "owasp.asvs", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Removed standard owasp.asvs")
	data, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))
	if strings.Contains(string(data), `"standards"`) {
		t.Errorf("empty standards key left in file:\n%s", data)
	}
	assertFileHashFresh(t, dir, "MYE")

	_, err = testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "MYE", "--id", "owasp.asvs", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("error = %v, want not declared", err)
	}
}

func TestEntityStandardsRemoveGuard(t *testing.T) {
	setup := func(t *testing.T, noteType string) string {
		dir := t.TempDir()
		setupEntityFileWithSub(t, dir, "PARENT", "CHILD", makeRule("CHILD-001", "D", "MUST x.", "@test"))
		editEntityFile(t, dir, "PARENT", func(f *entity.File) {
			f.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
			sub := findSubEntity(f, "CHILD")
			sub.Notes = []entity.Note{standardNote("CHILD-001/01", "CHILD-001", noteType, "owasp.asvs:V2.1")}
		})
		return dir
	}

	t.Run("refused while a contained note maps it", func(t *testing.T) {
		dir := setup(t, entity.NoteTypeStandard)
		before, _ := os.ReadFile(filepath.Join(dir, "PARENT.json"))
		_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "PARENT", "--id", "owasp.asvs", "--dir", dir)
		if err == nil || !strings.Contains(err.Error(), "CHILD-001/01") || !strings.Contains(err.Error(), "NTE-016") {
			t.Fatalf("error = %v, want refusal naming CHILD-001/01", err)
		}
		after, _ := os.ReadFile(filepath.Join(dir, "PARENT.json"))
		if !bytes.Equal(before, after) {
			t.Error("entity file changed on refusal")
		}
	})

	t.Run("allowed once the mapping is withdrawn", func(t *testing.T) {
		dir := setup(t, entity.NoteTypeReference)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "PARENT", "--id", "owasp.asvs", "--dir", dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("allowed when the Realm declares it too", func(t *testing.T) {
		dir := setup(t, entity.NoteTypeStandard)
		writeRealmManifest(t, dir, "dev.app")
		if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
			rf.Realm.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "PARENT", "--id", "owasp.asvs", "--dir", dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("sub-entity removal is refused when only it declares", func(t *testing.T) {
		dir := t.TempDir()
		setupEntityFileWithSub(t, dir, "PARENT", "CHILD", makeRule("CHILD-001", "D", "MUST x.", "@test"))
		editEntityFile(t, dir, "PARENT", func(f *entity.File) {
			sub := findSubEntity(f, "CHILD")
			sub.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
			sub.Notes = []entity.Note{standardNote("CHILD-001/01", "CHILD-001", entity.NoteTypeStandard, "owasp.asvs:V2.1")}
		})
		_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "CHILD", "--id", "owasp.asvs", "--dir", dir)
		if err == nil || !strings.Contains(err.Error(), "CHILD-001/01") {
			t.Fatalf("error = %v, want refusal naming CHILD-001/01", err)
		}
	})
}

func TestEntityStandardsList(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "MYE")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "list", "MYE", "--dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, out, `"standards": []`)

	out, err = testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "list", "MYE", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, out, "declares no standard")

	editEntityFile(t, dir, "MYE", func(f *entity.File) {
		f.Entity.Standards = []entity.StandardRef{{ID: "ietf.rfc2119", Relation: "derived_from", URL: "https://www.rfc-editor.org/rfc/rfc2119"}}
	})
	out, err = testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "list", "MYE", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, out, "ietf.rfc2119")
	testutil.AssertContains(t, out, "https://www.rfc-editor.org/rfc/rfc2119")
}

// With --dir at a sub-realm, a note resolves against the sub-realm, then the
// Realm listing it (entity.ParentRealm), as rulelint.CheckRealmStandards does.
func TestEntityStandardsRemoveGuardSubRealm(t *testing.T) {
	setup := func(t *testing.T, declareOnParent bool) string {
		dir := t.TempDir()
		writeRealmManifest(t, dir, "dev.app")
		sub := filepath.Join(dir, "sync")
		writeRealmManifest(t, sub, "dev.app.sync")
		if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
			rf.SubRealms = []string{"sync"}
			if declareOnParent {
				rf.Realm.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		setupEntityFile(t, sub, "SYN", makeRule("SYN-001", "D", "MUST x.", "@test"))
		editEntityFile(t, sub, "SYN", func(f *entity.File) {
			f.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}}
			f.Notes = []entity.Note{standardNote("SYN-001/01", "SYN-001", entity.NoteTypeStandard, "owasp.asvs:V2.1")}
		})
		return dir
	}

	t.Run("refused when nothing above declares it", func(t *testing.T) {
		dir := setup(t, false)
		_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "SYN", "--id", "owasp.asvs", "--dir", filepath.Join(dir, "sync"))
		if err == nil || !strings.Contains(err.Error(), "SYN-001/01") {
			t.Fatalf("error = %v, want refusal naming SYN-001/01", err)
		}
	})

	t.Run("allowed when the parent Realm declares it", func(t *testing.T) {
		dir := setup(t, true)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "remove", "SYN", "--id", "owasp.asvs", "--dir", filepath.Join(dir, "sync")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
