package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// setupVerifyRepo is setupCheckProject inside a git repository with two
// tracked source files, one untracked file and the excluded directories.
func setupVerifyRepo(t *testing.T, provider string) string {
	t.Helper()
	dir, _ := setupCheckProject(t, provider, []string{"I"})
	if err := os.Remove(filepath.Join(dir, "change.diff")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "src/main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "docs/notes.md", "# Notes\n")
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeFile(t, dir, "untracked.go", "package main\n")
	return dir
}

func runVerifyCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"rule", "verify"}, args...)...)
}

func decodeVerify(t *testing.T, out string) verifyOutput {
	t.Helper()
	var vo verifyOutput
	if err := json.Unmarshal([]byte(out), &vo); err != nil {
		t.Fatalf("decoding JSON: %v\n%s", err, out)
	}
	return vo
}

func verdictsOf(vo *verifyOutput) map[string]string {
	m := map[string]string{}
	for _, r := range vo.Rules {
		m[r.ID] = r.Verdict
	}
	return m
}

func TestTreePathspecs(t *testing.T) {
	// A backslash is a name character on Unix, escaped as such; on Windows
	// it is a path separator, which filepath.ToSlash turns into "/".
	backslashSpec := `:(glob)a[\\]b/x`
	if runtime.GOOS == "windows" {
		backslashSpec = ":(glob)a/b/x"
	}
	tests := []struct {
		name     string
		subDir   string
		paths    []string
		excludes []string
		want     []string
		wantErr  bool
	}{
		{"root, no paths", ".", nil, nil, []string{"."}, false},
		{"empty subdir", "", nil, nil, []string{"."}, false},
		{"subdir, no paths", "svc[1]", nil, nil, []string{":(literal)svc[1]"}, false},
		{"root glob", ".", []string{"src/**/*.go"}, nil, []string{":(glob)src/**/*.go"}, false},
		{"subdir path", "svc", []string{"./cmd/"}, nil, []string{":(glob)svc/cmd"}, false},
		{"blank skipped", ".", []string{" ", "src"}, nil, []string{":(glob)src"}, false},
		{"only blanks", ".", []string{" "}, nil, nil, true},
		{"climbs out", ".", []string{"../x"}, nil, nil, true},
		{"parent", "svc", []string{".."}, nil, nil, true},
		{"absolute", ".", []string{"/etc"}, nil, nil, true},
		{"exclude only", ".", nil, []string{"**/*_test.go", " "}, []string{".", ":(exclude,glob)**/*_test.go"}, false},
		{"path and exclude in subdir", "svc", []string{"cmd"}, []string{"cmd/gen"}, []string{":(glob)svc/cmd", ":(exclude,glob)svc/cmd/gen"}, false},
		{"subdir with glob characters", "a*b[1]?", []string{"x/*.go"}, nil, []string{`:(glob)a[*]b[[]1[]][?]/x/*.go`}, false},
		{"subdir with a backslash", `a\b`, []string{"x"}, nil, []string{backslashSpec}, false},
		{"exclude climbs out", ".", nil, []string{"../x"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := treePathspecs(tt.subDir, tt.paths, tt.excludes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func pathsOf(files []ruleeval.File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func TestCollectTreeFiles(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	ctx := context.Background()

	all, skipped, err := collectTreeFiles(ctx, dir, ".", []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pathsOf(all), ","); got != "docs/notes.md,src/main.go" || len(skipped) != 0 {
		t.Fatalf("files = %s, skipped %v; want docs and src only (untracked, .strspc/ and rules/ are not read)", got, skipped)
	}
	if all[1].Content != "package main\n\nfunc main() {}\n" {
		t.Errorf("content = %q", all[1].Content)
	}

	// Uncommitted edits to tracked files are read.
	writeFile(t, dir, "src/main.go", "package main\n\nfunc main() { run() }\n")
	only, _, err := collectTreeFiles(ctx, dir, ".", []string{":(glob)src/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || !strings.Contains(only[0].Content, "run()") {
		t.Errorf("working tree edit missing: %+v", only)
	}
}

// What git would not show as text is skipped and reported, never read.
func TestCollectTreeFilesSkips(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	writeFile(t, dir, "img.png", "\x89PNG\x00\x01")
	writeFile(t, dir, "latin1.txt", "caf\xe9\n")
	writeFile(t, dir, "gone.go", "package main\n")
	writeFile(t, dir, "gonedir/x.go", "package x\n")
	wantSkipped := []string{"gone.go=deleted", "gonedir/x.go=deleted", "img.png=binary", "latin1.txt=binary"}
	outside := t.TempDir()
	symlinks := runtime.GOOS != "windows"
	if symlinks {
		writeFile(t, outside, "secret.txt", "do not read\n")
		writeFile(t, outside, "inner.go", "package outside\n")
		writeFile(t, dir, "linked/inner.go", "package linked\n")
		if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
			t.Fatal(err)
		}
		wantSkipped = append(wantSkipped, "link.txt=symlink", "linked/inner.go=symlink")
	}
	added := []string{"add", "img.png", "latin1.txt", "gone.go", "gonedir"}
	if symlinks {
		added = append(added, "link.txt", "linked")
	}
	gitRun(t, dir, added...)
	// The working tree then departs from the index.
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "gonedir")); err != nil {
		t.Fatal(err)
	}
	if symlinks {
		// A tracked directory replaced by a symlink.
		if err := os.RemoveAll(filepath.Join(dir, "linked")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
			t.Fatal(err)
		}
	}

	files, skipped, err := collectTreeFiles(context.Background(), dir, ".", []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pathsOf(files), ","); got != "docs/notes.md,src/main.go" {
		t.Errorf("files = %s", got)
	}
	got := make([]string, len(skipped))
	for i, s := range skipped {
		got[i] = s.Path + "=" + s.Reason
	}
	sort.Strings(got)
	sort.Strings(wantSkipped)
	if strings.Join(got, " ") != strings.Join(wantSkipped, " ") {
		t.Errorf("skipped = %v, want %v", got, wantSkipped)
	}
}

func TestTreeFileSkipUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NUL in a path is refused differently on Windows")
	}
	if reason := treeFileSkip(t.TempDir(), "a\x00b", map[string]bool{}); reason == "" {
		t.Error("an unreadable path must be skipped")
	}
	if !underSymlink(t.TempDir(), "d\x00", map[string]bool{}) {
		t.Error("a directory that cannot be inspected counts as a symlink")
	}
}

// A glob in --path must not reach a sibling of a project whose directory
// name holds glob characters.
func TestCollectTreeFilesEscapesProjectDir(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "svc[1]/cmd/a.go", "package cmd\n")
	writeFile(t, dir, "svc[1]/cmd/a_test.go", "package cmd\n")
	writeFile(t, dir, "svc1/cmd/b.go", "package cmd\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")

	specs, err := treePathspecs("svc[1]", []string{"cmd/*.go"}, []string{"**/*_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := collectTreeFiles(context.Background(), dir, "svc[1]", specs)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pathsOf(files), ","); got != "svc[1]/cmd/a.go" {
		t.Errorf("files = %s, want svc[1]/cmd/a.go", got)
	}
}

func TestCollectTreeFilesNotARepo(t *testing.T) {
	if _, _, err := collectTreeFiles(context.Background(), t.TempDir(), ".", []string{"."}); err == nil {
		t.Fatal("expected an error outside a repository")
	}
}

func TestSelectVerifyInputs(t *testing.T) {
	inputs := []ruleeval.RuleInput{
		{Rule: entity.Rule{ID: "TST-001"}, EntityID: "TST"},
		{Rule: entity.Rule{ID: "TST-002"}, EntityID: "TST"},
		{Rule: entity.Rule{ID: "TST-001"}, EntityID: "TST"}, // duplicate from a second source
	}
	got, err := selectVerifyInputs(inputs, []string{"tst-001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rule.ID != "TST-001" {
		t.Fatalf("got %+v", got)
	}
	_, err = selectVerifyInputs(inputs, []string{"TST-002", "ZZZ-9", "AAA-1"})
	if err == nil || !strings.Contains(err.Error(), "AAA-1, ZZZ-9") {
		t.Fatalf("want missing IDs in sorted order, got %v", err)
	}
}

func TestOneVerifyGroup(t *testing.T) {
	in := []ruleeval.RuleInput{
		{Rule: entity.Rule{ID: "CFG-002"}, EntityID: "CFG"},
		{Rule: entity.Rule{ID: "CMD-001"}, EntityID: "CMD"},
		{Rule: entity.Rule{ID: "X-1"}, EntityID: "SUB via r:X", Owner: "r:X"}, // inherited: keeps its owner
	}
	got := oneVerifyGroup(in)
	wantOwner := []string{"CFG", "CMD", "r:X"}
	for i := range got {
		if got[i].EntityID != verifyGroupID || got[i].Owner != wantOwner[i] {
			t.Errorf("input %d = entity %q owner %q, want %q %q", i, got[i].EntityID, got[i].Owner, verifyGroupID, wantOwner[i])
		}
	}
	if in[0].EntityID != "CFG" || in[0].Owner != "" {
		t.Error("the inputs must not be modified")
	}
	if est := estimateVerify(nil, got); est.RuleGroups != 1 {
		t.Errorf("rule groups = %d, want 1", est.RuleGroups)
	}
}

func TestEstimateVerify(t *testing.T) {
	files := []ruleeval.File{{Path: "src/a.go", Content: "package a\n"}, {Path: "README.md", Content: "# R\n"}, {Path: "empty"}}
	inputs := make([]ruleeval.RuleInput, 0, 45)
	for i := 0; i < 41; i++ {
		inputs = append(inputs, ruleeval.RuleInput{EntityID: "BIG"})
	}
	inputs = append(inputs, ruleeval.RuleInput{EntityID: "SMALL", Notes: []entity.Note{{Content: "a note"}}})

	est := estimateVerify(files, inputs)
	if est.Files != 3 || est.Bytes != len("package a\n# R\n") {
		t.Errorf("files/bytes = %d/%d", est.Files, est.Bytes)
	}
	if est.BytesByDir["src"] != 10 || est.BytesByDir["."] != 4 {
		t.Errorf("bytes by dir = %v", est.BytesByDir)
	}
	// One part: a verdict request per rule group, and a detail per rule at most.
	if est.EstParts != 1 || est.RuleGroups != 3 || est.EstRequests != 3+42 {
		t.Errorf("parts/groups/requests = %d/%d/%d, want 1/3/45", est.EstParts, est.RuleGroups, est.EstRequests)
	}
	if est.EstInputTokens <= len("package a\n# R\n")/2 {
		t.Errorf("tokens = %d: line numbers and file tags count too", est.EstInputTokens)
	}

	// ~500k tokens: several parts, each read per rule group, then one judgement per group.
	big := []ruleeval.File{{Path: "big.txt", Content: strings.Repeat("x\n", 500000)}}
	got := estimateVerify(big, []ruleeval.RuleInput{{EntityID: "E"}})
	if got.EstParts < 5 || got.EstRequests != got.EstParts+1 {
		t.Errorf("parts/requests for a large tree = %d/%d", got.EstParts, got.EstRequests)
	}

	// Rules large enough to need a group each: every part is read once per group.
	heavy := make([]ruleeval.RuleInput, 3)
	for i := range heavy {
		heavy[i] = ruleeval.RuleInput{EntityID: "E", Rule: entity.Rule{ID: fmt.Sprintf("E-%03d", i+1), Body: strings.Repeat("y", 60000)}}
	}
	got = estimateVerify(big, heavy)
	if got.RuleGroups != 3 || got.EstRequests != got.EstParts*3+3 {
		t.Errorf("groups/parts/requests for heavy rules = %d/%d/%d", got.RuleGroups, got.EstParts, got.EstRequests)
	}
}

// planVerifyRules sizes groups by the rendered rules and their schema, as
// the provider does, not by rule count alone.
func TestPlanVerifyRules(t *testing.T) {
	rule := func(id string, bodyBytes int) ruleeval.RuleInput {
		return ruleeval.RuleInput{EntityID: "E", Rule: entity.Rule{ID: id, Body: strings.Repeat("y", bodyBytes)}}
	}
	small := []ruleeval.RuleInput{rule("E-1", 10), rule("E-2", 10)}
	groups, budget := planVerifyRules(prompt.AuditSystem(), nil, [][]ruleeval.RuleInput{small})
	if groups != 1 || budget <= 0 || budget >= 100000 {
		t.Errorf("small rules: groups %d, budget %d", groups, budget)
	}
	withSchema, schemaBudget := planVerifyRules(prompt.AuditExtractSystem(), prompt.ExtractSchema, [][]ruleeval.RuleInput{small})
	if withSchema != 1 || schemaBudget >= budget {
		t.Errorf("the extract schema must cost budget: %d vs %d", schemaBudget, budget)
	}

	// Two rules of ~30k tokens share no group of ~48k; one of ~60k fits none.
	mixed := []ruleeval.RuleInput{rule("E-1", 60000), rule("E-2", 60000), rule("E-3", 120000)}
	if groups, _ := planVerifyRules(prompt.AuditSystem(), nil, [][]ruleeval.RuleInput{mixed}); groups != 2 {
		t.Errorf("heavy rules: groups %d, want 2 (the oversize rule is sent in none)", groups)
	}

	// The per-request rule limit splits a group too, and entities never share one.
	many := make([]ruleeval.RuleInput, 41)
	for i := range many {
		many[i] = rule(fmt.Sprintf("E-%d", i), 10)
	}
	if groups, _ := planVerifyRules(prompt.AuditSystem(), nil, [][]ruleeval.RuleInput{many, small}); groups != 3 {
		t.Errorf("41 + 2 rules in two entities: groups %d, want 3", groups)
	}
}

func TestPackVerifyParts(t *testing.T) {
	tests := []struct {
		sizes  []int
		budget int
		want   int
	}{
		{nil, 5, 0},
		{[]int{3, 3, 3}, 5, 3},
		{[]int{2, 2, 1}, 5, 1},
		{[]int{12}, 5, 3},
		{[]int{2, 12, 2}, 5, 5},
		{[]int{4}, 0, 4}, // a budget below 1 counts as 1
	}
	for _, tt := range tests {
		if got := packVerifyParts(tt.sizes, tt.budget); got != tt.want {
			t.Errorf("packVerifyParts(%v, %d) = %d, want %d", tt.sizes, tt.budget, got, tt.want)
		}
	}
}

// When every file the scope matches is skipped, the failure still lists
// them.
func TestRuleVerifyAllSkipped(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	writeFile(t, dir, "assets/logo.png", "\x89PNG\x00")
	gitRun(t, dir, "add", "assets/logo.png")
	stubEvalProvider(t, &stubProvider{}, nil)

	out, err := runVerifyCmd(t, "TST-001", "--project", dir, "--path", "assets", "--format", "json")
	assertExit(t, err, exitConfig)
	vo := decodeVerify(t, out)
	if len(vo.Skipped) != 1 || vo.Skipped[0].Path != "assets/logo.png" || !strings.Contains(vo.Error, "1 tracked file(s) skipped") {
		t.Errorf("report = %+v", vo)
	}
	_, err = runVerifyCmd(t, "TST-001", "--project", dir, "--path", "assets")
	assertExit(t, err, exitConfig)
	if !strings.Contains(err.Error(), "skipped (deleted, symlinked, binary or not a regular file") {
		t.Errorf("err = %v", err)
	}
}

func TestRuleVerifyReportsEachVerdict(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stub := &batchStubProvider{stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": violatedResponse("Run has no doc comment"),
		"TST-002": {Verdict: ruleeval.Compliant, Explanation: "errors are wrapped"},
	}}}
	opts := stubEvalProvider(t, stub, nil)

	out, err := runVerifyCmd(t, "TST-001", "tst-002", "TST-003", "TST-004", "--project", dir, "--format", "json", "--effort", "high", "--model", "m1")
	assertExit(t, err, exitViolations)
	if opts.Effort != "high" || opts.Model != "m1" {
		t.Errorf("provider options = %+v, want effort high and model m1", *opts)
	}
	vo := decodeVerify(t, out)
	want := map[string]string{"TST-001": verifyViolated, "TST-002": verifyCompliant, "TST-003": verifyNotApplicable, "TST-004": verifyNotApplicable}
	if got := verdictsOf(&vo); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("verdicts = %v, want %v (a retired rule named explicitly is still judged)", got, want)
	}
	if vo.OK || vo.Effort != "high" || vo.Provider != "claude" || vo.Scope != "all tracked files" {
		t.Errorf("report header = %+v", vo)
	}
	if vo.Estimate.Files != 2 {
		t.Errorf("files = %d, want 2 (untracked, .strspc/ and rules/ are not read)", vo.Estimate.Files)
	}
	for _, d := range vo.Diagnostics {
		if d.Code == ruleeval.CodeViolated {
			t.Errorf("RE001 duplicates the per-rule report: %+v", d)
		}
	}
	if seen := strings.Join(stub.seenFiles(), ","); seen != "docs/notes.md,src/main.go" || stub.seenDiff() != "" {
		t.Errorf("provider audited %q (diff %q), want the tracked files and no diff", seen, stub.seenDiff())
	}
}

