package cmd

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// The resolvers read and parse rule files themselves and don't hand over the
// bytes, so SEC-004 is enforced on what they return: check validates every
// file they can have read, then requires each entity, rule and manifest it is
// about to use to equal one parsed from validated bytes.

// errNotValidated reports content that is about to be evaluated and doesn't
// match anything that passed schema validation.
var errNotValidated = errors.New("is not what was validated against its schema; " +
	"if a rule file changed during the run, run it again")

// depRealm is a dependency whose tree check validates, with the reference its
// relative sources are rebased on when it was fetched from GitHub.
type depRealm struct {
	realm  *realmresolve.ResolvedRealm
	origin *remote.Ref
}

// sourceDependencies returns the dependencies of a source realm and of its
// sub-realms.
func sourceDependencies(source string, t *realmTree) []depRealm {
	var origin *remote.Ref
	if strings.HasPrefix(source, remote.Scheme) {
		if ref, err := remote.ParseGitHub(source); err == nil {
			origin = &ref
		}
	}
	var out []depRealm
	add := func(deps []*realmresolve.ResolvedRealm, decls []entity.RealmDep, base *remote.Ref) {
		for _, d := range deps {
			var src string
			if i := slices.IndexFunc(decls, func(x entity.RealmDep) bool { return x.RealmID == d.Realm.Realm.ID }); i >= 0 {
				src = decls[i].Source
			}
			out = append(out, depRealm{d, dependencyOrigin(base, src)})
		}
	}
	add(t.resolved.Dependencies, t.manifest.Dependencies, origin)
	for _, sub := range t.resolved.SubRealms {
		add(sub.Dependencies, sub.Realm.Dependencies, dependencyOrigin(origin, filepath.Base(sub.Dir)))
	}
	return out
}

// dependencyOrigin returns the GitHub reference of the directory that source
// names from a realm at origin, or nil for a local one.
func dependencyOrigin(origin *remote.Ref, source string) *remote.Ref {
	if strings.HasPrefix(source, remote.Scheme) {
		if ref, err := remote.ParseGitHub(source); err == nil {
			return &ref
		}
		return nil
	}
	if origin == nil || source == "" || filepath.IsAbs(source) {
		return nil
	}
	o := *origin
	o.Path = path.Join(origin.Path, filepath.ToSlash(source))
	if o.Path == ".." || strings.HasPrefix(o.Path, "../") {
		return nil // outside the repository
	}
	if o.Path == "." {
		o.Path = ""
	}
	return &o
}

// validateDependencies validates the tree of each realm in queue, and of each
// of their own dependencies that an `inherits` or an `extends` note in them
// names: the resolver loads those on demand and doesn't report where from.
// One that can't be resolved is left to checkConsumed, which refuses whatever
// was read from it.
func (r *checkRun) validateDependencies(v *schemaValidator, queue []depRealm) error {
	cacheDir := filepath.Join(r.cwd, ".strspc", "cache")
	seen := map[string]bool{}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		walked := seen[d.realm.Dir]
		seen[d.realm.Dir] = true
		if !walked {
			if err := v.validateRealmTree(d.realm.Dir); err != nil {
				return fmt.Errorf("validating a dependency: %w", err)
			}
		}
		// Each time: two sources can have read the manifest at different times.
		if err := v.checkManifest(d.realm.Dir, d.realm.Realm, nil); err != nil {
			return fmt.Errorf("checking a dependency: %w", err)
		}
		if walked {
			continue
		}
		if err := v.indexRealm(d.realm.Dir, d.realm.Realm); err != nil {
			return fmt.Errorf("indexing a dependency: %w", err)
		}
		var needed []entity.RealmDep
		for _, decl := range d.realm.Realm.Dependencies {
			if refersTo(d.realm.Entities, decl.RealmID) {
				needed = append(needed, decl)
			}
		}
		if len(needed) == 0 {
			continue
		}
		// All at once: each Resolve walks the whole realm.
		only := *d.realm.Realm
		only.Dependencies, only.SubRealms = needed, nil
		out, _ := r.realmResolver(cacheDir, d.origin).Resolve(r.ctx, &only, d.realm.Dir)
		if err := r.ctx.Err(); err != nil {
			return fmt.Errorf("resolving further dependencies: %w", err)
		}
		for _, next := range out.Dependencies {
			var src string
			if i := slices.IndexFunc(needed, func(x entity.RealmDep) bool { return x.RealmID == next.Realm.Realm.ID }); i >= 0 {
				src = needed[i].Source
			}
			queue = append(queue, depRealm{next, dependencyOrigin(d.origin, src)})
		}
	}
	return nil
}

// refersTo reports whether an entity inherits from, or extends a rule of,
// realmID or one of its sub-realms. It may say yes too often, never too rarely.
func refersTo(entities map[string]*entity.File, realmID string) bool {
	names := func(s string) bool {
		return strings.Contains(s, realmID+":") || strings.Contains(s, realmID+".")
	}
	for _, e := range entities {
		if names(e.Entity.Inherits) {
			return true
		}
		for _, n := range e.Notes {
			if n.Type == "extends" && names(n.Content) {
				return true
			}
		}
	}
	return false
}

