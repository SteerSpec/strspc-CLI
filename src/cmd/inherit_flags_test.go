package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestRealmAddInherits(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "SAVACCT", "--title", "Savings",
		"--inherits", "dev.globalbank.core:ACCT", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := loadEntityFile(t, dir, "SAVACCT").Entity.Inherits; got != "dev.globalbank.core:ACCT" {
		t.Errorf("inherits = %q, want dev.globalbank.core:ACCT", got)
	}
}

func TestRealmAddInheritsInvalid(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"malformed", "Dev.Bank:ACCT", "invalid --inherits"},
		{"self", "SAVACCT", "cannot inherit itself"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			setupValidRealm(t, dir)
			_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "SAVACCT", "--title", "Savings",
				"--inherits", tc.value, "--dir", dir)
			if err == nil {
				t.Fatal("expected error")
			}
			testutil.AssertContains(t, err.Error(), tc.want)
		})
	}
}

func TestEntityAddInherits(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "PRODUCTS")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "add", "SAVACCT", "--parent", "PRODUCTS",
		"--title", "Savings", "--inherits", "ACCT", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("parsing JSON output: %v", err)
	}
	if res["inherits"] != "ACCT" {
		t.Errorf("JSON inherits = %v, want ACCT", res["inherits"])
	}

	f := loadEntityFile(t, dir, "PRODUCTS")
	if len(f.SubEntities) != 1 || f.SubEntities[0].Entity.Inherits != "ACCT" {
		t.Fatalf("sub-entity inherits not written: %+v", f.SubEntities)
	}
	assertFileHashFresh(t, dir, "PRODUCTS")
}

func TestRuleAddOverrides(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "SAVACCT")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "SAVACCT", "--body", "Withdrawals MUST be capped.",
		"--added-by", "@alice", "--overrides", "dev.globalbank.core:ACCT-005", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Overrides: dev.globalbank.core:ACCT-005")

	r := loadEntityFile(t, dir, "SAVACCT").Rules[0]
	if r.Overrides == nil || *r.Overrides != "dev.globalbank.core:ACCT-005" {
		t.Errorf("overrides = %v, want dev.globalbank.core:ACCT-005", r.Overrides)
	}
	assertFileHashFresh(t, dir, "SAVACCT")
}

func TestRuleAddOverridesInvalid(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "SAVACCT")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "SAVACCT", "--body", "b",
		"--added-by", "@alice", "--overrides", "acct-5", "--dir", dir)
	if err == nil {
		t.Fatal("expected error")
	}
	testutil.AssertContains(t, err.Error(), "invalid --overrides")
	if n := len(loadEntityFile(t, dir, "SAVACCT").Rules); n != 0 {
		t.Errorf("rule added despite invalid --overrides: %d rules", n)
	}
}

func TestRuleSupersedeOverrides(t *testing.T) {
	overriding := makeRule("SAVACCT-001", "I", "Old cap", "@alice")
	target := "core:ACCT-005"
	overriding.Overrides = &target

	t.Run("restated", func(t *testing.T) {
		dir := t.TempDir()
		setupEntityFile(t, dir, "SAVACCT", overriding)
		out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "supersede", "SAVACCT-001", "--body", "New cap",
			"--added-by", "@bob", "--overrides", target, "--dir", dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		testutil.AssertNotContains(t, out, "lapses")
		r := loadEntityFile(t, dir, "SAVACCT").Rules[1]
		if r.Overrides == nil || *r.Overrides != target {
			t.Errorf("overrides = %v, want %s", r.Overrides, target)
		}
		assertFileHashFresh(t, dir, "SAVACCT")
	})

	t.Run("lapsed", func(t *testing.T) {
		dir := t.TempDir()
		setupEntityFile(t, dir, "SAVACCT", overriding)
		out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "supersede", "SAVACCT-001", "--body", "New cap",
			"--added-by", "@bob", "--dir", dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		testutil.AssertContains(t, out, "override lapses unless restated with --overrides "+target)
		if r := loadEntityFile(t, dir, "SAVACCT").Rules[1]; r.Overrides != nil {
			t.Errorf("overrides = %v, want none", *r.Overrides)
		}
	})
}

// TestInheritanceFieldsSurviveRewrite checks that a command that rewrites the
// file without touching inheritance keeps inherits and overrides.
func TestInheritanceFieldsSurviveRewrite(t *testing.T) {
	dir := t.TempDir()
	target := "core:ACCT-005"
	r := makeRule("SAVACCT-001", "D", "Cap", "@alice")
	r.Overrides = &target
	setupEntityFile(t, dir, "SAVACCT", r)
	f := loadEntityFile(t, dir, "SAVACCT")
	f.Entity.Inherits = "core:ACCT"
	data, err := marshalJSON(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SAVACCT.json"), data, 0o644); err != nil {
		t.Fatalf("writing entity file: %v", err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "SAVACCT-001", "--dir", dir); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got := loadEntityFile(t, dir, "SAVACCT")
	if got.Entity.Inherits != "core:ACCT" {
		t.Errorf("inherits lost: %q", got.Entity.Inherits)
	}
	if got.Rules[0].Overrides == nil || *got.Rules[0].Overrides != target {
		t.Errorf("overrides lost: %v", got.Rules[0].Overrides)
	}
	if got.Rules[0].State != entity.StatePublished {
		t.Errorf("state = %s, want P", got.Rules[0].State)
	}
	assertFileHashFresh(t, dir, "SAVACCT")
}
