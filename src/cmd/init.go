package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// defaultCoreSource is the global rule source written by `strspc init`.
// Bumped on every strspc-rules release so fresh projects pin the current core realm.
const defaultCoreSource = "github://SteerSpec/strspc-rules@v1.12.0/rules/core"

// readmeURL is the only documentation link the CLI prints; steerspec.dev/docs
// pages do not exist yet (CFG-002).
const readmeURL = "https://github.com/SteerSpec/strspc-CLI#readme"

// initProviders lists the evaluator providers `strspc init --provider` accepts.
var initProviders = []string{"claude", "null"}

// defaultConfigTemplate is the config.yaml written by `strspc init`. It satisfies
// core rule SPCFG-004 (at least one rule source) out of the box. The two %s slots
// are the core source and the evaluator provider.
const defaultConfigTemplate = `# SteerSpec configuration
# Reference: ` + readmeURL + `

# Rule sources — where to fetch rules from
rules:
  # Core rules from the SteerSpec specification
  - source: %s
    scope: global

  # Local rules (uncomment after strspc realm init)
  # - source: ./rules/
  #   scope: local

# AI evaluator settings
evaluator:
  provider: %s          # requires ANTHROPIC_API_KEY; use "null" to skip AI evaluation
  # model: claude-sonnet-5-5   # model override
  # effort: medium             # low | medium | high

# Local cache settings
cache:
  ttl: 24h                # how long cached evaluations stay valid

# Which rule states cause a failure (state codes: D, A, P, I, R, T)
fail_on:
  - I                     # Implemented rules block merges when violated
  # - P                   # uncomment to also enforce Published rules

# Which rule states are sent to the AI evaluator (default: D, P, I)
# evaluate_on: [D, P, I]
`

// renderDefaultConfig returns the config.yaml content for the given provider.
func renderDefaultConfig(provider string) string {
	return fmt.Sprintf(defaultConfigTemplate, defaultCoreSource, provider)
}

// strspcGitignore keeps cache artifacts out of version control.
const strspcGitignore = "cache.db\ncache/\n"

func newInitCmd() *cobra.Command {
	var (
		force    bool
		provider string
	)

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize SteerSpec in a project",
		Long: `Bootstrap a .strspc/ directory with a default config.yaml for rule evaluation.

The generated config declares the SteerSpec core rules as a global source,
enables the Claude evaluator and fails on violated Implemented rules.

Examples:
  strspc init
  strspc init --provider null ./my-project   # offline: static checks only`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetDir := "."
			if len(args) > 0 {
				targetDir = args[0]
			}

			provider = strings.ToLower(strings.TrimSpace(provider))
			if !slices.Contains(initProviders, provider) {
				return fmt.Errorf("unknown provider %q: must be one of %s", provider, strings.Join(initProviders, ", "))
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

			if err := os.WriteFile(configPath, []byte(renderDefaultConfig(provider)), 0o644); err != nil {
				return fmt.Errorf("writing config.yaml: %w", err)
			}

			if err := os.WriteFile(filepath.Join(strspcDir, ".gitignore"), []byte(strspcGitignore), 0o644); err != nil {
				return fmt.Errorf("writing .strspc/.gitignore: %w", err)
			}

			if err := appendGitignore(targetDir); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update .gitignore: %v\n", err)
			}

			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Initialized SteerSpec in %s", strspcDir)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Review .strspc/config.yaml (rule sources, evaluator provider)"))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Add local rules: ")+cmdStyle.Render("strspc realm init"))
			writeln(w, cmdStyle.Render("  3.")+descStyle.Render(" Validate rules: ")+cmdStyle.Render("strspc lint rules/ --strict"))
			writeln(w, cmdStyle.Render("  4.")+descStyle.Render(" Run structural checks: ")+cmdStyle.Render("strspc check --static-only"))
			writeln(w)
			writeln(w, descStyle.Render("Reference: "+readmeURL))

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing configuration")
	cmd.Flags().StringVar(&provider, "provider", "claude", "evaluator provider to write: claude|null (null = static checks only)")

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
