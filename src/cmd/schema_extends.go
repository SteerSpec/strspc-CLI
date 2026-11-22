package cmd

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
)

// When one source reaches several copies of a realm, a rule an `extends` note
// names must be live in all of them or in none (SEC-004). checkRule covers a
// target the resolver returned. It leaves out one that isn't live in the copy
// it read, so that direction is found through the notes.

// extendsRefRe is realmresolve's ruleRefRe, which it doesn't export: group 1
// is the realm ID, group 2 the EUID and group 3 the rule number. As there, it
// is matched on the whole note, text in backticks included.
var extendsRefRe = regexp.MustCompile(`\b(?:([a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*):)?([A-Za-z0-9]{3,18})-(\d{3})\b`)

// checkExtendsCopies reports errDifferingCopies for a rule that an `extends`
// note of a source's tree, or of an entity that tree inherits, names and that
// is live in some copies of its realm and not in others.
func (r *checkRun) checkExtendsCopies(v *schemaValidator) error {
	for _, source := range slices.Sorted(maps.Keys(r.realmTrees)) {
		t := r.realmTrees[source]
		if t.resolved == nil {
			continue
		}
		for _, c := range t.resolved.Entities {
			for _, hop := range append([]inherit.Hop{c.Entity}, c.Ancestors...) {
				if err := v.checkHopExtends(source, &hop); err != nil {
					return fmt.Errorf("checking the extends notes of %s: %w", hop.Entity.Entity.ID, err)
				}
			}
		}
	}
	return nil
}

func (v *schemaValidator) checkHopExtends(source string, hop *inherit.Hop) error {
	for _, n := range hop.Entity.Notes {
		if n.Type != "extends" {
			continue
		}
		for _, m := range extendsRefRe.FindAllStringSubmatch(n.Content, -1) {
			realm, euid, id := m[1], m[2], m[2]+"-"+m[3]
			if realm == "" {
				realm = hop.RealmID
			}
			copies, ok := v.namedCopies(source, hop.RealmID, realm)
			if !ok {
				continue // the resolver can't take it for a reference
			}
			live := func(c realmCopy, f *entity.File) bool {
				i := slices.IndexFunc(f.Rules, func(rule entity.Rule) bool { return rule.ID == id })
				return i >= 0 && c.live(&f.Rules[i])
			}
			// errNotValidated here is only a target that is live in no copy.
			if err := matchInEveryCopy(copies, euid, live); errors.Is(err, errDifferingCopies) {
				return fmt.Errorf("rule %s, named by an extends note of %s and live in only some copies, %w", m[0], n.RuleRef, err)
			}
		}
	}
	return nil
}
