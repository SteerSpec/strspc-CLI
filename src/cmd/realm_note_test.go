package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
)

// setupNoteRealm writes a realm at dir pinning core 1.21.0, listing
// testContributors, and declaring owasp.asvs linked to a Standard Realm and
// ietf.rfc2119 unlinked.
func setupNoteRealm(t *testing.T, dir string) {
	t.Helper()
	setupLinkRealm(t, dir)
	setContributors(t, dir)
	for _, args := range [][]string{
		{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0", "--realm", testStdRealm},
		{"realm", "standards", "add", "--id", "ietf.rfc2119", "--relation", "derived_from"},
		{"entity", "rehash", "MYE"}, // so realm validate passes on the entity too
	} {
		if out, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// realmNotes returns the Realm Notes of the realm.json in dir.
func realmNotes(t *testing.T, dir string) []entity.RealmNote {
	t.Helper()
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatalf("loading realm.json: %v", err)
	}
	return rf.Notes
}

// addRealmNote runs realm note add with args on dir and fails on an error.
func addRealmNote(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "note", "add", "--dir", dir}, args...)...)
	if err != nil {
		t.Fatalf("realm note add %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// TestRealmNoteAdd adds an exclusion and a rationale, numbered REALM/001 and
// REALM/002, and checks the manifest passes realm validate with the core
// 1.21.0 schemas and the Realm Note checks (RM022 to RM024).
func TestRealmNoteAdd(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)

	out := addRealmNote(t, dir, "--type", "exclusion", "--ref", "owasp.asvs:V3.1.1", "--body", "The CLI renders no HTML.", "--added-by", "github:alice")
	testutil.AssertContains(t, out, "Added Realm Note REALM/001 (exclusion owasp.asvs:V3.1.1) to realm dev.steerspec.test")

	out = addRealmNote(t, dir, "--type", "rationale", "--body", "Why this realm exists.", "--added-by", "github:bob", "--json")
	var change realmNoteChange
	if err := json.Unmarshal([]byte(out), &change); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if change.NoteID != "REALM/002" || change.Type != "rationale" || change.Ref != "" || change.Revision != 0 || change.AddedBy != "github:bob" || change.Realm != "dev.steerspec.test" {
		t.Errorf("JSON = %+v", change)
	}

	notes := realmNotes(t, dir)
	if len(notes) != 2 {
		t.Fatalf("notes = %+v, want 2", notes)
	}
	n := notes[0]
	if n.ID != "REALM/001" || n.Type != "exclusion" || n.Ref != "owasp.asvs:V3.1.1" || n.Content != "The CLI renders no HTML." ||
		n.AddedBy != "github:alice" || n.Revision != 0 || !dateRe.MatchString(n.AddedAt) {
		t.Errorf("exclusion = %+v", n)
	}
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate rejects the manifest: %v\n%s", err, out)
	}
}

// TestRealmNoteAddRefusals checks each refusal leaves realm.json as it was.
func TestRealmNoteAddRefusals(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)
	path := filepath.Join(dir, "realm.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no type", []string{"--body", "x"}, "--type is required"},
		{"rule note type", []string{"--type", "changelog", "--body", "x"}, "invalid Realm Note type"},
		{"no body", []string{"--type", "rationale"}, "--body is required"},
		{"exclusion without ref", []string{"--type", "exclusion", "--body", "x"}, "has no ref"},
		{"ref on a rationale", []string{"--type", "rationale", "--ref", "owasp.asvs:V1", "--body", "x"}, "only an exclusion takes one"},
		{"ref on a new reference", []string{"--type", "reference", "--ref", "owasp.asvs:V1", "--body", "x"}, "only an exclusion takes one"},
		{"ref not <id>:<clause>", []string{"--type", "exclusion", "--ref", "owasp.asvs", "--body", "x"}, "is not <id>:<clause>"},
		{"undeclared standard", []string{"--type", "exclusion", "--ref", "iso.27001:A.5", "--body", "x"}, "doesn't declare"},
		{"unlinked standard", []string{"--type", "exclusion", "--ref", "ietf.rfc2119:2", "--body", "x"}, "links to no Standard Realm"},
		{"control character in ref", []string{"--type", "exclusion", "--ref", "owasp.asvs:V1\x1b", "--body", "x"}, "--ref"},
		{"not a contributor", []string{"--type", "rationale", "--body", "x", "--added-by", "github:mallory"}, "github:mallory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"realm", "note", "add", "--dir", dir, "--added-by", "github:alice"}, tt.args...)
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("realm.json changed on refusal:\n%s", after)
	}
}

// TestRealmNoteAddedByDefault takes added_by from the contributor whose email
// is the git user.email.
func TestRealmNoteAddedByDefault(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)
	orig := gitUserEmail
	t.Cleanup(func() { gitUserEmail = orig })
	gitUserEmail = func(string) string { return "alice@example.com" }

	out := addRealmNote(t, dir, "--type", "rationale", "--body", "x")
	testutil.AssertContains(t, out, "git user.email")
	if n := realmNotes(t, dir)[0]; n.AddedBy != "github:alice" {
		t.Errorf("added_by = %q, want github:alice", n.AddedBy)
	}
}

