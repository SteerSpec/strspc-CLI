package cmd

import (
	"bytes"
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
	case entity.StateDraft:
		return "Draft"
	case entity.StatePublished:
		return "Published"
	case entity.StateImplemented:
		return "Implemented"
	case entity.StateRetired:
		return "Retired"
	case entity.StateTerminated:
		return "Terminated"
	case entity.StateAbandoned:
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

// marshalJSON renders v as 2-space indented JSON with a trailing newline.
// Unlike json.MarshalIndent it does not HTML-escape <, > and &, so rule bodies
// such as "[<EUID>-NNN]" round-trip byte-identical through the CLI.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeJSONOutput marshals v as indented JSON and writes it to w with a trailing newline.
func writeJSONOutput(w io.Writer, v any) error {
	data, err := marshalJSON(v)
	if err != nil {
		return fmt.Errorf("marshaling JSON output: %w", err)
	}
	if _, err := w.Write(printableJSON(data)); err != nil {
		return withExitCode(exitOutput, fmt.Errorf("writing JSON output: %w", err))
	}
	return nil
}

// resolveEntityPath returns the path to an entity JSON file in the given directory.
func resolveEntityPath(dir, entityID string) string {
	return filepath.Join(dir, entityID+".json")
}

// entityIDFromRuleID extracts the entity ID prefix from a rule ID.
// For example, "MYE-001" returns "MYE". The prefix isn't checked here:
// resolveEntityTarget rejects one that isn't an EUID.
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

// loadModifySaveEntity loads an entity file, applies the mutate function, and
// writes it back. The rule, note and entity commands that edit one entity go
// through here, by way of modifyEntity; entity rehash and realm
// rewrite-added-by call replaceEntityFile themselves. An entity file that is a
// symbolic link is refused before it is loaded: its target may lie outside
// --dir. The write replaces the file by rename, so a link swapped in after the
// check is replaced, not written through.
func loadModifySaveEntity(entityPath string, mutate func(*entity.File) error) error {
	if link, err := isLinkPath(entityPath); err != nil {
		return fmt.Errorf("loading entity file: %w", err)
	} else if link {
		return linkedEntityError(entityPath)
	}
	f, err := entity.Load(entityPath)
	if err != nil {
		return fmt.Errorf("loading entity file: %w", err)
	}

	if err := mutate(f); err != nil {
		return err
	}

	return replaceEntityFile(entityPath, f)
}

// linkedEntityError refuses to edit an entity file that is a symbolic link.
func linkedEntityError(path string) error {
	return fmt.Errorf("%s is a symbolic link; edit its target where it lives", path)
}

// isLinkPath reports whether path is itself a symbolic link, without following
// it. Commands that rewrite entity files skip or refuse a link, whose target
// may lie outside --dir.
func isLinkPath(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return fi.Mode()&os.ModeSymlink != 0, nil
}

// replaceEntityFile writes f to path as indented JSON with a trailing newline,
// by way of a temporary file in the same directory and a rename. The rename replaces the directory entry and never
// follows a link, so a link swapped in after isLinkPath looked is replaced,
// not written through. The file keeps its permissions.
func replaceEntityFile(path string, f *entity.File) error {
	data, err := marshalJSON(f)
	if err != nil {
		return fmt.Errorf("marshaling entity file: %w", err)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing entity file: %w", err)
	}
	name := tmp.Name()
	// The mode is set through the open file, not by name: a name swapped for
	// a link before a Chmod by path would change the link's target instead.
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("writing entity file: %w", err)
	}
	return nil
}

// entityTarget identifies the entity file to modify and, when subID is set,
// the nested sub-entity inside it that the operation applies to.
type entityTarget struct {
	path  string
	subID string
}

// findSubEntity returns the sub-entity with the given EUID, searching nested
// sub_entities recursively, or nil if none matches.
func findSubEntity(f *entity.File, euid string) *entity.File {
	chain := findSubEntityChain(f, euid)
	if chain == nil {
		return nil
	}
	return chain[len(chain)-1]
}

// findSubEntityChain returns the path from f's first-level sub-entity down to
// the sub-entity with the given EUID (inclusive), or nil if none matches.
func findSubEntityChain(f *entity.File, euid string) []*entity.File {
	for i := range f.SubEntities {
		sub := &f.SubEntities[i]
		if sub.Entity.ID == euid {
			return []*entity.File{sub}
		}
		if nested := findSubEntityChain(sub, euid); nested != nil {
			return append([]*entity.File{sub}, nested...)
		}
	}
	return nil
}

// resolveEntityTarget locates the entity with the given EUID under dir.
// A top-level file <dir>/<euid>.json wins; otherwise every entity file under
// dir is scanned for a sub-entity with that EUID. Exactly one match is required.
// The EUID is checked first, so an argument such as ../x can't name a file
// outside dir. The target is about to be edited, so an entity file that is a
// symbolic link is refused; resolveEntityTargetForRead follows links.
func resolveEntityTarget(dir, euid string) (entityTarget, error) {
	return resolveTarget(dir, euid, false)
}

