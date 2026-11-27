package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRealmAddCmd() *cobra.Command {
	var (
		title       string
		description string
		inherits    string
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

			if err := entityops.ValidateEUID(euid); err != nil {
				return err
			}

			title = strings.TrimSpace(title)
			if title == "" {
				return fmt.Errorf("--title is required")
			}
			inheritsID, err := validateInherits(dir, euid, inherits)
			if err != nil {
				return err
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
					Inherits:    inheritsID,
				},
				RuleSet: entity.RuleSet{
					Version:   "0.1.0",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Hash:      nil,
				},
				Rules: []entity.Rule{},
				Notes: []entity.Note{},
			}

			data, marshalErr := marshalJSON(f)
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
			_, writeErr := out.Write(data)
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
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add rules: ")+cmdStyle.Render(fmt.Sprintf("strspc rule add %s --body \"...\" --added-by \"github:<login>\"%s", euid, realmDirArg(dir))))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Validate: ")+cmdStyle.Render("strspc realm validate"+realmDirArg(dir)))
			writeln(w)

			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "entity title (required)")
	cmd.Flags().StringVar(&description, "description", "", "entity description")
	cmd.Flags().StringVar(&inherits, "inherits", "", "entity whose rule set this entity inherits: EUID or <realm-id>:<EUID>")
	cmd.Flags().StringVar(&dir, "dir", defaultRealmDir, "realm directory")

	return cmd
}

// defaultRealmDir is the Realm directory a command uses without --dir.
const defaultRealmDir = "./rules"

// realmDirArg returns " --dir <dir>" for a suggested command, with dir quoted
// for a shell, so the command runs as printed; it returns "" when dir is the
// default the command uses without --dir. dir is printed with forward slashes,
// which strspc accepts on Windows too, so a separator never forces quoting.
func realmDirArg(dir string) string {
	clean := filepath.ToSlash(filepath.Clean(dir))
	if clean == "rules" {
		return ""
	}
	return " --dir " + shellQuote(clean)
}

// configSourcePath returns dir as a source path for .strspc/config.yaml, read
// from the project root: slash-separated, ending in "/", and starting with
// "./" when relative and inside the project (so ./rules gives "./rules/").
func configSourcePath(dir string) string {
	p := filepath.ToSlash(filepath.Clean(dir))
	switch {
	case p == ".":
		return "./"
	case filepath.IsAbs(dir), p == "..", strings.HasPrefix(p, "../"):
		return strings.TrimSuffix(p, "/") + "/"
	default:
		return "./" + p + "/"
	}
}

// yamlScalar returns s as a YAML scalar that reads back as s: plain when that
// is safe, quoted otherwise (a "#" or ": " in a path, for instance).
func yamlScalar(s string) string {
	out, _ := yaml.Marshal(s) // a string always marshals
	return strings.TrimSuffix(string(out), "\n")
}
