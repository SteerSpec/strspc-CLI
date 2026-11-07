package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRealmNoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Author the Realm Notes of realm.json",
		Long: "Add, revise and list Realm Notes: notes held in the `notes` array of a Realm's manifest\n" +
			"rather than next to a rule (NTE-029, RLMMNFST-012). Their IDs are REALM/NNN, numbered per\n" +
			"manifest from REALM/001 (NTE-030), and their types " + strings.Join(entity.RealmNoteTypes(), ", ") + "\n" +
			"(NTE-031). An exclusion declares one clause of a linked standard not applicable to the\n" +
			"Realm, its content being the justification (NTE-033). A Realm Note is never removed\n" +
			"(NTE-009): an exclusion is withdrawn by retyping it to reference (NTE-035).\n\n" +
			"These are Draft rules of dev.steerspec.core 1.21.0, which the notes need. Every write\n" +
			"recomputes the manifest hash when realm.json has one (RLMMNFST-014).",
	}
	cmd.AddCommand(newRealmNoteAddCmd())
	cmd.AddCommand(newRealmNoteUpdateCmd())
	cmd.AddCommand(newRealmNoteListCmd())
	return cmd
}

// realmNoteChange is the JSON output of `realm note add` and `realm note
// update`.
type realmNoteChange struct {
	Realm    string `json:"realm"`
	Path     string `json:"path"`
	NoteID   string `json:"note_id"`
	Type     string `json:"type"`
	Ref      string `json:"ref,omitempty"`
	Revision int    `json:"revision"`
	AddedBy  string `json:"added_by,omitempty"`
}