// resolveEntityTargetForRead is resolveEntityTarget for commands that only read
// the entity: a linked entity file is followed, as before links were refused
// for writes.
func resolveEntityTargetForRead(dir, euid string) (entityTarget, error) {
	return resolveTarget(dir, euid, true)
}

func resolveTarget(dir, euid string, followLinks bool) (entityTarget, error) {
	if err := entityops.ValidateEUID(euid); err != nil {
		return entityTarget{}, err
	}
	topPath := resolveEntityPath(dir, euid)
	if followLinks {
		if _, err := os.Stat(topPath); err == nil {
			return entityTarget{path: topPath}, nil
		}
	} else if fi, err := os.Lstat(topPath); err == nil {
		// Lstat, not Stat: a link that points nowhere is still a link,
		// refused as one rather than reported as a missing entity.
		if fi.Mode()&os.ModeSymlink != 0 {
			return entityTarget{}, linkedEntityError(topPath)
		}
		return entityTarget{path: topPath}, nil
	}

	var matches []string
	walkErr := entity.WalkEntityFiles(dir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil || ef == nil {
			return nil // unreadable files are reported by lint, not by authoring commands
		}
		if findSubEntity(ef, euid) != nil {
			matches = append(matches, path)
		}
		return nil
	})
	if walkErr != nil {
		return entityTarget{}, fmt.Errorf("scanning %s for sub-entity %q: %w", dir, euid, walkErr)
	}

	switch len(matches) {
	case 0:
		return entityTarget{}, fmt.Errorf("entity %q not found in %s: no %s.json and no sub-entity with that ID", euid, dir, euid)
	case 1:
		// The walk reads through a link (strspc-manager's walker reads each
		// file before calling back), so a sub-entity can be found in one;
		// refuse it here, before anything else reads or writes it.
		if followLinks {
			return entityTarget{path: matches[0], subID: euid}, nil
		}
		if link, err := isLinkPath(matches[0]); err != nil {
			return entityTarget{}, fmt.Errorf("loading entity file: %w", err)
		} else if link {
			return entityTarget{}, linkedEntityError(matches[0])
		}
		return entityTarget{path: matches[0], subID: euid}, nil
	default:
		return entityTarget{}, fmt.Errorf("sub-entity %q is defined in %d files under %s: %s", euid, len(matches), dir, strings.Join(matches, ", "))
	}
}

// resolveSubEntityOf locates sub-entity subID declared under parentID, which
// may itself be a top-level entity or a nested sub-entity anywhere under dir.
func resolveSubEntityOf(dir, parentID, subID string) (entityTarget, error) {
	parent, err := resolveEntityTarget(dir, parentID)
	if err != nil {
		return entityTarget{}, err
	}
	data, err := os.ReadFile(parent.path)
	if err != nil {
		return entityTarget{}, fmt.Errorf("reading entity file: %w", err)
	}
	ef, err := entity.Parse(data)
	if err != nil {
		return entityTarget{}, fmt.Errorf("parsing %s: %w", parent.path, err)
	}
	scope := ef
	if parent.subID != "" {
		scope = findSubEntity(ef, parent.subID)
		if scope == nil {
			return entityTarget{}, fmt.Errorf("sub-entity %q not found in %s", parent.subID, parent.path)
		}
	}
	if findSubEntity(scope, subID) == nil {
		return entityTarget{}, fmt.Errorf("sub-entity %q not found under %q in %s", subID, parentID, parent.path)
	}
	return entityTarget{path: parent.path, subID: subID}, nil
}

// modifyEntity loads the target's file, applies mutate to the target entity
// (the file itself or a nested sub-entity), refreshes the parent's rule_set
// metadata when a sub-entity changed, and writes the file back.
func modifyEntity(t entityTarget, mutate func(target *entity.File) error) error {
	return loadModifySaveEntity(t.path, func(f *entity.File) error {
		target := f
		var ancestors []*entity.File // innermost first, excluding target
		if t.subID != "" {
			chain := findSubEntityChain(f, t.subID)
			if chain == nil {
				return fmt.Errorf("sub-entity %q not found in %s", t.subID, t.path)
			}
			target = chain[len(chain)-1]
			for i := len(chain) - 2; i >= 0; i-- {
				ancestors = append(ancestors, chain[i])
			}
			ancestors = append(ancestors, f)
		}
		if err := mutate(target); err != nil {
			return err
		}
		// The target's own version and hash were refreshed by entityops.
		// Every ancestor changed too: each hash covers its whole subtree
		// (RL011) and the top-level rule_set.version must rise on any
		// change in the file (RD007), so bump a patch and refresh metadata
		// from the innermost ancestor outwards.
		for _, a := range ancestors {
			v, err := entityops.BumpPatch(a.RuleSet.Version)
			if err != nil {
				return fmt.Errorf("bumping version of %s: %w", a.Entity.ID, err)
			}
			a.RuleSet.Version = v
			if err := entityops.UpdateMeta(a); err != nil {
				return fmt.Errorf("updating metadata of %s: %w", a.Entity.ID, err)
			}
		}
		return nil
	})
}
