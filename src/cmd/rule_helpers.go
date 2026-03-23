package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// stateLabel returns a human-readable label for a rule state code.
func stateLabel(state string) string {
	switch state {
	case entityops.StateDraft:
		return "Draft"
	case entityops.StatePublished:
		return "Published"
	case entityops.StateImplemented:
		return "Implemented"
	case entityops.StateRetired:
		return "Retired"
	case entityops.StateTerminated:
		return "Terminated"
	case entityops.StateAbandoned:
		return "Abandoned"
	default:
		return state
	}
}

// findRule returns the rule with the given ID, or nil if not found.
func findRule(f *entity.File, ruleID string) *entity.Rule {
	for i := range f.Rules {
		if f.Rules[i].ID == ruleID {
			return &f.Rules[i]
		}
	}
	return nil
}

// writeJSONOutput marshals v as indented JSON and writes it to w with a trailing newline.
func writeJSONOutput(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON output: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	_, err = w.Write([]byte{'\n'})
	return err
}

// resolveEntityPath returns the path to an entity JSON file in the given directory.
func resolveEntityPath(dir, entityID string) string {
	return filepath.Join(dir, entityID+".json")
}

// entityIDFromRuleID extracts the entity ID prefix from a rule ID.
// For example, "MYE-001" returns "MYE", "MY-ENT-001" returns "MY-ENT".
func entityIDFromRuleID(ruleID string) (string, error) {
	idx := strings.LastIndex(ruleID, "-")
	if idx < 1 {
		return "", fmt.Errorf("invalid rule ID %q: expected format ENTITY-NNN", ruleID)
	}

	suffix := ruleID[idx+1:]
	if len(suffix) != 3 || suffix[0] < '0' || suffix[0] > '9' || suffix[1] < '0' || suffix[1] > '9' || suffix[2] < '0' || suffix[2] > '9' {
		return "", fmt.Errorf("invalid rule ID %q: suffix must be exactly 3 digits", ruleID)
	}

	return ruleID[:idx], nil
}

// loadModifySaveEntity loads an entity file, applies the mutate function, and writes it back.
func loadModifySaveEntity(entityPath string, mutate func(*entity.File) error) error {
	f, err := entity.Load(entityPath)
	if err != nil {
		return fmt.Errorf("loading entity file: %w", err)
	}

	if err := mutate(f); err != nil {
		return err
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling entity file: %w", err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(entityPath, data, 0o644); err != nil {
		return fmt.Errorf("writing entity file: %w", err)
	}

	return nil
}
