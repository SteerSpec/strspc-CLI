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

func TestStateLabel(t *testing.T) {
	for state, want := range map[string]string{
		entity.StateDraft: "Draft", entity.StatePublished: "Published", entity.StateImplemented: "Implemented",
		entity.StateRetired: "Retired", entity.StateTerminated: "Terminated", entity.StateAbandoned: "Abandoned",
		"X": "X",
	} {
		if got := stateLabel(state); got != want {
			t.Errorf("stateLabel(%q) = %q, want %q", state, got, want)
		}
	}
}

func TestFindRuleMissing(t *testing.T) {
	f := &entity.File{Rules: []entity.Rule{{ID: "ENT-001"}}}
	if findRule(f, "ENT-001") == nil {
		t.Error("ENT-001 must be found")
	}
	if findRule(f, "ENT-002") != nil {
		t.Error("ENT-002 must not be found")
	}
}

func TestJSONOutputMarshalError(t *testing.T) {
	if _, err := marshalJSON(make(chan int)); err == nil {
		t.Error("marshalJSON of a channel must fail")
	}
	var sb strings.Builder
	err := writeJSONOutput(&sb, make(chan int))
	if err == nil || !strings.Contains(err.Error(), "marshaling JSON output") {
		t.Errorf("writeJSONOutput: got %v", err)
	}
	if sb.Len() != 0 {
		t.Errorf("nothing may be written, got %q", sb.String())
	}
}

// writeHelperEntity writes ENT, holding the sub-entity SUB, at version.
func writeHelperEntity(t *testing.T, dir, name, version string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	writeFile(t, dir, name, histEntityJSON(version, []histRule{{id: "ENT-001", state: "D", body: "a"}}, []histRule{{id: "SUB-001", state: "D", body: "s"}}))
	return p
}

func TestLoadModifySaveEntityErrors(t *testing.T) {
	dir := t.TempDir()
	noop := func(*entity.File) error { return nil }

	err := loadModifySaveEntity(filepath.Join(dir, "MISSING.json"), noop)
	if err == nil || !strings.Contains(err.Error(), "loading entity file") {
		t.Errorf("missing file: got %v", err)
	}

	writeFile(t, dir, "BAD.json", "{")
	err = loadModifySaveEntity(filepath.Join(dir, "BAD.json"), noop)
	if err == nil || !strings.Contains(err.Error(), "loading entity file") {
		t.Errorf("malformed file: got %v", err)
	}

	target := writeHelperEntity(t, dir, "ENT.json", "0.1.0")
	link := filepath.Join(dir, "LNK.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	err = loadModifySaveEntity(link, noop)
	if err == nil || !strings.Contains(err.Error(), "is a symbolic link") {
		t.Errorf("linked file: got %v", err)
	}
}

func TestReplaceEntityFileErrors(t *testing.T) {
	dir := t.TempDir()
	f := &entity.File{Entity: entity.Entity{ID: "ENT"}}

	err := replaceEntityFile(filepath.Join(dir, "missing", "ENT.json"), f)
	if err == nil || !strings.Contains(err.Error(), "writing entity file") {
		t.Errorf("missing directory: got %v", err)
	}

	// A directory in the way: the rename fails and the temporary file goes.
	writeFile(t, dir, "ENT.json/keep", "x")
	err = replaceEntityFile(filepath.Join(dir, "ENT.json"), f)
	if err == nil || !strings.Contains(err.Error(), "writing entity file") {
		t.Errorf("directory in the way: got %v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}
}

func TestResolveTargetSubEntities(t *testing.T) {
	dir := t.TempDir()
	writeHelperEntity(t, dir, "ENT.json", "0.1.0")
	writeFile(t, dir, "BROKEN.json", "{") // skipped by the sub-entity scan

	got, err := resolveEntityTarget(dir, "SUB")
	if err != nil || got.subID != "SUB" || filepath.Base(got.path) != "ENT.json" {
		t.Errorf("sub-entity: %+v, %v", got, err)
	}

	writeFile(t, dir, "OTHER.json", strings.Replace(histEntityJSON("0.1.0", nil, nil), `"id": "ENT"`, `"id": "OTHER"`, 1))
	_, err = resolveEntityTarget(dir, "SUB")
	if err == nil || !strings.Contains(err.Error(), `sub-entity "SUB" is defined in 2 files`) {
		t.Errorf("duplicate sub-entity: got %v", err)
	}

	_, err = resolveEntityTarget(filepath.Join(dir, "missing"), "NOPE")
	if err == nil || !strings.Contains(err.Error(), `scanning`) {
		t.Errorf("missing directory: got %v", err)
	}
}

func TestResolveSubEntityOfErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := resolveSubEntityOf(dir, "NOPE", "SUB"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown parent: got %v", err)
	}

	writeFile(t, dir, "BAD.json", "{")
	if _, err := resolveSubEntityOf(dir, "BAD", "SUB"); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("unparsable parent: got %v", err)
	}
}

func TestModifyEntityErrors(t *testing.T) {
	dir := t.TempDir()
	p := writeHelperEntity(t, dir, "ENT.json", "0.1.0")
	noop := func(*entity.File) error { return nil }

	err := modifyEntity(entityTarget{path: p, subID: "GONE"}, noop)
	if err == nil || !strings.Contains(err.Error(), `sub-entity "GONE" not found`) {
		t.Errorf("unknown sub-entity: got %v", err)
	}

	// The parent's version can't be bumped: nothing is written.
	bad := writeHelperEntity(t, dir, "BADV.json", "one")
	before, readErr := os.ReadFile(bad)
	if readErr != nil {
		t.Fatal(readErr)
	}
	err = modifyEntity(entityTarget{path: bad, subID: "SUB"}, noop)
	if err == nil || !strings.Contains(err.Error(), "bumping version of ENT") {
		t.Errorf("bad parent version: got %v", err)
	}
	after, readErr := os.ReadFile(bad)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Error("the file must be left as it was")
	}
	testutil.AssertContains(t, string(after), `"version": "one"`)
}
