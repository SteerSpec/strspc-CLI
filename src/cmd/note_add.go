package cmd

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// noteTypes is the closed enum of note types from entity.v1.schema.json
// (core rules NTE-*). strspc-manager does not export it, so it is mirrored here.
var noteTypes = []string{
	"rationale",
	"example",
	"counter_example",
	"reference",
	"applies_to",
	"changelog",
	"clarification",
	"deprecation_notice",
	"supersedes",
	"extends",
	"related",
}

// maxNoteNumber is the maximum note number supported by the 2-digit ID format.
const maxNoteNumber = 99

// addNote appends a note to ruleID in f at the rule's current revision, bumps
// the patch version and refreshes rule_set metadata. entityops has no note primitive, so the JSON manipulation
// lives here; the hash/timestamp update is delegated to entityops.UpdateMeta.
func addNote(f *entity.File, ruleID, noteType, content, addedBy string) (string, error) {
	rule := findRule(f, ruleID)
	if rule == nil {
		return "", fmt.Errorf("rule %q not found in entity %q", ruleID, f.Entity.ID)
	}

	next := 1
	for _, n := range f.Notes {
		if n.RuleRef != ruleID {
			continue
		}
		if idx := strings.LastIndex(n.ID, "/"); idx >= 0 {
			if num, err := strconv.Atoi(n.ID[idx+1:]); err == nil && num >= next {
				next = num + 1
			}
		}
	}
	if next > maxNoteNumber {
		return "", fmt.Errorf("cannot add note: maximum number of notes (%d) exceeded for rule %q", maxNoteNumber, ruleID)
	}

	noteID := fmt.Sprintf("%s/%02d", ruleID, next)
	f.Notes = append(f.Notes, entity.Note{
		ID:       noteID,
		RuleRef:  ruleID,
		Type:     noteType,
		Content:  content,
		AddedBy:  addedBy,
		AddedAt:  time.Now().UTC().Format("2006-01-02"),
		Revision: rule.Revision,
	})

	v, err := entityops.BumpPatch(f.RuleSet.Version)
	if err != nil {
		return "", fmt.Errorf("bumping version: %w", err)
	}
	f.RuleSet.Version = v
	if err := entityops.UpdateMeta(f); err != nil {
		return "", fmt.Errorf("updating metadata: %w", err)
	}
	return noteID, nil
}

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
		Long: `Append a note to a rule in its entity file. The note ID is <rule_id>/NN where NN
is the next free number for that rule. The entity's rule_set version is patch-bumped
and its timestamp and hash refreshed.

Types: ` + strings.Join(noteTypes, ", ") + `

Examples:
  strspc note add MYE-001 --type rationale --body "Required by GDPR art. 17" --added-by "@alice"
  strspc note add MYE-001 --type applies_to --body "src/cmd/*.go" --added-by "@bob" --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]

			if noteType == "" {
				return fmt.Errorf("--type is required (one of: %s)", strings.Join(noteTypes, ", "))
			}
			if !slices.Contains(noteTypes, noteType) {
				return fmt.Errorf("invalid note type %q: must be one of %s", noteType, strings.Join(noteTypes, ", "))
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

			var (
				noteID, oldVersion, version string
				revision                    int
			)
			err = modifyEntity(target, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				var addErr error
				noteID, addErr = addNote(f, ruleID, noteType, body, addedBy)
				if addErr != nil {
					return addErr
				}
				version = f.RuleSet.Version
				revision = f.Notes[len(f.Notes)-1].Revision
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
					"revision":    revision,
					"version":     version,
					"old_version": oldVersion,
				})
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Added note %s (%s)", noteID, noteType)))
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&noteType, "type", "", "note type (required): "+strings.Join(noteTypes, "|"))
	cmd.Flags().StringVar(&body, "body", "", "note content (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
