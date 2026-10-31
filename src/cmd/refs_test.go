package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// setupRefsProject writes the realm dev.app in rules/, with APP rules in every
// interesting state, depending on the realm dev.lib in lib/, and moves into it.
func setupRefsProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	writeRealmManifest(t, rules, "dev.app", `{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}`)
	writeInheritEntity(t, rules, "APP",
		"",
		inheritRule{id: "APP-001", state: "I"},
		inheritRule{id: "APP-002", state: "A"},
		inheritRule{id: "APP-003", state: "R"},
		inheritRule{id: "APP-004", state: "D"},
	)
	lib := filepath.Join(dir, "lib")
	writeRealmManifest(t, lib, "dev.lib")
	writeInheritEntity(t, lib, "ACCT", "", inheritRule{id: "ACCT-001", state: "P"}, inheritRule{id: "ACCT-002", state: "A"})
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules/\n    scope: local\nevaluator:\n  provider: null\n")
	t.Chdir(dir)
	return dir
}

func writeRefsFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runRefsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return testutil.ExecuteCommand(NewRootCmd(), append([]string{"refs"}, args...)...)
}

func TestRefsClean(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "README.md", "See APP-001 and ACCT-001.\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "No errors or warnings found")
}

func TestRefsFindings(t *testing.T) {
	tests := []struct {
		name string
		text string
		args []string
		code string
		exit int
	}{
		{"unknown", "APP-999", nil, "RF001", exitViolations},
		{"unknown prefix", "CLAMS-001", nil, "RF001", exitViolations},
		{"abandoned", "APP-002", nil, "RF002", exitViolations},
		{"abandoned in a dependency", "ACCT-002", nil, "RF002", exitViolations},
		{"qualified with a dependency", "dev.lib:ACCT-002", nil, "RF002", exitViolations},
		{"qualified with the realm itself", "dev.app:APP-002", nil, "RF002", exitViolations},
		{"retired is a warning", "APP-003", nil, "RF003", exitOK},
		{"retired under strict", "APP-003", []string{"--strict"}, "RF003", exitViolations},
		{"draft is a warning", "APP-004", nil, "RF006", exitOK},
		{"draft under strict", "APP-004", []string{"--strict"}, "RF006", exitViolations},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			writeRefsFile(t, dir, "docs/a.md", "line one\n"+tt.text+"\n")
			out, err := runRefsCmd(t, tt.args...)
			assertExit(t, err, tt.exit)
			testutil.AssertContains(t, out, tt.code)
			testutil.AssertContains(t, out, "docs/a.md:2")
		})
	}
}

func TestRefsSuppressing(t *testing.T) {
	tests := []struct {
		name string
		text string
		args []string
	}{
		{"ignore", "SHA-256", []string{"--ignore", "SHA-256"}},
		{"allow-state code", "APP-002", []string{"--allow-state", "A"}},
		{"allow-state name", "APP-002", []string{"--allow-state", "abandoned"}},
		{"allow-state list", "APP-002 APP-003", []string{"--allow-state", "A,R", "--strict"}},
		{"marker", "<!-- strspc-refs: ignore -->\nAPP-002", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			writeRefsFile(t, dir, "a.md", tt.text+"\n")
			_, err := runRefsCmd(t, tt.args...)
			assertExit(t, err, exitOK)
		})
	}
}

func TestRefsConfigurationErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"format", []string{"--format", "xml"}, "invalid --format"},
		{"state", []string{"--allow-state", "X"}, "--allow-state"},
		{"glob", []string{"--include", "["}, "invalid glob"},
		{"missing path", []string{"nowhere"}, "nowhere"},
		{"outside the project", []string{".."}, "outside the project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupRefsProject(t)
			_, err := runRefsCmd(t, tt.args...)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), tt.want)
		})
	}
}

func TestRefsNoConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := runRefsCmd(t)
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), "strspc init")
}

