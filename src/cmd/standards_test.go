package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

const (
	coverageAppStandards = `"standards": [
    {"id": "owasp.asvs", "relation": "implements", "version": "4.0.3", "url": "https://owasp.org/asvs"},
    {"id": "ietf.rfc2119", "relation": "derived_from"},
    {"id": "nist.ssdf", "relation": "implements", "version": "1.1"}
  ]`
	coverageLibStandards = `"standards": [{"id": "iso.iec27001", "relation": "implements", "version": "2022"}]`
)

// withStandardNotes adds one `standard` note per pair of rule ID and content to
// an entity written by inheritEntityJSON.
func withStandardNotes(body string, pairs ...[2]string) string {
	notes := make([]string, 0, len(pairs))
	for _, p := range pairs {
		notes = append(notes, fmt.Sprintf(`{"id": "%s/01", "rule_ref": %q, "type": "standard",`+
			` "content": %q, "added_by": "@test", "added_at": "2026-01-01", "revision": 0}`, p[0], p[0], p[1]))
	}
	return strings.Replace(body, `"notes": []`, `"notes": [`+strings.Join(notes, ",")+`]`, 1)
}

func writeStandardsManifest(t *testing.T, dir, id, standards string, deps ...string) {
	t.Helper()
	writeEntityFile(t, dir, "realm.json", fmt.Sprintf(
		`{"realm": {"id": %q, "title": "t", "version": "1.0.0", %s}, "dependencies": [%s]}`,
		id, standards, strings.Join(deps, ","),
	))
}

// setupCoverageProject writes the realm dev.app in rules/, depending on dev.lib
// in lib/. BILL inherits ACCT and overrides the rule mapping A.6 with one that
// maps nothing, and the config lists sources with the given provider.
func setupCoverageProject(t *testing.T, provider string, sources ...string) string {
	t.Helper()
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	writeStandardsManifest(t, rules, "dev.app", coverageAppStandards,
		`{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`)
	writeInheritFile(t, rules, "AUTH", withStandardNotes(inheritEntityJSON("AUTH", "",
		"",
		inheritRule{id: "AUTH-001", state: "I"},
		inheritRule{id: "AUTH-002", state: "P"},
		inheritRule{id: "AUTH-003", state: "I"},
		inheritRule{id: "AUTH-004", state: "I"}),
		[2]string{"AUTH-001", "owasp.asvs:2.1.1"},
		[2]string{"AUTH-002", "owasp.asvs:2.1.2"},
		[2]string{"AUTH-003", "ietf.rfc2119:MUST"},
		[2]string{"AUTH-004", "undeclared.std:1"}))
	writeInheritEntity(t, rules, "BILL", "dev.lib:ACCT",
		inheritRule{id: "BILL-001", state: "I", overrides: "dev.lib:ACCT-002"})

	lib := filepath.Join(dir, "lib")
	writeStandardsManifest(t, lib, "dev.lib", coverageLibStandards)
	writeInheritFile(t, lib, "ACCT", withStandardNotes(inheritEntityJSON("ACCT", "",
		"",
		inheritRule{id: "ACCT-001", state: "I"},
		inheritRule{id: "ACCT-002", state: "I"}),
		[2]string{"ACCT-001", "iso.iec27001:A.5"},
		[2]string{"ACCT-002", "iso.iec27001:A.6"}))

	var b strings.Builder
	b.WriteString("rules:\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "  - source: %s\n    scope: local\n", s)
	}
	fmt.Fprintf(&b, "evaluator:\n  provider: %s\ncache:\n  ttl: 24h\n", provider)
	writeRawCheckConfig(t, dir, b.String())
	return dir
}

func runCoverage(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"standards", "coverage"}, args...)...)
}