// validateGoverning validates each manifest that chose a schema: the nearest
// one of an entity or the manifest itself, and those above that it inherits a
// pin from, listed or not. Each must be the manifest the choice was made
// from. Validating one can bring in those above it, so it goes on until none
// is left. Call it once everything else has been validated.
func (v *schemaValidator) validateGoverning() error {
	checked := map[string]bool{}
	for len(checked) < len(v.governing) {
		for _, dir := range slices.Sorted(maps.Keys(v.governing)) {
			if checked[dir] {
				continue
			}
			checked[dir] = true
			if err := v.validateManifestIn(dir); err != nil {
				return fmt.Errorf("validating a governing manifest: %w", err)
			}
			chose := v.governing[dir]
			if chose == nil {
				return fmt.Errorf("%s %w", filepath.Join(dir, "realm.json"), errNotValidated)
			}
			if err := v.checkManifest(dir, chose, nil); err != nil {
				return fmt.Errorf("checking a governing manifest: %w", err)
			}
		}
	}
	return nil
}

// realmEntity names an entity as a reference does: by a name of the realm
// tree that holds it, and its EUID. Content is compared within one realm, so
// an unchanged copy in another can't vouch for what changed in this one.
type realmEntity struct{ realm, euid string }

// indexRealm records the validated entities below dir as those of the realm
// tree rf roots. realmresolve takes a reference qualified with the root's ID
// or with that of a sub-realm it lists to mean any entity of the tree.
func (v *schemaValidator) indexRealm(dir string, rf *entity.RealmFile) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", dir, err)
	}
	names := []string{rf.Realm.ID}
	for _, sub := range rf.SubRealms {
		names = append(names, rf.Realm.ID+"."+sub)
	}
	var add func(*entity.File)
	add = func(f *entity.File) {
		for _, n := range names {
			k := realmEntity{n, f.Entity.ID}
			v.entities[k] = append(v.entities[k], f)
		}
		for i := range f.SubEntities {
			add(&f.SubEntities[i])
		}
	}
	for p, f := range v.parsed {
		if rel, err := filepath.Rel(abs, p); err == nil && filepath.IsLocal(rel) {
			add(f)
		}
	}
	return nil
}

// checkConsumed reports an error for anything the evaluation uses that
// doesn't equal what was validated: each resolved file against the same
// path, the entities of each inheritance chain and the rules `extends` notes
// name against the same realm.
func (r *checkRun) checkConsumed(v *schemaValidator) error {
	for _, rf := range r.resolved {
		abs, err := filepath.Abs(rf.Path)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", rf.Path, err)
		}
		if ok := v.parsed[abs]; ok == nil || !reflect.DeepEqual(ok, rf.File) {
			return fmt.Errorf("%s: entity %s %w", rf.Path, rf.File.Entity.ID, errNotValidated)
		}
	}
	for _, c := range r.chains {
		for _, hop := range append([]inherit.Hop{c.Entity}, c.Ancestors...) {
			if err := v.checkEntity(hop.RealmID, hop.Entity); err != nil {
				return fmt.Errorf("checking an inheritance chain: %w", err)
			}
		}
	}
	for _, bySource := range r.extended {
		for key, targets := range bySource {
			// The realm of the note, which an unqualified reference is in.
			from, _, _ := strings.Cut(key, ":")
			for _, t := range targets {
				if !v.hasRule(from, t) {
					return fmt.Errorf("rule %s, named by an extends note, %w", t.ID, errNotValidated)
				}
			}
		}
	}
	return nil
}

func (v *schemaValidator) checkEntity(realm string, f *entity.File) error {
	same := func(ok *entity.File) bool { return reflect.DeepEqual(ok, f) }
	if slices.ContainsFunc(v.entities[realmEntity{realm, f.Entity.ID}], same) {
		return nil
	}
	return fmt.Errorf("entity %s of %s %w", f.Entity.ID, realm, errNotValidated)
}

// hasRule reports whether t, named by a note in the realm from, is a
// validated rule of the realm the note means.
func (v *schemaValidator) hasRule(from string, t inherit.Extended) bool {
	realm, id := from, t.ID
	if q, rest, qualified := strings.Cut(t.ID, ":"); qualified {
		realm, id = q, rest
	}
	i := strings.LastIndex(id, "-")
	if i < 0 {
		return false
	}
	for _, f := range v.entities[realmEntity{realm, id[:i]}] {
		for j := range f.Rules {
			rule := &f.Rules[j]
			if rule.ID == id && rule.Revision == t.Revision && rule.Body == t.Body {
				return true
			}
		}
	}
	return false
}

// checkManifest reports an error when used, the resolver's manifest of the
// realm in dir, doesn't equal the validated one. For a sub-realm, parent is
// the realm listing it: the resolver replaces a sub-realm's dependencies with
// the effective list, so that is what the validated one must come to.
func (v *schemaValidator) checkManifest(dir string, used, parent *entity.RealmFile) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("can't validate %s: %w", dir, err)
	}
	if ok := v.manifests[abs]; ok != nil {
		want := *ok
		if parent != nil {
			want.Dependencies = effectiveDeps(parent.Dependencies, ok.Dependencies)
		}
		if reflect.DeepEqual(want, *used) {
			return nil
		}
	}
	return fmt.Errorf("%s %w", filepath.Join(dir, "realm.json"), errNotValidated)
}

// effectiveDeps is the dependency list realmresolve gives a sub-realm: those
// of its parent that it doesn't declare itself, then its own.
func effectiveDeps(parent, own []entity.RealmDep) []entity.RealmDep {
	out := make([]entity.RealmDep, 0, len(parent)+len(own))
	for _, d := range parent {
		if !slices.ContainsFunc(own, func(o entity.RealmDep) bool { return o.RealmID == d.RealmID }) {
			out = append(out, d)
		}
	}
	return append(out, own...)
}
