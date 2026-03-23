package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

var euidPattern = regexp.MustCompile(`^[a-zA-Z0-9]{3,18}$`)

func newRealmAddCmd() *cobra.Command {
	var (
		title       string
		description string
		dir         string
	)

	cmd := &cobra.Command{
		Use:           "add <EUID>",
		Short:         "Add a new entity to a Realm",
		Long:          "Scaffold a new entity JSON file within an existing Realm directory.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			euid := args[0]

			if !euidPattern.MatchString(euid) {
				return fmt.Errorf("invalid EUID %q: must be 3-18 alphanumeric characters", euid)
			}

			if title == "" {
				return fmt.Errorf("--title is required")
			}

			// Check realm.json exists.
			realmPath := filepath.Join(dir, "realm.json")
			if _, err := os.Stat(realmPath); err != nil {
				return fmt.Errorf("not a valid Realm directory: %s (missing realm.json)", dir)
			}

			// Check entity file doesn't already exist.
			entityPath := filepath.Join(dir, euid+".json")
			if _, err := os.Stat(entityPath); err == nil {
				return fmt.Errorf("entity %q already exists: %s", euid, entityPath)
			}

			f := entity.File{
				Schema: "./_schema/entity.v1.schema.json",
				Entity: entity.Entity{
					ID:          euid,
					Title:       title,
					Description: description,
				},
				RuleSet: entity.RuleSet{
					Version:   "0.1.0",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Hash:      nil,
				},
				Rules: []entity.Rule{},
				Notes: []entity.Note{},
			}

			data, err := json.MarshalIndent(f, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling entity: %w", err)
			}

			if err := os.WriteFile(entityPath, append(data, '\n'), 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", entityPath, err)
			}

			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Created entity %q (%s) in %s", euid, title, entityPath)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add rules: ")+cmdStyle.Render(fmt.Sprintf("strspc rule add %s --body \"...\" --added-by \"@handle\"", euid)))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Validate: ")+cmdStyle.Render("strspc realm validate"))
			writeln(w)

			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "entity title (required)")
	cmd.Flags().StringVar(&description, "description", "", "entity description")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")

	return cmd
}
