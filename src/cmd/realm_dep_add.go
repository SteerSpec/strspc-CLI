package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func newRealmDepAddCmd() *cobra.Command {
	var (
		dir    string
		source string
	)

	cmd := &cobra.Command{
		Use:           "add <realm_id>@<version>",
		Short:         "Add a dependency to a Realm",
		Long:          "Add a dependency to realm.json. Format: <realm_id>@<version>",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dep, err := parseDependency(args[0])
			if err != nil {
				return err
			}

			if source != "" {
				dep.Source = source
			}

			realmPath := filepath.Join(dir, "realm.json")
			if _, statErr := os.Stat(realmPath); statErr != nil {
				if os.IsNotExist(statErr) {
					return fmt.Errorf("not a valid Realm directory: %s (missing realm.json)", dir)
				}
				return fmt.Errorf("accessing realm.json: %w", statErr)
			}

			err = loadAndWriteRealm(realmPath, func(deps []entity.RealmDep) ([]entity.RealmDep, error) {
				for _, d := range deps {
					if d.RealmID == dep.RealmID {
						return nil, fmt.Errorf("dependency %q already exists (version %s)", dep.RealmID, d.Version)
					}
				}
				return append(deps, dep), nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			msg := fmt.Sprintf("Added dependency %s@%s", dep.RealmID, dep.Version)
			if dep.Source != "" {
				msg += fmt.Sprintf(" (source: %s)", dep.Source)
			}
			writeln(w, brandStyle.Render(msg))

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&source, "source", "", "dependency source URI")

	return cmd
}
