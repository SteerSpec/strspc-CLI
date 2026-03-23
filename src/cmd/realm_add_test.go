package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestRealmAddBasic(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "MYENT", "--title", "My Entity", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "MYENT")
	testutil.AssertContains(t, output, "My Entity")

	// Verify file contents.
	data, readErr := os.ReadFile(filepath.Join(dir, "MYENT.json"))
	if readErr != nil {
		t.Fatalf("reading entity file: %v", readErr)
	}

	var f entity.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parsing entity file: %v", err)
	}
	if f.Entity.ID != "MYENT" {
		t.Errorf("got entity ID %q, want %q", f.Entity.ID, "MYENT")
	}
	if f.Entity.Title != "My Entity" {
		t.Errorf("got title %q, want %q", f.Entity.Title, "My Entity")
	}
	if f.RuleSet.Version != "0.1.0" {
		t.Errorf("got version %q, want %q", f.RuleSet.Version, "0.1.0")
	}
	if f.RuleSet.Hash != nil {
		t.Errorf("expected nil hash, got %v", f.RuleSet.Hash)
	}
	if len(f.Rules) != 0 {
		t.Errorf("expected empty rules, got %d", len(f.Rules))
	}
	if len(f.Notes) != 0 {
		t.Errorf("expected empty notes, got %d", len(f.Notes))
	}
}

func TestRealmAddWithDescription(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "ERRHNDL", "--title", "Error Handling", "--description", "Rules for error handling", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(dir, "ERRHNDL.json"))
	if readErr != nil {
		t.Fatalf("reading entity file: %v", readErr)
	}

	var f entity.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parsing entity file: %v", err)
	}
	if f.Entity.Description != "Rules for error handling" {
		t.Errorf("got description %q, want %q", f.Entity.Description, "Rules for error handling")
	}
}

func TestRealmAddInvalidEUID(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	tests := []struct {
		name string
		euid string
	}{
		{"too short", "AB"},
		{"invalid chars", "INVALID!"},
		{"too long", "ABCDEFGHIJKLMNOPQRST"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", tc.euid, "--title", "Test", "--dir", dir)
			if err == nil {
				t.Fatalf("expected error for EUID %q, got nil", tc.euid)
			}
			testutil.AssertContains(t, err.Error(), "invalid EUID")
		})
	}
}

func TestRealmAddDuplicateEUID(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	// Create entity first time.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "DUP", "--title", "First", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error on first add: %v", err)
	}

	// Try to create same EUID again.
	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "DUP", "--title", "Second", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for duplicate EUID, got nil")
	}
	testutil.AssertContains(t, err.Error(), "already exists")
}

func TestRealmAddNoRealm(t *testing.T) {
	dir := t.TempDir()
	// No realm.json — should fail.

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "MYENT", "--title", "Test", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for missing realm.json, got nil")
	}
	testutil.AssertContains(t, err.Error(), "realm.json")
}

func TestRealmAddDefaultDir(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	setupValidRealm(t, rulesDir)

	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(dir)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "DEFENT", "--title", "Default Dir Entity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(rulesDir, "DEFENT.json")); statErr != nil {
		t.Fatalf("entity file not created in default dir: %v", statErr)
	}
}

func TestRealmAddMissingTitle(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)

	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "MYENT", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for missing --title, got nil")
	}
	testutil.AssertContains(t, err.Error(), "--title")
}

func TestRealmAddHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--title")
	testutil.AssertContains(t, output, "--description")
	testutil.AssertContains(t, output, "--dir")
}
