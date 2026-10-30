package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

func newRuleSupersedeCmd() *cobra.Command {
	var (
		body      string
		addedBy   string
		dir       string
		overrides string
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "supersede <rule_id>",
		Short: "Create a rule that supersedes an existing one",
		Long: `Create a new draft rule that supersedes an existing Published, Implemented, or Retired rule.
The original rule is not modified — it must be retired separately.

When the superseded rule overrides an inherited rule, pass the same
--overrides to keep the override in force (RUL-011); otherwise it lapses.

Examples:
  strspc rule supersede MYE-001 --body "Updated requirement MUST..." --added-by "@alice"
  strspc rule supersede MYE-001 --body "New rule" --added-by "@bob" --json
  strspc rule supersede SAVACCT-003 --body "..." --added-by "@alice" --overrides dev.globalbank.core:ACCT-005`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := args[0]

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

			entityID, err := entityIDFromRuleID(ruleID)
			if err != nil {
				return err
			}

			target, err := resolveEntityTarget(dir, entityID)
			if err != nil {
				return err
			}

			var newRuleID, lapsed string
			var oldVersion, version string

			err = modifyEntity(target, func(f *entity.File) error {
				oldVersion = f.RuleSet.Version
				for _, r := range f.Rules {
					if r.ID == ruleID && r.Overrides != nil && *r.Overrides != overridesID {
						lapsed = *r.Overrides
					}
				}
				var supersedeErr error
				newRuleID, supersedeErr = entityops.SupersedeRule(f, ruleID, body, addedBy)
				if supersedeErr != nil {
					return supersedeErr
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
					"supersedes":  ruleID,
					"version":     version,
					"old_version": oldVersion,
				}
				if overridesID != "" {
					out["overrides"] = overridesID
				}
				if lapsed != "" {
					out["lapsed_overrides"] = lapsed
				}
				return writeJSONOutput(w, out)
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Created rule %s (Draft) — supersedes %s", newRuleID, ruleID)))
			if overridesID != "" {
				writeln(w, descStyle.Render("  Overrides: "+overridesID))
			}
			if lapsed != "" {
				writeln(w, descStyle.Render(fmt.Sprintf("  Note: %s overrides %s; the override lapses unless restated with --overrides %s (RUL-011)", ruleID, lapsed, lapsed)))
			}
			writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

			return nil
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "new rule statement text (required)")
	cmd.Flags().StringVar(&addedBy, "added-by", "", "contributor identifier (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&overrides, "overrides", "", "inherited rule the new rule replaces for the entity: <EUID>-NNN or <realm-id>:<EUID>-NNN")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
