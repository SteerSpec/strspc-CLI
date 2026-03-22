package entity

import (
	"testing"
)

func TestLoadValidFile(t *testing.T) {
	f, err := Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.Schema != "https://steerspec.dev/schemas/entity/v1.json" {
		t.Errorf("schema = %q, want steerspec.dev schema", f.Schema)
	}
	if f.Entity.ID != "TST" {
		t.Errorf("entity.id = %q, want TST", f.Entity.ID)
	}
	if f.Entity.Title != "Test Entity" {
		t.Errorf("entity.title = %q, want Test Entity", f.Entity.Title)
	}
	if f.Entity.Description != "A test entity for unit tests." {
		t.Errorf("entity.description = %q", f.Entity.Description)
	}
	if f.Entity.Parent != "" {
		t.Errorf("entity.parent = %q, want empty", f.Entity.Parent)
	}
	if f.RuleSet.Version != "0.1.0" {
		t.Errorf("rule_set.version = %q, want 0.1.0", f.RuleSet.Version)
	}
	if f.RuleSet.Hash == nil {
		t.Error("rule_set.hash is nil, want non-nil")
	}
	if len(f.Rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(f.Rules))
	}
	if f.Rules[0].ID != "TST-001" {
		t.Errorf("rules[0].id = %q, want TST-001", f.Rules[0].ID)
	}
	if f.Rules[0].State != "D" {
		t.Errorf("rules[0].state = %q, want D", f.Rules[0].State)
	}
	if f.Rules[1].Revision != 1 {
		t.Errorf("rules[1].revision = %d, want 1", f.Rules[1].Revision)
	}
	if f.Rules[0].Supersedes != nil {
		t.Errorf("rules[0].supersedes = %v, want nil", f.Rules[0].Supersedes)
	}
}

func TestLoadNotes(t *testing.T) {
	f, err := Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(f.Notes) != 1 {
		t.Fatalf("len(notes) = %d, want 1", len(f.Notes))
	}
	n := f.Notes[0]
	if n.ID != "TST-001/01" {
		t.Errorf("note.id = %q, want TST-001/01", n.ID)
	}
	if n.RuleRef != "TST-001" {
		t.Errorf("note.rule_ref = %q, want TST-001", n.RuleRef)
	}
	if n.Type != "rationale" {
		t.Errorf("note.type = %q, want rationale", n.Type)
	}
	if n.Content != "Testing is essential for quality assurance." {
		t.Errorf("note.content = %q", n.Content)
	}
}

func TestLoadWithSubEntities(t *testing.T) {
	f, err := Load("testdata/nested.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.Entity.ID != "PAR" {
		t.Errorf("entity.id = %q, want PAR", f.Entity.ID)
	}
	if len(f.SubEntities) != 1 {
		t.Fatalf("len(sub_entities) = %d, want 1", len(f.SubEntities))
	}

	child := f.SubEntities[0]
	if child.Entity.ID != "CHILD" {
		t.Errorf("sub.entity.id = %q, want CHILD", child.Entity.ID)
	}
	if child.Entity.Parent != "PAR" {
		t.Errorf("sub.entity.parent = %q, want PAR", child.Entity.Parent)
	}
	if len(child.Rules) != 1 {
		t.Fatalf("len(sub.rules) = %d, want 1", len(child.Rules))
	}
	if child.Rules[0].ID != "CHILD-001" {
		t.Errorf("sub.rules[0].id = %q, want CHILD-001", child.Rules[0].ID)
	}
}

func TestLoadNullHash(t *testing.T) {
	f, err := Load("testdata/nested.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if f.RuleSet.Hash != nil {
		t.Errorf("rule_set.hash = %v, want nil", f.RuleSet.Hash)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("testdata/nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	_, err := Load("testdata/invalid.json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadEmptyRules(t *testing.T) {
	f, err := Load("testdata/nested.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	child := f.SubEntities[0]
	if len(child.Notes) != 0 {
		t.Errorf("len(sub.notes) = %d, want 0", len(child.Notes))
	}
}
