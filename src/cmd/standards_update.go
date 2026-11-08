package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// standardEdit holds the flags of `entity|realm standards update`: the id of
// the reference to edit and the fields to change.
type standardEdit struct {
	id           string
	relation     string
	version      string
	url          string
	clearVersion bool
	clearURL     bool
	realm        string
	profile      string
	clearRealm   bool
	clearProfile bool
}

func (e *standardEdit) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&e.id, "id", "", "id of the standard to edit (required)")
	cmd.Flags().StringVar(&e.relation, "relation", "", "new relation: "+entity.RelationImplements+"|"+entity.RelationDerivedFrom)
	cmd.Flags().StringVar(&e.version, "version", "", "new edition of the standard (not empty)")
	cmd.Flags().StringVar(&e.url, "url", "", "new absolute http(s) URL of the standard (not empty)")
	cmd.Flags().BoolVar(&e.clearVersion, "clear-version", false, "remove the reference's version")
	cmd.Flags().BoolVar(&e.clearURL, "clear-url", false, "remove the reference's url")
	cmd.Flags().StringVar(&e.realm, "realm", "", "new "+standardRealmUsage)
	cmd.Flags().StringVar(&e.profile, "profile", "", "new "+standardProfileUsage)
	cmd.Flags().BoolVar(&e.clearRealm, "clear-realm", false, "remove the reference's realm, and so its link to a Standard Realm")
	cmd.Flags().BoolVar(&e.clearProfile, "clear-profile", false, "remove the reference's profile")
	cmd.MarkFlagsMutuallyExclusive("version", "clear-version")
	cmd.MarkFlagsMutuallyExclusive("url", "clear-url")
	cmd.MarkFlagsMutuallyExclusive("realm", "clear-realm")
	cmd.MarkFlagsMutuallyExclusive("profile", "clear-profile")
}

// checkLink refuses a new realm or profile when the core pin that validates the
// file at path predates manifestMinCore, and a new realm that no Realm of the
// tree at dir declares as a dependency (STDREF-017). Clearing either needs no
// check.
func (e *standardEdit) checkLink(dir, path string) error {
	if err := checkCoreAcceptsStandardLink(dir, path, &entity.StandardRef{Realm: e.realm, Profile: e.profile}); err != nil {
		return err
	}
	return checkStandardRealmDeclared(dir, e.realm)
}

// check validates the flags before any file is read.
func (e *standardEdit) check(cmd *cobra.Command) error {
	if e.id == "" {
		return errors.New("--id is required")
	}
	changed := func(name string) bool { return cmd.Flags().Changed(name) }
	if !changed("relation") && !changed("version") && !changed("url") && !changed("realm") && !changed("profile") &&
		!e.clearVersion && !e.clearURL && !e.clearRealm && !e.clearProfile {
		return errors.New("nothing to change: give --relation, --version, --url, --realm, --profile or a --clear-* flag")
	}
	if changed("version") && e.version == "" {
		return errors.New("--version must not be empty: use --clear-version to remove it (STDREF-007)")
	}
	if changed("url") && e.url == "" {
		return errors.New("--url must not be empty: use --clear-url to remove it")
	}
	if err := checkStandardRealm(cmd, e.realm, e.profile); err != nil {
		return err
	}
	for _, f := range [][2]string{{"--id", e.id}, {"--relation", e.relation}, {"--version", e.version}, {"--url", e.url}, {"--profile", e.profile}} {
		if err := rejectControlChars(f[0], f[1]); err != nil {
			return err
		}
	}
	return nil
}