// TestStandardsCoverageJSON checks each class of clause, a declared standard
// nothing maps, and a note that resolves to nothing.
func TestStandardsCoverageJSON(t *testing.T) {
	dir := setupCoverageProject(t, "claude", "./rules/")
	output, err := runCoverage(t, dir, "--json")
	assertExit(t, err, exitOK)

	var got coverageOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	want := []string{
		"ietf.rfc2119 / MUST AUTH cited",
		"iso.iec27001 2022 A.5 BILL covered",
		"iso.iec27001 2022 A.6 BILL lapsed",
		"owasp.asvs 4.0.3 2.1.1 AUTH covered",
		"owasp.asvs 4.0.3 2.1.2 AUTH mapped",
	}
	var clauses []string
	for _, c := range got.Clauses {
		version := c.Version
		if version == "" {
			version = "/"
		}
		clauses = append(clauses, strings.Join([]string{c.Standard, version, c.Clause, c.Entity, c.Status}, " "))
	}
	if !slices.Equal(clauses, want) {
		t.Errorf("clauses = %q\nwant %q", clauses, want)
	}
	if !slices.ContainsFunc(got.Declared, func(r entity.StandardRef) bool { return r.ID == "nist.ssdf" }) {
		t.Errorf("declared = %+v, want nist.ssdf, which nothing maps", got.Declared)
	}
	if len(got.Unresolved) != 1 || got.Unresolved[0].Rule != "dev.app:AUTH-004" {
		t.Errorf("unresolved = %+v, want dev.app:AUTH-004", got.Unresolved)
	}
	if got.Evaluator.Provider != "claude" || !got.Evaluator.RunsOnImplemented {
		t.Errorf("evaluator = %+v, want claude running on Implemented rules", got.Evaluator)
	}
}

// TestStandardsCoverageHuman checks the report's wording: it says "Implemented",
// shows the evaluator setting and says what it can't show.
func TestStandardsCoverageHuman(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		evaluator string
	}{
		{"evaluator runs", "claude", "Evaluator: claude, runs on Implemented rules"},
		{"no evaluator", "null", "Evaluator: null, so nothing evaluates Implemented rules"},
		{
			"evaluator skips Implemented", "claude\nevaluate_on: [P]",
			"Evaluator: claude, not run on Implemented rules (evaluate_on)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupCoverageProject(t, tt.config, "./rules/")
			output, err := runCoverage(t, dir)
			assertExit(t, err, exitOK)
			for _, want := range []string{
				tt.evaluator,
				"owasp.asvs 4.0.3 (implements)",
				"https://owasp.org/asvs",
				"2.1.1  AUTH  covered  dev.app:AUTH-001 (Implemented)",
				"dev.app:AUTH-002 (Published)",
				"dev.lib:ACCT-002 (Implemented)",
				"nist.ssdf 1.1 (implements)",
				"no clause mapped",
				"dev.app:AUTH-004",
				"a clause no rule maps isn't shown",
			} {
				testutil.AssertContains(t, output, want)
			}
		})
	}
}

// TestStandardsCoverageSources checks which configured sources are reported.
func TestStandardsCoverageSources(t *testing.T) {
	const unreachable = "github://SteerSpec/no-such-repo@v0.0.0/rules"

	t.Run("default leaves remote sources out", func(t *testing.T) {
		dir := setupCoverageProject(t, "null", unreachable, "./rules/")
		output, err := runCoverage(t, dir)
		assertExit(t, err, exitOK)
		testutil.AssertContains(t, output, "owasp.asvs")
	})

	t.Run("nothing local", func(t *testing.T) {
		dir := setupCoverageProject(t, "null", unreachable)
		output, err := runCoverage(t, dir)
		assertExit(t, err, exitConfig)
		testutil.AssertNotContains(t, output, "owasp.asvs")
		if !strings.Contains(err.Error(), "--all-sources") {
			t.Errorf("error = %q, want it to name --all-sources", err)
		}
	})

	t.Run("all sources", func(t *testing.T) {
		entries := []ruleresolve.SourceEntry{{Source: unreachable}, {Source: "./rules/"}}
		if got := coverageEntries(entries, false); len(got) != 1 || got[0].Source != "./rules/" {
			t.Errorf("local entries = %+v, want ./rules/ alone", got)
		}
		if got := coverageEntries(entries, true); len(got) != 2 {
			t.Errorf("all entries = %+v, want both", got)
		}
	})

	t.Run("a source listed twice counts once", func(t *testing.T) {
		dir := setupCoverageProject(t, "null", "./rules/", "./lib/")
		output, err := runCoverage(t, dir, "--json")
		assertExit(t, err, exitOK)
		var got coverageOutput
		if err := json.Unmarshal([]byte(output), &got); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, output)
		}
		var acct int
		for _, c := range got.Clauses {
			if c.Entity == "ACCT" {
				acct++
			}
		}
		if acct != 2 {
			t.Errorf("ACCT has %d clauses, want 2: %+v", acct, got.Clauses)
		}
	})
}