// exclusionsNaming returns the IDs of the exclusions of rf whose ref names the
// standard id. Each needs that standard declared with a realm (NTE-034); a
// withdrawn exclusion, now a reference, no longer does.
func exclusionsNaming(rf *entity.RealmFile, id string) []string {
	var ids []string
	for _, n := range rf.Notes {
		if n.Type != entity.NoteTypeExclusion {
			continue
		}
		if refID, _, _ := strings.Cut(n.Ref, ":"); refID == id {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// checkNoExclusionNames refuses to change, as verb says, the standard id that
// exclusions of rf name: they would no longer resolve (NTE-034).
func checkNoExclusionNames(rf *entity.RealmFile, id, verb string) error {
	ids := exclusionsNaming(rf, id)
	if len(ids) == 0 {
		return nil
	}
	return fmt.Errorf("cannot %s standard %q: %d exclusion(s) name it (NTE-034): %s\n"+
		"withdraw each first with `strspc realm note update <REALM/NNN> --type reference`",
		verb, id, len(ids), coverageCell(strings.Join(ids, ", ")))
}

// describeRealmNote renders a Realm Note's type and ref for human output.
func describeRealmNote(noteType, ref string) string {
	if ref == "" {
		return coverageCell(noteType)
	}
	return coverageCell(noteType + " " + ref)
}

func newRealmNoteAddCmd() *cobra.Command {
	var (
		noteType string
		body     string
		ref      string
		addedBy  string
		dir      string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a Realm Note to realm.json",
		Long: `Append a Realm Note at revision 0, with the next REALM/NNN of the manifest
(NTE-030) and today's date. --ref is required on an exclusion and refused on any
other type: it is <id>:<clause>, whose <id> names a Standard Reference of the
same manifest linked to a Standard Realm with --realm (NTE-034). Whether the
clause is one of that Realm's is checked by lint.

--added-by is resolved as for rule notes, and must name one of the Realm's
contributors when it lists any, or for a sub-realm its parent's (CTRB-005,
CTRB-006). realm.version is left as it is.

Types: ` + strings.Join(entity.RealmNoteTypes(), ", ") + `

Examples:
  strspc realm note add --type exclusion --ref owasp.asvs:V3.1.1 --body "The CLI renders no HTML." --added-by github:alice
  strspc realm note add --type rationale --body "Why this realm exists." --json`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if noteType == "" {
				return fmt.Errorf("--type is required (one of: %s)", strings.Join(entity.RealmNoteTypes(), ", "))
			}
			if !entity.IsValidRealmNoteType(noteType) {
				return fmt.Errorf("invalid Realm Note type %q: must be one of %s", noteType, strings.Join(entity.RealmNoteTypes(), ", "))
			}
			if body == "" {
				return fmt.Errorf("--body is required")
			}
			if err := rejectControlChars("--ref", ref); err != nil {
				return err
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			if err := checkCoreAccepts(dir, realmPath, "notes", manifestMinCore); err != nil {
				return err
			}
			list, err := entity.RealmContributors(dir)
			if err != nil {
				return fmt.Errorf("reading the contributors of the realm in %s: %w", dir, err)
			}
			addedBy, addedBySource, err := resolveAddedByFrom(addedBy, cmd.Flags().Changed("added-by"), list, dir)
			if err != nil {
				return err
			}

			change := realmNoteChange{Path: realmPath, Type: noteType, Ref: ref, AddedBy: addedBy}
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				id, err := entityops.AddRealmNote(rf, noteType, body, ref, addedBy, list)
				if err != nil {
					return err
				}
				change.Realm, change.NoteID = rf.Realm.ID, id
				return nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if jsonOut {
				return writeJSONOutput(w, change)
			}
			writeln(w, brandStyle.Render(fmt.Sprintf("Added Realm Note %s (%s) to realm %s", change.NoteID, describeRealmNote(noteType, ref), printableRealmID(change.Realm))))
			writeAddedBySource(w, addedBy, addedBySource)
			return nil
		},
	}
	cmd.Flags().StringVar(&noteType, "type", "", "note type (required): "+strings.Join(entity.RealmNoteTypes(), "|"))
	cmd.Flags().StringVar(&body, "body", "", "note content (required); for an exclusion, the justification (NTE-033)")
	cmd.Flags().StringVar(&ref, "ref", "", "the excluded clause, <id>:<clause> (required on an exclusion, refused otherwise)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier such as github:<login>; must name a contributor when the realm lists any. Defaults to $STRSPC_ADDED_BY, then to the contributor whose email is your git user.email")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmNoteUpdateCmd() *cobra.Command {
	var (
		noteType string
		body     string
		dir      string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "update <REALM/NNN>",
		Short: "Revise a Realm Note in place",
		Long: `Change the content and/or type of a Realm Note. Its revision goes up by one
(NTE-008); its author, date and ref are kept. A Realm Note is never removed
(NTE-009).

The only change of type is withdrawing an exclusion: --type reference, with the
ref and content unchanged, so --body can't be given with it (NTE-035).

Examples:
  strspc realm note update REALM/002 --body "Clarified wording."
  strspc realm note update REALM/001 --type reference`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if body == "" && noteType == "" {
				return fmt.Errorf("at least one of --body or --type is required")
			}
			if err := rejectControlChars("note ID", id); err != nil {
				return err
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			if err := checkCoreAccepts(dir, realmPath, "notes", manifestMinCore); err != nil {
				return err
			}
			change := realmNoteChange{Path: realmPath, NoteID: id}
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				if err := entityops.UpdateRealmNote(rf, id, body, noteType); err != nil {
					return err
				}
				for _, n := range rf.Notes {
					if n.ID == id {
						change.Type, change.Ref, change.Revision = n.Type, n.Ref, n.Revision
					}
				}
				change.Realm = rf.Realm.ID
				return nil
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if jsonOut {
				return writeJSONOutput(w, change)
			}
			writeln(w, brandStyle.Render(fmt.Sprintf("Updated Realm Note %s (%s, revision %d)", coverageCell(id), describeRealmNote(change.Type, change.Ref), change.Revision)))
			return nil
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "new note content")
	cmd.Flags().StringVar(&noteType, "type", "", "new note type; only "+entity.NoteTypeReference+", to withdraw an exclusion (NTE-035)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmNoteListCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List the Realm Notes of realm.json",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			rf, err := entity.LoadRealm(realmPath)
			if err != nil {
				return fmt.Errorf("loading realm.json: %w", err)
			}
			notes := rf.Notes
			if notes == nil {
				notes = []entity.RealmNote{}
			}
			w := cmd.OutOrStdout()
			if jsonOut {
				return writeJSONOutput(w, map[string]any{
					"realm": rf.Realm.ID,
					"path":  realmPath,
					"notes": notes,
				})
			}
			writeRealmNoteList(w, rf.Realm.ID, notes)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

// writeRealmNoteList prints the Realm Notes of a Realm as a table.
func writeRealmNoteList(w io.Writer, realmID string, notes []entity.RealmNote) {
	if len(notes) == 0 {
		writeln(w, descStyle.Render(fmt.Sprintf("Realm %s has no Realm Notes", printableRealmID(realmID))))
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, n := range notes {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\tr%d\t%s\t%s\t%s\n",
			coverageCell(n.ID), coverageCell(n.Type), coverageCell(n.Ref), n.Revision,
			coverageCell(n.AddedBy), coverageCell(n.AddedAt), coverageCell(n.Content))
	}
	_ = tw.Flush()
}
