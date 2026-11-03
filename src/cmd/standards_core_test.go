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

const coreSource = "github://SteerSpec/strspc-rules@v1.14.0/rules/core"

// pinCore sets the dev.steerspec.core dependency of the realm at dir, from
// coreSource, or removes it when version is "".
func pinCore(t *testing.T, dir, version string) {
	t.Helper()
	pinCoreFrom(t, dir, version, coreSource)
}

func pinCoreFrom(t *testing.T, dir, version, source string) {
	t.Helper()
	err := loadModifyWriteRealm(filepath.Join(dir, "realm.json"), func(rf *entity.RealmFile) error {
		rf.Dependencies = nil
		if version != "" {
			rf.Dependencies = []entity.RealmDep{{RealmID: coreRealmID, Version: version, Source: source}}
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
		if err == nil || !strings.Contains(err.Error(), "1.14.0") || !strings.Contains(err.Error(), "realm dep upgrade dev.steerspec.core --to 1.15.0 --dir "+shellQuote(dir)+"`") {
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

func TestCheckCoreAcceptsStandards(t *testing.T) {
	refused := func(t *testing.T, dir, path string) bool {
		t.Helper()
		return checkCoreAcceptsStandards(dir, path) != nil
	}
	t.Run("no manifest", func(t *testing.T) {
		dir := t.TempDir()
		if refused(t, dir, filepath.Join(dir, "MYE.json")) {
			t.Error("refused without a manifest")
		}
	})
	for _, pin := range []string{"", "main", "1.14", "v1.14.0", "1.14.0-rc.1"} {
		t.Run("pin "+pin+" selects no release", func(t *testing.T) {
			dir := t.TempDir()
			setupValidRealm(t, dir)
			pinCore(t, dir, pin)
			if refused(t, dir, filepath.Join(dir, "MYE.json")) {
				t.Errorf("refused for pin %q, which lint doesn't read as a release", pin)
			}
		})
	}

	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	sub := filepath.Join(dir, "auth")
	subFile := filepath.Join(sub, "MYE.json")
	for _, tc := range []struct {
		name, root, sub string
		dir             string
		want            bool
	}{
		{"sub-realm inherits a recent root pin", "1.15.0", "", dir, false},
		{"sub-realm inherits an old root pin", "1.14.0", "", dir, true},
		{"sub-realm's own old pin", "1.15.0", "1.14.0", dir, true},
		{"sub-realm's own recent pin under an old root pin", "1.14.0", "1.15.0", dir, false},
		{"sub-realm as --dir with an old parent pin", "1.14.0", "", sub, true},
		{"sub-realm as --dir with a recent parent pin", "1.15.0", "", sub, false},
		{"sub-realm as --dir with its own recent pin under an old parent", "1.14.0", "1.15.0", sub, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinCore(t, dir, tc.root)
			pinCore(t, sub, tc.sub)
			if got := refused(t, tc.dir, subFile); got != tc.want {
				t.Errorf("refused = %v, want %v", got, tc.want)
			}
		})
	}

	// The upgrade command names the realm holding the old pin.
	pinCore(t, dir, "1.15.0")
	pinCore(t, sub, "1.14.0")
	err := checkCoreAcceptsStandards(dir, subFile)
	if err == nil || !strings.Contains(err.Error(), "--dir "+shellQuote(sub)+"`") {
		t.Errorf("error = %v, want the upgrade to target %s", err, sub)
	}
}

// The remedy names a command that can move the pin: realm dep upgrade only
// moves a github:// source pinned to a release tag.
func TestCoreAcceptsStandardsRemedy(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	upgrade := "`strspc realm dep upgrade dev.steerspec.core --to 1.15.0 --dir " + shellQuote(dir) + "`"
	// Built, not literal: strspc-rules' pin_core.py rejects stray core pins.
	pin := coreRealmID + "@" + standardsMinCore.String()
	for _, tc := range []struct {
		name, source, want string
	}{
		{"release tag", coreSource, upgrade},
		{"no source", "", "`strspc realm dep add " + pin + " --dir " + shellQuote(dir) + "`"},
		{"local", "./core", "`strspc realm dep add " + pin + "=./core --dir " + shellQuote(dir) + "`"},
		{"latest", "github://SteerSpec/strspc-rules@latest/rules/core", pin + "=github://SteerSpec/strspc-rules@latest/rules/core"},
		{"branch", "github://SteerSpec/strspc-rules@main/rules/core", pin + "=github://SteerSpec/strspc-rules@main/rules/core"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pinCoreFrom(t, dir, "1.14.0", tc.source)
			err := checkCoreAcceptsStandards(dir, filepath.Join(dir, "MYE.json"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to contain %s", err, tc.want)
			}
			if tc.source != coreSource && !strings.Contains(err.Error(), "`strspc realm dep remove dev.steerspec.core --dir "+shellQuote(dir)+"`") {
				t.Errorf("error = %v, want the remove step first", err)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"./rules":      "./rules",
		"/tmp/a-b_c.d": "/tmp/a-b_c.d",
		"my rules":     "'my rules'",
		"it's":         `'it'\''s'`,
		"$(rm -rf ~)":  "'$(rm -rf ~)'",
		"":             "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