// TestStandardsCoverageWarnsOnSourceWithoutManifest checks that mappings the
// report can't reach are named, not dropped in silence.
func TestStandardsCoverageWarnsOnSourceWithoutManifest(t *testing.T) {
	dir := setupCoverageProject(t, "null", "./rules/", "./loose/")
	writeInheritFile(t, filepath.Join(dir, "loose"), "LOOSE", withStandardNotes(
		inheritEntityJSON("LOOSE", "", "", inheritRule{id: "LOOSE-001", state: "I"}),
		[2]string{"LOOSE-001", "owasp.asvs:9.9.9"},
	))

	output, err := runCoverage(t, dir)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, output, "./loose/: 1 `standard` note(s) left out: the source has no realm.json")

	output, err = runCoverage(t, dir, "--json")
	assertExit(t, err, exitOK)
	var got coverageOutput
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "./loose/") {
		t.Errorf("warnings = %q, want ./loose/", got.Warnings)
	}
}

// TestStandardsCoverageFailsOnBrokenDependency checks that a Realm that doesn't
// resolve gives no report: one would understate what the rules cover.
func TestStandardsCoverageFailsOnBrokenDependency(t *testing.T) {
	dir := setupCoverageProject(t, "null", "./rules/")
	if err := os.RemoveAll(filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	output, err := runCoverage(t, dir)
	assertExit(t, err, exitConfig)
	testutil.AssertNotContains(t, output, "owasp.asvs 4.0.3")
}

// TestRewritesKeepStandards checks that the commands rewriting a manifest or an
// entity file keep what declares and maps a standard.
func TestRewritesKeepStandards(t *testing.T) {
	const standards = `"standards": [{"id": "owasp.asvs", "relation": "implements"}]`
	entityBody := strings.Replace(
		withStandardNotes(inheritEntityJSON("TST", "", "", inheritRule{id: "TST-001", state: "D"}),
			[2]string{"TST-001", "owasp.asvs:2.1.1"}),
		`"description": "d"`, `"description": "d", `+standards, 1,
	)

	tests := []struct {
		name string
		file string
		args []string
		want []string
	}{
		{
			"realm dep add", "realm.json",
			[]string{"realm", "dep", "add", "dev.other@1.0.0"},
			[]string{`"owasp.asvs"`},
		},
		{
			"rule add", "TST.json",
			[]string{"rule", "add", "TST", "--body", "A new rule MUST exist.", "--added-by", "@test"},
			[]string{`"owasp.asvs"`, `"standard"`, "owasp.asvs:2.1.1"},
		},
		{
			"note add", "TST.json",
			[]string{"note", "add", "TST-001", "--type", "standard", "--body", "owasp.asvs:2.1.2", "--added-by", "@test"},
			[]string{`"owasp.asvs"`, "owasp.asvs:2.1.1", "owasp.asvs:2.1.2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStandardsManifest(t, dir, "dev.app", standards)
			writeEntityFile(t, dir, "TST.json", entityBody)
			if output, err := testutil.ExecuteCommand(NewRootCmd(), append(tt.args, "--dir", dir)...); err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			data, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				testutil.AssertContains(t, string(data), want)
			}
		})
	}
}
