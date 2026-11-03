package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func readRealmStandards(t *testing.T, dir string) []entity.StandardRef {
	t.Helper()
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatalf("loading realm.json: %v", err)
	}
	return rf.Realm.Standards
}

func TestRealmStandardsAddListRemove(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add",
		"--id", "owasp.asvs", "--relation", "implements", "--version", "4.0.3", "--dir", dir); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := readRealmStandards(t, dir)
	if len(got) != 1 || got[0] != (entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "4.0.3"}) {
		t.Fatalf("standards = %+v", got)
	}

	// Writing deps afterwards keeps the standards.
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "dep", "add", coreRealmID+"@"+standardsMinCore.String(), "--dir", dir); err != nil {
		t.Fatalf("dep add: %v", err)
	}
	if len(readRealmStandards(t, dir)) != 1 {
		t.Fatal("realm dep add dropped the standards")
	}

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "list", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	testutil.AssertContains(t, out, `"id": "owasp.asvs"`)

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add",
		"--id", "owasp.asvs", "--relation", "derived_from", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "STDREF-003") {
		t.Fatalf("duplicate error = %v, want STDREF-003", err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "remove", "--id", "owasp.asvs", "--dir", dir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if strings.Contains(string(data), `"standards"`) {
		t.Errorf("empty standards key left in realm.json:\n%s", data)
	}
	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "list", "--dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, out, `"standards": []`)
}

func TestRealmStandardsAddValidation(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	before, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	for _, args := range [][]string{
		{"--id", "Bad", "--relation", "implements"},
		{"--id", "iso.27001", "--relation", "nope"},
		{"--id", "iso.27001", "--relation", "implements", "--version", ""},
		{"--id", "iso.27001", "--relation", "implements", "--url", "ftp://x.org"},
	} {
		if _, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "standards", "add", "--dir", dir}, args...)...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(before, after) {
		t.Error("realm.json changed on error")
	}
}

func TestRealmStandardsRemoveGuard(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Realm.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: "implements"}, {ID: "iso.27001", Relation: "implements"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	setupEntityFile(t, dir, "MYE", makeRule("MYE-001", "D", "MUST x.", "@test"))
	editEntityFile(t, dir, "MYE", func(f *entity.File) {
		f.Notes = []entity.Note{standardNote("MYE-001/01", "MYE-001", entity.NoteTypeStandard, "owasp.asvs:V2.1")}
	})
	before, _ := os.ReadFile(filepath.Join(dir, "realm.json"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "remove", "--id", "owasp.asvs", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "MYE-001/01") {
		t.Fatalf("error = %v, want refusal naming MYE-001/01", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	if !bytes.Equal(before, after) {
		t.Error("realm.json changed on refusal")
	}

	// A standard no note maps can go.
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "remove", "--id", "iso.27001", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
