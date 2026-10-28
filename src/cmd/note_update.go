package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// ruleIDFromNoteID returns the <rule_id> part of a <rule_id>/NN note ID.
func ruleIDFromNoteID(noteID string) (string, error) {
	ruleID, num, ok := strings.Cut(noteID, "/")
	if !ok || ruleID == "" || len(num) != 2 || num[0] < '0' || num[0] > '9' || num[1] < '0' || num[1] > '9' {
		return "", fmt.Errorf("invalid note ID %q: expected format ENTITY-NNN/NN", noteID)
	}
	return ruleID, nil
}

func newNoteUpdateCmd() *cobra.Command {
	var (
		noteType string
		body     string
		dir      string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "update <note_id>",
		Short: "Revise a note in place",
		Long: `Change the body and/or type of an existing note. The note's revision is
incremented once, the entity's rule_set version is patch-bumped and its timestamp
and hash refreshed. The original author and date are kept; the edit is visible in
the diff. Notes are never removed (NTE-009).

Notes of type changelog, supersedes, extends or related cannot change once added,
and no note can be retyped into one of them: add a new note instead (NTE-010).

Types: ` + strings.Join(entity.NoteTypes(), ", ") + `

Examples:
  strspc note update TST-001/01 --body "Superseded by TST-008 split rules."
  strspc note update TST-001/02 --type clarification --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			noteID := args[0]

			if body == "" && noteType == "" {
				return fmt.Errorf("at least one of --body or --type is required")
			}
			if noteType != "" && !entity.IsValidNoteType(noteType) {
				return fmt.Errorf("invalid note type %q: must be one of %s", noteType, strings.Join(entity.NoteTypes(), ", "))
			}

			ruleID, err := ruleIDFromNoteID(noteID)
			if err != nil {
				return err
			}
			entityID, err := entityIDFromRuleID(ruleID)
			if err != nil {
				return err
			}
			target, err := resolveEntityTarget(dir, entityID)
			if err != nil {
				return err
			}

			var (
				oldVersion, version string
				revision            int
			)
			err = modifyEntity(target, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				if updErr := entityops.UpdateNote(f, noteID, body, noteType); updErr != nil {
					return updErr
				}
				version = f.RuleSet.Version
				for _, n := range f.Notes {
					if n.ID == noteID {
						revision = n.Revision
					}
				}
				return nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				return writeJSONOutput(w, map[string]any{
					"note_id":     noteID,
					"rule_id":     ruleID,
					"revision":    revision,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Updated note %s (revision %d)", noteID, revision)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "new note content")
	cmd.Flags().StringVar(&noteType, "type", "", "new note type: "+strings.Join(entity.NoteTypes(), "|"))
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
