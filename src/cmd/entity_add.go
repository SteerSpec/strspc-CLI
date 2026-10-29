package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newEntityAddCmd() *cobra.Command {
	var (
		parent      string
		title       string
		description string
		inherits    string
		dir         string
		jsonOut     bool
	)

	cmd := &cobra.Command{
		Use:   "add <EUID>",
		Short: "Add a sub-entity to an existing entity",
		Long: `Add a new sub-entity under a parent entity within a Realm directory.

--parent may be a top-level entity or a sub-entity declared in any entity file
under --dir. The new EUID must be unique across the Realm (RM006). Version and
hash metadata of the parent and its ancestors are refreshed.

Examples:
  strspc entity add GMAILOAUTH --parent CONNECTOR --title "Gmail OAuth"
  strspc entity add TOKENS --parent GMAILOAUTH --title "Token storage" --dir ./specs --json
  strspc entity add SAVACCT --parent PRODUCTS --title "Savings" --inherits dev.globalbank.core:ACCT`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			euid := args[0]

			if !euidPattern.MatchString(euid) {
				return fmt.Errorf("invalid EUID %q: must be 3-18 alphanumeric characters", euid)
			}
			parent = strings.TrimSpace(parent)
			if parent == "" {
				return fmt.Errorf("--parent is required")
			}
			if !euidPattern.MatchString(parent) {
				return fmt.Errorf("invalid --parent %q: must be 3-18 alphanumeric characters", parent)
			}
			title = strings.TrimSpace(title)
			if title == "" {
				return fmt.Errorf("--title is required")
			}
			target, err := resolveEntityTarget(dir, parent)
			if err != nil {
				return err
			}
			inheritsID, err := validateInherits(filepath.Dir(target.path), euid, inherits)
			if err != nil {
				return err
			}
			if err := ensureEUIDUnused(dir, euid); err != nil {
				return err
			}

			child, err := entityops.NewFile(euid, title, strings.TrimSpace(description), parent)
			if err != nil {
				return err
			}
			if inheritsID != "" {
				child.Entity.Inherits = inheritsID
				if err := entityops.UpdateMeta(child); err != nil {
					return err
				}
			}

			var oldVersion, version string
			err = modifyEntity(target, func(p *entity.File) error {
				oldVersion = p.RuleSet.Version
				p.SubEntities = append(p.SubEntities, *child)
				v, bumpErr := entityops.BumpPatch(p.RuleSet.Version)
				if bumpErr != nil {
					return fmt.Errorf("bumping version of %s: %w", p.Entity.ID, bumpErr)
				}
				p.RuleSet.Version = v
				version = v
				return entityops.UpdateMeta(p)
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				out := map[string]any{
					"entity_id":      euid,
					"parent":         parent,
					"file":           target.path,
					"parent_version": version,
					"old_version":    oldVersion,
				}
				if inheritsID != "" {
					out["inherits"] = inheritsID
				}
				return writeJSONOutput(w, out)
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Added sub-entity %q (%s) under %q in %s", euid, title, parent, target.path)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Parent version: %s → %s", oldVersion, version)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add rules: ")+cmdStyle.Render(fmt.Sprintf("strspc rule add %s --body \"...\" --added-by \"@handle\"", euid)))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Validate: ")+cmdStyle.Render("strspc realm validate"))
			writeln(w)

			return nil
		},
	}

	cmd.Flags().StringVar(&parent, "parent", "", "parent entity EUID (required)")
	cmd.Flags().StringVar(&title, "title", "", "sub-entity title (required)")
	cmd.Flags().StringVar(&description, "description", "", "sub-entity description")
	cmd.Flags().StringVar(&inherits, "inherits", "", "entity whose rule set this sub-entity inherits: EUID or <realm-id>:<EUID>")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}

// ensureEUIDUnused fails if euid names any entity or sub-entity under dir (RM006).
func ensureEUIDUnused(dir, euid string) error {
	var found string
	err := entity.WalkEntityFiles(dir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
		// An unreadable file may hold the EUID, so uniqueness can't be proven.
		if parseErr != nil {
			return fmt.Errorf("cannot verify EUID uniqueness: %s: %w", path, parseErr)
		}
		if ef == nil || found != "" {
			return nil
		}
		if ef.Entity.ID == euid || findSubEntity(ef, euid) != nil {
			found = path
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scanning %s: %w", dir, err)
	}
	if found != "" {
		return fmt.Errorf("EUID %q already exists in %s", euid, found)
	}
	return nil
}
