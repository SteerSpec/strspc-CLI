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
  # Core rules from the SteerSpec specification
  - source: github://SteerSpec/strspc-rules@latest/rules/core
    scope: global

  # Local rules (uncomment to enable)
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

			appendGitignore(targetDir)

			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render("Initialized SteerSpec in .strspc/"))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Edit .strspc/config.yaml to configure rule sources"))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Run ")+cmdStyle.Render("strspc sync")+descStyle.Render(" to fetch rules"))
			writeln(w, cmdStyle.Render("  3.")+descStyle.Render(" Run ")+cmdStyle.Render("strspc check")+descStyle.Render(" to validate your code"))
			writeln(w)
			writeln(w, descStyle.Render("Docs: https://steerspec.dev/docs/getting-started"))

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing configuration")

	return cmd
}

// appendGitignore adds .strspc/cache.db to .gitignore if the file exists
// and doesn't already contain the entry.
func appendGitignore(dir string) {
	gitignorePath := filepath.Join(dir, ".gitignore")

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return // no .gitignore — nothing to do
	}

	entry := ".strspc/cache.db"
	if strings.Contains(string(data), entry) {
		return // already present
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	// Ensure we start on a new line.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, _ = f.WriteString("\n")
	}
	_, _ = f.WriteString(entry + "\n")
}
