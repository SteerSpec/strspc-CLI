package cmd

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// strspcSkill is the agent skill that teaches the strspc workflow. It is
// embedded so that it always describes the commands of the binary that ships it.
//
//go:embed skills/strspc/SKILL.md
var strspcSkill string

// skillInstallPath is where --install writes the skill, relative to the target directory.
var skillInstallPath = filepath.Join(".claude", "skills", "strspc", "SKILL.md")

func newSkillCmd() *cobra.Command {
	var (
		install bool
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "skill [dir]",
		Short: "Print or install the agent skill for using strspc",
		Long: `Print the strspc agent skill: a SKILL.md that teaches an AI agent the strspc
workflow (set up, author rules, validate, check). It ships inside the binary, so
it always matches the commands of this version.

With --install the skill is written to .claude/skills/strspc/SKILL.md under dir
(the current directory by default) instead of printed. An existing file is
kept unless --force is given.

Examples:
  strspc skill                          # print to stdout
  strspc skill --install                # write .claude/skills/strspc/SKILL.md here
  strspc skill --install ./my-project --force`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !install {
				if len(args) > 0 || force {
					return errors.New("dir and --force only apply with --install")
				}
				_, err := fmt.Fprint(cmd.OutOrStdout(), strspcSkill)
				return err
			}

			targetDir := "."
			if len(args) > 0 {
				targetDir = args[0]
			}
			path := filepath.Join(targetDir, skillInstallPath)

			// A cloned repository may carry symlinks that would redirect the
			// write outside targetDir, so refuse them before touching anything.
			if err := checkCIDest(targetDir, skillInstallPath, force); err != nil {
				return err
			}
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", targetDir, err)
			}
			root, err := os.OpenRoot(targetDir)
			if err != nil {
				return fmt.Errorf("opening %s: %w", targetDir, err)
			}
			defer func() { _ = root.Close() }()

			// All writes go through root, which refuses a path that escapes
			// targetDir, and without --force the file is created exclusively,
			// so a concurrent creator is never truncated.
			if err := root.MkdirAll(filepath.Dir(skillInstallPath), 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
			}
			if err := writeCIFile(root, skillInstallPath, strspcSkill, force); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}

			writeln(cmd.OutOrStdout(), brandStyle.Render("Installed the strspc skill in "+path))
			return nil
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "write the skill to .claude/skills/strspc/SKILL.md instead of printing it")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing skill file (with --install)")

	return cmd
}
