package cmd

import (
	"github.com/spf13/cobra"
)

func newEntityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity",
		Short: "Maintain entity files",
		Long:  "Commands for maintaining SteerSpec entity files as a whole, such as recomputing rule_set metadata.",
	}

	cmd.AddCommand(newEntityRehashCmd())

	return cmd
}
