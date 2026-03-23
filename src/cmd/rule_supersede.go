package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleSupersedeCmd() *cobra.Command {
	var (
		body    string
		addedBy string
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "supersede <rule_id>",
		Short: "Create a rule that supersedes an existing one",
		Long: `Create a new draft rule that supersedes an existing Published, Implemented, or Retired rule.
The original rule is not modified — it must be retired separately.

Examples:
  strspc rule supersede MYE-001 --body "Updated requirement MUST..." --added-by "@alice"
  strspc rule supersede MYE-001 --body "New rule" --added-by "@bob" --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]

			if body == "" {
				return fmt.Errorf("--body is required")
			}
			if addedBy == "" {
				return fmt.Errorf("--added-by is required")
			}

			entityID, err := entityIDFromRuleID(ruleID)
			if err != nil {
				return err
			}

			entityPath := resolveEntityPath(dir, entityID)

			var newRuleID string
			var oldVersion, version string

			err = loadModifySaveEntity(entityPath, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				var supersedeErr error
				newRuleID, supersedeErr = entityops.SupersedeRule(f, ruleID, body, addedBy)
				if supersedeErr != nil {
					return supersedeErr
				}
				version = f.RuleSet.Version
				return nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				return writeJSONOutput(w, map[string]any{
					"rule_id":     newRuleID,
					"state":       entityops.StateDraft,
					"revision":    0,
					"supersedes":  ruleID,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Created rule %s (Draft) — supersedes %s", newRuleID, ruleID)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "new rule statement text (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
