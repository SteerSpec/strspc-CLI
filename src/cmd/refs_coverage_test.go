package cmd

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// setupRefsCoverage writes the project of setupRefsProject with refs.coverage
// set to coverage, a YAML list indented under "coverage:".
func setupRefsCoverage(t *testing.T, coverage string) string {
	t.Helper()
	dir := setupRefsProject(t)
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules/\n    scope: local\nevaluator:\n  provider: null\n"+
		"refs:\n  coverage:\n"+coverage)
	return dir
}

// APP-001 (I) and APP-004 (D) are the rules of APP in the default states.
const refsCoverageAgents = "    - entities: [APP]\n      files: ['agents/*.md']\n"

func TestRefsCoverageClean(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, "agents/a.md", "APP-001\n")
	writeRefsFile(t, dir, "agents/b.md", "APP-004\n")
	_, err := runRefsCmd(t)
	assertExit(t, err, exitOK)
}

func TestRefsCoverageUncited(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, "agents/a.md", "APP-001\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, ".strspc/config.yaml")
	testutil.AssertContains(t, out, "RF008")
	testutil.AssertContains(t, out, "APP-004 (entity APP) is not cited in any file of this coverage set: agents/*.md")
	if strings.Contains(out, "APP-001 (entity") || strings.Contains(out, "APP-002") || strings.Contains(out, "APP-003") {
		t.Errorf("only APP-004 is uncited in the default states:\n%s", out)
	}
}

func TestRefsCoverageJSON(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, "agents/a.md", "APP-001\n")
	out, err := runRefsCmd(t, "--json")
	assertExit(t, err, exitViolations)
	var diags []struct{ Code, Path, Message string }
	if err := json.Unmarshal([]byte(out), &diags); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(diags) != 1 || diags[0].Code != "RF008" || diags[0].Path != ".strspc/config.yaml" ||
		!strings.HasSuffix(diags[0].Message, ": agents/*.md") {
		t.Errorf("got %+v", diags)
	}
}

func TestRefsCoverageGitHubFormat(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, "agents/a.md", "APP-001\n")
	out, err := runRefsCmd(t, "--format", "github")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "::error file=.strspc/config.yaml,title=rule-refs RF008::APP-004 (entity APP)")
}

// A coverage set reads its files wherever they are, even a .txt file that the
// main scan doesn't select, and judges their citations as the scan does.
func TestRefsCoverageStatesAndFilesOutsideTheScan(t *testing.T) {
	dir := setupRefsCoverage(t, "    - entities: [APP]\n      files: ['notes/*.txt']\n      states: [A]\n")
	writeRefsFile(t, dir, "notes/a.txt", "APP-002\n")
	_, err := runRefsCmd(t, "--allow-state", "A")
	assertExit(t, err, exitOK)

	writeRefsFile(t, dir, "notes/a.txt", "APP-002\nAPP-999\n")
	out, err := runRefsCmd(t, "--allow-state", "A")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RF001")
	testutil.AssertContains(t, out, "notes/a.txt:2")

	writeRefsFile(t, dir, "notes/a.txt", "APP-001\n")
	out, err = runRefsCmd(t)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "APP-002 (entity APP)")
}

// The sets run whatever paths the main scan is given.
func TestRefsCoverageIgnoresPathArguments(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, "agents/a.md", "APP-001 APP-004\n")
	writeRefsFile(t, dir, "docs/a.md", "APP-001\n")
	_, err := runRefsCmd(t, "docs", "--exclude", "agents/**")
	assertExit(t, err, exitOK)

	writeRefsFile(t, dir, "agents/a.md", "APP-001\n")
	out, err := runRefsCmd(t, "docs")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "APP-004 (entity APP)")
}

// Each set counts only its own files.
func TestRefsCoverageSetsAreSeparate(t *testing.T) {
	dir := setupRefsCoverage(t,
		"    - entities: [APP]\n      files: ['a/*.md']\n"+
			"    - entities: [APP]\n      files: ['b/*.md', '*.txt']\n")
	writeRefsFile(t, dir, "a/x.md", "APP-001 APP-004\n")
	writeRefsFile(t, dir, "b/x.md", "APP-001\n")
	writeRefsFile(t, dir, "b/x.txt", "APP-001\n")
	writeRefsFile(t, dir, "b/bin.txt", "APP-004\x00\n")
	out, err := runRefsCmd(t, "--json")
	assertExit(t, err, exitViolations)
	var all []struct{ Code, Message string }
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	diags := slices.DeleteFunc(all, func(d struct{ Code, Message string }) bool { return d.Code != "RF008" })
	if len(diags) != 1 || !strings.HasSuffix(diags[0].Message, "APP-004 (entity APP) is not cited in any file of this coverage set: b/*.md, *.txt") {
		t.Errorf("got %+v", diags)
	}
}

