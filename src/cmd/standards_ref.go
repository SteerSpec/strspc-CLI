package cmd

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// standardFlags holds the flags that describe one Standard Reference, shared
// by `entity standards add` and `realm standards add`.
type standardFlags struct {
	id       string
	relation string
	version  string
	url      string
	realm    string
	profile  string
}

// Help for the flags that link a Standard Reference to a Standard Realm,
// shared by add and update.
const (
	standardRealmUsage   = "realm_id of the Standard Realm the reference links to, a dependency of the Realm tree (STDREF-017); its edition is the --version (STDREF-018)"
	standardProfileUsage = "profile of the linked Standard Realm limiting the clauses (STDREF-019, STDREF-027); needs a realm"
)

func (s *standardFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.id, "id", "", "standard id, a namespaced slug such as ietf.rfc2119 (required)")
	cmd.Flags().StringVar(&s.relation, "relation", "", "relation (required): "+entity.RelationImplements+"|"+entity.RelationDerivedFrom)
	cmd.Flags().StringVar(&s.version, "version", "", "edition of the standard (optional, not empty when given)")
	cmd.Flags().StringVar(&s.url, "url", "", "absolute http(s) URL of the standard (optional)")
	cmd.Flags().StringVar(&s.realm, "realm", "", standardRealmUsage)
	cmd.Flags().StringVar(&s.profile, "profile", "", standardProfileUsage)
}

// checkStandardRealm refuses a --realm that isn't a realm ID and an empty
// --profile. Whether the Realm tree declares the realm as a dependency is
// checked against the tree (checkStandardRealmDeclared); the rest of the link
// (STDREF-017, STDREF-018, STDREF-027) needs the linked Realm, so lint checks
// it.
func checkStandardRealm(cmd *cobra.Command, realm, profile string) error {
	if cmd.Flags().Changed("realm") && !realmIDPattern.MatchString(realm) {
		return fmt.Errorf("--realm %q is not a realm ID such as dev.steerspec.std.owasp.asvs.v5", realm)
	}
	if cmd.Flags().Changed("profile") && profile == "" {
		return errors.New("--profile must not be empty")
	}
	return nil
}

// ref builds and validates the Standard Reference the flags describe
// (STDREF-002, STDREF-004, STDREF-007, STDREF-008, STDREF-019).
func (s *standardFlags) ref(cmd *cobra.Command) (entity.StandardRef, error) {
	if s.id == "" {
		return entity.StandardRef{}, errors.New("--id is required")
	}
	if s.relation == "" {
		return entity.StandardRef{}, fmt.Errorf("--relation is required (one of: %s, %s)", entity.RelationImplements, entity.RelationDerivedFrom)
	}
	if cmd.Flags().Changed("version") && s.version == "" {
		return entity.StandardRef{}, errors.New("--version must not be empty: omit it when the reference names no edition (STDREF-007)")
	}
	if err := checkStandardRealm(cmd, s.realm, s.profile); err != nil {
		return entity.StandardRef{}, err
	}
	for _, f := range [][2]string{{"--id", s.id}, {"--relation", s.relation}, {"--version", s.version}, {"--url", s.url}, {"--profile", s.profile}} {
		if err := rejectControlChars(f[0], f[1]); err != nil {
			return entity.StandardRef{}, err
		}
	}
	ref := entity.StandardRef{ID: s.id, Relation: s.relation, Version: s.version, URL: s.url, Realm: s.realm, Profile: s.profile}
	if err := entity.ValidateStandardRef(ref); err != nil {
		return entity.StandardRef{}, err
	}
	return ref, nil
}

// addStandardRef appends ref to refs, refusing an id already declared there
// (STDREF-003).
func addStandardRef(refs []entity.StandardRef, ref *entity.StandardRef) ([]entity.StandardRef, error) {
	if entity.FindStandard(refs, ref.ID) != nil {
		return nil, fmt.Errorf("standard %q is already declared (STDREF-003): remove it first to change it", ref.ID)
	}
	out := append(slices.Clone(refs), *ref)
	if errs := entity.ValidateStandardRefs(out); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// removeStandardRef drops the reference with the given id. An empty result is
// nil, so the `standards` key is left out of the file again.
func removeStandardRef(refs []entity.StandardRef, id string) (rest []entity.StandardRef, removed entity.StandardRef, err error) {
	i := slices.IndexFunc(refs, func(r entity.StandardRef) bool { return r.ID == id })
	if i < 0 {
		return nil, entity.StandardRef{}, fmt.Errorf("standard %q is not declared", id)
	}
	rest = slices.Delete(slices.Clone(refs), i, i+1)
	if len(rest) == 0 {
		rest = nil
	}
	return rest, refs[i], nil
}

// standardsOrEmpty returns refs, or an empty slice so JSON prints [] not null.
func standardsOrEmpty(refs []entity.StandardRef) []entity.StandardRef {
	if refs == nil {
		return []entity.StandardRef{}
	}
	return refs
}

// describeStandard renders one reference on one line for human output.
func describeStandard(ref *entity.StandardRef) string {
	s := ref.ID
	if ref.Version != "" {
		s += " " + ref.Version
	}
	detail := ref.Relation
	if ref.Realm != "" {
		detail += ", realm " + ref.Realm
	}
	if ref.Profile != "" {
		detail += ", profile " + ref.Profile
	}
	return coverageCell(fmt.Sprintf("%s (%s)", s, detail))
}

// writeStandardsList prints the references of one scope as a table.
func writeStandardsList(w io.Writer, scope string, refs []entity.StandardRef) {
	if len(refs) == 0 {
		writeln(w, descStyle.Render(fmt.Sprintf("%s declares no standard", scope)))
		return
	}
	// The realm and profile columns appear only when a reference links to a
	// Standard Realm, so a list without one prints as it always did.
	linked := slices.ContainsFunc(refs, func(r entity.StandardRef) bool { return r.Realm != "" || r.Profile != "" })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range refs {
		line := fmt.Sprintf("%s\t%s\t%s\t%s", coverageCell(r.ID), coverageCell(r.Relation), coverageCell(r.Version), coverageCell(r.URL))
		if linked {
			line += fmt.Sprintf("\t%s\t%s", coverageCell(r.Realm), coverageCell(r.Profile))
		}
		_, _ = fmt.Fprintln(tw, line)
	}
	_ = tw.Flush()
}

// unresolvedNotesError reports a removal that would leave `standard`
// notes resolving to nothing (NTE-016), with how to withdraw them.
func unresolvedNotesError(id string, notes []string) error {
	return fmt.Errorf("cannot remove standard %q: %d `standard` note(s) would no longer resolve (NTE-016): %s\n"+
		"withdraw each mapping first with `strspc note update <note_id> --type reference`", id, len(notes), strings.Join(notes, ", "))
}

// notesNaming returns the ids of the unresolved notes that name the standard
// id, once each. Notes failing on other ids are lint's to report and don't
// block the change.
func notesNaming(unresolved []rulelint.UnresolvedNote, id string) []string {
	var notes []string
	for _, u := range unresolved {
		if u.StandardID == id && !slices.Contains(notes, u.NoteID) {
			notes = append(notes, u.NoteID)
		}
	}
	return notes
}
