package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const defaultConfig = `# SteerSpec configuration
# Docs: https://steerspec.dev/docs/config

# Rule sources — where to fetch rules from
rules:
  # Core rules from the SteerSpec specification (strspc sync not yet available)
  # - source: github://SteerSpec/strspc-rules@latest/rules/core
  #   scope: global

  # Local rules (uncomment after strspc realm init)
  # - source: ./rules/
  #   scope: local

# AI evaluator settings
# Requires an LLM provider for behavioral rule evaluation.
# Without this, only structural checks (schema validation) run.
evaluator:
  provider: null          # claude, openai, ollama, or null (structural checks only)
  # endpoint: null        # custom endpoint (defaults per provider)
  # model: null           # model override

# Local cache settings
cache:
  ttl: 24h                # how long cached evaluations stay valid

# Which rule states cause a failure
# Rules in these states will block merges if violated
fail_on:
  - implemented           # I-state rules are enforced
  # - published           # uncomment to also enforce P-state rules
`

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize SteerSpec in a project",
		Long:  "Bootstrap a .strspc/ directory with a default config.yaml for rule evaluation.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetDir := "."
			if len(args) > 0 {
				targetDir = args[0]
			}

			strspcDir := filepath.Join(targetDir, ".strspc")
			configPath := filepath.Join(strspcDir, "config.yaml")

			if !force {
				if _, err := os.Stat(strspcDir); err == nil {
					return fmt.Errorf(".strspc/ already exists in %s — use --force to overwrite", targetDir)
				}
			}

			if err := os.MkdirAll(strspcDir, 0o755); err != nil {
				return fmt.Errorf("creating .strspc/: %w", err)
			}

			if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
				return fmt.Errorf("writing config.yaml: %w", err)
			}

			if err := appendGitignore(targetDir); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update .gitignore: %v\n", err)
			}

			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Initialized SteerSpec in %s", filepath.Join(targetDir, ".strspc"))))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Edit .strspc/config.yaml to configure rule sources"))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Add local rules: ")+cmdStyle.Render("strspc realm init"))
			writeln(w, cmdStyle.Render("  3.")+descStyle.Render(" Validate your code: ")+cmdStyle.Render("strspc check")+descStyle.Render(" (coming soon)"))
			writeln(w)
			writeln(w, descStyle.Render("Docs: https://steerspec.dev/docs/getting-started"))

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing configuration")

	return cmd
}

// appendGitignore adds .strspc/cache.db to .gitignore if the file exists
// and doesn't already contain the entry. Returns nil if .gitignore doesn't
// exist (nothing to do) or if the entry was added successfully.
func appendGitignore(dir string) error {
	gitignorePath := filepath.Join(dir, ".gitignore")

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return nil // no .gitignore — nothing to do
	}

	entry := ".strspc/cache.db"
	if strings.Contains(string(data), entry) {
		return nil // already present
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening .gitignore: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Ensure we start on a new line.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return fmt.Errorf("writing to .gitignore: %w", err)
		}
	}
	if _, err := f.WriteString(entry + "\n"); err != nil {
		return fmt.Errorf("writing to .gitignore: %w", err)
	}
	return nil
}
