package cmd

import (
	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleTerminateCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "terminate <rule_id>",
		Short: "Terminate a superseded Published rule or a Retired rule",
		Long: `Move a rule to Terminated: Published → Terminated or Retired → Terminated.

A Published rule's text can't change, so a wrong text is fixed by a rule that
supersedes it ("strspc rule supersede"). The old rule may be terminated in or
after the change in which a rule of the same entity that supersedes it enters
Published (RULST-012). It is refused while no superseding rule is Published or
later. An Implemented rule is retired first ("strspc rule retire").

Examples:
  strspc rule terminate SYNCOP-008
  strspc rule terminate SYNCOP-008 --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRuleTransition(cmd.OutOrStdout(), dir, args[0], jsonOut, entityops.TerminateRule)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
