package cmd

import (
	"github.com/spf13/cobra"
)

func newRealmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "realm",
		Short: "Manage Realm directories for rule authoring",
		Long: "Commands for creating and managing SteerSpec Realm directories.\n\n" +
			"A command that rewrites a realm.json carrying a manifest hash recomputes it (RLMMNFST-014);\n" +
			"`realm rehash` adds or refreshes it after a hand edit.",
	}

	cmd.AddCommand(newRealmInitCmd())
	cmd.AddCommand(newRealmAddSubrealmCmd())
	cmd.AddCommand(newRealmAddCmd())
	cmd.AddCommand(newRealmDepCmd())
	cmd.AddCommand(newRealmValidateCmd())
	cmd.AddCommand(newRealmStandardsCmd())
	cmd.AddCommand(newRealmContributorCmd())
	cmd.AddCommand(newRealmRewriteAddedByCmd())
	cmd.AddCommand(newRealmRehashCmd())

	return cmd
}