// A project in a sub-directory labels its local source relative to the
// repository root, the base the diff paths use.
func TestRuleVerifySubdirSourceLabel(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	writeFile(t, repo, "svc/rules/TST.json", checkEntityJSON)
	writeCheckConfig(t, filepath.Join(repo, "svc"), "./rules/", "claude", nil, nil)
	writeFile(t, repo, "svc/main.go", "package main\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "base")

	stub := &batchStubProvider{}
	var sources []string
	stubEvalProvider(t, &sourceSpy{batchStubProvider: stub, sources: &sources}, nil)
	_, err := runVerifyCmd(t, "TST-002", "--project", filepath.Join(repo, "svc"))
	assertExit(t, err, exitOK)
	if len(sources) != 1 || sources[0] != "svc/rules/" {
		t.Errorf("sources = %q, want [svc/rules/]", sources)
	}
	if seen := stub.seenFiles(); len(seen) != 1 || seen[0] != "svc/main.go" {
		t.Errorf("file paths should be repository-relative: %q", seen)
	}
}

// sourceSpy records the Source of every batched input.
type sourceSpy struct {
	*batchStubProvider
	sources *[]string
}

func (s *sourceSpy) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) { //nolint:gocritic // ruleeval.BatchProvider signature
	for i := range req.Inputs {
		*s.sources = append(*s.sources, req.Inputs[i].Source)
	}
	return s.batchStubProvider.EvaluateBatch(ctx, req)
}

