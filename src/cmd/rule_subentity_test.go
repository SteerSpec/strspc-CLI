package cmd

import (
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestRuleAddSubEntityAutoDetect(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "SUB",
		"--body", "Sub rule", "--added-by", "@alice", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "SUB-001")

	f := loadEntityFile(t, dir, "TST")
	if len(f.Rules) != 0 {
		t.Errorf("parent should have no rules, got %d", len(f.Rules))
	}
	sub := f.SubEntities[0]
	if len(sub.Rules) != 1 || sub.Rules[0].ID != "SUB-001" {
		t.Fatalf("expected SUB-001 in sub-entity, got %+v", sub.Rules)
	}
	if sub.RuleSet.Version != "0.1.1" {
		t.Errorf("sub-entity version should bump to 0.1.1, got %s", sub.RuleSet.Version)
	}
	if sub.RuleSet.Hash == nil {
		t.Error("sub-entity hash should be set")
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestRuleAddSubEntityFlag(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST", "--sub-entity", "SUB",
		"--body", "Sub rule", "--added-by", "@alice", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f := loadEntityFile(t, dir, "TST")
	if len(f.SubEntities[0].Rules) != 1 || f.SubEntities[0].Rules[0].ID != "SUB-001" {
		t.Fatalf("expected SUB-001 in sub-entity, got %+v", f.SubEntities[0].Rules)
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestRuleAddSubEntityFlagUnknown(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST", "--sub-entity", "NOPE",
		"--body", "x", "--added-by", "@alice", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for unknown sub-entity")
	}
	testutil.AssertContains(t, err.Error(), "NOPE")
}

func TestRuleAddTopLevelUnchangedWithSubPresent(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST",
		"--body", "Parent rule", "--added-by", "@alice", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	if len(f.Rules) != 1 || f.Rules[0].ID != "TST-001" {
		t.Fatalf("expected TST-001 on parent, got %+v", f.Rules)
	}
	if len(f.SubEntities[0].Rules) != 0 {
		t.Error("sub-entity should be untouched")
	}
}

func TestRuleAddUnknownEntityMentionsSubEntities(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB")

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "NOPE",
		"--body", "x", "--added-by", "@alice", "--dir", dir)
	if err == nil {
		t.Fatal("expected error")
	}
	testutil.AssertContains(t, err.Error(), "NOPE")
}

func TestRulePromoteSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Draft", "@alice"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "promote", "SUB-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Draft → Published")

	f := loadEntityFile(t, dir, "TST")
	sub := f.SubEntities[0]
	if sub.Rules[0].State != "P" {
		t.Errorf("expected state P, got %s", sub.Rules[0].State)
	}
	if sub.RuleSet.Version != "0.2.0" {
		t.Errorf("expected sub version 0.2.0, got %s", sub.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestRuleUpdateSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Draft", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "update", "SUB-001", "--body", "New body", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	if f.SubEntities[0].Rules[0].Body != "New body" {
		t.Errorf("body not updated: %q", f.SubEntities[0].Rules[0].Body)
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestRuleAbandonSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "D", "Draft", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "abandon", "SUB-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	if f.SubEntities[0].Rules[0].State != "A" {
		t.Errorf("expected state A, got %s", f.SubEntities[0].Rules[0].State)
	}
}

func TestRuleRetireSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "I", "Impl", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "retire", "SUB-001", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	if f.SubEntities[0].Rules[0].State != "R" {
		t.Errorf("expected state R, got %s", f.SubEntities[0].Rules[0].State)
	}
}

func TestRuleSupersedeSubEntity(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "TST", "SUB", makeRule("SUB-001", "P", "Published", "@alice"))

	_, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "supersede", "SUB-001",
		"--body", "Replacement", "--added-by", "@bob", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f := loadEntityFile(t, dir, "TST")
	sub := f.SubEntities[0]
	if len(sub.Rules) != 2 || sub.Rules[1].ID != "SUB-002" {
		t.Fatalf("expected SUB-002, got %+v", sub.Rules)
	}
	if sub.Rules[1].Supersedes == nil || *sub.Rules[1].Supersedes != "SUB-001" {
		t.Error("SUB-002 should supersede SUB-001")
	}
	assertFileHashFresh(t, dir, "TST")
}

func TestRuleAddHelp(t *testing.T) {
	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "--sub-entity")
	testutil.AssertContains(t, out, "--body")
}
