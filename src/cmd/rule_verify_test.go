package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
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

func TestCollectTreeDiff(t *testing.T) {
	dir := setupVerifyRepo(t, "claude")
	ctx := context.Background()

	all, err := collectTreeDiff(ctx, dir, ".", []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, all, "docs/notes.md", "src/main.go")
	if !strings.Contains(all, "+func main() {}") {
		t.Errorf("tree diff should present every line as added:\n%s", all)
	}

	// Uncommitted edits to tracked files are read.
	writeFile(t, dir, "src/main.go", "package main\n\nfunc main() { run() }\n")
	only, err := collectTreeDiff(ctx, dir, ".", []string{":(glob)src/**"})
	if err != nil {
		t.Fatal(err)
	}
	assertFiles(t, only, "src/main.go")
	if !strings.Contains(only, "run()") {
		t.Errorf("working tree edit missing:\n%s", only)
	}
}

func TestCollectTreeDiffNotARepo(t *testing.T) {
	if _, err := collectTreeDiff(context.Background(), t.TempDir(), ".", []string{"."}); err == nil {
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
	if est := estimateVerify("", got); est.RuleGroups != 1 {
		t.Errorf("rule groups = %d, want 1", est.RuleGroups)
	}
}

func TestEstimateVerify(t *testing.T) {
	diff := "diff --git a/src/a.go b/src/a.go\n--- /dev/null\n+++ b/src/a.go\n@@ -0,0 +1 @@\n+package a\n" +
		"diff --git a/README.md b/README.md\n--- /dev/null\n+++ b/README.md\n@@ -0,0 +1 @@\n+# R\n"
	inputs := make([]ruleeval.RuleInput, 0, 45)
	for i := 0; i < 41; i++ {
		inputs = append(inputs, ruleeval.RuleInput{EntityID: "BIG"})
	}
	inputs = append(inputs, ruleeval.RuleInput{EntityID: "SMALL"})

	est := estimateVerify(diff, inputs)
	if est.Files != 2 || est.Bytes != len(diff) {
		t.Errorf("files/bytes = %d/%d", est.Files, est.Bytes)
	}
	if est.BytesByDir["src"] == 0 || est.BytesByDir["."] == 0 {
		t.Errorf("bytes by dir = %v", est.BytesByDir)
	}
	if est.EstChunks != 1 || est.RuleGroups != 3 || est.EstRequests != 3 {
		t.Errorf("chunks/groups/requests = %d/%d/%d, want 1/3/3", est.EstChunks, est.RuleGroups, est.EstRequests)
	}

	big := strings.Repeat("x", 3*200000) // ~200k tokens: several parts
	if got := estimateVerify(big, nil); got.EstChunks < 4 {
		t.Errorf("chunks for ~200k tokens = %d", got.EstChunks)
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
	if seen := stub.seenDiff(); strings.Contains(seen, "untracked.go") || !strings.Contains(seen, "+func main() {}") {
		t.Errorf("provider saw:\n%s", seen)
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
	if seen := stub.seenDiff(); !strings.Contains(seen, "b/svc/main.go") {
		t.Errorf("diff paths should be repository-relative:\n%s", seen)
	}
}

// sourceSpy records the Source of every batched input.
type sourceSpy struct {
	*batchStubProvider
	sources *[]string
}

func (s *sourceSpy) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
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
	if !vo.DryRun || !vo.OK || vo.Provider != "" || vo.Estimate.Files != 2 || vo.Estimate.EstRequests != 1 || len(vo.Rules) != 0 {
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
		{"no files", []string{"TST-001", "--path", "nothing/**"}, "no tracked files"},
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
	rec := &verdictRecorder{verdicts: map[string]*ruleeval.EvalResponse{}}
	plain := wrapRecording(&stubProvider{}, rec)
	if _, ok := plain.(ruleeval.BatchProvider); ok {
		t.Error("a non-batch provider must not gain EvaluateBatch")
	}
	if d := plain.(ruleeval.Describer); d.Model() != "stub-model" || d.PromptVersion() != "test/v1" {
		t.Errorf("describer not forwarded: %q %q", d.Model(), d.PromptVersion())
	}
	if u := plain.(ruleeval.UsageReporter).Usage(); u.Requests != 0 {
		t.Errorf("usage = %+v", u)
	}

	bare := wrapRecording(bareProvider{}, rec)
	if d := bare.(ruleeval.Describer); d.Model() != "" || d.PromptVersion() != "" {
		t.Error("a provider without Describer reports empty model and prompt version")
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

// bareProvider implements only ruleeval.Provider, with a verdict the
// recorder does not recognise.
type bareProvider struct{}

func (bareProvider) Evaluate(context.Context, ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) { //nolint:gocritic // ruleeval.Provider signature
	return &ruleeval.EvalResponse{Verdict: "maybe"}, nil
}

func TestExcerptNeedle(t *testing.T) {
	tests := map[string]string{
		`if resp, err := http.Get("x"); err == nil {`: `if resp, err := http.Get("x"); err == nil {`,
		"  \n\t+resp, err := http.Get(url)\n}":        "resp, err := http.Get(url)",
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

// relocateDiff is an all-added diff where the needle line appears twice.
const relocateDiff = "diff --git a/a.go b/a.go\nnew file mode 100644\n--- /dev/null\n+++ b/a.go\n@@ -0,0 +1,6 @@\n" +
	"+package a\n+\n+func f() { http.Get(u) }\n+\n+++counter\n+func g() { http.Get(u) }\n" +
	"diff --git a/b.go b/b.go\n--- /dev/null\n+++ b/b.go\n@@ -0,0 +1 @@\n+package b\n"

func TestTreeFileLines(t *testing.T) {
	files := treeFileLines(relocateDiff)
	if got := files["a.go"]; len(got) != 6 || got[2] != "func f() { http.Get(u) }" || got[4] != "++counter" {
		t.Errorf("a.go lines = %q", got)
	}
	if got := files["b.go"]; len(got) != 1 {
		t.Errorf("b.go lines = %q", got)
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
		ev("../../etc/passwd", 1, "root:x:0:0"),    // not a file of the diff
		ev("a.go", 1, "}"),                         // too short to match
		ev("", 1, "package a hello"),               // no file
	}}}
	relocateEvidence(rules, relocateDiff)
	got := rules[0].Evidence
	type want struct {
		line, reported int
		confirmed      bool
	}
	wants := []want{{6, 99, true}, {2, 0, false}, {6, 0, true}, {4, 0, false}, {1, 0, false}, {1, 0, false}, {1, 0, false}}
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
}