func TestRuleVerifyAllCompliantHuman(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stub := &stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-002": {Verdict: ruleeval.Compliant, Explanation: "fine"},
	}}
	stubEvalProvider(t, stub, nil)

	out, err := runVerifyCmd(t, "TST-002", "--project", dir, "--path", "src", "--verbose")
	assertExit(t, err, exitOK)
	for _, want := range []string{"TST-002", "compliant", "fine", "1 compliant, 0 violated", "tracked files under src", "Estimate:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if stub.callCount() != 1 {
		t.Errorf("non-batch provider calls = %d, want 1", stub.callCount())
	}
}

func TestRuleVerifyUnknownVerdictFails(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stub := &stubProvider{
		verdicts: map[string]*ruleeval.EvalResponse{"TST-002": {Verdict: ruleeval.Compliant}},
		errFor:   map[string]error{"TST-001": errors.New("malformed response")},
	}
	stubEvalProvider(t, stub, nil)

	out, err := runVerifyCmd(t, "TST-001", "TST-002", "--project", dir, "--format", "json")
	assertExit(t, err, exitViolations)
	vo := decodeVerify(t, out)
	if got := verdictsOf(&vo); got["TST-001"] != verifyUnknown || got["TST-002"] != verifyCompliant {
		t.Errorf("verdicts = %v", got)
	}
	var re002 bool
	for _, d := range vo.Diagnostics {
		re002 = re002 || d.Code == ruleeval.CodeProviderError
	}
	if !re002 {
		t.Errorf("RE002 should explain the unknown verdict: %+v", vo.Diagnostics)
	}

	human, err := runVerifyCmd(t, "TST-001", "TST-002", "--project", dir)
	assertExit(t, err, exitViolations)
	for _, want := range []string{"RE002", "TST-001      unknown", "1 unknown"} {
		if !strings.Contains(human, want) {
			t.Errorf("human output missing %q:\n%s", want, human)
		}
	}
}