// TestRealmNoteAddOldCore refuses a Realm Note under a core pin whose realm
// schema rejects `notes`.
func TestRealmNoteAddOldCore(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	pinCoreFrom(t, dir, "1.20.0", "")
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "note", "add", "--type", "rationale", "--body", "x", "--added-by", "github:alice", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "reject `notes`") {
		t.Fatalf("error = %v, want a refusal naming `notes`", err)
	}
}

// TestRealmNoteUpdateOldCore refuses to revise a hand-written Realm Note under
// a core pin whose realm schema rejects `notes`, and leaves realm.json alone.
func TestRealmNoteUpdateOldCore(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	pinCoreFrom(t, dir, "1.20.0", "")
	editRealmJSON(t, dir, func(m map[string]any) {
		m["notes"] = []entity.RealmNote{{ID: "REALM/001", Type: "rationale", Content: "Why.", AddedBy: "github:alice", AddedAt: "2026-11-07"}}
	})
	path := filepath.Join(dir, "realm.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "note", "update", "REALM/001", "--body", "Why, clarified.", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "reject `notes`") {
		t.Fatalf("error = %v, want a refusal naming `notes`", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("realm.json changed on refusal:\n%s", after)
	}
}

// TestRealmNoteUpdate revises a note, withdraws an exclusion keeping its ref
// and content (NTE-035), and refuses every other change of type, a content
// change with the withdrawal, and a no-op.
func TestRealmNoteUpdate(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)
	addRealmNote(t, dir, "--type", "exclusion", "--ref", "owasp.asvs:V3.1.1", "--body", "The CLI renders no HTML.", "--added-by", "github:alice")
	addRealmNote(t, dir, "--type", "rationale", "--body", "Why.", "--added-by", "github:alice")
	update := func(args ...string) (string, error) {
		return testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "note", "update", "--dir", dir}, args...)...)
	}

	out, err := update("REALM/002", "--body", "Why, clarified.")
	if err != nil {
		t.Fatalf("body update: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "Updated Realm Note REALM/002 (rationale, revision 1)")

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"REALM/001", "--type", "reference", "--body", "Changed."}, "keeps its content"},
		{[]string{"REALM/002", "--type", "clarification"}, "the only change of type"},
		{[]string{"REALM/001", "--type", "rationale"}, "the only change of type"},
		{[]string{"REALM/002", "--body", "Why, clarified."}, "unchanged"},
		{[]string{"REALM/009", "--body", "x"}, "not found"},
		{[]string{"REALM/001"}, "at least one of --body or --type"},
	} {
		if _, err := update(tt.args...); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("update %s: error = %v, want %q", strings.Join(tt.args, " "), err, tt.want)
		}
	}

	out, err = update("REALM/001", "--type", "reference", "--json")
	if err != nil {
		t.Fatalf("withdraw: %v\n%s", err, out)
	}
	var change realmNoteChange
	if err := json.Unmarshal([]byte(out), &change); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if change.Type != "reference" || change.Ref != "owasp.asvs:V3.1.1" || change.Revision != 1 {
		t.Errorf("JSON = %+v", change)
	}
	n := realmNotes(t, dir)[0]
	if n.Type != "reference" || n.Ref != "owasp.asvs:V3.1.1" || n.Content != "The CLI renders no HTML." || n.Revision != 1 || n.AddedBy != "github:alice" {
		t.Errorf("withdrawn exclusion = %+v", n)
	}
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate rejects the manifest: %v\n%s", err, out)
	}
}

// TestRealmNoteKeepsManifest checks a note write changes nothing else of the
// manifest, and recomputes its hash when it has one (RLMMNFST-014).
func TestRealmNoteKeepsManifest(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)
	runRealmRehash(t, dir)
	path := filepath.Join(dir, "realm.json")
	before, err := entity.LoadRealm(path)
	if err != nil {
		t.Fatal(err)
	}

	addRealmNote(t, dir, "--type", "exclusion", "--ref", "owasp.asvs:V3.1.1", "--body", "x", "--added-by", "github:alice")
	after, err := entity.LoadRealm(path)
	if err != nil {
		t.Fatal(err)
	}
	if declared, computed := manifestHashOf(t, path); declared != computed || declared == before.Hash {
		t.Errorf("hash = %s, want the new content's %s", declared, computed)
	}
	after.Notes, after.Hash = nil, ""
	before.Hash = ""
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if !bytes.Equal(a, b) {
		t.Errorf("the rest of the manifest changed:\n%s\nwas\n%s", a, b)
	}
}

