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
// dependencies and rule_identifier_format, so both fields are always
// present in serialized output (matching realm init's output format). Every
// other field of the manifest is copied too, so a write keeps it: notes and
// hash stay omitted when the manifest has none (RLMMNFST-012, RLMMNFST-013).
type realmFileForWrite struct {
	Schema               string                       `json:"$schema"`
	Realm                entity.RealmMeta             `json:"realm"`
	Dependencies         []entity.RealmDep            `json:"dependencies"`
	SubRealms            []string                     `json:"sub_realms,omitempty"`
	RuleIdentifierFormat *entity.RuleIdentifierFormat `json:"rule_identifier_format"`
	Notes                []entity.RealmNote           `json:"notes,omitempty"`
	Hash                 string                       `json:"hash,omitempty"`
}

// checkRealmJSON verifies realm.json exists and is a regular file.
func checkRealmJSON(dir string) (string, error) {
	realmPath := filepath.Join(dir, "realm.json")
	info, err := os.Stat(realmPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("not a valid Realm directory: %s (missing realm.json)", dir)
		}
		return "", fmt.Errorf("accessing realm.json: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a valid Realm directory: %s (realm.json is not a regular file)", dir)
	}
	return realmPath, nil
}

// printableRealmID returns a realm ID from realm.json for output. An invalid ID
// is quoted because it can start a line, where GitHub would parse `::` as a
// workflow command.
func printableRealmID(id string) string {
	if !realmIDPattern.MatchString(id) {
		return fmt.Sprintf("%q", id)
	}
	return id
}

// loadAndWriteRealm is a helper that loads realm.json, applies a mutation
// to its dependencies, and writes it back.
func loadAndWriteRealm(realmPath string, mutate func([]entity.RealmDep) ([]entity.RealmDep, error)) error {
	return loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
		deps, err := mutate(rf.Dependencies)
		if err != nil {
			return err
		}
		rf.Dependencies = deps
		return nil
	})
}

// loadModifyWriteRealm loads realm.json, applies mutate to the manifest, and
// writes it back in the layout realm init uses. A manifest with a hash gets it
// recomputed over what is written (RLMMNFST-014); one without keeps none.
// Nothing is written when mutate fails.
func loadModifyWriteRealm(realmPath string, mutate func(*entity.RealmFile) error) error {
	rf, err := entity.LoadRealm(realmPath)
	if err != nil {
		return fmt.Errorf("loading realm.json: %w", err)
	}

	if err := mutate(rf); err != nil {
		return err
	}
	return replaceRealmManifest(realmPath, rf, rf.Hash != "")
}

// marshalRealmManifest returns rf as realm.json holds it, in the layout realm
// init uses. withHash sets rf.Hash, and the hash written, to the manifest hash
// of the result (RLMMNFST-013); otherwise the result has no hash.
// ComputeRealmHash leaves out the hash key, so hashing the bytes without it
// gives the hash of the bytes with it.
func marshalRealmManifest(rf *entity.RealmFile, withHash bool) ([]byte, error) {
	deps := rf.Dependencies
	if deps == nil {
		deps = []entity.RealmDep{}
	}

	out := realmFileForWrite{
		Schema:               rf.Schema,
		Realm:                rf.Realm,
		Dependencies:         deps,
		SubRealms:            rf.SubRealms,
		RuleIdentifierFormat: rf.RuleIdentifierFormat,
		Notes:                rf.Notes,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling realm.json: %w", err)
	}
	rf.Hash = ""
	if withHash {
		h, err := entity.ComputeRealmHash(data)
		if err != nil {
			return nil, fmt.Errorf("computing the manifest hash: %w", err)
		}
		out.Hash, rf.Hash = h, h
		if data, err = json.MarshalIndent(out, "", "  "); err != nil {
			return nil, fmt.Errorf("marshaling realm.json: %w", err)
		}
	}
	return append(data, '\n'), nil
}

// replaceRealmManifest writes rf to realmPath as marshalRealmManifest gives it,
// through replaceFile, so a link at realmPath is replaced, not written through.
func replaceRealmManifest(realmPath string, rf *entity.RealmFile, withHash bool) error {
	data, err := marshalRealmManifest(rf, withHash)
	if err != nil {
		return err
	}
	return replaceFile(realmPath, data, "realm.json")
}

func newRealmDepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "Manage Realm dependencies",
		Long:  "Add, remove, list, or upgrade dependencies declared in realm.json.",
	}

	cmd.AddCommand(newRealmDepAddCmd())
	cmd.AddCommand(newRealmDepRemoveCmd())
	cmd.AddCommand(newRealmDepListCmd())
	cmd.AddCommand(newRealmDepUpgradeCmd())

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
		Long:          "List the dependencies declared in realm.json with their versions and sources.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			realmPath, checkErr := checkRealmJSON(dir)
			if checkErr != nil {
				return checkErr
			}
			rf, err := entity.LoadRealm(realmPath)
			if err != nil {
				return fmt.Errorf("loading realm.json: %w", err)
			}

			w := cmd.OutOrStdout()

			if jsonOutput {
				deps := rf.Dependencies
				if deps == nil {
					deps = []entity.RealmDep{}
				}
				return writeJSONOutput(w, deps)
			}

			if len(rf.Dependencies) == 0 {
				writeln(w, descStyle.Render("No dependencies declared"))
				return nil
			}

			writeln(w, brandStyle.Render("Dependencies:"))
			for _, dep := range rf.Dependencies {
				// realm.json isn't validated here, and a cloned realm can hold
				// anything, so each field is neutralised before it reaches a
				// terminal.
				line := cmdStyle.Render(sanitizeLine(fmt.Sprintf("  %s@%s", printableRealmID(dep.RealmID), dep.Version)))
				if dep.Source != "" {
					line += descStyle.Render(sanitizeLine(fmt.Sprintf("  (source: %s)", dep.Source)))
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
