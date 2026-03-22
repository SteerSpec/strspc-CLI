package cmd

import (
	"github.com/spf13/cobra"
)

func newRealmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "realm",
		Short: "Manage Realm directories for rule authoring",
		Long:  "Commands for creating and managing SteerSpec Realm directories.",
	}

	cmd.AddCommand(newRealmInitCmd())
	cmd.AddCommand(newRealmValidateCmd())

	return cmd
}
