package cmd

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// undeclaredStdRealm is a Standard Realm no fixture declares.
const undeclaredStdRealm = "dev.steerspec.std.other.v1"

// addDep declares realmID@version in the realm.json at dir.
func addDep(t *testing.T, dir, realmID, version string) {
	t.Helper()
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Dependencies = append(rf.Dependencies, entity.RealmDep{RealmID: realmID, Version: version})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func runStrspc(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), args...)
}

// TestIsSubRealmName accepts what realm validate accepts (RM008): a name
// directly below the Realm, with neither slash on any OS.
func TestIsSubRealmName(t *testing.T) {
	for name, want := range map[string]bool{
		"auth": true, "auth-v2": true,
		"": false, ".": false, "..": false, "a/b": false, `a\b`: false, `\`: false, "/abs": false, "../up": false,
	} {
		if got := isSubRealmName(name); got != want {
			t.Errorf("isSubRealmName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestStandardsLinkIgnoresBadSubRealm doesn't count a dependency declared
// only under a sub_realms entry validate skips (RM008).
func TestStandardsLinkIgnoresBadSubRealm(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	pinCoreFrom(t, dir, manifestMinCore.String(), "")
	bad := `foo\bar`
	if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.SubRealms = []string{bad}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// On Unix the entry is a directory name; give it a manifest declaring
	// the realm, which validate still never reads.
	if runtime.GOOS != "windows" {
		setupSubRealm(t, dir, bad, "dev.steerspec.test")
		addDep(t, filepath.Join(dir, bad), testStdRealm, "5.0.0")
	}
	if err := checkStandardRealmDeclared(dir, testStdRealm); err == nil {
		t.Error("counted a dependency declared under a sub_realms entry validate skips")
	}
}

// TestStandardsLinkRefusesUndeclaredRealm refuses, at both scopes and on add
// and update, a --realm no Realm of the tree declares (STDREF-017, RM026),
// writing nothing.
func TestStandardsLinkRefusesUndeclaredRealm(t *testing.T) {
	dir := t.TempDir()
	setupLinkRealm(t, dir)
	if _, err := runStrspc(t, "realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0",
		"--realm", testStdRealm, "--dir", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := runStrspc(t, "entity", "standards", "add", "MYE", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0",
		"--realm", testStdRealm, "--dir", dir); err != nil {
		t.Fatal(err)
	}
	link := []string{"--version", "1.0.0", "--realm", undeclaredStdRealm, "--dir", dir}
	for _, args := range [][]string{
		{"realm", "standards", "add", "--id", "iso.27001", "--relation", "implements"},
		{"entity", "standards", "add", "MYE", "--id", "iso.27001", "--relation", "implements"},
		{"realm", "standards", "update", "--id", "owasp.asvs"},
		{"entity", "standards", "update", "MYE", "--id", "owasp.asvs"},
	} {
		_, err := runStrspc(t, append(args, link...)...)
		if err == nil {
			t.Errorf("%s: no error", strings.Join(args[:3], " "))
			continue
		}
		for _, want := range []string{"is not a dependency of the Realm tree", "STDREF-017", "strspc realm dep add " + undeclaredStdRealm + "@<version>"} {
			testutil.AssertContains(t, err.Error(), want)
		}
	}
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.Realm.Standards) != 1 || rf.Realm.Standards[0].Realm != testStdRealm {
		t.Errorf("realm standards = %+v, want the declared link only", rf.Realm.Standards)
	}
	if got := loadEntityFile(t, dir, "MYE").Entity.Standards; len(got) != 1 || got[0].Realm != testStdRealm {
		t.Errorf("entity standards = %+v, want the declared link only", got)
	}
	// Clearing the realm needs no dependency.
	if _, err := runStrspc(t, "entity", "standards", "update", "MYE", "--id", "owasp.asvs", "--clear-realm", "--dir", dir); err != nil {
		t.Errorf("clear realm: %v", err)
	}
}

// TestStandardsLinkTreeDeclarations accepts a --realm declared anywhere in
// the tree realm validate reads from --dir: a sub-realm, or the parent of the
// sub-realm written in.
func TestStandardsLinkTreeDeclarations(t *testing.T) {
	add := func(t *testing.T, dir string) error {
		t.Helper()
		_, err := runStrspc(t, "realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0",
			"--realm", testStdRealm, "--dir", dir)
		return err
	}
	t.Run("declared by a sub-realm", func(t *testing.T) {
		dir := t.TempDir()
		setupValidRealmWithSubs(t, dir)
		pinCoreFrom(t, dir, manifestMinCore.String(), "")
		addDep(t, filepath.Join(dir, "auth"), testStdRealm, "5.0.0")
		if err := add(t, dir); err != nil {
			t.Errorf("add: %v", err)
		}
	})
	t.Run("declared by the parent", func(t *testing.T) {
		dir := t.TempDir()
		setupValidRealmWithSubs(t, dir)
		pinCoreFrom(t, dir, manifestMinCore.String(), "")
		addDep(t, dir, testStdRealm, "5.0.0")
		if err := add(t, filepath.Join(dir, "auth")); err != nil {
			t.Errorf("add in the sub-realm: %v", err)
		}
	})
	t.Run("no manifest", func(t *testing.T) {
		if err := checkStandardRealmDeclared(t.TempDir(), testStdRealm); err != nil {
			t.Errorf("no realm.json: %v", err)
		}
		if err := checkStandardRealmDeclared(t.TempDir(), ""); err != nil {
			t.Errorf("no realm: %v", err)
		}
	})
}

// TestRealmDepRemoveKeepsLinkedDependency refuses to remove a dependency a
// Standard Reference of the tree still links to, naming the references, and
// removes it once they are unlinked.
func TestRealmDepRemoveKeepsLinkedDependency(t *testing.T) {
	dir := t.TempDir()
	setupLinkRealm(t, dir)
	for _, args := range [][]string{
		{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0", "--realm", testStdRealm},
		{"entity", "standards", "add", "MYE", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0", "--realm", testStdRealm},
	} {
		if _, err := runStrspc(t, append(args, "--dir", dir)...); err != nil {
			t.Fatal(err)
		}
	}

	_, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", dir)
	if err == nil {
		t.Fatal("removed a linked dependency")
	}
	for _, want := range []string{"still linked by 2 Standard Reference(s)", "realm.json (standard owasp.asvs)", "MYE.json (standard owasp.asvs on MYE)", "--clear-realm"} {
		testutil.AssertContains(t, err.Error(), want)
	}
	rf, loadErr := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !declaresDep(rf, testStdRealm) {
		t.Error("the dependency was removed")
	}

	for _, args := range [][]string{
		{"realm", "standards", "update", "--id", "owasp.asvs", "--clear-realm"},
		{"entity", "standards", "update", "MYE", "--id", "owasp.asvs", "--clear-realm"},
	} {
		if _, err := runStrspc(t, append(args, "--dir", dir)...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", dir); err != nil {
		t.Errorf("remove once unlinked: %v", err)
	}
}

// TestRealmDepRemoveTree allows removing a linked dependency another Realm of
// the tree declares too, and from a sub-realm refuses it while an entity of
// the parent links to it.
func TestRealmDepRemoveTree(t *testing.T) {
	setup := func(t *testing.T) (dir, sub string) {
		t.Helper()
		dir = t.TempDir()
		setupValidRealmWithSubs(t, dir)
		pinCoreFrom(t, dir, manifestMinCore.String(), "")
		setupEntityFile(t, dir, "MYE", makeRule("MYE-001", "D", "The system MUST validate.", "github:alice"))
		return dir, filepath.Join(dir, "auth")
	}

	t.Run("declared elsewhere in the tree", func(t *testing.T) {
		dir, sub := setup(t)
		addDep(t, dir, testStdRealm, "5.0.0")
		addDep(t, sub, testStdRealm, "5.0.0")
		if _, err := runStrspc(t, "entity", "standards", "add", "MYE", "--id", "owasp.asvs", "--relation", "implements",
			"--version", "5.0.0", "--realm", testStdRealm, "--dir", dir); err != nil {
			t.Fatal(err)
		}
		if _, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", dir); err != nil {
			t.Errorf("remove while the sub-realm still declares it: %v", err)
		}
	})

	t.Run("linked from the parent", func(t *testing.T) {
		dir, sub := setup(t)
		addDep(t, sub, testStdRealm, "5.0.0")
		if _, err := runStrspc(t, "entity", "standards", "add", "MYE", "--id", "owasp.asvs", "--relation", "implements",
			"--version", "5.0.0", "--realm", testStdRealm, "--dir", dir); err != nil {
			t.Fatal(err)
		}
		_, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", sub)
		if err == nil {
			t.Fatal("removed a dependency the parent's entity links to")
		}
		testutil.AssertContains(t, err.Error(), "standard owasp.asvs on MYE")
	})

	// withSibling adds the sub-realm billing next to auth.
	withSibling := func(t *testing.T, dir string) string {
		t.Helper()
		setupSubRealm(t, dir, "billing", "dev.steerspec.test")
		if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
			rf.SubRealms = append(rf.SubRealms, "billing")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, "billing")
	}
	linkManifest := func(t *testing.T, dir string) {
		t.Helper()
		if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
			rf.Realm.Standards = append(rf.Realm.Standards, entity.StandardRef{
				ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0", Realm: testStdRealm,
			})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Validated from the parent, a sibling's manifest finds the realm in
	// auth's declaration, so auth must keep it.
	t.Run("linked from a sibling's manifest", func(t *testing.T) {
		dir, sub := setup(t)
		billing := withSibling(t, dir)
		addDep(t, sub, testStdRealm, "5.0.0")
		linkManifest(t, billing)
		_, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", sub)
		if err == nil {
			t.Fatal("removed a dependency a sibling's manifest links to")
		}
		testutil.AssertContains(t, err.Error(), "../billing/realm.json (standard owasp.asvs)")
	})

	// A sibling that declares the realm itself doesn't need auth's.
	t.Run("sibling declares it too", func(t *testing.T) {
		dir, sub := setup(t)
		billing := withSibling(t, dir)
		addDep(t, sub, testStdRealm, "5.0.0")
		addDep(t, billing, testStdRealm, "5.0.0")
		setupEntityFile(t, billing, "BIL", makeRule("BIL-001", "D", "The system MUST bill.", "github:alice"))
		if _, err := runStrspc(t, "entity", "standards", "add", "BIL", "--id", "owasp.asvs", "--relation", "implements",
			"--version", "5.0.0", "--realm", testStdRealm, "--dir", billing); err != nil {
			t.Fatal(err)
		}
		if _, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", sub); err != nil {
			t.Errorf("remove while billing declares it too: %v", err)
		}
	})

	// "." isn't a sub-realm (RM008): the manifest's own references count once.
	t.Run("dot in sub_realms", func(t *testing.T) {
		dir := t.TempDir()
		setupLinkRealm(t, dir)
		linkManifest(t, dir)
		if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
			rf.SubRealms = []string{"."}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", dir)
		if err == nil {
			t.Fatal("removed a linked dependency")
		}
		testutil.AssertContains(t, err.Error(), "still linked by 1 ")
	})

	// A Realm that doesn't declare the dependency has nothing to remove: the
	// command's own error, not a link scan.
	t.Run("not declared here", func(t *testing.T) {
		dir := t.TempDir()
		setupLinkRealm(t, dir)
		if err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
			rf.Realm.Standards = append(rf.Realm.Standards, entity.StandardRef{
				ID: "iso.27001", Relation: entity.RelationImplements, Version: "1.0.0", Realm: undeclaredStdRealm,
			})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err := runStrspc(t, "realm", "dep", "remove", undeclaredStdRealm, "--dir", dir)
		if err == nil || !strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "still linked") {
			t.Errorf("err = %v, want the dependency not found", err)
		}
	})

	// From a nested sub-realm, the parent's references also find the realm in
	// the parent's own parent, as validate on the parent reads them.
	t.Run("nested sub-realm", func(t *testing.T) {
		top, mid := setup(t)
		setupSubRealm(t, mid, "leaf", "dev.steerspec.test.auth")
		leaf := filepath.Join(mid, "leaf")
		if err := loadModifyWriteRealm(filepath.Join(mid, "realm.json"), func(rf *entity.RealmFile) error {
			rf.SubRealms = []string{"leaf"}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		addDep(t, top, testStdRealm, "5.0.0")
		addDep(t, leaf, testStdRealm, "5.0.0")
		setupEntityFile(t, mid, "MID", makeRule("MID-001", "D", "The system MUST log.", "github:alice"))
		if _, err := runStrspc(t, "entity", "standards", "add", "MID", "--id", "owasp.asvs", "--relation", "implements",
			"--version", "5.0.0", "--realm", testStdRealm, "--dir", mid); err != nil {
			t.Fatal(err)
		}
		if _, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", leaf); err != nil {
			t.Errorf("remove from the leaf while the top Realm still declares it: %v", err)
		}
	})

	t.Run("many links", func(t *testing.T) {
		dir, _ := setup(t)
		addDep(t, dir, testStdRealm, "5.0.0")
		for i := range 7 {
			euid := fmt.Sprintf("MY%c", 'A'+i)
			setupEntityFile(t, dir, euid, makeRule(euid+"-001", "D", "The system MUST validate.", "github:alice"))
			if _, err := runStrspc(t, "entity", "standards", "add", euid, "--id", "owasp.asvs", "--relation", "implements",
				"--version", "5.0.0", "--realm", testStdRealm, "--dir", dir); err != nil {
				t.Fatal(err)
			}
		}
		_, err := runStrspc(t, "realm", "dep", "remove", testStdRealm, "--dir", dir)
		if err == nil {
			t.Fatal("removed a linked dependency")
		}
		testutil.AssertContains(t, err.Error(), "still linked by 7")
		testutil.AssertContains(t, err.Error(), "and 2 more")
	})
}