// apply edits the reference with e's id in refs, returning the new array and
// the reference before and after. The edit must change something and leave a
// valid reference (STDREF-004, STDREF-007, STDREF-008, STDREF-019).
func (e *standardEdit) apply(cmd *cobra.Command, refs []entity.StandardRef) (out []entity.StandardRef, before, after entity.StandardRef, err error) {
	i := slices.IndexFunc(refs, func(r entity.StandardRef) bool { return r.ID == e.id })
	if i < 0 {
		return nil, before, after, fmt.Errorf("standard %q is not declared", e.id)
	}
	before = refs[i]
	after = before
	if cmd.Flags().Changed("relation") {
		after.Relation = e.relation
	}
	switch {
	case e.clearVersion:
		after.Version = ""
	case cmd.Flags().Changed("version"):
		after.Version = e.version
	}
	switch {
	case e.clearURL:
		after.URL = ""
	case cmd.Flags().Changed("url"):
		after.URL = e.url
	}
	switch {
	case e.clearRealm:
		after.Realm = ""
	case cmd.Flags().Changed("realm"):
		after.Realm = e.realm
	}
	switch {
	case e.clearProfile:
		after.Profile = ""
	case cmd.Flags().Changed("profile"):
		after.Profile = e.profile
	}
	if after == before {
		err := fmt.Errorf("standard %q already reads %s: nothing to change", e.id, describeStandard(&before))
		return nil, before, after, err
	}
	if err := entity.ValidateStandardRef(after); err != nil {
		return nil, before, after, err
	}
	out = slices.Clone(refs)
	out[i] = after
	return out, before, after, nil
}

// standardChangeNotice describes what a version change means for the notes
// that resolve to the reference: they now map clauses of another edition
// (STDREF-011).
func standardChangeNotice(before, after *entity.StandardRef, notes []string) string {
	if before.Version == after.Version || len(notes) == 0 {
		return ""
	}
	from, to := before.Version, after.Version
	if from == "" {
		from = "no version"
	}
	if to == "" {
		to = "no version"
	}
	return fmt.Sprintf("%d `standard` note(s) now map clauses of %s %s instead of %s (STDREF-011): %s",
		len(notes), coverageCell(after.ID), coverageCell(to), coverageCell(from), coverageCell(strings.Join(notes, ", ")))
}

// entityNotesUsing returns the `standard` notes naming id, in f and the entities
// it contains, that resolve to f's own declaration of it. Resolution is nearest
// first (NTE-016), and f's declaration is nearer to them than any containing
// entity's or Realm's, so the scopes beyond f are left out on purpose: the notes
// that resolve to nothing once f's declaration is gone are those with no nearer
// declaration, which are exactly those f's declaration resolves.
func entityNotesUsing(f *entity.File, id string) []string {
	without := *f
	without.Entity.Standards = slices.DeleteFunc(slices.Clone(f.Entity.Standards), func(r entity.StandardRef) bool { return r.ID == id })
	return notesNaming(rulelint.UnresolvedEntityNotes(&without, nil, ""), id)
}

// realmNotesUsing returns the `standard` notes naming id, in the Realm at dir
// and its sub-realms, that resolve to rf's declaration of it: those whose
// entities declare no such id, in dir and in each sub-realm that doesn't declare
// it itself.
func realmNotesUsing(dir string, rf *entity.RealmFile, id string) []string {
	var unresolved []rulelint.UnresolvedNote
	walk := func(d string, opts ...entity.WalkOption) {
		_ = entity.WalkEntityFiles(d, func(path string, _ []byte, ef *entity.File, parseErr error) error {
			if parseErr == nil {
				unresolved = append(unresolved, rulelint.UnresolvedEntityNotes(ef, nil, path)...)
			}
			return nil
		}, opts...)
	}
	var opts []entity.WalkOption
	if len(rf.SubRealms) > 0 {
		opts = append(opts, entity.WithExcludeDirs(rf.SubRealms))
	}
	walk(dir, opts...)
	for _, name := range rf.SubRealms {
		sub, err := entity.LoadRealm(filepath.Join(dir, name, "realm.json"))
		if err != nil || entity.FindStandard(sub.Realm.Standards, id) != nil {
			continue
		}
		walk(filepath.Join(dir, name))
	}
	return notesNaming(unresolved, id)
}

// standardUpdate is the JSON output of `entity|realm standards update`.
type standardUpdate struct {
	Entity      string             `json:"entity,omitempty"`
	Realm       string             `json:"realm,omitempty"`
	Path        string             `json:"path"`
	Standard    entity.StandardRef `json:"standard"`
	OldStandard entity.StandardRef `json:"old_standard"`
	Version     string             `json:"version,omitempty"`
	OldVersion  string             `json:"old_version,omitempty"`
	// Notes are the `standard` notes resolving to the reference, listed when
	// its version changed.
	Notes []string `json:"notes,omitempty"`
}