// A rule of a sub-entity counts toward its parent.
func TestRefsCoverageSubEntities(t *testing.T) {
	dir := setupRefsCoverage(t, "    - entities: [TOP]\n      files: ['agents/*.md']\n")
	sub := inheritEntityJSON("SUBX", "", "", inheritRule{id: "SUBX-001", state: "P"})
	writeInheritFile(t, filepath.Join(dir, "rules"), "TOP",
		inheritEntityJSON("TOP", "", "["+sub+"]", inheritRule{id: "TOP-001", state: "P"}))
	writeRefsFile(t, dir, "agents/a.md", "TOP-001\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "SUBX-001 (entity SUBX)")

	writeRefsFile(t, dir, "agents/a.md", "TOP-001 SUBX-001\n")
	_, err = runRefsCmd(t)
	assertExit(t, err, exitOK)
}

// A sub-realm of a local source is local too.
func TestRefsCoverageSubRealm(t *testing.T) {
	dir := setupRefsCoverage(t, "    - entities: [SUBE]\n      files: ['agents/*.txt']\n      states: [A]\n")
	writeRefsSubRealm(t, dir)
	writeRefsFile(t, dir, "agents/a.txt", "dev.app.sub:SUBE-001\n")
	_, err := runRefsCmd(t, "--allow-state", "A")
	assertExit(t, err, exitOK)

	writeRefsFile(t, dir, "agents/a.txt", "nothing\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "SUBE-001 (entity SUBE)")
}

func TestRefsCoverageGitignoredFileDoesNotMatch(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, ".gitignore", "agents/\n")
	writeRefsFile(t, dir, "agents/a.md", "APP-001 APP-004\n")
	initRefsGit(t, dir)
	_, err := runRefsCmd(t)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), `no file matches "agents/*.md"`)
}

func TestRefsCoverageConfigurationErrors(t *testing.T) {
	tests := []struct {
		name     string
		coverage string
		want     string
	}{
		{"no entity", "    - entities: [' ']\n      files: ['agents/*.md']\n", "/refs/coverage/0/entities/0"},
		{"no file", "    - entities: [APP]\n      files: ['']\n", "files must list at least one glob"},
		{"bad glob", "    - entities: [APP]\n      files: ['[']\n", `invalid files glob "["`},
		{"bad state", "    - entities: [APP]\n      files: ['agents/*.md']\n      states: [X]\n", "/refs/coverage/0/states/0"},
		{"no match", refsCoverageAgents + "    - entities: [APP]\n      files: ['agents/*.md', 'none/*']\n", `refs.coverage[1] in .strspc/config.yaml: no file matches "none/*"`},
		{"dependency entity", "    - entities: [ACCT]\n      files: ['agents/*.md']\n", `refs.coverage[0] in .strspc/config.yaml: entity "ACCT" is not in the local realm`},
		{"unknown entity", "    - entities: [NOPE]\n      files: ['agents/*.md']\n", `entity "NOPE"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsCoverage(t, tt.coverage)
			writeRefsFile(t, dir, "agents/a.md", "APP-001 APP-004\n")
			_, err := runRefsCmd(t)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

// A file of a set that can't be read is a configuration error. The main
// scan doesn't read .txt files, so the error comes from the coverage set.
func TestRefsCoverageUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs paths the current user cannot read")
	}
	dir := setupRefsCoverage(t, "    - entities: [APP]\n      files: ['agents/*.txt']\n")
	writeRefsFile(t, dir, "agents/a.txt", "APP-001 APP-004\n")
	if err := os.Chmod(filepath.Join(dir, "agents", "a.txt"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, err := runRefsCmd(t)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "reading agents/a.txt")
}

// Listing the project's files can fail outside the main scan's roots.
func TestRefsCoverageListingFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs paths the current user cannot read")
	}
	dir := setupRefsCoverage(t, refsCoverageAgents)
	writeRefsFile(t, dir, "agents/a.md", "APP-001 APP-004\n")
	writeRefsFile(t, dir, "docs/a.md", "APP-001\n")
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, err := runRefsCmd(t, "docs")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "listing files")
}

// rule show resolves the sources as refs does, but doesn't read refs.coverage.
func TestRuleShowIgnoresRefsCoverage(t *testing.T) {
	setupRefsCoverage(t, "    - entities: [APP]\n      files: ['[']\n")
	out, err := testutil.ExecuteCommand(NewRootCmd(), "rule", "show", "APP-001")
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "APP-001")
}

// A file that the main scan and two sets all read is judged once.
func TestRefsCoverageFileReportedOnce(t *testing.T) {
	dir := setupRefsCoverage(t, refsCoverageAgents+refsCoverageAgents+
		"    - entities: [APP]\n      files: ['notes/*.txt']\n"+
		"    - entities: [APP]\n      files: ['notes/*.txt']\n")
	writeRefsFile(t, dir, "agents/a.md", "APP-001 APP-004 APP-999\n")
	writeRefsFile(t, dir, "notes/a.txt", "APP-001 APP-004 APP-998\n")
	out, err := runRefsCmd(t, "--json")
	assertExit(t, err, exitViolations)
	var diags []struct{ Code, Path, Message string }
	if err := json.Unmarshal([]byte(out), &diags); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	count := map[string]int{}
	for _, d := range diags {
		count[d.Code+" "+d.Path]++
	}
	want := map[string]int{"RF001 agents/a.md": 1, "RF006 agents/a.md": 1, "RF001 notes/a.txt": 1, "RF006 notes/a.txt": 1}
	if !maps.Equal(count, want) {
		t.Errorf("got %v, want %v", count, want)
	}
}