// A dependency that doesn't resolve is a broken setup. Without its rules every
// citation of them would look unknown.
func TestRefsUnresolvedDependencyIsConfigurationError(t *testing.T) {
	dir := setupRefsProject(t)
	if err := os.RemoveAll(filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	writeRefsFile(t, dir, "a.md", "ACCT-001\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitConfig)
	if strings.Contains(out, "RF001") {
		t.Errorf("reported citations: %s", out)
	}
}

// A non-fatal resolution warning reaches stderr, and stays out of the JSON.
func TestRefsReportsResolutionNotices(t *testing.T) {
	dir := setupRefsProject(t)
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawCheckConfig(t, dir, "rules:\n  - source: ./rules/\n    scope: local\n  - source: ./empty/\n    scope: local\n")

	var out, errOut bytes.Buffer
	err := runRefs(context.Background(), &out, &errOut, dir, nil, &refsOptions{format: formatHuman, jsonOutput: true})
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, errOut.String(), "RSV006")
	if !json.Valid(out.Bytes()) {
		t.Errorf("stdout is not JSON: %s", out.String())
	}
}

// A fatal resolution error names its cause on stderr, on printable lines only.
func TestRefsReportsResolutionFailure(t *testing.T) {
	dir := setupRefsProject(t)
	if err := os.RemoveAll(filepath.Join(dir, "lib")); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err := runRefs(context.Background(), &out, &errOut, dir, nil, &refsOptions{format: formatHuman, jsonOutput: true})
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, errOut.String(), "dev.lib")
	if out.Len() != 0 {
		t.Errorf("stdout: %s", out.String())
	}
}

func TestRefsSubRealm(t *testing.T) {
	dir := setupRefsProject(t)
	sub := filepath.Join(dir, "rules", "sub")
	writeRealmManifest(t, sub, "dev.app.sub")
	writeInheritEntity(t, sub, "SUBE", "", inheritRule{id: "SUBE-001", state: "A"})
	writeRefsFile(t, dir, "rules/realm.json",
		`{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0"},
		  "dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}],
		  "sub_realms": ["sub"]}`)
	writeRefsFile(t, dir, "a.md", "dev.app.sub:SUBE-001\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RF002")
}

// writeRefsSubRealm adds the sub-realm dev.app.sub, holding SUBE-001 in state A,
// to the project of setupRefsProject. deps are the sub-realm's own dependencies.
func writeRefsSubRealm(t *testing.T, dir string, deps ...string) {
	t.Helper()
	sub := filepath.Join(dir, "rules", "sub")
	writeRealmManifest(t, sub, "dev.app.sub", deps...)
	writeInheritEntity(t, sub, "SUBE", "", inheritRule{id: "SUBE-001", state: "A"})
	writeRefsFile(t, dir, "rules/realm.json",
		`{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0"},
		  "dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}],
		  "sub_realms": ["sub"]}`)
}

func TestRefsSubRealmQualifiers(t *testing.T) {
	tests := []struct {
		name string
		text string
		code string
	}{
		{"unqualified", "SUBE-001", "RF002"},
		{"its own realm", "dev.app.sub:SUBE-001", "RF002"},
		{"the parent does not hold it", "dev.app:SUBE-001", "RF001"},
		{"the sub-realm does not hold the parent's rule", "dev.app.sub:APP-002", "RF001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			writeRefsSubRealm(t, dir)
			writeRefsFile(t, dir, "a.md", tt.text+"\n")
			out, err := runRefsCmd(t)
			assertExit(t, err, exitViolations)
			testutil.AssertContains(t, out, tt.code)
		})
	}
}

