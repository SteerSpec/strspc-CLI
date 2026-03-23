package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleAbandonCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "abandon <rule_id>",
		Short: "Abandon a draft rule",
		Long: `Abandon a draft rule that is no longer needed. Only works on rules in state D (Draft).

Examples:
  strspc rule abandon MYE-002
  strspc rule abandon MYE-002 --json`,
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
				if abandonErr := entityops.AbandonRule(f, ruleID); abandonErr != nil {
					return abandonErr
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

			writeln(w, brandStyle.Render(fmt.Sprintf("Abandoned rule %s → %s", ruleID, stateLabel(newState))))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
