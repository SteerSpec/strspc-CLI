package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestEntityAddUnderTopLevel(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "PARENT")

	output, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "add", "CHILD", "--parent", "PARENT", "--title", "Child", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "CHILD")

	f := loadEntityFile(t, dir, "PARENT")
	if len(f.SubEntities) != 1 {
		t.Fatalf("got %d sub-entities, want 1", len(f.SubEntities))
	}
	sub := f.SubEntities[0]
	if sub.Entity.ID != "CHILD" || sub.Entity.Parent != "PARENT" || sub.Entity.Title != "Child" {
		t.Errorf("unexpected sub-entity: %+v", sub.Entity)
	}
	if sub.RuleSet.Hash == nil {
		t.Error("sub-entity hash is null")
	}
	if f.RuleSet.Version != "0.1.1" {
		t.Errorf("parent version %q, want 0.1.1", f.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "PARENT")
}

func TestEntityAddNested(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "PARENT", "MIDDLE")

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "add", "LEAF", "--parent", "MIDDLE", "--title", "Leaf", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f := loadEntityFile(t, dir, "PARENT")
	mid := findSubEntity(f, "MIDDLE")
	if mid == nil || len(mid.SubEntities) != 1 {
		t.Fatalf("LEAF not nested under MIDDLE: %+v", mid)
	}
	if mid.SubEntities[0].Entity.Parent != "MIDDLE" {
		t.Errorf("got parent %q, want MIDDLE", mid.SubEntities[0].Entity.Parent)
	}
	if mid.RuleSet.Version != "0.1.1" || f.RuleSet.Version != "0.1.1" {
		t.Errorf("ancestor versions: middle %q, root %q, want 0.1.1", mid.RuleSet.Version, f.RuleSet.Version)
	}
	assertFileHashFresh(t, dir, "PARENT")
}

func TestEntityAddErrors(t *testing.T) {
	dir := t.TempDir()
	setupEntityFileWithSub(t, dir, "PARENT", "MIDDLE")
	setupEntityFile(t, dir, "OTHER")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"duplicate top-level", []string{"OTHER", "--parent", "PARENT", "--title", "x"}, "already exists"},
		{"duplicate nested", []string{"MIDDLE", "--parent", "OTHER", "--title", "x"}, "already exists"},
		{"unknown parent", []string{"NEWONE", "--parent", "MISSING", "--title", "x"}, "not found"},
		{"invalid EUID", []string{"a.b", "--parent", "PARENT", "--title", "x"}, "invalid EUID"},
		{"path traversal parent", []string{"NEWONE", "--parent", "../OTHER", "--title", "x"}, "invalid --parent"},
		{"missing parent", []string{"NEWONE", "--title", "x"}, "--parent is required"},
		{"missing title", []string{"NEWONE", "--parent", "PARENT"}, "--title is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"entity", "add"}, tt.args...)
			args = append(args, "--dir", dir)
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil {
				t.Fatal("expected error")
			}
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestEntityAddAbortsOnUnparseableEntityFile(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "PARENT")
	if err := os.WriteFile(filepath.Join(dir, "BROKEN.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "add", "CHILD", "--parent", "PARENT", "--title", "x", "--dir", dir)
	if err == nil {
		t.Fatal("expected error")
	}
	testutil.AssertContains(t, err.Error(), "cannot verify EUID uniqueness")
	if f := loadEntityFile(t, dir, "PARENT"); len(f.SubEntities) != 0 {
		t.Errorf("parent modified despite failed scan")
	}
}

func TestEntityAddThenRuleAddAndJSON(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "PARENT")

	output, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "add", "CHILD", "--parent", "PARENT", "--title", "Child", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output, err)
	}
	if got["entity_id"] != "CHILD" || got["parent"] != "PARENT" {
		t.Errorf("unexpected JSON: %v", got)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "CHILD", "--body", "Child MUST work", "--added-by", "@t", "--dir", dir); err != nil {
		t.Fatalf("rule add on new sub-entity: %v", err)
	}
	f := loadEntityFile(t, dir, "PARENT")
	if sub := findSubEntity(f, "CHILD"); sub == nil || len(sub.Rules) != 1 {
		t.Fatalf("rule not added to CHILD: %+v", sub)
	}
	assertFileHashFresh(t, dir, "PARENT")
}
