package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleAddCmd() *cobra.Command {
	var (
		body    string
		addedBy string
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "add <entity_id>",
		Short: "Add a new draft rule to an entity",
		Long: `Add a new draft rule to an entity file within a Realm directory.

Examples:
  strspc rule add MYE --body "The system MUST validate input" --added-by "@alice"
  strspc rule add MYE --body "Users SHOULD be notified" --added-by "@bob" --dir ./specs --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			entityID := args[0]

			if body == "" {
				return fmt.Errorf("--body is required")
			}
			if addedBy == "" {
				return fmt.Errorf("--added-by is required")
			}

			entityPath := resolveEntityPath(dir, entityID)

			var newRuleID string
			var oldVersion, version string

			err := loadModifySaveEntity(entityPath, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				var addErr error
				newRuleID, addErr = entityops.AddRule(f, body, addedBy)
				if addErr != nil {
					return addErr
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
					"state":       entity.StateDraft,
					"revision":    0,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Added rule %s (Draft, revision 0)", newRuleID)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "rule statement text (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
