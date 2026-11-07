package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

func newRealmStandardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "standards",
		Short: "Declare the external standards a Realm cites",
		Long: "Add, update, remove or list the Standard References in the `standards` array of realm.json\n" +
			"(RLMMNFST-007). The `standard` notes of the Realm's rules, and of its sub-realms' rules,\n" +
			"resolve against them after the entities' own (NTE-016).",
	}
	cmd.AddCommand(newRealmStandardsAddCmd())
	cmd.AddCommand(newRealmStandardsRemoveCmd())
	cmd.AddCommand(newRealmStandardsUpdateCmd())
	cmd.AddCommand(newRealmStandardsListCmd())
	return cmd
}

func newRealmStandardsAddCmd() *cobra.Command {
	var (
		std     standardFlags
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Declare a standard on a Realm",
		Long: `Append a Standard Reference to realm.json. The id must be a dot-separated
lowercase slug (STDREF-002) not already declared by the Realm (STDREF-003), and
the relation implements or derived_from (STDREF-004).

--realm links the reference to a Standard Realm the Realm tree depends on
(STDREF-017), whose edition is the --version (STDREF-018); --profile limits it
to one of that Realm's profiles (STDREF-019, STDREF-027). Lint checks the link
against the Realm. Both need dev.steerspec.core ` + manifestMinCore.String() + ` or later. A Realm
Note of type exclusion names a linked reference (NTE-034).

Examples:
  strspc realm standards add --id owasp.asvs --relation implements --version 4.0.3 --url https://owasp.org/www-project-application-security-verification-standard/
  strspc realm standards add --id owasp.asvs --relation implements --version 5.0.0 --realm dev.steerspec.std.owasp.asvs.v5`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ref, err := std.ref(cmd)
			if err != nil {
				return err
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			if err := checkCoreAcceptsStandards(dir, realmPath); err != nil {
				return err
			}
			if err := checkCoreAcceptsStandardLink(dir, realmPath, &ref); err != nil {
				return err
			}
			var realmID string
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				refs, err := addStandardRef(rf.Realm.Standards, &ref)
				if err != nil {
					return err
				}
				rf.Realm.Standards = refs
				realmID = rf.Realm.ID
				return nil
			})
			if err != nil {
				return err
			}
			return writeRealmStandardChange(cmd, jsonOut, "Declared", realmID, realmPath, &ref)
		},
	}
	std.register(cmd)
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmStandardsRemoveCmd() *cobra.Command {
	var (
		id      string
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a standard from a Realm",
		Long: `Remove the Standard Reference with the given id from realm.json. The removal
is refused while a ` + "`standard`" + ` note in the Realm or its sub-realms would no longer
resolve (NTE-016): withdraw those mappings first with
` + "`strspc note update <note_id> --type reference`" + `. It is refused too while
an exclusion names the standard (NTE-034): withdraw it first with
` + "`strspc realm note update <REALM/NNN> --type reference`" + `.

Example:
  strspc realm standards remove --id owasp.asvs`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return fmt.Errorf("--id is required")
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			var (
				realmID string
				removed entity.StandardRef
			)
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				refs, r, err := removeStandardRef(rf.Realm.Standards, id)
				if err != nil {
					return err
				}
				rf.Realm.Standards = refs
				if notes := notesNaming(rulelint.UnresolvedStandardNotes(dir, rf), id); len(notes) > 0 {
					return unresolvedNotesError(id, notes)
				}
				if err := checkNoExclusionNames(rf, id, "remove"); err != nil {
					return err
				}
				realmID, removed = rf.Realm.ID, r
				return nil
			})
			if err != nil {
				return err
			}
			return writeRealmStandardChange(cmd, jsonOut, "Removed", realmID, realmPath, &removed)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "id of the standard to remove (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmStandardsListCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List the standards a Realm declares",
		Long:          "List the Standard References in realm.json. `strspc standards coverage` reports on them with the entities' own.",
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
			w := cmd.OutOrStdout()
			if jsonOut {
				return writeJSONOutput(w, map[string]any{
					"realm":     rf.Realm.ID,
					"path":      realmPath,
					"standards": standardsOrEmpty(rf.Realm.Standards),
				})
			}
			writeStandardsList(w, "Realm "+printableRealmID(rf.Realm.ID), rf.Realm.Standards)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func writeRealmStandardChange(cmd *cobra.Command, jsonOut bool, verb, realmID, path string, ref *entity.StandardRef) error {
	w := cmd.OutOrStdout()
	if jsonOut {
		return writeJSONOutput(w, map[string]any{
			"realm":    realmID,
			"path":     path,
			"standard": ref,
		})
	}
	writeln(w, brandStyle.Render(fmt.Sprintf("%s standard %s on Realm %s", verb, describeStandard(ref), printableRealmID(realmID))))
	return nil
}
