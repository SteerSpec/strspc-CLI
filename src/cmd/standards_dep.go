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
	if parent := entity.ParentRealm(dir, rf.Realm.ID); parent != nil {
		if abs, err := filepath.Abs(dir); err == nil {
			tree = append(tree, treeRealm{filepath.Dir(abs), parent})
		}
	}
	for _, name := range rf.SubRealms {
		if !filepath.IsLocal(name) || strings.ContainsAny(name, `/\`) {
			continue // RM008
		}
		subDir := filepath.Join(dir, name)
		if sub, err := entity.LoadRealm(filepath.Join(subDir, "realm.json")); err == nil {
			tree = append(tree, treeRealm{subDir, sub})
		}
	}
	return tree
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
// dir while a Standard Reference of the tree links to it and no other Realm
// of the tree declares it: the references would be left naming no
// dependency (RM026, STDREF-017). It names those references.
func checkDepUnlinked(dir, realmID string) error {
	tree := standardScope(dir)
	if tree == nil {
		return nil
	}
	for _, r := range tree[1:] {
		if declaresDep(r.rf, realmID) {
			return nil // the references still find it there
		}
	}
	var linked []string
	name := func(path string) string {
		if rel, err := filepath.Rel(dir, path); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(rel)
		}
		return path
	}
	for _, r := range tree {
		for _, ref := range r.rf.Realm.Standards {
			if ref.Realm == realmID {
				linked = append(linked, fmt.Sprintf("%s (standard %s)", name(filepath.Join(r.dir, "realm.json")), ref.ID))
			}
		}
	}
	// The entities of the whole tree: from the parent when dir is a
	// sub-realm, which walks dir and its siblings too.
	walkDir, walkRF := tree[0].dir, tree[0].rf
	if len(tree) > 1 && entity.ParentRealm(dir, tree[0].rf.Realm.ID) != nil {
		walkDir, walkRF = tree[1].dir, tree[1].rf
	}
	rulelint.WalkRealmStandards(walkDir, walkRF, func(path string, ef *entity.File, _ [][]entity.StandardRef) {
		for _, ref := range ef.Entity.Standards {
			if ref.Realm == realmID {
				linked = append(linked, fmt.Sprintf("%s (standard %s on %s)", name(path), ref.ID, ef.Entity.ID))
			}
		}
	})
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
