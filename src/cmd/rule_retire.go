package cmd

import (
	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleRetireCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "retire <rule_id>",
		Short: "Retire a rule (I→R; R→T kept for compatibility)",
		Long: `Move an Implemented rule to Retired.

A Retired rule given to retire moves on to Terminated, as before; use
"strspc rule terminate" for that instead. A Published rule can't be retired:
once a rule that supersedes it is Published, terminate it (RULST-012).

Examples:
  strspc rule retire MYE-001
  strspc rule retire MYE-001 --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRuleTransition(cmd.OutOrStdout(), dir, args[0], jsonOut, entityops.RetireRule)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
