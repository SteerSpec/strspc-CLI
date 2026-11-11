package cmd

import (
	"fmt"
	"io"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// ruleTransitionFunc moves one rule of an entity file to its next state, as
// entityops.RetireRule and entityops.TerminateRule do.
type ruleTransitionFunc func(f *entity.File, ruleID string) error

// runRuleTransition applies op to ruleID in the entity file under dir that
// holds it, then reports the state change and the entity version change to
// w, as text or as JSON. Nothing is written when op fails. action names the
// transition in errors ("retiring", "terminating"), each wrapped with %w
// (CONV-004).
func runRuleTransition(w io.Writer, dir, ruleID, action string, jsonOut bool, op ruleTransitionFunc) error {
	entityID, err := entityIDFromRuleID(ruleID)
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, ruleID, err)
	}

	target, err := resolveEntityTarget(dir, entityID)
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, ruleID, err)
	}

	var oldVersion, version string
	var oldState, newState string

	err = modifyEntity(target, func(f *entity.File) error {
		oldVersion = f.RuleSet.Version
		if r := findRule(f, ruleID); r != nil {
			oldState = r.State
		}
		if opErr := op(f, ruleID); opErr != nil {
			return opErr // modifyEntity returns it as is; wrapped with %w below
		}
		version = f.RuleSet.Version
		if r := findRule(f, ruleID); r != nil {
			newState = r.State
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, ruleID, err)
	}

	if jsonOut {
		return writeJSONOutput(w, map[string]string{
			"rule_id":     ruleID,
			"new_state":   newState,
			"old_state":   oldState,
			"version":     version,
			"old_version": oldVersion,
		})
	}

	writeln(w, brandStyle.Render(fmt.Sprintf("%s rule %s: %s → %s", stateLabel(newState), ruleID, stateLabel(oldState), stateLabel(newState))))
	writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", oldVersion, version)))

	return nil
}
