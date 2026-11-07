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

// testStdRealm is the realm_id of a Standard Realm a reference links to.
const testStdRealm = "dev.steerspec.std.owasp.asvs.v5"

// setupLinkRealm writes a realm at dir pinning core at manifestMinCore, with no source so
// validation fetches nothing, and entity MYE with one rule.
func setupLinkRealm(t *testing.T, dir string) {
	t.Helper()
	setupValidRealm(t, dir)
	pinCoreFrom(t, dir, manifestMinCore.String(), "")
	setupEntityFile(t, dir, "MYE", makeRule("MYE-001", "D", "The system MUST validate.", "github:alice"))
}

// TestStandardsAddRealmAndProfile links a new reference to a Standard Realm
// and one of its profiles, at both scopes.
func TestStandardsAddRealmAndProfile(t *testing.T) {
	dir := t.TempDir()
	setupLinkRealm(t, dir)
	want := entity.StandardRef{ID: "owasp.asvs", Relation: "implements", Version: "5.0.0", Realm: testStdRealm, Profile: "L1"}

	for _, args := range [][]string{
		{"realm", "standards", "add"},
		{"entity", "standards", "add", "MYE"},
	} {
		args = append(args, "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0",
			"--realm", testStdRealm, "--profile", "L1", "--dir", dir)
		out, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		if err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
		testutil.AssertContains(t, out, "owasp.asvs 5.0.0 (implements, realm "+testStdRealm+", profile L1)")
	}
	if got := readRealmStandards(t, dir); len(got) != 1 || got[0] != want {
		t.Errorf("realm standards = %+v, want [%+v]", got, want)
	}
	if got := loadEntityFile(t, dir, "MYE").Entity.Standards; len(got) != 1 || got[0] != want {
		t.Errorf("entity standards = %+v, want [%+v]", got, want)
	}
	assertFileHashFresh(t, dir, "MYE")

	// The vendored core schemas accept the linked reference.
	if out, err := validateWithSchemas(t, dir); err != nil {
		t.Fatalf("realm validate rejects the manifest: %v\n%s", err, out)
	}
}

// TestStandardsListShowsLink adds the realm and profile columns only when a
// reference links to a Standard Realm.
func TestStandardsListShowsLink(t *testing.T) {
	dir := t.TempDir()
	setupLinkRealm(t, dir)
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add", "--id", "ietf.rfc2119", "--relation", "derived_from", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "list", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, testStdRealm) {
		t.Errorf("unlinked list mentions a realm:\n%s", out)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements",
		"--version", "5.0.0", "--realm", testStdRealm, "--profile", "L2", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "list", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, out, testStdRealm)
	testutil.AssertContains(t, out, "L2")

	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "list", "--dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Standards []entity.StandardRef `json:"standards"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(res.Standards) != 2 || res.Standards[1].Realm != testStdRealm || res.Standards[1].Profile != "L2" {
		t.Errorf("standards = %+v", res.Standards)
	}
}

// TestStandardsAddRealmValidation refuses a profile without a realm
// (STDREF-019), a --realm that isn't a realm ID and an empty --profile, and
// writes nothing.
func TestStandardsAddRealmValidation(t *testing.T) {
	dir := t.TempDir()
	setupLinkRealm(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		extra []string
		want  string
	}{
		{"profile without realm", []string{"--profile", "L1"}, "STDREF-019"},
		{"realm not a realm ID", []string{"--realm", "Not A Realm"}, "is not a realm ID"},
		{"empty profile", []string{"--realm", testStdRealm, "--profile", ""}, "--profile must not be empty"},
		{"control character in profile", []string{"--realm", testStdRealm, "--profile", "L1\x1b"}, "--profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0", "--dir", dir}, tt.extra...)
			_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
	after, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("realm.json changed on refusal:\n%s", after)
	}
}

