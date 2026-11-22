package cmd

import (
	"errors"
	"fmt"
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
		if seen[d.realm.Dir] {
			continue
		}
		seen[d.realm.Dir] = true
		if err := v.validateRealmTree(d.realm.Dir); err != nil {
			return err
		}
		if err := v.checkManifest(d.realm.Dir, d.realm.Realm, true); err != nil {
			return err
		}
		for _, decl := range d.realm.Realm.Dependencies {
			if !refersTo(d.realm.Entities, decl.RealmID) {
				continue
			}
			only := *d.realm.Realm
			only.Dependencies, only.SubRealms = []entity.RealmDep{decl}, nil
			out, _ := r.realmResolver(cacheDir, d.origin).Resolve(r.ctx, &only, d.realm.Dir)
			if err := r.ctx.Err(); err != nil {
				return err
			}
			for _, next := range out.Dependencies {
				queue = append(queue, depRealm{next, dependencyOrigin(d.origin, decl.Source)})
			}
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

// index records an entity parsed from validated bytes, and its sub-entities.
func (v *schemaValidator) index(f *entity.File) {
	v.entities[f.Entity.ID] = append(v.entities[f.Entity.ID], f)
	for i := range f.SubEntities {
		v.index(&f.SubEntities[i])
	}
}

// checkConsumed reports an error for anything the evaluation uses that
// doesn't equal what was validated: the resolved files, the entities of each
// inheritance chain, and the rules `extends` notes name.
func (r *checkRun) checkConsumed(v *schemaValidator) error {
	for _, rf := range r.resolved {
		if err := v.checkEntity(rf.File); err != nil {
			return fmt.Errorf("%s: %w", rf.Path, err)
		}
	}
	for _, c := range r.chains {
		for _, hop := range append([]inherit.Hop{c.Entity}, c.Ancestors...) {
			if err := v.checkEntity(hop.Entity); err != nil {
				return err
			}
		}
	}
	for _, bySource := range r.extended {
		for _, targets := range bySource {
			for _, t := range targets {
				if !v.hasRule(t) {
					return fmt.Errorf("rule %s, named by an extends note, %w", t.ID, errNotValidated)
				}
			}
		}
	}
	return nil
}

func (v *schemaValidator) checkEntity(f *entity.File) error {
	if slices.ContainsFunc(v.entities[f.Entity.ID], func(ok *entity.File) bool { return reflect.DeepEqual(ok, f) }) {
		return nil
	}
	return fmt.Errorf("entity %s %w", f.Entity.ID, errNotValidated)
}

func (v *schemaValidator) hasRule(t inherit.Extended) bool {
	id := t.ID[strings.LastIndex(t.ID, ":")+1:] // the note may qualify it
	i := strings.LastIndex(id, "-")
	if i < 0 {
		return false
	}
	for _, f := range v.entities[id[:i]] {
		for i := range f.Rules {
			rule := &f.Rules[i]
			if rule.ID == id && rule.Revision == t.Revision && rule.Body == t.Body {
				return true
			}
		}
	}
	return false
}

// checkManifest reports an error when used, the resolver's manifest of the
// realm in dir, doesn't equal the validated one. withDeps is false for a
// sub-realm, whose dependencies the resolver merges with its parent's.
func (v *schemaValidator) checkManifest(dir string, used *entity.RealmFile, withDeps bool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("can't validate %s: %w", dir, err)
	}
	if ok := v.manifests[abs]; ok != nil {
		a, b := *ok, *used
		if !withDeps {
			a.Dependencies, b.Dependencies = nil, nil
		}
		if reflect.DeepEqual(a, b) {
			return nil
		}
	}
	return fmt.Errorf("%s %w", filepath.Join(dir, "realm.json"), errNotValidated)
}
