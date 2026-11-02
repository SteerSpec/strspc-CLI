package cmd

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// entityStandardChange is the JSON output of `entity standards add|remove`.
type entityStandardChange struct {
	Entity     string             `json:"entity"`
	Path       string             `json:"path"`
	Standard   entity.StandardRef `json:"standard"`
	Version    string             `json:"version"`
	OldVersion string             `json:"old_version"`
}

func newEntityStandardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "standards",
		Short: "Declare the external standards an entity cites",
		Long: "Add, remove or list the Standard References in an entity's `standards` array (ENT-017).\n" +
			"They apply to the entity and the entities it contains, and the `standard` notes of their rules\n" +
			"resolve against them (NTE-016). Map a rule to a clause with `strspc note add --type standard`.",
	}
	cmd.AddCommand(newEntityStandardsAddCmd())
	cmd.AddCommand(newEntityStandardsRemoveCmd())
	cmd.AddCommand(newEntityStandardsListCmd())
	return cmd
}

func newEntityStandardsAddCmd() *cobra.Command {
	var (
		std     standardFlags
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "add <EUID>",
		Short: "Declare a standard on an entity",
		Long: `Append a Standard Reference to the entity's standards array. The id must be a
dot-separated lowercase slug (STDREF-002) not already declared on the entity
(STDREF-003), and the relation implements or derived_from (STDREF-004). The
entity's rule_set version is patch-bumped and its timestamp and hash refreshed,
along with those of the entities containing it.

Examples:
  strspc entity standards add KWRD --id ietf.rfc2119 --relation derived_from --url https://www.rfc-editor.org/rfc/rfc2119
  strspc entity standards add API --id owasp.asvs --relation implements --version 4.0.3 --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := std.ref(cmd)
			if err != nil {
				return err
			}
			target, err := resolveEntityTarget(dir, args[0])
			if err != nil {
				return err
			}
			change := entityStandardChange{Entity: args[0], Path: target.path, Standard: ref}
			err = modifyEntity(target, func(f *entity.File) error {
				refs, err := addStandardRef(f.Entity.Standards, ref)
				if err != nil {
					return err
				}
				f.Entity.Standards = refs
				return bumpEntity(f, &change)
			})
			if err != nil {
				return err
			}
			return writeEntityStandardChange(cmd, jsonOut, "Declared", &change)
		},
	}
	std.register(cmd)
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newEntityStandardsRemoveCmd() *cobra.Command {
	var (
		id      string
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "remove <EUID>",
		Short: "Remove a standard from an entity",
		Long: `Remove the Standard Reference with the given id from the entity's standards
array. The removal is refused while a ` + "`standard`" + ` note of the entity, or of an
entity it contains, would no longer resolve (NTE-016): withdraw those mappings
first with ` + "`strspc note update <note_id> --type reference`" + `. The entity's rule_set
version is patch-bumped and its timestamp and hash refreshed.

Example:
  strspc entity standards remove KWRD --id ietf.rfc2119`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return fmt.Errorf("--id is required")
			}
			target, err := resolveEntityTarget(dir, args[0])
			if err != nil {
				return err
			}
			containing, err := containingStandards(target)
			if err != nil {
				return err
			}
			scopes := slices.Concat(containing, realmStandardScopes(dir, target.path))
			change := entityStandardChange{Entity: args[0], Path: target.path}
			err = modifyEntity(target, func(f *entity.File) error {
				refs, removed, err := removeStandardRef(f.Entity.Standards, id)
				if err != nil {
					return err
				}
				f.Entity.Standards = refs
				if notes := unresolvedStandardNotes(f, scopes, id); len(notes) > 0 {
					return unresolvedNotesError(id, notes)
				}
				change.Standard = removed
				return bumpEntity(f, &change)
			})
			if err != nil {
				return err
			}
			return writeEntityStandardChange(cmd, jsonOut, "Removed", &change)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "id of the standard to remove (required)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newEntityStandardsListCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "list <EUID>",
		Short: "List the standards an entity declares",
		Long: "List the Standard References in the entity's own standards array. Those of the entities\n" +
			"containing it and of its Realm also apply to its notes; `strspc standards coverage` reports on all of them.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := resolveEntityTarget(dir, args[0])
			if err != nil {
				return err
			}
			f, err := entity.Load(target.path)
			if err != nil {
				return fmt.Errorf("loading entity file: %w", err)
			}
			if target.subID != "" {
				if f = findSubEntity(f, target.subID); f == nil {
					return fmt.Errorf("sub-entity %q not found in %s", target.subID, target.path)
				}
			}
			w := cmd.OutOrStdout()
			if jsonOut {
				return writeJSONOutput(w, map[string]any{
					"entity":    f.Entity.ID,
					"path":      target.path,
					"standards": standardsOrEmpty(f.Entity.Standards),
				})
			}
			writeStandardsList(w, "Entity "+coverageCell(f.Entity.ID), f.Entity.Standards)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

// bumpEntity patch-bumps f and refreshes its timestamp and hash, as the other
// entity operations do, recording the versions in change.
func bumpEntity(f *entity.File, change *entityStandardChange) error {
	change.OldVersion = f.RuleSet.Version
	v, err := entityops.BumpPatch(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}
	f.RuleSet.Version = v
	if err := entityops.UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata: %w", err)
	}
	change.Version = v
	return nil
}

func writeEntityStandardChange(cmd *cobra.Command, jsonOut bool, verb string, c *entityStandardChange) error {
	w := cmd.OutOrStdout()
	if jsonOut {
		return writeJSONOutput(w, c)
	}
	writeln(w, brandStyle.Render(fmt.Sprintf("%s standard %s on %s", verb, describeStandard(c.Standard), coverageCell(c.Entity))))
	writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", c.OldVersion, c.Version)))
	return nil
}

// containingStandards returns the references of the entities around the
// target, nearest first. A top-level target has none.
func containingStandards(t entityTarget) ([][]entity.StandardRef, error) {
	if t.subID == "" {
		return nil, nil
	}
	f, err := entity.Load(t.path)
	if err != nil {
		return nil, fmt.Errorf("loading entity file: %w", err)
	}
	chain := findSubEntityChain(f, t.subID)
	if chain == nil {
		return nil, fmt.Errorf("sub-entity %q not found in %s", t.subID, t.path)
	}
	var scopes [][]entity.StandardRef
	for i := len(chain) - 2; i >= 0; i-- {
		scopes = append(scopes, chain[i].Entity.Standards)
	}
	return append(scopes, f.Entity.Standards), nil
}

// realmStandardScopes returns the Realm declarations a `standard` note in the
// entity file at path resolves against after its entities (NTE-016), as
// rulelint.CheckRealmStandards finds them: the Realm at dir then its parent,
// or, for a file inside one of dir's sub-realms, that sub-realm then dir's
// Realm. Without a manifest there are none.
func realmStandardScopes(dir, path string) [][]entity.StandardRef {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return nil
	}
	if rel, err := filepath.Rel(dir, path); err == nil {
		if first, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && slices.Contains(rf.SubRealms, first) {
			sub, err := entity.LoadRealm(filepath.Join(dir, first, "realm.json"))
			if err != nil {
				return [][]entity.StandardRef{rf.Realm.Standards}
			}
			return [][]entity.StandardRef{sub.Realm.Standards, rf.Realm.Standards}
		}
	}
	var parent []entity.StandardRef
	if p := entity.ParentRealm(dir, rf.Realm.ID); p != nil {
		parent = p.Realm.Standards
	}
	return [][]entity.StandardRef{rf.Realm.Standards, parent}
}

// unresolvedStandardNotes returns the `standard` notes naming id, in f and the
// entities it contains, that resolve to no declaration. outer holds the
// declarations around f, nearest first.
func unresolvedStandardNotes(f *entity.File, outer [][]entity.StandardRef, id string) []string {
	scopes := append([][]entity.StandardRef{f.Entity.Standards}, outer...)
	var out []string
	for _, n := range f.Notes {
		if n.Type != entity.NoteTypeStandard {
			continue
		}
		if noteID, _, err := entity.ParseStandardNote(n.Content); err != nil || noteID != id {
			continue
		}
		if entity.ResolveStandard(id, scopes...) == nil {
			out = append(out, n.ID)
		}
	}
	for i := range f.SubEntities {
		out = append(out, unresolvedStandardNotes(&f.SubEntities[i], scopes, id)...)
	}
	return out
}