func TestRuleVerifyDryRunSendsNothing(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, nil, errors.New("provider must not be built in a dry run"))

	out, err := runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run", "--format", "json")
	assertExit(t, err, exitOK)
	vo := decodeVerify(t, out)
	if !vo.DryRun || !vo.OK || vo.Provider != "" || vo.Estimate.Files != 2 || vo.Estimate.EstRequests != 2 || len(vo.Rules) != 0 {
		t.Errorf("dry run report = %+v", vo)
	}

	human, err := runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run")
	assertExit(t, err, exitOK)
	for _, want := range []string{"Scope: all tracked files: 2 file(s)", "src", "Dry run: nothing was sent"} {
		if !strings.Contains(human, want) {
			t.Errorf("dry run output missing %q:\n%s", want, human)
		}
	}
}

func TestRuleVerifyConfigErrors(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, &stubProvider{}, nil)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown rule", []string{"NOPE-1"}, "not found"},
		{"github format", []string{"TST-001", "--format", "github"}, "invalid --format"},
		{"path escapes", []string{"TST-001", "--path", "../x"}, "stay inside"},
		{"no files", []string{"TST-001", "--path", "nothing/**"}, "no tracked text files"},
		{"static provider", []string{"TST-001", "--provider", "null"}, "needs an AI provider"},
		{"control char effort", []string{"TST-001", "--effort", "hi\x1bgh"}, "control character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runVerifyCmd(t, append(tt.args, "--project", dir)...)
			assertExit(t, err, exitConfig)
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRuleVerifyConfigErrorJSON(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, &stubProvider{}, nil)
	out, err := runVerifyCmd(t, "NOPE-1", "--project", dir, "--format", "json")
	assertExit(t, err, exitConfig)
	vo := decodeVerify(t, out)
	if vo.OK || vo.ExitCode != exitConfig || !strings.Contains(vo.Error, "NOPE-1") {
		t.Errorf("report = %+v", vo)
	}
}

