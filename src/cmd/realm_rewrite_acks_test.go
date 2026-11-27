package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// writeAckEntity writes an entity file whose rule is by ruleBy and whose
// override_ack note on it is by ackBy.
func writeAckEntity(t *testing.T, dir, euid, ruleBy, ackBy string) {
	t.Helper()
	f := entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: euid, Title: "Ack " + euid},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules:   []entity.Rule{makeRule(euid+"-001", "D", "The system MUST run.", ruleBy)},
		Notes: []entity.Note{{
			ID: euid + "-001/01", RuleRef: euid + "-001", Type: entity.NoteTypeOverrideAck,
			Content: "Acknowledged.", AddedBy: ackBy, AddedAt: "2026-01-01",
		}},
		SubEntities: []entity.File{},
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, euid+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An override_ack note never changes once added (NTE-044): its added_by stays
// and the command lists it as kept. A file whose only matches are such notes
// isn't touched.
func TestRealmRewriteAddedByKeepsOverrideAcks(t *testing.T) {
	dir := rewriteFixture(t)
	writeAckEntity(t, dir, "ACKONLY", "github:x", "@alice")
	writeAckEntity(t, dir, "MIXED", "@alice", "@alice")
	before := readFiles(t, dir, "ACKONLY.json")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res addedByResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	want := []addedByKept{
		{Entity: "ACKONLY", Note: "ACKONLY-001/01", AddedBy: "@alice", Path: filepath.Join(dir, "ACKONLY.json")},
		{Entity: "MIXED", Note: "MIXED-001/01", AddedBy: "@alice", Path: filepath.Join(dir, "MIXED.json")},
	}
	if len(res.Kept) != len(want) {
		t.Fatalf("kept = %+v, want %+v", res.Kept, want)
	}
	for i := range want {
		if res.Kept[i] != want[i] {
			t.Errorf("kept[%d] = %+v, want %+v", i, res.Kept[i], want[i])
		}
	}
	if !bytes.Equal(readFiles(t, dir, "ACKONLY.json")["ACKONLY.json"], before["ACKONLY.json"]) {
		t.Error("a file whose only match is an override_ack note was rewritten")
	}
	mixed := loadEntityFile(t, dir, "MIXED")
	if mixed.Rules[0].AddedBy != "github:alice" || mixed.Notes[0].AddedBy != "@alice" {
		t.Errorf("MIXED: rule added_by %q, ack added_by %q; want github:alice and @alice", mixed.Rules[0].AddedBy, mixed.Notes[0].AddedBy)
	}
	for _, c := range res.Updated {
		if c.Entity == "ACKONLY" {
			t.Errorf("ACKONLY listed as updated: %+v", c)
		}
	}
}

func TestRealmRewriteAddedByHumanKeptAcks(t *testing.T) {
	dir := rewriteFixture(t)
	writeAckEntity(t, dir, "ACKONLY", "github:x", "@alice")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dry-run", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, "Kept ACKONLY-001/01 in ACKONLY as it is: an override_ack note doesn't change once added (NTE-044); added_by @alice")
	testutil.AssertContains(t, out, ", 1 override_ack note(s) kept")
}

// Without a kept note, the JSON still lists kept, empty.
func TestRealmRewriteAddedByJSONKeptEmpty(t *testing.T) {
	dir := rewriteFixture(t)
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "rewrite-added-by", "--github-handles", "--dry-run", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, out, `"kept": []`)
	testutil.AssertNotContains(t, out, "override_ack")
}
