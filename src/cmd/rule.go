package cmd

import (
	"github.com/spf13/cobra"
)

func newRuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage rules within entity files",
		Long:  "Commands for adding, updating, transitioning and showing rules in SteerSpec entity files.",
	}

	cmd.AddCommand(newRuleAddCmd())
	cmd.AddCommand(newRuleUpdateCmd())
	cmd.AddCommand(newRulePromoteCmd())
	cmd.AddCommand(newRuleRetireCmd())
	cmd.AddCommand(newRuleAbandonCmd())
	cmd.AddCommand(newRuleSupersedeCmd())
	cmd.AddCommand(newRuleShowCmd())
	cmd.AddCommand(newRuleHistoryCmd())

	return cmd
}