func TestRuleVerifyNoConfig(t *testing.T) {
	_, err := runVerifyCmd(t, "TST-001", "--project", t.TempDir())
	assertExit(t, err, exitConfig)
}

func TestRuleVerifyNotARepo(t *testing.T) {
	dir, _ := setupCheckProject(t, "claude", nil)
	stubEvalProvider(t, &stubProvider{}, nil)
	_, err := runVerifyCmd(t, "TST-001", "--project", dir)
	assertExit(t, err, exitConfig)
}

func TestRuleVerifyResolutionError(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	writeCheckConfig(t, dir, "./missing/", "claude", nil, nil)
	stubEvalProvider(t, &stubProvider{}, nil)
	out, err := runVerifyCmd(t, "TST-001", "--project", dir, "--format", "json")
	assertExit(t, err, exitConfig)
	vo := decodeVerify(t, out)
	if !strings.Contains(vo.Error, "rule resolution failed") || len(vo.Diagnostics) == 0 {
		t.Errorf("report = %+v", vo)
	}
	_, err = runVerifyCmd(t, "TST-001", "--project", dir)
	assertExit(t, err, exitConfig)
}

func TestRuleVerifyProviderUnavailable(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, nil, fmt.Errorf("%w: no API key", ruleeval.ErrUnavailable))
	_, err := runVerifyCmd(t, "TST-001", "--project", dir)
	assertExit(t, err, exitUnavailable)

	// Unavailable mid-run, and every request failing.
	stubEvalProvider(t, &stubProvider{err: fmt.Errorf("%w: 401", ruleeval.ErrUnavailable)}, nil)
	_, err = runVerifyCmd(t, "TST-001", "--project", dir)
	assertExit(t, err, exitUnavailable)
	stubEvalProvider(t, &stubProvider{err: errors.New("boom")}, nil)
	_, err = runVerifyCmd(t, "TST-001", "--project", dir)
	assertExit(t, err, exitUnavailable)
}

// failWriter fails every write, to exercise exit 4 (CMD-008).
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRuleVerifyJSONWriteFailure(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, &stubProvider{}, nil)
	err := runVerify(context.Background(), failWriter{}, failWriter{}, []string{"TST-001"},
		&verifyOptions{project: dir, check: checkOptions{format: formatJSON}})
	assertExit(t, err, exitOutput)
}

func TestVerdictProviderForwards(t *testing.T) {
	rec := newVerdictRecorder()
	plain := wrapRecording(&stubProvider{}, rec)
	if _, ok := plain.(ruleeval.BatchProvider); ok {
		t.Error("a non-batch provider must not gain EvaluateBatch")
	}
	if d := plain.(ruleeval.Describer); d.Model() != "stub-model" || d.PromptVersion() != "test/v1" {
		t.Errorf("describer not forwarded: %q %q", d.Model(), d.PromptVersion())
	}
	if a := plain.(ruleeval.Auditor); a.AuditPromptVersion() != "test/audit" {
		t.Errorf("auditor not forwarded: %q", a.AuditPromptVersion())
	}
	if u := plain.(ruleeval.UsageReporter).Usage(); u.Requests != 0 {
		t.Errorf("usage = %+v", u)
	}

	bare := wrapRecording(bareProvider{}, rec)
	if d := bare.(ruleeval.Describer); d.Model() != "" || d.PromptVersion() != "" {
		t.Error("a provider without Describer reports empty model and prompt version")
	}
	if a := bare.(ruleeval.Auditor); a.AuditPromptVersion() != "" {
		t.Error("a provider that cannot audit reports an empty audit prompt version")
	}
	if _, err := bare.Evaluate(context.Background(), ruleeval.EvalRequest{Rule: entity.Rule{ID: "X-1"}}); err != nil {
		t.Fatal(err)
	}
	rec.put("X-2", nil) // ignored
	got := rec.report([]ruleeval.RuleInput{{Rule: entity.Rule{ID: "X-1"}}, {Rule: entity.Rule{ID: "X-2"}}})
	if got[0].Verdict != verifyUnknown || got[1].Verdict != verifyUnknown {
		t.Errorf("an unrecognised or missing verdict is unknown: %+v", got)
	}
}

