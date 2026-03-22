package entity

import (
	"encoding/json"
	"fmt"
	"os"
)

// File represents a SteerSpec entity file conforming to entity.v1.schema.json.
type File struct {
	Schema      string  `json:"$schema"`
	Entity      Entity  `json:"entity"`
	RuleSet     RuleSet `json:"rule_set"`
	Rules       []Rule  `json:"rules"`
	SubEntities []File  `json:"sub_entities,omitempty"`
	Notes       []Note  `json:"notes"`
}

// Entity holds the identity and metadata for a SteerSpec entity.
type Entity struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"`
}

// RuleSet holds versioning and integrity metadata for an entity's rule set.
type RuleSet struct {
	Version   string  `json:"version"`
	Timestamp string  `json:"timestamp"`
	Hash      *string `json:"hash"`
}

// Rule represents a single rule within an entity.
type Rule struct {
	ID         string  `json:"id"`
	Revision   int     `json:"revision"`
	State      string  `json:"state"`
	Body       string  `json:"body"`
	AddedBy    string  `json:"added_by"`
	AddedAt    string  `json:"added_at"`
	Supersedes *string `json:"supersedes"`
}

// Note represents an annotation attached to a specific rule.
type Note struct {
	ID       string `json:"id"`
	RuleRef  string `json:"rule_ref"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	AddedBy  string `json:"added_by"`
	AddedAt  string `json:"added_at"`
	Revision int    `json:"revision"`
}

// Load reads and parses an entity JSON file from the given path.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading entity file: %w", err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing entity file: %w", err)
	}

	return &f, nil
}
