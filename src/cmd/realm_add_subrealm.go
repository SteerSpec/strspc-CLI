package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/spf13/cobra"
)

func newRealmAddSubrealmCmd() *cobra.Command {
	var (
		realmID       string
		title         string
		dir           string
		parentDir     string
		force         bool
		noInheritDeps bool
	)

	cmd := &cobra.Command{
		Use:           "add-subrealm",
		Short:         "Scaffold a sub-realm inside an existing Realm",
		Long:          "Create a sub-realm directory with realm.json and schemas copied from the parent Realm.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate parent has realm.json.
			parentRealmPath, err := checkRealmJSON(parentDir)
			if err != nil {
				return fmt.Errorf("parent realm: %w", err)
			}

			// Validate sub-realm ID.
			if !realmIDPattern.MatchString(realmID) {
				return fmt.Errorf("invalid realm ID %q: must be lowercase alphanumeric with dots and hyphens (e.g. com.acme.myproject.sub)", realmID)
			}

			// Check target doesn't already have realm.json.
			targetRealmPath := filepath.Join(dir, "realm.json")
			if !force {
				if _, err := os.Stat(targetRealmPath); err == nil {
					return fmt.Errorf("realm already exists in %s — use --force to overwrite", dir)
				}
			}

			// Load parent to get dependencies.
			parentRealm, err := entity.LoadRealm(parentRealmPath)
			if err != nil {
				return fmt.Errorf("loading parent realm.json: %w", err)
			}

			// Determine dependencies.
			var deps []entity.RealmDep
			if !noInheritDeps {
				deps = parentRealm.Dependencies
			}
			if deps == nil {
				deps = []entity.RealmDep{}
			}

			// Create target directory.
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dir, err)
			}

			// Set up _schema/: copy from parent if available, otherwise fetch.
			schemaDir := filepath.Join(dir, "_schema")
			if err := os.MkdirAll(schemaDir, 0o755); err != nil {
				return fmt.Errorf("creating _schema/: %w", err)
			}

			parentSchemaDir := filepath.Join(parentDir, "_schema")
			if info, err := os.Stat(parentSchemaDir); err == nil && info.IsDir() {
				if err := copySchemas(parentSchemaDir, schemaDir); err != nil {
					return err
				}
			} else {
				if err := fetchSchemas(schemaDir); err != nil {
					return err
				}
			}

			// Write realm.json.
			realm := realmJSON{
				Schema: "./_schema/realm.v1.schema.json",
				Realm: realmMeta{
					ID:      realmID,
					Title:   title,
					Version: "0.1.0",
				},
				Dependencies:      deps,
				RuleIdentifierFmt: nil,
			}

			data, err := json.MarshalIndent(realm, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling realm.json: %w", err)
			}

			if err := os.WriteFile(targetRealmPath, append(data, '\n'), 0o644); err != nil {
				return fmt.Errorf("writing realm.json: %w", err)
			}

			// Print success.
			cleanDir := filepath.Clean(dir)
			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Initialized sub-realm %q in %s", realmID, cleanDir)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add entities: ")+cmdStyle.Render(fmt.Sprintf("strspc realm add MYENTITY --title \"My Entity\" --dir %s", cleanDir)))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Validate: ")+cmdStyle.Render(fmt.Sprintf("strspc realm validate --dir %s", cleanDir)))
			writeln(w)

			return nil
		},
	}

	cmd.Flags().StringVar(&realmID, "id", "", "sub-realm ID (reverse domain notation, e.g. com.acme.myproject.sub)")
	cmd.Flags().StringVar(&title, "title", "", "sub-realm title")
	cmd.Flags().StringVar(&dir, "dir", "", "target directory for the sub-realm")
	cmd.Flags().StringVar(&parentDir, "parent-dir", ".", "parent realm directory (must contain realm.json)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing sub-realm")
	cmd.Flags().BoolVar(&noInheritDeps, "no-inherit-deps", false, "do not inherit parent realm dependencies")

	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("dir")

	return cmd
}

// copySchemas copies all schema files from srcDir to dstDir.
func copySchemas(srcDir, dstDir string) error {
	for _, sf := range schemaFiles {
		src := filepath.Join(srcDir, sf.local)
		dst := filepath.Join(dstDir, sf.local)

		data, err := os.ReadFile(src)
		if err != nil {
			// Fall back to fetch if a file is missing from parent.
			if os.IsNotExist(err) {
				if fetchErr := fetchSchema(dstDir, sf.remote, sf.local); fetchErr != nil {
					return fetchErr
				}
				continue
			}
			return fmt.Errorf("reading schema %s: %w", sf.local, err)
		}

		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("writing schema %s: %w", sf.local, err)
		}
	}
	return nil
}