// A rule reported more than once (one response per part) keeps the
// strongest verdict: a later compliant part never hides a violation.
func TestVerdictRecorderMerges(t *testing.T) {
	rec := newVerdictRecorder()
	loc := func(line int) []ruleeval.Location { return []ruleeval.Location{{File: "a.go", Line: line}} }
	rec.put("R-1", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant, Explanation: "part 1"})
	rec.put("R-1", &ruleeval.EvalResponse{Verdict: ruleeval.Violated, Evidence: loc(3), Confidence: 0.6})
	rec.put("R-1", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant, Explanation: "part 3"})
	rec.put("R-1", &ruleeval.EvalResponse{Verdict: ruleeval.Violated, Evidence: loc(9), Confidence: 0.9, Explanation: "part 4"})
	rec.put("R-2", &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable})
	rec.put("R-2", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant, Explanation: "touched"})
	rec.put("R-2", &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable})

	got := rec.report([]ruleeval.RuleInput{{Rule: entity.Rule{ID: "R-1"}}, {Rule: entity.Rule{ID: "R-2"}}})
	if r := got[0]; r.Verdict != verifyViolated || len(r.Evidence) != 2 || r.Confidence != 0.9 || r.Explanation != "part 4" {
		t.Errorf("R-1 = %+v, want violated with both locations, confidence 0.9, part 4's explanation", r)
	}
	if r := got[1]; r.Verdict != verifyCompliant || r.Explanation != "touched" {
		t.Errorf("R-2 = %+v, want compliant", r)
	}
}

// partialBatchProvider is a batch provider that judged only part of the
// tree: its response carries Errors next to the verdicts.
type partialBatchProvider struct{ batchStubProvider }

func (p *partialBatchProvider) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) { //nolint:gocritic // ruleeval.BatchProvider signature
	resp, err := p.batchStubProvider.EvaluateBatch(ctx, req)
	if resp != nil {
		resp.Errors = []error{errors.New("hunk in big.go exceeds the input budget and was not evaluated")}
	}
	return resp, err
}

func TestRuleVerifyPartialBatchIsUnknown(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, &partialBatchProvider{batchStubProvider{stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": violatedResponse("found anyway"),
		"TST-002": {Verdict: ruleeval.Compliant, Explanation: "fine where seen"},
	}}}}, nil)

	out, err := runVerifyCmd(t, "TST-001", "TST-002", "TST-003", "--project", dir, "--format", "json")
	assertExit(t, err, exitViolations)
	vo := decodeVerify(t, out)
	want := map[string]string{"TST-001": verifyViolated, "TST-002": verifyUnknown, "TST-003": verifyUnknown}
	if got := verdictsOf(&vo); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("verdicts = %v, want %v", got, want)
	}
	if e := vo.Rules[1].Explanation; !strings.Contains(e, "Partial verdict: compliant. fine where seen") {
		t.Errorf("TST-002 explanation = %q", e)
	}
}

// bareProvider implements only ruleeval.Provider, with a verdict the
// recorder does not recognise.
type bareProvider struct{}

func (bareProvider) Evaluate(context.Context, ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	return &ruleeval.EvalResponse{Verdict: "maybe"}, nil
}

func TestExcerptNeedle(t *testing.T) {
	tests := map[string]string{
		`if resp, err := http.Get("x"); err == nil {`: `if resp, err := http.Get("x"); err == nil {`,
		"  \n\tresp, err := http.Get(url)\n}":         "resp, err := http.Get(url)",
		" 42\tresp, err := http.Get(url)":             "resp, err := http.Get(url)",
		"x\ty := strings.Repeat(s, n)":                "x y := strings.Repeat(s, n)",
		"httpClient:   offlineHTTPClient(),":          "httpClient: offlineHTTPClient(),",
		"if resp, err := http.Get(...); err != nil":   "if resp, err := http.Get(",
		"client.Do(req)… // later":                    "client.Do(req)",
		"}":                                           "",
		"if err":                                      "",
		"return nil":                                  "return nil",
		"x...":                                        "",
		"":                                            "",
	}
	for in, want := range tests {
		if got := excerptNeedle(in); got != want {
			t.Errorf("excerptNeedle(%q) = %q, want %q", in, got, want)
		}
	}
}

// relocateFiles is a file set where the needle line appears twice.
var relocateFiles = []ruleeval.File{
	{Path: "a.go", Content: "package a\n\nfunc f() { http.Get(u) }\n\n++counter\nfunc g() { http.Get(u) }\n"},
	{Path: "b.go", Content: "package b\n\nvar client = x{\n\thttpClient:   offline(),\n}\n"},
}

func TestContentLines(t *testing.T) {
	if got := contentLines(relocateFiles[0].Content); len(got) != 6 || got[2] != "func f() { http.Get(u) }" {
		t.Errorf("a.go lines = %q", got)
	}
	if got := contentLines("x"); len(got) != 1 {
		t.Errorf("a file without a final newline still has its line: %q", got)
	}
	if got := contentLines(""); got != nil {
		t.Errorf("an empty file has no lines: %q", got)
	}
}

