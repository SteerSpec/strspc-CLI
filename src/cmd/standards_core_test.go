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

// pinCore sets the dev.steerspec.core dependency of the realm at dir, or
// removes it when version is "".
func pinCore(t *testing.T, dir, version string) {
	t.Helper()
	err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Dependencies = nil
		if version != "" {
			rf.Dependencies = []entity.RealmDep{{RealmID: coreRealmID, Version: version}}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStandardsAddRefusesOldCore(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	pinCore(t, dir, "1.14.0")
	setupEntityFile(t, dir, "MYE")
	realmBefore, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	entityBefore, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))

	for _, args := range [][]string{
		{"realm", "standards", "add"},
		{"entity", "standards", "add", "MYE"},
	} {
		args = append(args, "--id", "iso.27001", "--relation", "implements", "--dir", dir)
		_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		if err == nil || !strings.Contains(err.Error(), "1.14.0") || !strings.Contains(err.Error(), "realm dep upgrade dev.steerspec.core --to 1.15.0") {
			t.Errorf("%s: error = %v, want a refusal naming the pin and the upgrade", args[0], err)
		}
	}
	realmAfter, _ := os.ReadFile(filepath.Join(dir, "realm.json"))
	entityAfter, _ := os.ReadFile(filepath.Join(dir, "MYE.json"))
	if !bytes.Equal(realmBefore, realmAfter) || !bytes.Equal(entityBefore, entityAfter) {
		t.Error("a file changed on refusal")
	}

	// Once the realm is on 1.15.0 the same commands go through.
	pinCore(t, dir, "1.15.0")
	for _, args := range [][]string{
		{"realm", "standards", "add"},
		{"entity", "standards", "add", "MYE"},
	} {
		args = append(args, "--id", "iso.27001", "--relation", "implements", "--dir", dir)
		if _, err := testutil.ExecuteCommand(NewRootCmd(), args...); err != nil {
			t.Errorf("%s: unexpected error: %v", args[0], err)
		}
	}
}

func TestCorePinFor(t *testing.T) {
	t.Run("no manifest", func(t *testing.T) {
		dir := t.TempDir()
		if id, pin := corePinFor(dir, filepath.Join(dir, "MYE.json")); id != "" || pin != "" {
			t.Errorf("got %q %q, want none", id, pin)
		}
	})
	t.Run("no pin", func(t *testing.T) {
		dir := t.TempDir()
		setupValidRealm(t, dir)
		if err := checkCoreAcceptsStandards(dir, filepath.Join(dir, "MYE.json")); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("not a version", func(t *testing.T) {
		dir := t.TempDir()
		setupValidRealm(t, dir)
		pinCore(t, dir, "main")
		if err := checkCoreAcceptsStandards(dir, filepath.Join(dir, "MYE.json")); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	pinCore(t, dir, "1.15.0")
	sub := filepath.Join(dir, "auth")
	subFile := filepath.Join(sub, "MYE.json")

	t.Run("sub-realm inherits the root pin", func(t *testing.T) {
		pinCore(t, sub, "")
		if _, pin := corePinFor(dir, subFile); pin != "1.15.0" {
			t.Errorf("pin = %q, want 1.15.0", pin)
		}
	})
	t.Run("sub-realm's own pin wins", func(t *testing.T) {
		pinCore(t, sub, "1.14.0")
		if _, pin := corePinFor(dir, subFile); pin != "1.14.0" {
			t.Errorf("pin = %q, want 1.14.0", pin)
		}
		if err := checkCoreAcceptsStandards(dir, subFile); err == nil {
			t.Error("expected a refusal for the sub-realm's 1.14.0 pin")
		}
	})
	t.Run("sub-realm as --dir falls back to its parent", func(t *testing.T) {
		pinCore(t, sub, "")
		if _, pin := corePinFor(sub, subFile); pin != "1.15.0" {
			t.Errorf("pin = %q, want 1.15.0 from the parent", pin)
		}
	})
}