// TestRealmNoteSubRealm writes Realm Notes in a sub-realm's own manifest: its
// ref names the sub-realm's own linked standard, its number starts at
// REALM/001 whatever the parent holds, and added_by names a contributor of the
// parent (CTRB-006).
func TestRealmNoteSubRealm(t *testing.T) {
	dir := t.TempDir()
	setupValidRealmWithSubs(t, dir)
	pinCoreFrom(t, dir, manifestMinCore.String(), "")
	setContributors(t, dir)
	sub := filepath.Join(dir, "auth")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements",
		"--version", "5.0.0", "--realm", testStdRealm, "--dir", dir); err != nil {
		t.Fatal(err)
	}
	addRealmNote(t, dir, "--type", "exclusion", "--ref", "owasp.asvs:V3.1.1", "--body", "Parent.", "--added-by", "github:alice")

	// The parent's standard doesn't serve the sub-realm's exclusions.
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "note", "add", "--dir", sub, "--type", "exclusion",
		"--ref", "owasp.asvs:V3.1.1", "--body", "x", "--added-by", "github:alice")
	if err == nil || !strings.Contains(err.Error(), "doesn't declare") {
		t.Fatalf("error = %v, want the sub-realm's own declaration required", err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements",
		"--version", "5.0.0", "--realm", testStdRealm, "--dir", sub); err != nil {
		t.Fatal(err)
	}
	out := addRealmNote(t, sub, "--type", "exclusion", "--ref", "owasp.asvs:V2.1.1", "--body", "Sub.", "--added-by", "github:bob")
	testutil.AssertContains(t, out, "REALM/001")
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "note", "add", "--dir", sub, "--type", "rationale",
		"--body", "x", "--added-by", "github:mallory"); err == nil || !strings.Contains(err.Error(), "github:mallory") {
		t.Errorf("unknown contributor in a sub-realm: error = %v", err)
	}
	if notes := realmNotes(t, sub); len(notes) != 1 || notes[0].Ref != "owasp.asvs:V2.1.1" {
		t.Errorf("sub-realm notes = %+v", notes)
	}
	if notes := realmNotes(t, dir); len(notes) != 1 || notes[0].Content != "Parent." {
		t.Errorf("parent notes = %+v", notes)
	}
}

// TestRealmNoteList prints the notes in human and JSON form, and says so when
// there are none.
func TestRealmNoteList(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)
	list := func(args ...string) string {
		out, err := testutil.ExecuteCommand(NewRootCmd(), append([]string{"realm", "note", "list", "--dir", dir}, args...)...)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return out
	}
	testutil.AssertContains(t, list(), "has no Realm Notes")
	if out := list("--json"); !strings.Contains(out, `"notes": []`) {
		t.Errorf("empty JSON list:\n%s", out)
	}

	addRealmNote(t, dir, "--type", "exclusion", "--ref", "owasp.asvs:V3.1.1", "--body", "No HTML.", "--added-by", "github:alice")
	out := list()
	for _, want := range []string{"REALM/001", "exclusion", "owasp.asvs:V3.1.1", "r0", "github:alice", "No HTML."} {
		testutil.AssertContains(t, out, want)
	}
	var res struct {
		Realm string             `json:"realm"`
		Notes []entity.RealmNote `json:"notes"`
	}
	if err := json.Unmarshal([]byte(list("--json")), &res); err != nil {
		t.Fatal(err)
	}
	if res.Realm != "dev.steerspec.test" || len(res.Notes) != 1 || res.Notes[0].Ref != "owasp.asvs:V3.1.1" {
		t.Errorf("JSON list = %+v", res)
	}
}

// TestRealmStandardsGuardExclusions refuses to remove or unlink a standard an
// exclusion names (NTE-034), and allows both once it is withdrawn.
func TestRealmStandardsGuardExclusions(t *testing.T) {
	dir := t.TempDir()
	setupNoteRealm(t, dir)
	addRealmNote(t, dir, "--type", "exclusion", "--ref", "owasp.asvs:V3.1.1", "--body", "x", "--added-by", "github:alice")

	for _, args := range [][]string{
		{"realm", "standards", "remove", "--id", "owasp.asvs"},
		{"realm", "standards", "update", "--id", "owasp.asvs", "--clear-realm"},
	} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...)
		if err == nil || !strings.Contains(err.Error(), "REALM/001") || !strings.Contains(err.Error(), "realm note update") {
			t.Errorf("%s: error = %v, want a refusal naming REALM/001 and the withdrawal", args[2], err)
		}
	}
	// A `standard` note mapping the same standard is reported with the
	// exclusion, so both are withdrawn in one pass.
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "note", "add", "MYE-001", "--type", "standard", "--body", "owasp.asvs:V1.1.1",
		"--added-by", "github:alice", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	_, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "remove", "--id", "owasp.asvs", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "NTE-016") || !strings.Contains(err.Error(), "REALM/001") {
		t.Errorf("remove with both blockers: error = %v, want the standard note and the exclusion", err)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "note", "update", "MYE-001/01", "--type", "reference", "--dir", dir); err != nil {
		t.Fatal(err)
	}

	// Changing the linked realm keeps a realm, so it goes through.
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "update", "--id", "owasp.asvs", "--realm", "dev.steerspec.std.owasp.asvs.v6", "--dir", dir); err != nil {
		t.Errorf("relink: %v", err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "note", "update", "REALM/001", "--type", "reference", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"realm", "standards", "update", "--id", "owasp.asvs", "--clear-realm"},
		{"realm", "standards", "remove", "--id", "owasp.asvs"},
	} {
		if _, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...); err != nil {
			t.Errorf("%s after withdrawal: %v", args[2], err)
		}
	}
}
