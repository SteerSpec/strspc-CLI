package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newNoteAddCmd() *cobra.Command {
	var (
		noteType string
		body     string
		addedBy  string
		dir      string
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "add <rule_id>",
		Short: "Attach a note to a rule",
		Long: `Append a note to a rule in its entity file at revision 0. The note ID is
<rule_id>/NN where NN is the next free number for that rule. The entity's rule_set version is patch-bumped
and its timestamp and hash refreshed.

Types: ` + strings.Join(entity.NoteTypes(), ", ") + `

Examples:
  strspc note add MYE-001 --type rationale --body "Required by GDPR art. 17" --added-by "@alice"
  strspc note add MYE-001 --type applies_to --body "src/cmd/*.go" --added-by "@bob" --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]

			if noteType == "" {
				return fmt.Errorf("--type is required (one of: %s)", strings.Join(entity.NoteTypes(), ", "))
			}
			if !entity.IsValidNoteType(noteType) {
				return fmt.Errorf("invalid note type %q: must be one of %s", noteType, strings.Join(entity.NoteTypes(), ", "))
			}
			if body == "" {
				return fmt.Errorf("--body is required")
			}
			if addedBy == "" {
				return fmt.Errorf("--added-by is required")
			}

			entityID, err := entityIDFromRuleID(ruleID)
			if err != nil {
				return err
			}

			target, err := resolveEntityTarget(dir, entityID)
			if err != nil {
				return err
			}

			var noteID, oldVersion, version string
			err = modifyEntity(target, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				var addErr error
				noteID, addErr = entityops.AddNote(f, ruleID, noteType, body, addedBy)
				if addErr != nil {
					return addErr
				}
				version = f.RuleSet.Version
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
					"type":        noteType,
					"revision":    0,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Added note %s (%s)", noteID, noteType)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&noteType, "type", "", "note type (required): "+strings.Join(entity.NoteTypes(), "|"))
	cmd.Flags().StringVar(&body, "body", "", "note content (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