func writeStandardUpdate(cmd *cobra.Command, jsonOut bool, scope string, u *standardUpdate) error {
	w := cmd.OutOrStdout()
	if jsonOut {
		return writeJSONOutput(w, u)
	}
	writeln(w, brandStyle.Render(fmt.Sprintf("Updated standard %s on %s", describeStandard(&u.Standard), scope)))
	writeln(w, descStyle.Render("  was "+describeStandard(&u.OldStandard)))
	if u.Version != "" {
		writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", u.OldVersion, u.Version)))
	}
	if notice := standardChangeNotice(&u.OldStandard, &u.Standard, u.Notes); notice != "" {
		writeln(w, valueStyle.Render("  "+notice))
	}
	return nil
}

func newEntityStandardsUpdateCmd() *cobra.Command {
	var (
		edit    standardEdit
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "update <EUID>",
		Short: "Edit a standard an entity declares",
		Long: `Change the relation, version, url, realm or profile of a Standard Reference the
entity declares, in place, so the ` + "`standard`" + ` notes mapping it keep resolving. The entity's rule_set
version is patch-bumped and its timestamp and hash refreshed, along with those of
the entities containing it.

A new version names another edition: the notes that resolve to the reference then
map clauses of that edition (STDREF-011), and are listed.

--realm links the reference to a Standard Realm (STDREF-017) and --profile limits
it to one of that Realm's profiles (STDREF-019); --clear-realm and --clear-profile
remove them. A profile needs a realm.

Examples:
  strspc entity standards update API --id owasp.asvs --version 5.0.0
  strspc entity standards update KWRD --id ietf.rfc2119 --relation implements --clear-url
  strspc entity standards update API --id owasp.asvs --realm dev.steerspec.std.owasp.asvs.v5`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := edit.check(cmd); err != nil {
				return err
			}
			target, err := resolveEntityTarget(dir, args[0])
			if err != nil {
				return err
			}
			if err := edit.checkLink(dir, target.path); err != nil {
				return err
			}
			u := standardUpdate{Entity: args[0], Path: target.path}
			err = modifyEntity(target, func(f *entity.File) error {
				refs, before, after, err := edit.apply(cmd, f.Entity.Standards)
				if err != nil {
					return err
				}
				f.Entity.Standards = refs
				u.Standard, u.OldStandard = after, before
				if before.Version != after.Version {
					u.Notes = entityNotesUsing(f, edit.id)
				}
				change := entityStandardChange{}
				if err := bumpEntity(f, &change); err != nil {
					return err
				}
				u.Version, u.OldVersion = change.Version, change.OldVersion
				return nil
			})
			if err != nil {
				return err
			}
			return writeStandardUpdate(cmd, jsonOut, coverageCell(u.Entity), &u)
		},
	}
	edit.register(cmd)
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmStandardsUpdateCmd() *cobra.Command {
	var (
		edit    standardEdit
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Edit a standard a Realm declares",
		Long: `Change the relation, version, url, realm or profile of a Standard Reference in
realm.json, in place, so the ` + "`standard`" + ` notes mapping it keep resolving.

A new version names another edition: the notes that resolve to the reference then
map clauses of that edition (STDREF-011), and are listed.

--realm links the reference to a Standard Realm (STDREF-017) and --profile limits
it to one of that Realm's profiles (STDREF-019); --clear-realm and --clear-profile
remove them. A profile needs a realm.

Examples:
  strspc realm standards update --id owasp.asvs --version 5.0.0
  strspc realm standards update --id owasp.asvs --realm dev.steerspec.std.owasp.asvs.v5 --profile L1`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := edit.check(cmd); err != nil {
				return err
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			if err := edit.checkLink(dir, realmPath); err != nil {
				return err
			}
			u := standardUpdate{Path: realmPath}
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				refs, before, after, err := edit.apply(cmd, rf.Realm.Standards)
				if err != nil {
					return err
				}
				if before.Realm != "" && after.Realm == "" {
					if err := checkNoExclusionNames(rf, edit.id, "unlink"); err != nil {
						return err
					}
				}
				rf.Realm.Standards = refs
				u.Realm, u.Standard, u.OldStandard = rf.Realm.ID, after, before
				if before.Version != after.Version {
					u.Notes = realmNotesUsing(dir, rf, edit.id)
				}
				return nil
			})
			if err != nil {
				return err
			}
			return writeStandardUpdate(cmd, jsonOut, "Realm "+printableRealmID(u.Realm), &u)
		},
	}
	edit.register(cmd)
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}
