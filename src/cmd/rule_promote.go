package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRulePromoteCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "promote <rule_id>",
		Short: "Advance a rule to the next state",
		Long: `Promote a rule through its lifecycle: Draft → Published or Published → Implemented.

Examples:
  strspc rule promote MYE-001
  strspc rule promote MYE-001 --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]

			entityID, err := entityIDFromRuleID(ruleID)
			if err != nil {
				return err
			}

			entityPath := resolveEntityPath(dir, entityID)

			var oldVersion, version string
			var newState string

			err = loadModifySaveEntity(entityPath, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				if promoteErr := entityops.PromoteRule(f, ruleID); promoteErr != nil {
					return promoteErr
				}
				version = f.RuleSet.Version
				if r := findRule(f, ruleID); r != nil {
					newState = r.State
				}
				return nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				return writeJSONOutput(w, map[string]string{
					"rule_id":     ruleID,
					"state":       newState,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Promoted rule %s → %s", ruleID, stateLabel(newState))))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
