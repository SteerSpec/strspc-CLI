package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func newRealmDepRemoveCmd() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:           "remove <realm_id>",
		Short:         "Remove a dependency from a Realm",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			realmID := args[0]

			if !realmIDPattern.MatchString(realmID) {
				return fmt.Errorf("invalid realm ID %q", realmID)
			}

			realmPath, checkErr := checkRealmJSON(dir)
			if checkErr != nil {
				return checkErr
			}

			err := loadAndWriteRealm(realmPath, func(deps []entity.RealmDep) ([]entity.RealmDep, error) {
				filtered := make([]entity.RealmDep, 0, len(deps))
				found := false
				for _, d := range deps {
					if d.RealmID == realmID {
						found = true
						continue
					}
					filtered = append(filtered, d)
				}
				if !found {
					return nil, fmt.Errorf("dependency %q not found", realmID)
				}
				return filtered, nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Removed dependency %s", realmID)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")

	return cmd
}
