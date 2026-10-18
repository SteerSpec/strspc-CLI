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

// setupEntityFile creates a minimal entity JSON file with the given rules.
func setupEntityFile(t *testing.T, dir, entityID string, rules ...entity.Rule) {
	t.Helper()

	hash := "blake3:0000000000000000000000000000000000000000000000000000000000000000"
	f := entity.File{
		Schema: "./_schema/entity.v1.schema.json",
		Entity: entity.Entity{
			ID:    entityID,
			Title: "Test Entity",
		},
		RuleSet: entity.RuleSet{
			Version:   "0.1.0",
			Timestamp: "2026-01-01T00:00:00Z",
			Hash:      &hash,
		},
		Rules: rules,
		Notes: []entity.Note{},
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("marshaling test entity: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, entityID+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writing test entity: %v", err)
	}
}

// loadEntityFile reads and parses an entity JSON file from the test directory.
func loadEntityFile(t *testing.T, dir, entityID string) *entity.File {
	t.Helper()
	f, err := entity.Load(filepath.Join(dir, entityID+".json"))
	if err != nil {
		t.Fatalf("loading entity file: %v", err)
	}
	return f
}

// makeRule creates a Rule with the given fields for test fixtures.
func makeRule(id, state, body, addedBy string) entity.Rule {
	return entity.Rule{
		ID:       id,
		Revision: 0,
		State:    state,
		Body:     body,
		AddedBy:  addedBy,
		AddedAt:  "2026-01-01",
	}
}

// setupEntityFileWithSub creates an entity file whose single sub-entity subID
// holds the given rules. The parent itself has no rules.
func setupEntityFileWithSub(t *testing.T, dir, entityID, subID string, subRules ...entity.Rule) {
	t.Helper()

	hash := "blake3:0000000000000000000000000000000000000000000000000000000000000000"
	f := entity.File{
		Schema: "./_schema/entity.v1.schema.json",
		Entity: entity.Entity{ID: entityID, Title: "Parent Entity"},
		RuleSet: entity.RuleSet{
			Version:   "0.1.0",
			Timestamp: "2026-01-01T00:00:00Z",
			Hash:      &hash,
		},
		Rules: []entity.Rule{},
		SubEntities: []entity.File{{
			Schema: "./_schema/entity.v1.schema.json",
			Entity: entity.Entity{ID: subID, Title: "Sub Entity", Parent: entityID},
			RuleSet: entity.RuleSet{
				Version:   "0.1.0",
				Timestamp: "2026-01-01T00:00:00Z",
				Hash:      nil,
			},
			Rules: subRules,
			Notes: []entity.Note{},
		}},
		Notes: []entity.Note{},
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("marshaling test entity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, entityID+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writing test entity: %v", err)
	}
}

// assertFileHashFresh fails unless the stored rule_set.hash of the entity file
// matches the blake3 hash of its on-disk bytes (what lint RL011 checks).
func assertFileHashFresh(t *testing.T, dir, entityID string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, entityID+".json"))
	if err != nil {
		t.Fatalf("reading entity file: %v", err)
	}
	f, err := entity.Parse(data)
	if err != nil {
		t.Fatalf("parsing entity file: %v", err)
	}
	computed, err := entity.ComputeHash(data)
	if err != nil {
		t.Fatalf("computing hash: %v", err)
	}
	if f.RuleSet.Hash == nil {
		t.Fatalf("%s: rule_set.hash is null", entityID)
	}
	if *f.RuleSet.Hash != computed {
		t.Errorf("%s: stored hash %s != computed %s", entityID, *f.RuleSet.Hash, computed)
	}
}

func TestSaveEntityDoesNotEscapeHTML(t *testing.T) {
	dir := t.TempDir()
	setupEntityFile(t, dir, "TST")

	body := "IS referenced as [<EUID>-NNN] and compared with a < b > c."
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "add", "TST",
		"--body", body, "--added-by", "@alice", "--dir", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(dir, "TST.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "[<EUID>-NNN]") {
		t.Errorf("body should be written verbatim, got:\n%s", first)
	}
	// json.Marshal("<") yields the HTML-escaped form (quoted); that sequence must be absent.
	escapedLT, _ := json.Marshal("<")
	escapedGT, _ := json.Marshal(">")
	for _, esc := range [][]byte{escapedLT, escapedGT} {
		needle := strings.Trim(string(esc), `"`)
		if strings.HasPrefix(needle, "<") || strings.HasPrefix(needle, ">") {
			t.Fatalf("test setup: expected json.Marshal to HTML-escape, got %q", needle)
		}
		if strings.Contains(string(first), needle) {
			t.Errorf("file must not contain HTML-escaped %q:\n%s", needle, first)
		}
	}
	if first[len(first)-1] != '\n' {
		t.Error("file should end with a trailing newline")
	}

	// Load and save again: byte-identical round trip.
	f, err := entity.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if f.Rules[0].Body != body {
		t.Errorf("body changed on parse: %q", f.Rules[0].Body)
	}
	if err := saveEntity(path, f); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Errorf("round trip not byte-identical:\n--- first\n%s\n--- second\n%s", first, second)
	}
	assertFileHashFresh(t, dir, "TST")
}
