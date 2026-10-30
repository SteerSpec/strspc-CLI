package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// ciWorkflowRef is the ref of SteerSpec/.github the generated workflows call.
// Callers outside the org pin a release tag rather than main.
const ciWorkflowRef = "v1"

// ciKinds lists the repository kinds `strspc ci init --kind` accepts.
var ciKinds = []string{"auto", "realm", "consumer"}

// realmWorkflowTemplate validates a Realm and its lifecycle transitions. The
// slots are the workflow ref and the realm path.
const realmWorkflowTemplate = `name: strspc lint

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  rules:
    uses: SteerSpec/.github/.github/workflows/strspc-lint.yml@%s
    permissions:
      contents: read
      pull-requests: read
    with:
      path: %s
      strict: true
`

// consumerWorkflowTemplate checks a change against the rules in
// .strspc/config.yaml. Without ANTHROPIC_API_KEY only structural checks run.
// The slot is the workflow ref.
const consumerWorkflowTemplate = `name: strspc check

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  check:
    uses: SteerSpec/.github/.github/workflows/strspc-check.yml@%s
    permissions:
      contents: read
      pull-requests: read
    secrets:
      # Optional: without it the job runs structural checks only.
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
`

// ciSafeValue matches the characters allowed in values interpolated into the
// generated YAML: paths, git refs and github:// sources. It excludes
// whitespace, quotes and YAML syntax, so a value cannot add keys or jobs.
var ciSafeValue = regexp.MustCompile(`^[A-Za-z0-9._/@:~+-]+$`)

// ciFile is one file `strspc ci init` writes, relative to the target directory.
type ciFile struct {
	path    string
	content string
}

func newCIInitCmd() *cobra.Command {
	var (
		kind        string
		rulesPath   string
		rulesSource string
		workflowRef string
		force       bool
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Add the SteerSpec rule workflows to a repository's CI",
		Long: `Write the GitHub Actions workflow that runs SteerSpec in CI.

A realm repository (one with rules/realm.json) gets a workflow that validates
the realm, lints its entities and checks lifecycle transitions on pull requests.
Any other repository gets a .strspc/config.yaml and a workflow that checks
changes against the rules it consumes; without an ANTHROPIC_API_KEY secret the
check runs structural checks only.

Existing files are never overwritten without --force.

Examples:
  strspc ci init
  strspc ci init --kind consumer --rules-source github://acme/specs@v1.0.0/rules
  strspc ci init --dry-run ./my-project`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetDir := "."
			if len(args) > 0 {
				targetDir = args[0]
			}

			kind = strings.ToLower(strings.TrimSpace(kind))
			if !slices.Contains(ciKinds, kind) {
				return fmt.Errorf("unknown kind %q: must be one of %s", kind, strings.Join(ciKinds, ", "))
			}
			if kind == "auto" {
				kind = detectCIKind(targetDir, rulesPath)
			}

			for name, v := range map[string]string{"--rules-path": rulesPath, "--rules-source": rulesSource, "--workflow-ref": workflowRef} {
				if !ciSafeValue.MatchString(v) {
					return fmt.Errorf("invalid %s %q: only letters, digits and ._/@:~+- are allowed", name, v)
				}
			}

			files := ciFiles(kind, rulesPath, rulesSource, workflowRef)

			for _, f := range files {
				if err := checkCIDest(targetDir, f.path, force); err != nil {
					return err
				}
			}

			w := cmd.OutOrStdout()
			var root *os.Root
			if !dryRun {
				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", targetDir, err)
				}
				var err error
				if root, err = os.OpenRoot(targetDir); err != nil {
					return fmt.Errorf("opening %s: %w", targetDir, err)
				}
				defer func() { _ = root.Close() }()
			}
			for _, f := range files {
				dest := filepath.Join(targetDir, f.path)
				if dryRun {
					writeln(w, brandStyle.Render("Would write "+dest))
					writeln(w, f.content)
					continue
				}
				// All filesystem access goes through root, which refuses any
				// path that escapes targetDir via a symlink, closing the gap
				// between the check above and the write.
				if err := root.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
					return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
				}
				if err := writeCIFile(root, f.path, f.content, force); err != nil {
					return fmt.Errorf("writing %s: %w", dest, err)
				}
				writeln(w, brandStyle.Render("Wrote "+dest))
			}

			if !dryRun && kind == "consumer" {
				writeln(w)
				writeln(w, descStyle.Render("Add an ANTHROPIC_API_KEY repository secret to enable AI evaluation."))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "auto", "repository kind: auto|realm|consumer (auto = realm if rules/realm.json exists)")
	cmd.Flags().StringVar(&rulesPath, "rules-path", "rules/", "realm directory (realm kind)")
	cmd.Flags().StringVar(&rulesSource, "rules-source", defaultCoreSource, "rule source written to .strspc/config.yaml (consumer kind)")
	cmd.Flags().StringVar(&workflowRef, "workflow-ref", ciWorkflowRef, "ref of SteerSpec/.github the workflow calls")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the files instead of writing them")

	return cmd
}

// detectCIKind returns "realm" when the repository holds a realm manifest
// under rulesPath, "consumer" otherwise.
func detectCIKind(dir, rulesPath string) string {
	if _, err := os.Stat(filepath.Join(dir, rulesPath, "realm.json")); err == nil {
		return "realm"
	}
	return "consumer"
}

// ciFiles returns the files to write for a repository kind.
func ciFiles(kind, rulesPath, rulesSource, workflowRef string) []ciFile {
	if kind == "realm" {
		return []ciFile{{
			path:    filepath.Join(".github", "workflows", "strspc-lint.yml"),
			content: fmt.Sprintf(realmWorkflowTemplate, workflowRef, rulesPath),
		}}
	}
	return []ciFile{
		{
			path:    filepath.Join(".strspc", "config.yaml"),
			content: fmt.Sprintf(defaultConfigTemplate, rulesSource, "claude"),
		},
		{
			path:    filepath.Join(".strspc", ".gitignore"),
			content: strspcGitignore,
		},
		{
			path:    filepath.Join(".github", "workflows", "strspc-check.yml"),
			content: fmt.Sprintf(consumerWorkflowTemplate, workflowRef),
		},
	}
}

func newCICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Manage SteerSpec CI integration",
		Long:  "Commands that wire SteerSpec rule checks into a repository's CI.",
	}
	cmd.AddCommand(newCIInitCmd())
	return cmd
}

// checkCIDest rejects a destination that is, or sits under, a symlink, and an
// existing file unless force is set. A cloned repository may carry symlinks
// that would redirect the write outside targetDir.
func checkCIDest(targetDir, rel string, force bool) error {
	cur := targetDir
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("accessing %s: %w", cur, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink — refusing to write through it", cur)
		}
	}
	if !force {
		return fmt.Errorf("%s already exists — use --force to overwrite", rel)
	}
	return nil
}

// writeCIFile creates path inside root exclusively unless force is set, in
// which case it truncates an existing file.
func writeCIFile(root *os.Root, path, content string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	f, err := root.OpenFile(path, flags, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
