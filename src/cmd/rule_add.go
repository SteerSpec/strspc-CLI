package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleAddCmd() *cobra.Command {
	var (
		body      string
		addedBy   string
		dir       string
		subEntity string
		overrides string
		clause    string
		profiles  []string
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "add <entity_id>",
		Short: "Add a new draft rule to an entity",
		Long: `Add a new draft rule to an entity file within a Realm directory.

<entity_id> may be a top-level entity or a sub-entity declared in any entity
file under --dir (detected automatically). Use --sub-entity to target a named
sub-entity of <entity_id> explicitly.

In a Standard Realm, a realm.json with kind standard, each rule transcribes a
clause of the standard: --clause names it (RUL-015, STDRLM-005), and --profiles
lists the Realm's profiles the rule belongs to (RUL-016), which the Realm's
manifest must declare (STDRLM-008) and every rule needs when it declares any
(STDRLM-018). The rules of one clause in an entity share its profiles
(STDRLM-009). Neither flag is allowed outside a Standard Realm (RUL-017). The
Realm is the one of the nearest realm.json above the entity file, so a
sub-realm's own.

Examples:
  strspc rule add MYE --body "The system MUST validate input" --added-by github:alice
  strspc rule add MYE --body "Users SHOULD be notified" --added-by github:bob --dir ./specs --json
  strspc rule add MYE --sub-entity MYESUB --body "Sub-entity rule" --added-by github:alice
  strspc rule add SAVACCT --body "Withdrawals MUST..." --added-by github:alice --overrides dev.globalbank.core:ACCT-005
  strspc rule add V01S01 --body "Verify that..." --clause V1.1.1 --profiles L1,L2,L3 --added-by github:alice`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			entityID := args[0]

			if body == "" {
				return fmt.Errorf("--body is required")
			}
			overridesID, err := validateOverrides(overrides)
			if err != nil {
				return err
			}

			var target entityTarget
			if subEntity != "" {
				target, err = resolveSubEntityOf(dir, entityID, subEntity)
			} else {
				target, err = resolveEntityTarget(dir, entityID)
			}
			if err != nil {
				return err
			}
			addedBy, addedBySource, err := resolveAddedBy(addedBy, cmd.Flags().Changed("added-by"), target, dir)
			if err != nil {
				return err
			}
			realm, err := realmManifestOf(target.path, dir)
			if err != nil {
				return err
			}
			if err := checkStandardFlags(cmd, realm, dir, target); err != nil {
				return err
			}
			opts := entityops.AddRuleOptions{Clause: clause, Profiles: profiles, Realm: realm}

			var newRuleID string
			var oldVersion, version string

			err = modifyEntity(target, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				var addErr error
				newRuleID, addErr = entityops.AddRuleWith(f, body, addedBy, opts)
				if addErr != nil {
					return addErr
				}
				version = f.RuleSet.Version
				return setOverrides(f, newRuleID, overridesID)
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				out := map[string]any{
					"rule_id":     newRuleID,
					"state":       entity.StateDraft,
					"revision":    0,
					"version":     version,
					"old_version": oldVersion,
					"added_by":    addedBy,
				}
				if overridesID != "" {
					out["overrides"] = overridesID
				}
				addStandardFields(out, clause, profiles)
				return writeJSONOutput(w, out)
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Added rule %s (Draft, revision 0)", newRuleID)))
			if overridesID != "" {
				writeln(w, descStyle.Render("  Overrides: "+overridesID))
			}
			writeStandardFields(w, clause, profiles)
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))
			writeAddedBySource(w, addedBy, addedBySource)

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "rule statement text (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier such as github:<login>; must name a contributor when the realm lists any. Defaults to $STRSPC_ADDED_BY, then to the contributor whose email is your git user.email")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&subEntity, "sub-entity", "", "target this sub-entity of <entity_id> instead of the entity itself")
	cmd.Flags().StringVar(&overrides, "overrides", "", "inherited rule this rule replaces for the entity: <EUID>-NNN or <realm-id>:<EUID>-NNN")
	cmd.Flags().StringVar(&clause, "clause", "", "in a Standard Realm, the clause of the standard the rule transcribes, such as V1.1.1")
	cmd.Flags().StringSliceVar(&profiles, "profiles", nil, "in a Standard Realm, the profiles the rule belongs to, comma-separated, such as L1,L2,L3")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}

// checkStandardFlags refuses --clause or --profiles, even given empty, outside
// a Standard Realm (RUL-017), and an empty --profiles (RUL-016); AddRuleWith
// would take an empty value for no flag. In a Standard Realm it refuses them
// when the core pin's schemas predate them.
func checkStandardFlags(cmd *cobra.Command, realm *entity.RealmMeta, dir string, target entityTarget) error {
	clauseSet, profilesSet := cmd.Flags().Changed("clause"), cmd.Flags().Changed("profiles")
	if !clauseSet && !profilesSet {
		return nil
	}
	if realm == nil || realm.Kind != entity.KindStandard {
		return fmt.Errorf("--clause and --profiles describe a rule of a Standard Realm: the realm holding %s isn't one (RUL-017)", target.path)
	}
	if profiles, _ := cmd.Flags().GetStringSlice("profiles"); profilesSet && len(profiles) == 0 {
		return errors.New("--profiles: want one or more profile names (RUL-016)")
	}
	return checkCoreAccepts(dir, target.path, "clause", manifestMinCore)
}

// addStandardFields adds a rule's clause and profiles to its JSON output, when
// it has them.
func addStandardFields(out map[string]any, clause string, profiles []string) {
	if clause != "" {
		out["clause"] = clause
	}
	if len(profiles) > 0 {
		out["profiles"] = profiles
	}
}

// standardFieldsText describes a rule's clause and profiles, or is empty when
// it has neither. Both can come from a remote source, so they are made
// printable.
func standardFieldsText(clause string, profiles []string) string {
	var parts []string
	if clause != "" {
		parts = append(parts, "Clause: "+printableText(clause))
	}
	if len(profiles) > 0 {
		shown := make([]string, 0, len(profiles))
		for _, p := range profiles {
			shown = append(shown, printableText(p))
		}
		parts = append(parts, "Profiles: "+strings.Join(shown, ", "))
	}
	return strings.Join(parts, " · ")
}

// writeStandardFields prints a rule's clause and profiles, when it has them.
func writeStandardFields(w io.Writer, clause string, profiles []string) {
	if text := standardFieldsText(clause, profiles); text != "" {
		writeln(w, descStyle.Render("  "+text))
	}
}
