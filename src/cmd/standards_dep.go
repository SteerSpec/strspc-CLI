package cmd

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// treeRealm is one Realm of a Realm tree and the directory holding it.
type treeRealm struct {
	dir string
	rf  *entity.RealmFile
}

// subRealmsOf returns the sub-realms rf, the Realm at dir, declares and that
// can be read, skipping a name realm validate rejects (RM008) or a manifest
// it can't load (RM009), as validate does.
func subRealmsOf(dir string, rf *entity.RealmFile) []treeRealm {
	var subs []treeRealm
	for _, name := range rf.SubRealms {
		if !isSubRealmName(name) {
			continue // RM008
		}
		subDir := filepath.Join(dir, name)
		if sub, err := entity.LoadRealm(filepath.Join(subDir, "realm.json")); err == nil {
			subs = append(subs, treeRealm{subDir, sub})
		}
	}
	return subs
}

// parentOf returns the Realm whose sub-realm the Realm rf at dir is, or nil.
func parentOf(dir string, rf *entity.RealmFile) *treeRealm {
	parent := entity.ParentRealm(dir, rf.Realm.ID)
	if parent == nil {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	return &treeRealm{filepath.Dir(abs), parent}
}

// standardScope returns the Realms whose dependencies the `realm` of a
// Standard Reference written under dir may name, as realm validate run on dir
// judges it (RM026, STDREF-017): the Realm at dir, its parent when dir is a
// sub-realm, and its sub-realms. It returns nil when dir holds no realm.json
// that can be read, which leaves nothing to validate the reference against.
func standardScope(dir string) []treeRealm {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return nil
	}
	tree := []treeRealm{{dir, rf}}
	if parent := parentOf(dir, rf); parent != nil {
		tree = append(tree, *parent)
	}
	return append(tree, subRealmsOf(dir, rf)...)
}

// declaresDep reports whether rf declares a dependency on realmID.
func declaresDep(rf *entity.RealmFile, realmID string) bool {
	return slices.ContainsFunc(rf.Dependencies, func(d entity.RealmDep) bool { return d.RealmID == realmID })
}

// checkStandardRealmDeclared refuses a Standard Reference linked to realmID
// when no Realm of the tree at dir declares it as a dependency: realm
// validate would reject it (RM026, STDREF-017). Whether that dependency is a
// Standard Realm needs it fetched, so validate checks that. No realmID, or
// no realm.json at dir, needs no check.
func checkStandardRealmDeclared(dir, realmID string) error {
	if realmID == "" {
		return nil
	}
	tree := standardScope(dir)
	if tree == nil {
		return nil
	}
	for _, r := range tree {
		if declaresDep(r.rf, realmID) {
			return nil
		}
	}
	return fmt.Errorf("realm %s is not a dependency of the Realm tree, so realm validate would reject the link (STDREF-017, RM026): "+
		"add it first with `strspc realm dep add %s@<version> --dir %s`",
		printableRealmID(realmID), printableRealmID(realmID), coverageCell(shellQuote(dir)))
}

// checkDepUnlinked refuses to remove the dependency realmID from the Realm at
// dir while a Standard Reference would be left naming no dependency (RM026,
// STDREF-017), and names those references. It judges them as realm validate
// does, from both Realms it can be run on:
//
//   - from dir: the references of dir and its sub-realms, which find their
//     realm in dir, its parent or its sub-realms;
//   - from dir's parent, when dir is a sub-realm: every reference of the
//     parent's tree, which find their realm in the parent, its own parent or
//     its sub-realms.
//
// A scope where another Realm declares realmID too loses nothing. A Realm
// that doesn't declare realmID has nothing to remove, which the command
// reports itself.
func checkDepUnlinked(dir, realmID string) error {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil || !declaresDep(rf, realmID) {
		return nil
	}
	self := treeRealm{dir, rf}
	parent := parentOf(dir, rf)
	selfAbs, _ := filepath.Abs(dir)
	// declaredElsewhere reports whether a Realm of scope other than dir
	// declares realmID.
	declaredElsewhere := func(scope []treeRealm) bool {
		return slices.ContainsFunc(scope, func(r treeRealm) bool {
			abs, _ := filepath.Abs(r.dir)
			return abs != selfAbs && declaresDep(r.rf, realmID)
		})
	}

	var linked []string
	seen := map[string]bool{}
	note := func(s string) {
		if !seen[s] {
			seen[s] = true
			linked = append(linked, s)
		}
	}
	// name gives path relative to dir, so a reference found from dir and
	// from its parent reads the same.
	name := func(path string) string {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		if rel, err := filepath.Rel(selfAbs, abs); err == nil {
			return filepath.ToSlash(rel)
		}
		return abs
	}
	// collect notes the references of root and the Realms below it.
	collect := func(root treeRealm, realms []treeRealm) {
		for _, r := range realms {
			for _, ref := range r.rf.Realm.Standards {
				if ref.Realm == realmID {
					note(fmt.Sprintf("%s (standard %s)", name(filepath.Join(r.dir, "realm.json")), ref.ID))
				}
			}
		}
		rulelint.WalkRealmStandards(root.dir, root.rf, func(path string, ef *entity.File, _ [][]entity.StandardRef) {
			for _, ref := range ef.Entity.Standards {
				if ref.Realm == realmID {
					note(fmt.Sprintf("%s (standard %s on %s)", name(path), ref.ID, ef.Entity.ID))
				}
			}
		})
	}

	own := append([]treeRealm{self}, subRealmsOf(dir, rf)...)
	scope := own
	if parent != nil {
		scope = append(slices.Clone(own), *parent)
	}
	if !declaredElsewhere(scope) {
		collect(self, own)
	}
	if parent != nil {
		// Validated from the parent: its references and its sub-realms' find
		// their realm in the parent's scope, its own parent included.
		if !declaredElsewhere(standardScope(parent.dir)) {
			collect(*parent, append([]treeRealm{*parent}, subRealmsOf(parent.dir, parent.rf)...))
		}
	}
	if len(linked) == 0 {
		return nil
	}
	const shown = 5
	list := linked
	if len(list) > shown {
		list = append(slices.Clone(list[:shown]), fmt.Sprintf("and %d more", len(linked)-shown))
	}
	return fmt.Errorf("dependency %s is still linked by %d Standard Reference(s) of the Realm tree (STDREF-017): %s; "+
		"remove them, or clear their realm with `standards update --clear-realm`, first",
		printableRealmID(realmID), len(linked), coverageCell(strings.Join(list, ", ")))
}
