package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleRetireCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "retire <rule_id>",
		Short: "Retire or terminate a rule",
		Long: `Move a rule toward end-of-life: Implemented → Retired or Retired → Terminated.

Examples:
  strspc rule retire MYE-001
  strspc rule retire MYE-001 --json`,
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

			var version string
			var newState string

			err = loadModifySaveEntity(entityPath, func(f *entity.File) error {
				if retireErr := entityops.RetireRule(f, ruleID); retireErr != nil {
					return retireErr
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
					"rule_id": ruleID,
					"state":   newState,
					"version": version,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Retired rule %s → %s", ruleID, stateLabel(newState))))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s", version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
