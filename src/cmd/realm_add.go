package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

			title = strings.TrimSpace(title)
			if title == "" {
				return fmt.Errorf("--title is required")
			}

			// Check realm.json exists and is accessible.
			realmPath := filepath.Join(dir, "realm.json")
			info, err := os.Stat(realmPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("not a valid Realm directory: %s (missing realm.json)", dir)
				}
				return fmt.Errorf("accessing realm.json: %w", err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("not a valid Realm directory: %s (realm.json is not a regular file)", dir)
			}

			f := entity.File{
				Schema: "./_schema/entity.v1.schema.json",
				Entity: entity.Entity{
					ID:          euid,
					Title:       title,
					Description: strings.TrimSpace(description),
				},
				RuleSet: entity.RuleSet{
					Version:   "0.1.0",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Hash:      nil,
				},
				Rules: []entity.Rule{},
				Notes: []entity.Note{},
			}

			data, marshalErr := json.MarshalIndent(f, "", "  ")
			if marshalErr != nil {
				return fmt.Errorf("marshaling entity: %w", marshalErr)
			}

			// Use O_CREATE|O_EXCL for atomic create — fails if file already exists.
			entityPath := filepath.Join(dir, euid+".json")
			out, createErr := os.OpenFile(entityPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if createErr != nil {
				if os.IsExist(createErr) {
					return fmt.Errorf("entity %q already exists: %s", euid, entityPath)
				}
				return fmt.Errorf("creating %s: %w", entityPath, createErr)
			}
			_, writeErr := out.Write(append(data, '\n'))
			closeErr := out.Close()
			if writeErr != nil {
				return fmt.Errorf("writing %s: %w", entityPath, writeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("closing %s: %w", entityPath, closeErr)
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