func TestRelocateEvidence(t *testing.T) {
	ev := func(file string, line int, excerpt string) verifyEvidence {
		return verifyEvidence{Location: ruleeval.Location{File: file, Line: line, Excerpt: excerpt}}
	}
	rules := []verifyRule{{Evidence: []verifyEvidence{
		ev("a.go", 99, "func g() { http.Get(u) }"), // corrected: only one match
		ev("./a.go", 2, "http.Get(u)"),             // two matches, neither at 2: kept, unconfirmed
		ev("a.go", 6, "http.Get(u)"),               // the reported line matches: confirmed
		ev("a.go", 4, "os.WriteFile(p)"),           // not in the file
		ev("../../etc/passwd", 1, "root:x:0:0"),    // not a file that was sent
		ev("a.go", 1, "}"),                         // too short to match
		ev("", 1, "package a hello"),               // no file
		ev("b.go", 4, "httpClient: offline(),"),    // alignment padding shrunk: confirmed
	}}}
	relocateEvidence(rules, relocateFiles)
	got := rules[0].Evidence
	type want struct {
		line, reported int
		confirmed      bool
	}
	wants := []want{{6, 99, true}, {2, 0, false}, {6, 0, true}, {4, 0, false}, {1, 0, false}, {1, 0, false}, {1, 0, false}, {4, 0, true}}
	for i, w := range wants {
		if got[i].Line != w.line || got[i].ReportedLine != w.reported || got[i].LineConfirmed != w.confirmed {
			t.Errorf("evidence %d = line %d reported %d confirmed %v, want %+v", i, got[i].Line, got[i].ReportedLine, got[i].LineConfirmed, w)
		}
	}
}

func TestRuleVerifyCorrectsEvidenceLine(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stub := &batchStubProvider{stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-001": {
			Verdict: ruleeval.Violated, Explanation: "main has no doc comment",
			Evidence: []ruleeval.Location{{File: "src/main.go", Line: 40, Excerpt: "func main() {}"}},
		},
		"TST-002": violatedResponse("not in the tree"), // excerpt "func Run() {}" is nowhere
	}}}
	stubEvalProvider(t, stub, nil)

	out, err := runVerifyCmd(t, "TST-001", "TST-002", "--project", dir, "--format", "json")
	assertExit(t, err, exitViolations)
	vo := decodeVerify(t, out)
	got := vo.Rules[0].Evidence[0]
	if got.Line != 3 || got.ReportedLine != 40 || !got.LineConfirmed {
		t.Errorf("TST-001 evidence = %+v, want line 3 (reported 40), confirmed", got)
	}
	if e := vo.Rules[1].Evidence[0]; e.LineConfirmed || e.Line != 3 {
		t.Errorf("TST-002 evidence = %+v, want the model's line, unconfirmed", e)
	}

	human, err := runVerifyCmd(t, "TST-001", "TST-002", "--project", dir)
	assertExit(t, err, exitViolations)
	if !strings.Contains(human, "src/main.go:3  main has no doc comment") {
		t.Errorf("corrected line missing:\n%s", human)
	}
	if !strings.Contains(human, "src/main.go:3 (line approximate)") {
		t.Errorf("unconfirmed line should be flagged:\n%s", human)
	}
}

func TestDiffSourceTreeString(t *testing.T) {
	if got := (diffSource{Kind: diffFromTree}).String(); got != "all tracked files" {
		t.Errorf("got %q", got)
	}
	if got := (diffSource{Kind: diffFromTree, Ref: treeScope([]string{"src"}, nil)}).String(); got != "tracked files under src" {
		t.Errorf("got %q", got)
	}
	if got := treeScope(nil, []string{"**/*_test.go", "docs"}); got != "all tracked files excluding **/*_test.go, docs" {
		t.Errorf("got %q", got)
	}
}

// verify judges every named rule whatever evaluate_on says. An entity with
// an inheritance chain contributes its effective rule set, which
// inherit.Effective filters by the states it is given: the entity's own
// SAVACCT-001 (Published) must survive evaluate_on: [I].
func TestRuleVerifyIgnoresEvaluateOnForInheritingEntity(t *testing.T) {
	dir, _ := setupInheritProject(t, "claude", "./rules/")
	writeInheritEntity(t, filepath.Join(dir, "rules"), "SAVACCT", "dev.lib:ACCT", inheritRule{id: "SAVACCT-001", state: "P"})
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules/\n    scope: global\n"+
		"evaluator:\n  provider: claude\ncache:\n  ttl: 24h\nevaluate_on: [I]\n")
	writeFile(t, dir, "src/main.go", "package main\n")
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	stubEvalProvider(t, &batchStubProvider{stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"SAVACCT-001": {Verdict: ruleeval.Compliant},
		"ACCT-001":    {Verdict: ruleeval.Compliant},
	}}}, nil)

	out, err := runVerifyCmd(t, "SAVACCT-001", "ACCT-001", "--project", dir, "--format", "json")
	assertExit(t, err, exitOK)
	vo := decodeVerify(t, out)
	if got := verdictsOf(&vo); got["SAVACCT-001"] != verifyCompliant || got["ACCT-001"] != verifyCompliant {
		t.Errorf("verdicts = %v, want SAVACCT-001 (own, P, outside evaluate_on) and ACCT-001 (inherited) compliant", got)
	}
}

