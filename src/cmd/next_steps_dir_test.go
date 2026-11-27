package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
)

// chdirTemp makes a new temporary directory the working directory for the
// rest of the test.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// TestRealmInitNextStepsRunAsPrinted runs realm init with --dir ., then the
// commands its next steps print, which must carry --dir to find the Realm.
func TestRealmInitNextStepsRunAsPrinted(t *testing.T) {
	setupSchemaServer(t)
	setupRealmValidateTest(t)
	chdirTemp(t)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--id", "com.example.demo", "--title", "Demo realm", "--dir", ".")
	if err != nil {
		t.Fatalf("realm init: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, `strspc realm add MYENTITY --title "My Entity" --dir .`)
	testutil.AssertContains(t, out, "2. Validate: strspc realm validate --dir .")
	testutil.AssertContains(t, out, "3. Reference from config: source: ./ in .strspc/config.yaml")

	out, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "MYENTITY", "--title", "My Entity", "--dir", ".")
	if err != nil {
		t.Fatalf("realm add as printed: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, `strspc rule add MYENTITY --body "..." --added-by "github:<login>" --dir .`)
	testutil.AssertContains(t, out, "strspc realm validate --dir .")

	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", "--dir", "."); err != nil {
		t.Fatalf("realm validate as printed: %v\n%s", err, out)
	}
}

// TestRealmInitNextStepsDefaultDir checks the next steps for the default
// ./rules name no --dir and keep ./rules/ as the config source.
func TestRealmInitNextStepsDefaultDir(t *testing.T) {
	setupSchemaServer(t)
	chdirTemp(t)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--id", "com.example.demo")
	if err != nil {
		t.Fatalf("realm init: %v\n%s", err, out)
	}
	testutil.AssertNotContains(t, out, "--dir")
	testutil.AssertContains(t, out, `1. Add entities: strspc realm add MYENTITY --title "My Entity"`)
	testutil.AssertNotContains(t, out, "strspc rule add")
	testutil.AssertContains(t, out, "2. Validate: strspc realm validate\n")
	testutil.AssertContains(t, out, "3. Reference from config: source: ./rules/ in .strspc/config.yaml")
}

// TestRealmInitNextStepsQuoteDir checks a directory with a space is quoted in
// every command and named in the config source.
func TestRealmInitNextStepsQuoteDir(t *testing.T) {
	setupSchemaServer(t)
	chdirTemp(t)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--id", "com.example.demo", "--dir", "my rules",
		"--kind", "standard", "--edition", "2024", "--license", "CC-BY-4.0",
		"--attribution-text", "Example", "--attribution-url", "https://example.com", "--profiles", "L1")
	if err != nil {
		t.Fatalf("realm init: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, `strspc realm add MYENTITY --title "My Entity" --dir 'my rules'`)
	testutil.AssertContains(t, out, `--clause <clause> --profiles L1 --dir 'my rules'`)
	testutil.AssertContains(t, out, "3. Validate: strspc realm validate --dir 'my rules'")
	testutil.AssertContains(t, out, "source: ./my rules/ in .strspc/config.yaml")
}

// TestRealmInitNextStepsYAMLSource checks the config source line stays valid
// YAML for a directory holding "#", which would otherwise start a comment.
func TestRealmInitNextStepsYAMLSource(t *testing.T) {
	setupSchemaServer(t)
	chdirTemp(t)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "init", "--id", "com.example.demo", "--dir", "my #rules")
	if err != nil {
		t.Fatalf("realm init: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "source: './my #rules/' in .strspc/config.yaml")
}

func TestYAMLScalar(t *testing.T) {
	for _, in := range []string{"./rules/", "./my #rules/", "./a: b/", "'x/", `"x/`, "./my rules/"} {
		var got map[string]string
		if err := yaml.Unmarshal([]byte("source: "+yamlScalar(in)), &got); err != nil {
			t.Fatalf("yamlScalar(%q) = %s: %v", in, yamlScalar(in), err)
		}
		if got["source"] != in {
			t.Errorf("yamlScalar(%q) = %s, reads back as %q", in, yamlScalar(in), got["source"])
		}
	}
	if got := yamlScalar("./rules/"); got != "./rules/" {
		t.Errorf("yamlScalar(./rules/) = %s, want it plain", got)
	}
}

func TestRealmAddNextStepsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my rules")
	setupValidRealm(t, dir)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "MYENT", "--title", "My Entity", "--dir", dir)
	if err != nil {
		t.Fatalf("realm add: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, `--added-by "github:<login>" --dir `+shellQuote(filepath.ToSlash(dir)))
	testutil.AssertContains(t, out, "strspc realm validate --dir "+shellQuote(filepath.ToSlash(dir)))
}

func TestRealmAddNextStepsDefaultDir(t *testing.T) {
	setupValidRealm(t, filepath.Join(chdirTemp(t), "rules"))

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add", "MYENT", "--title", "My Entity")
	if err != nil {
		t.Fatalf("realm add: %v\n%s", err, out)
	}
	testutil.AssertNotContains(t, out, "--dir")
	testutil.AssertContains(t, out, "strspc realm validate\n")
}

func TestEntityAddNextStepsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	setupEntityFile(t, dir, "PARENT")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "entity", "add", "CHILD", "--parent", "PARENT", "--title", "Child", "--dir", dir)
	if err != nil {
		t.Fatalf("entity add: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, `strspc rule add CHILD --body "..." --added-by "github:<login>" --dir `+shellQuote(filepath.ToSlash(dir)))
	testutil.AssertContains(t, out, "strspc realm validate --dir "+shellQuote(filepath.ToSlash(dir)))
}

func TestRealmAddSubrealmNextStepsQuoteDir(t *testing.T) {
	parentDir := makeParentRealm(t, "com.test.parent", nil)
	childDir := filepath.Join(t.TempDir(), "child realm")

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "add-subrealm",
		"--id", "com.test.parent.child", "--dir", childDir, "--parent-dir", parentDir)
	if err != nil {
		t.Fatalf("add-subrealm: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, `strspc realm add MYENTITY --title "My Entity" --dir `+shellQuote(filepath.ToSlash(childDir)))
	testutil.AssertContains(t, out, "strspc realm validate --dir "+shellQuote(filepath.ToSlash(childDir)))
}

func TestRealmValidateDirFlag(t *testing.T) {
	setupRealmValidateTest(t)
	dir := t.TempDir()
	setupValidRealm(t, dir)

	out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", "--dir", dir)
	if err != nil {
		t.Fatalf("realm validate --dir: %v\n%s", err, out)
	}
	testutil.AssertContains(t, out, "No errors or warnings")

	// The same directory both ways is accepted.
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir+string(filepath.Separator), "--dir", dir); err != nil {
		t.Fatalf("realm validate path --dir same: %v\n%s", err, out)
	}

	// A relative path and an absolute --dir naming the same directory work.
	t.Chdir(dir)
	if out, err := testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", ".", "--dir", dir); err != nil {
		t.Fatalf("realm validate . --dir <abs>: %v\n%s", err, out)
	}

	_, err = testutil.ExecuteCommand(NewRootCmd(), "realm", "validate", dir, "--dir", t.TempDir())
	if err == nil {
		t.Fatal("realm validate with a path and a different --dir: want an error")
	}
	testutil.AssertContains(t, err.Error(), "name different directories")
}

func TestRealmDirArg(t *testing.T) {
	for in, want := range map[string]string{
		"./rules":  "",
		"rules":    "",
		"rules/":   "",
		".":        " --dir .",
		"my rules": " --dir 'my rules'",
		"a/b":      " --dir a/b",
		"it's":     ` --dir 'it'\''s'`,
	} {
		if got := realmDirArg(in); got != want {
			t.Errorf("realmDirArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigSourcePath(t *testing.T) {
	abs := t.TempDir()
	for in, want := range map[string]string{
		".":        "./",
		"./rules":  "./rules/",
		"a/b/":     "./a/b/",
		"..":       "../",
		"../x":     "../x/",
		abs:        filepath.ToSlash(abs) + "/",
		"my rules": "./my rules/",
	} {
		if got := configSourcePath(in); got != want {
			t.Errorf("configSourcePath(%q) = %q, want %q", in, got, want)
		}
	}
}