// A project's own pin decides what a citation means, not a sub-realm's.
func TestRefsProjectPinWinsOverSubRealmPin(t *testing.T) {
	dir := setupRefsProject(t)
	other := filepath.Join(dir, "lib2")
	writeRefsFile(t, dir, "lib2/realm.json", `{"realm": {"id": "dev.lib", "title": "t", "version": "2.0.0"}}`)
	writeInheritEntity(t, other, "ACCT", "", inheritRule{id: "ACCT-001", state: "A"}, inheritRule{id: "ACCT-002", state: "A"})
	writeRefsSubRealm(t, dir, `{"realm_id": "dev.lib", "version": "2.0.0", "source": "../../lib2/"}`)
	writeRefsFile(t, dir, "a.md", "ACCT-001\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitOK)
	if strings.Contains(out, "RF") {
		t.Errorf("judged against the sub-realm's pin: %s", out)
	}
}

func TestRefsSubRealmOfDependency(t *testing.T) {
	dir := setupRefsProject(t)
	inner := filepath.Join(dir, "lib", "inner")
	writeRealmManifest(t, inner, "dev.lib.inner")
	writeInheritEntity(t, inner, "INNR", "", inheritRule{id: "INNR-001", state: "I"})
	writeRefsFile(t, dir, "lib/realm.json",
		`{"realm": {"id": "dev.lib", "title": "t", "version": "1.0.0"}, "sub_realms": ["inner"]}`)
	writeRefsFile(t, dir, "a.md", "INNR-001\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitOK)
	testutil.AssertContains(t, out, "No errors or warnings found")
}

// A path that leaves the project through a link is outside it.
func TestRefsRejectsPathsThroughLinksOutOfTheProject(t *testing.T) {
	outside := t.TempDir()
	writeRefsFile(t, outside, "secret.md", "APP-001\n")
	tests := []struct {
		name string
		arg  string
	}{
		{"file below a link", filepath.Join("out", "secret.md")},
		{"link to a directory", "out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
				t.Skip(err)
			}
			_, err := runRefsCmd(t, tt.arg)
			assertExit(t, err, exitConfig)
			testutil.AssertContains(t, err.Error(), "outside the project")
		})
	}
}

func TestRefsAcceptsLinksInsideTheProject(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "docs/a.md", "APP-002\n")
	if err := os.Symlink(filepath.Join(dir, "docs"), filepath.Join(dir, "guide")); err != nil {
		t.Skip(err)
	}
	_, err := runRefsCmd(t, filepath.Join("guide", "a.md"))
	assertExit(t, err, exitViolations)
}

func TestRefsRenderedForm(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "rules/realm.json",
		`{"realm": {"id": "dev.app", "title": "t", "version": "1.0.0"},
		  "dependencies": [{"realm_id": "dev.lib", "version": "1.0.0", "source": "../lib/"}],
		  "rule_identifier_format": {"id_splitter": "_"}}`)
	writeRefsFile(t, dir, "a.md", "[APP_002.0/A] and MAX_100\n")
	out, err := runRefsCmd(t)
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "RF002")
}

func TestRefsJSON(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "a.md", "APP-002\n")
	out, err := runRefsCmd(t, "--json")
	assertExit(t, err, exitViolations)
	var diags []struct{ Code, Path string }
	if err := json.Unmarshal([]byte(out), &diags); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(diags) != 1 || diags[0].Code != "RF002" || diags[0].Path != "a.md" {
		t.Errorf("got %+v", diags)
	}
}

func TestRefsGitHubFormat(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "a.md", "\nAPP-002\nAPP-003\n")
	out, err := runRefsCmd(t, "--format", "github")
	assertExit(t, err, exitViolations)
	testutil.AssertContains(t, out, "::error file=a.md,line=2,title=rule-refs RF002::")
	testutil.AssertContains(t, out, "::warning file=a.md,line=3,title=rule-refs RF003::")
}

func TestRefsJSONAndFormatConflict(t *testing.T) {
	setupRefsProject(t)
	if _, err := runRefsCmd(t, "--json", "--format", "github"); err == nil {
		t.Fatal("want an error")
	}
}

func TestRefsFileSelection(t *testing.T) {
	bad := "APP-002\n"
	layout := map[string]string{
		"a.md":                bad,
		"docs/b.md":           bad,
		".claude/agents/c.md": bad,
		"CHANGELOG.md":        bad,
		"docs/CHANGELOG.md":   bad,
		".strspc/cache/d.md":  bad,
		"e.yml":               bad,
		"docs/old/f.md":       bad,
		"bin.md":              "APP-002\x00\n",
	}
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"defaults", nil, []string{".claude/agents/c.md", "a.md", "docs/b.md", "docs/old/f.md"}},
		{"directory", []string{"docs"}, []string{"docs/b.md", "docs/old/f.md"}},
		{"include", []string{"--include", "*.yml"}, []string{".claude/agents/c.md", "a.md", "docs/b.md", "docs/old/f.md", "e.yml"}},
		{"exclude a tree", []string{"--exclude", "docs/old/**"}, []string{".claude/agents/c.md", "a.md", "docs/b.md"}},
		{"exclude a path", []string{"--exclude", "docs/*.md"}, []string{".claude/agents/c.md", "a.md", "docs/old/f.md"}},
		{"a named file skips the includes", []string{"e.yml"}, []string{"e.yml"}},
		{"a named file meets the excludes", []string{"e.yml", "--exclude", "e.yml"}, nil},
		{"named and listed once", []string{"a.md", "."}, []string{".claude/agents/c.md", "a.md", "docs/b.md", "docs/old/f.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupRefsProject(t)
			for name, body := range layout {
				writeRefsFile(t, dir, name, body)
			}
			for _, inGit := range []bool{false, true} {
				if inGit {
					initRefsGit(t, dir)
				}
				got, err := refsFiles(dir, refsArgs(tt.args), refsFlags(tt.args))
				if err != nil {
					t.Fatal(err)
				}
				got = slices.DeleteFunc(got, func(f string) bool { return f == "bin.md" })
				if !slices.Equal(got, tt.want) {
					t.Errorf("git=%v: got %v, want %v", inGit, got, tt.want)
				}
			}
		})
	}
}

