package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// realmFileForWrite mirrors entity.RealmFile but without omitempty on
// dependencies, so the field is always present in serialized output.
type realmFileForWrite struct {
	Schema               string            `json:"$schema"`
	Realm                entity.RealmMeta  `json:"realm"`
	Dependencies         []entity.RealmDep `json:"dependencies"`
	RuleIdentifierFormat interface{}       `json:"rule_identifier_format"`
}

// loadAndWriteRealm is a helper that loads realm.json, applies a mutation
// to its dependencies, and writes it back.
func loadAndWriteRealm(realmPath string, mutate func([]entity.RealmDep) ([]entity.RealmDep, error)) error {
	rf, err := entity.LoadRealm(realmPath)
	if err != nil {
		return err
	}

	deps, err := mutate(rf.Dependencies)
	if err != nil {
		return err
	}

	if deps == nil {
		deps = []entity.RealmDep{}
	}

	out := realmFileForWrite{
		Schema:               rf.Schema,
		Realm:                rf.Realm,
		Dependencies:         deps,
		RuleIdentifierFormat: rf.RuleIdentifierFormat,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling realm.json: %w", err)
	}

	return os.WriteFile(realmPath, append(data, '\n'), 0o644)
}

func newRealmDepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "Manage Realm dependencies",
		Long:  "Add, remove, or list dependencies declared in realm.json.",
	}

	cmd.AddCommand(newRealmDepAddCmd())
	cmd.AddCommand(newRealmDepRemoveCmd())
	cmd.AddCommand(newRealmDepListCmd())

	return cmd
}

func newRealmDepListCmd() *cobra.Command {
	var (
		dir        string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List Realm dependencies",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			realmPath := filepath.Join(dir, "realm.json")
			rf, err := entity.LoadRealm(realmPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("not a valid Realm directory: %s (missing realm.json)", dir)
				}
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOutput {
				deps := rf.Dependencies
				if deps == nil {
					deps = []entity.RealmDep{}
				}
				data, marshalErr := json.MarshalIndent(deps, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("marshaling dependencies: %w", marshalErr)
				}
				_, writeErr := w.Write(append(data, '\n'))
				return writeErr
			}

			if len(rf.Dependencies) == 0 {
				writeln(w, descStyle.Render("No dependencies declared"))
				return nil
			}

			writeln(w, brandStyle.Render("Dependencies:"))
			for _, dep := range rf.Dependencies {
				line := cmdStyle.Render(fmt.Sprintf("  %s@%s", dep.RealmID, dep.Version))
				if dep.Source != "" {
					line += descStyle.Render(fmt.Sprintf("  (source: %s)", dep.Source))
				}
				writeln(w, line)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}