// TestStandardsLinkRefusesOldCore refuses a realm or profile under a core pin
// whose schemas reject it, at both scopes and for add and update, and accepts
// the same reference without one.
func TestStandardsLinkRefusesOldCore(t *testing.T) {
	dir := t.TempDir()
	setupValidRealm(t, dir)
	pinCore(t, dir, "1.20.0")
	setupEntityFile(t, dir, "MYE")

	link := []string{"--realm", testStdRealm}
	for _, args := range [][]string{
		{"realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0"},
		{"entity", "standards", "add", "MYE", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0"},
	} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), append(append(args, link...), "--dir", dir)...)
		if err == nil || !strings.Contains(err.Error(), "reject `realm`") || !strings.Contains(err.Error(), manifestMinCore.String()) {
			t.Errorf("%s: error = %v, want a refusal naming `realm` and %s", args[0], err, manifestMinCore)
		}
		if _, err := testutil.ExecuteCommand(NewRootCmd(), append(args, "--dir", dir)...); err != nil {
			t.Errorf("%s without a realm: %v", args[0], err)
		}
	}
	for _, args := range [][]string{
		{"realm", "standards", "update", "--id", "owasp.asvs"},
		{"entity", "standards", "update", "MYE", "--id", "owasp.asvs"},
	} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), append(append(args, link...), "--dir", dir)...)
		if err == nil || !strings.Contains(err.Error(), "reject `realm`") {
			t.Errorf("%s update: error = %v, want a refusal naming `realm`", args[0], err)
		}
	}
}

// TestStandardsUpdateRealmAndProfile sets, changes and clears the link of a
// reference, and refuses to leave a profile without a realm (STDREF-019).
func TestStandardsUpdateRealmAndProfile(t *testing.T) {
	dir := t.TempDir()
	setupLinkRealm(t, dir)
	run := func(extra ...string) (string, error) {
		args := append([]string{"realm", "standards", "update", "--id", "owasp.asvs", "--dir", dir}, extra...)
		return testutil.ExecuteCommand(NewRootCmd(), args...)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "standards", "add", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0", "--dir", dir); err != nil {
		t.Fatal(err)
	}

	out, err := run("--realm", testStdRealm, "--profile", "L1")
	if err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "realm "+testStdRealm+", profile L1")
	if got := readRealmStandards(t, dir)[0]; got.Realm != testStdRealm || got.Profile != "L1" {
		t.Fatalf("after link: %+v", got)
	}

	if _, err := run("--clear-realm"); err == nil || !strings.Contains(err.Error(), "STDREF-019") {
		t.Errorf("clearing the realm under a profile: error = %v, want STDREF-019", err)
	}
	if _, err := run("--realm", testStdRealm); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Errorf("same realm: error = %v, want nothing to change", err)
	}
	if _, err := run("--profile", "L2"); err != nil {
		t.Fatalf("profile change: %v", err)
	}
	if _, err := run("--clear-profile", "--clear-realm"); err != nil {
		t.Fatalf("clear both: %v", err)
	}
	if got := readRealmStandards(t, dir)[0]; got.Realm != "" || got.Profile != "" {
		t.Errorf("after clearing: %+v", got)
	}
	if _, err := run("--realm", "Bad Realm"); err == nil || !strings.Contains(err.Error(), "is not a realm ID") {
		t.Errorf("bad realm: error = %v", err)
	}
	if _, err := run("--realm", testStdRealm, "--clear-realm"); err == nil {
		t.Error("--realm with --clear-realm: expected an error")
	}

	// The entity scope edits the same fields.
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "add", "MYE", "--id", "owasp.asvs", "--relation", "implements", "--version", "5.0.0", "--dir", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "standards", "update", "MYE", "--id", "owasp.asvs", "--realm", testStdRealm, "--dir", dir); err != nil {
		t.Fatalf("entity link: %v", err)
	}
	if got := loadEntityFile(t, dir, "MYE").Entity.Standards[0]; got.Realm != testStdRealm {
		t.Errorf("entity reference after link: %+v", got)
	}
	assertFileHashFresh(t, dir, "MYE")
}
