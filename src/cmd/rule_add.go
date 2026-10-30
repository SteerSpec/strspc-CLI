package cmd

import (
	"fmt"

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
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "add <entity_id>",
		Short: "Add a new draft rule to an entity",
		Long: `Add a new draft rule to an entity file within a Realm directory.

<entity_id> may be a top-level entity or a sub-entity declared in any entity
file under --dir (detected automatically). Use --sub-entity to target a named
sub-entity of <entity_id> explicitly.

Examples:
  strspc rule add MYE --body "The system MUST validate input" --added-by "@alice"
  strspc rule add MYE --body "Users SHOULD be notified" --added-by "@bob" --dir ./specs --json
  strspc rule add MYE --sub-entity MYESUB --body "Sub-entity rule" --added-by "@alice"
  strspc rule add SAVACCT --body "Withdrawals MUST..." --added-by "@alice" --overrides dev.globalbank.core:ACCT-005`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			entityID := args[0]

			if body == "" {
				return fmt.Errorf("--body is required")
			}
			if addedBy == "" {
				return fmt.Errorf("--added-by is required")
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

			var newRuleID string
			var oldVersion, version string

			err = modifyEntity(target, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				var addErr error
				newRuleID, addErr = entityops.AddRule(f, body, addedBy)
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
				}
				if overridesID != "" {
					out["overrides"] = overridesID
				}
				return writeJSONOutput(w, out)
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Added rule %s (Draft, revision 0)", newRuleID)))
			if overridesID != "" {
				writeln(w, descStyle.Render("  Overrides: "+overridesID))
			}
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "rule statement text (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&subEntity, "sub-entity", "", "target this sub-entity of <entity_id> instead of the entity itself")
	cmd.Flags().StringVar(&overrides, "overrides", "", "inherited rule this rule replaces for the entity: <EUID>-NNN or <realm-id>:<EUID>-NNN")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
