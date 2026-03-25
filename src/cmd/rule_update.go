package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleUpdateCmd() *cobra.Command {
	var (
		body    string
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "update <rule_id>",
		Short: "Update the body of a draft rule",
		Long: `Update the body text of a Draft rule. Only works on rules in state D (Draft).

Examples:
  strspc rule update MYE-001 --body "The system MUST validate all input"
  strspc rule update MYE-001 --body "Updated text" --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]

			if body == "" {
				return fmt.Errorf("--body is required")
			}

			entityID, err := entityIDFromRuleID(ruleID)
			if err != nil {
				return err
			}

			entityPath := resolveEntityPath(dir, entityID)

			var oldVersion, version string
			var revision int

			err = loadModifySaveEntity(entityPath, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				if updateErr := entityops.UpdateRuleBody(f, ruleID, body); updateErr != nil {
					return updateErr
				}
				version = f.RuleSet.Version
				if r := findRule(f, ruleID); r != nil {
					revision = r.Revision
				}
				return nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				return writeJSONOutput(w, map[string]any{
					"rule_id":     ruleID,
					"state":       entity.StateDraft,
					"revision":    revision,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Updated rule %s (Draft, revision %d)", ruleID, revision)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "new rule statement text (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