// refsArgs and refsFlags split the arguments of a TestRefsFileSelection case.
func refsArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func refsFlags(args []string) *refsOptions {
	o := &refsOptions{}
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--include":
			o.include = append(o.include, args[i+1])
		case "--exclude":
			o.exclude = append(o.exclude, args[i+1])
		}
	}
	return o
}

func initRefsGit(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func TestRefsHonoursGitignore(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "a.md", "APP-002\n")
	writeRefsFile(t, dir, "out/b.md", "APP-002\n")
	writeRefsFile(t, dir, ".gitignore", "out/\n")
	initRefsGit(t, dir)
	got, err := refsFiles(dir, nil, &refsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.md"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRefsSkipsLinksAndDeletedFiles(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "a.md", "APP-001\n")
	writeRefsFile(t, dir, "gone.md", "APP-002\n")
	initRefsGit(t, dir)
	if out, err := exec.Command("git", "-C", dir, "add", "gone.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "link.md")); err != nil {
		t.Skip(err)
	}
	got, err := refsFiles(dir, nil, &refsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.md"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRefsSkipsBinaryFiles(t *testing.T) {
	dir := setupRefsProject(t)
	writeRefsFile(t, dir, "a.md", "\x00 APP-002\n")
	_, err := runRefsCmd(t)
	assertExit(t, err, exitOK)
}

func TestPrintableText(t *testing.T) {
	tests := []struct{ path, want string }{
		{"docs/a.md", "docs/a.md"},
		{"docs/é.md", "docs/é.md"},
		{"a\x1b[31m.md", `a\x1b[31m.md`},
		{"a\n.md", `a\n.md`},
	}
	for _, tt := range tests {
		if got := printableText(tt.path); got != tt.want {
			t.Errorf("printableText(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestWriteRefsNoticesStayOnOneLine(t *testing.T) {
	var out bytes.Buffer
	writeRefsNotices(&out, []result.Diagnostic{{
		Code: "RR001", Severity: result.Warning, Message: "bad\n::error::forged \x1b[31mred",
	}})
	got := strings.TrimSuffix(out.String(), "\n")
	if strings.ContainsAny(got, "\n\x1b") {
		t.Errorf("control character in output: %q", got)
	}
}

func TestRefsPathWithControlCharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names can't hold control characters")
	}
	dir := setupRefsProject(t)
	name := "a\x1b[31m.md"
	writeRefsFile(t, dir, name, "APP-002\n")
	out, err := runRefsCmd(t, "--format", "github")
	assertExit(t, err, exitViolations)
	if strings.Contains(out, "\x1b") {
		t.Errorf("control character in output: %q", out)
	}
}

func TestRefsGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		file    string
		want    bool
	}{
		{"*.md", "a.md", true},
		{"*.md", "x/y/a.md", true},
		{"*.md", "a.txt", false},
		{"docs/*.md", "docs/a.md", true},
		{"docs/*.md", "docs/x/a.md", false},
		{"docs/**", "docs/x/a.md", true},
		{"docs/**", "docs2/a.md", false},
		{".strspc/**", ".strspc/cache/a.md", true},
		{"CHANGELOG.md", "x/CHANGELOG.md", true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" "+tt.file, func(t *testing.T) {
			if got := refsGlobMatch([]string{tt.pattern}, tt.file); got != tt.want {
				t.Errorf("got %v", got)
			}
		})
	}
}
