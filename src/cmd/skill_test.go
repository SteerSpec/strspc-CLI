package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

func TestSkillPrintsEmbeddedFile(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != strspcSkill {
		t.Errorf("output differs from the embedded skill")
	}
	if !strings.HasPrefix(output, "---\nname: strspc\ndescription: ") {
		t.Errorf("skill must open with name and description frontmatter, got:\n%.80s", output)
	}
	if !strings.Contains(output, "\n---\n") {
		t.Errorf("skill frontmatter is not closed")
	}
}

func TestSkillInstallWritesFile(t *testing.T) {
	dir := t.TempDir()
	output, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--install", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	path := filepath.Join(dir, ".claude", "skills", "strspc", "SKILL.md")
	testutil.AssertContains(t, output, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	if string(data) != strspcSkill {
		t.Errorf("installed file differs from the embedded skill")
	}
}

func TestSkillInstallDefaultsToCurrentDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--install"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(skillInstallPath); err != nil {
		t.Fatalf("skill not written to the current directory: %v", err)
	}
}

func TestSkillInstallRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, skillInstallPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--install", dir)
	if err == nil {
		t.Fatal("expected an error for an existing skill file")
	}
	testutil.AssertContains(t, err.Error(), "--force")
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Errorf("existing file was modified: %q", data)
	}
}

func TestSkillInstallForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, skillInstallPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--install", "--force", dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != strspcSkill {
		t.Errorf("file was not overwritten")
	}
}

func TestSkillInstallOnlyFlagsWithoutInstall(t *testing.T) {
	for _, args := range [][]string{{"skill", "somewhere"}, {"skill", "--force"}} {
		_, err := testutil.ExecuteCommand(NewRootCmd(), args...)
		if err == nil {
			t.Fatalf("%v: expected an error", args)
		}
		testutil.AssertContains(t, err.Error(), "--install")
	}
}

func TestSkillInstallWriteErrors(t *testing.T) {
	t.Run("directory cannot be created", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".claude"), []byte("a file"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--install", dir)
		if err == nil {
			t.Fatal("expected an error")
		}
		testutil.AssertContains(t, err.Error(), "creating")
	})

	t.Run("file cannot be written", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where the file belongs: --force skips the existence check
		// and the write itself fails.
		if err := os.MkdirAll(filepath.Join(dir, skillInstallPath), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--install", "--force", dir)
		if err == nil {
			t.Fatal("expected an error")
		}
		testutil.AssertContains(t, err.Error(), "writing")
	})
}

func TestSkillHelp(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "skill", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "--install")
	testutil.AssertContains(t, output, "Examples:")
}

func TestRootHelpPointsToSkill(t *testing.T) {
	output, err := testutil.ExecuteCommand(NewRootCmd(), "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.AssertContains(t, output, "strspc skill --install")
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// TestSkillCommandsExist guards the skill against drift: every `strspc …`
// command it shows must resolve in the command tree.
func TestSkillCommandsExist(t *testing.T) {
	invocation := regexp.MustCompile("strspc ([^`\n]+)")
	inline := regexp.MustCompile("`([^`\n]+)`")

	var snippets []string
	inFence := false
	for _, line := range strings.Split(strspcSkill, "\n") {
		switch {
		case strings.HasPrefix(line, "```"):
			inFence = !inFence
		case inFence:
			snippets = append(snippets, line)
		default:
			for _, m := range inline.FindAllStringSubmatch(line, -1) {
				snippets = append(snippets, m[1])
			}
		}
	}

	root := NewRootCmd()
	checked := 0
	for _, snippet := range snippets {
		m := invocation.FindStringSubmatch(snippet)
		if m == nil {
			continue
		}
		words := strings.Fields(m[1])
		if strings.HasPrefix(words[0], "<") {
			continue // a placeholder such as `strspc <command> --help`
		}
		found, rest, err := root.Find(words)
		if err != nil || found == root {
			t.Errorf("%q: not a strspc command", snippet)
			continue
		}
		checked++
		// A group such as `rule` runs nothing itself: a word after it must be a subcommand.
		if found.HasSubCommands() && found.Run == nil && found.RunE == nil &&
			len(rest) > 0 && unicode.IsLetter(firstRune(rest[0])) {
			t.Errorf("%q: %q is not a subcommand of %q", snippet, rest[0], found.CommandPath())
		}
	}
	if checked < 10 {
		t.Errorf("only %d commands checked; the skill should show the workflow", checked)
	}
}