func TestRuleVerifyExclude(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	writeFile(t, dir, "src/main_test.go", "package main\n")
	gitRun(t, dir, "add", "src/main_test.go")
	stubEvalProvider(t, nil, errors.New("dry run"))

	out, err := runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run", "--format", "json", "--exclude", "**/*_test.go", "--exclude", "docs")
	assertExit(t, err, exitOK)
	vo := decodeVerify(t, out)
	if vo.Estimate.Files != 1 || vo.Scope != "all tracked files excluding **/*_test.go, docs" {
		t.Errorf("files = %d, scope = %q; want only src/main.go", vo.Estimate.Files, vo.Scope)
	}

	_, err = runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run", "--exclude", "/abs")
	assertExit(t, err, exitConfig)

	// The scope line echoes the flags: a control sequence must not reach the terminal.
	human, err := runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run", "--exclude", "docs\n::error::x\x1b[2J")
	assertExit(t, err, exitOK)
	if strings.Contains(human, "\n::error::") || strings.Contains(human, "\x1b") {
		t.Errorf("scope line not sanitized:\n%q", human)
	}
}

// storeSpy is a batch provider that uses the store an audit gets for what
// it keeps per file: it records the request's detail level and whether a
// store came with it, and keeps a value in it.
type storeSpy struct {
	batchStubProvider
	detail ruleeval.Detail
	store  bool
	hit    bool
}

func (s *storeSpy) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) { //nolint:gocritic // ruleeval.BatchProvider signature
	s.detail, s.store, s.hit = req.Detail, req.Store != nil, false
	if req.Store != nil {
		_, s.hit, _ = req.Store.GetBlob("spy")
		if err := req.Store.PutBlobs(map[string][]byte{"spy": []byte("findings")}); err != nil {
			return nil, err
		}
	}
	return s.batchStubProvider.EvaluateBatch(ctx, req)
}

// The audit asks for compliant verdicts to be detailed, and keeps what the
// provider learns per file in .strspc/cache.db, but never serves a whole
// verdict from it: that would bypass the recorder.
func TestRuleVerifyCachesPerFileFindings(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	spy := &storeSpy{batchStubProvider: batchStubProvider{stubProvider: stubProvider{verdicts: map[string]*ruleeval.EvalResponse{
		"TST-002": {Verdict: ruleeval.Compliant, Explanation: "errors are wrapped", Confidence: 0.9},
	}}}}
	stubEvalProvider(t, spy, nil)

	out, err := runVerifyCmd(t, "TST-002", "--project", dir, "--format", "json")
	assertExit(t, err, exitOK)
	if spy.detail != ruleeval.DetailCompliant || !spy.store || spy.hit {
		t.Errorf("first run: detail %v, store %v, hit %v; want compliant detail and an empty store", spy.detail, spy.store, spy.hit)
	}
	if vo := decodeVerify(t, out); vo.Rules[0].Explanation != "errors are wrapped" || vo.Rules[0].Confidence != 0.9 {
		t.Errorf("compliant rule = %+v, want its explanation and confidence", vo.Rules[0])
	}
	if _, err := os.Stat(filepath.Join(dir, ".strspc", "cache.db")); err != nil {
		t.Errorf("cache.db: %v", err)
	}

	_, err = runVerifyCmd(t, "TST-002", "--project", dir)
	assertExit(t, err, exitOK)
	if !spy.hit || spy.callCount() != 2 {
		t.Errorf("second run: hit %v, calls %d; want the kept value and the verdict asked again", spy.hit, spy.callCount())
	}

	_, err = runVerifyCmd(t, "TST-002", "--project", dir, "--no-cache", "--verbose")
	assertExit(t, err, exitOK)
	if spy.store {
		t.Error("--no-cache must not hand the provider a store")
	}
}

func TestRuleVerifyProviderCannotAudit(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	stubEvalProvider(t, bareProvider{}, nil)

	_, err := runVerifyCmd(t, "TST-001", "--project", dir, "--no-cache")
	assertExit(t, err, exitConfig)
	if !strings.Contains(err.Error(), "cannot audit") {
		t.Errorf("err = %v", err)
	}
	// With the cache, the evaluator refuses a provider that cannot name its model.
	_, err = runVerifyCmd(t, "TST-001", "--project", dir)
	assertExit(t, err, exitConfig)
	if !strings.Contains(err.Error(), "initializing evaluator") {
		t.Errorf("err = %v", err)
	}
}

func TestRuleVerifyReportsSkippedFiles(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	writeFile(t, dir, "logo.png", "\x89PNG\x00")
	gitRun(t, dir, "add", "logo.png")
	stubEvalProvider(t, nil, errors.New("dry run"))

	out, err := runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run", "--format", "json")
	assertExit(t, err, exitOK)
	vo := decodeVerify(t, out)
	if len(vo.Skipped) != 1 || vo.Skipped[0] != (skippedFile{Path: "logo.png", Reason: skipBinary}) || vo.Estimate.Files != 2 {
		t.Errorf("skipped = %+v, files = %d", vo.Skipped, vo.Estimate.Files)
	}
	human, err := runVerifyCmd(t, "TST-001", "--project", dir, "--dry-run")
	assertExit(t, err, exitOK)
	if !strings.Contains(human, "Skipped: 1 tracked file(s) not read") {
		t.Errorf("human output missing the skipped count:\n%s", human)
	}
}
