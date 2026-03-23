package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
