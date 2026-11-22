package cmd

import (
	"errors"
	"fmt"
	"maps"
	"math"
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
var errDifferingCopies = errors.New("has copies that differ in realms of the same ID, " +
	"so the one evaluated can't be matched to the one validated; use one version of that realm in the source")

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
	origin := dependencyOrigin(nil, source)
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
func (r *checkRun) validateDependencies(v *schemaValidator, source string, queue []depRealm) error {
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
		if err := v.indexRealm(source, d.realm.Dir, d.realm.Realm); err != nil {
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

// checkResolutionCurrent resolves each source again, once its files are
// validated, and reports an error unless that gives the chains and the extends
// targets that will be used. checkConsumed compares what was resolved; this
// also covers what resolution left out, and what it derived from the manifests
// it loaded on demand, such as the sub-realm IDs an override can be named by.
//
// It reads only what is cached: @latest stays on the tag the run resolved,
// however old, and nothing is downloaded after validation.
func (r *checkRun) checkResolutionCurrent() error {
	cacheDir := filepath.Join(r.cwd, ".strspc", "cache")
	for _, source := range slices.Sorted(maps.Keys(r.realmTrees)) {
		t := r.realmTrees[source]
		if t.resolved == nil {
			continue
		}
		cached := *r.fetcher
		cached.TTL, cached.Force, cached.Client = math.MaxInt64, false, offlineHTTPClient()
		// The diagnostics were reported by the first resolution.
		rr := r.realmResolver(cacheDir, dependencyOrigin(nil, source), realmresolve.WithRemoteFetcher(&cached))
		now, _ := rr.Resolve(r.ctx, t.manifest, t.dir)
		if err := r.ctx.Err(); err != nil {
			return fmt.Errorf("resolving %s again: %w", source, err)
		}
		if !reflect.DeepEqual(now.Inheritance, t.resolved.Inheritance) || !reflect.DeepEqual(now.Extended, r.extended[source]) {
			return fmt.Errorf("what %s inherits or extends %w", source, errNotValidated)
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

// realmName names a realm tree as a reference in the resolution of one
// source does. Content is compared within that, so an unchanged copy in
// another source's dependencies or in another realm can't vouch for what
// changed in this one.
//
// One source can still reach two directories with the same realm ID, two
// versions of a realm. The resolver doesn't say which it read, so what is
// used must then be in both, equal, and is refused when they differ or one
// lacks it: it may have been deleted since.
type realmName struct{ source, realm string }

// realmCopy is what was validated of one directory of a realm.
type realmCopy struct {
	root         string // the realm ID of the directory
	dependencies []entity.RealmDep
	entities     map[string][]*entity.File // by EUID
	superseded   map[string]bool           // the IDs of the rules superseded in it
}

// live reports whether rule is in force in the copy (RUL-011), judged as
// realmresolve does before it offers an extends target: neither Abandoned nor
// Terminated, nor superseded by a rule of the copy that isn't Abandoned. Draft
// and Retired rules are live.
func (c realmCopy) live(rule *entity.Rule) bool {
	return rule.State != entity.StateAbandoned && rule.State != entity.StateTerminated && !c.superseded[rule.ID]
}

// indexRealm records the validated entities below dir as a copy of the realm
// tree rf roots. realmresolve takes a reference qualified with the root's ID
// or with that of a sub-realm it lists to mean any entity of the tree.
func (v *schemaValidator) indexRealm(source, dir string, rf *entity.RealmFile) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", dir, err)
	}
	tree := realmCopy{
		root:         rf.Realm.ID,
		dependencies: rf.Dependencies,
		entities:     map[string][]*entity.File{},
		superseded:   map[string]bool{},
	}
	var add func(*entity.File)
	add = func(f *entity.File) {
		tree.entities[f.Entity.ID] = append(tree.entities[f.Entity.ID], f)
		for i := range f.Rules {
			rule := &f.Rules[i]
			if rule.Supersedes != nil && rule.State != entity.StateAbandoned {
				tree.superseded[*rule.Supersedes] = true
			}
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
	names := []string{rf.Realm.ID}
	for _, sub := range rf.SubRealms {
		names = append(names, rf.Realm.ID+"."+sub)
	}
	for _, n := range names {
		k := realmName{source, n}
		v.trees[k] = append(v.trees[k], tree)
	}
	return nil
}

// matchInEveryCopy reports an error unless each copy of the realm holds euid
// and matches accepts all it holds of it.
func (v *schemaValidator) matchInEveryCopy(source, realm, euid string, matches func(realmCopy, *entity.File) bool) error {
	copies := v.trees[realmName{source, realm}]
	matched := 0
	for _, tree := range copies {
		files := tree.entities[euid]
		if len(files) > 0 && !slices.ContainsFunc(files, func(f *entity.File) bool { return !matches(tree, f) }) {
			matched++
		}
	}
	switch {
	case matched == 0:
		return errNotValidated
	case matched < len(copies):
		return errDifferingCopies
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
	// By source, not from r.chains, which merges the sources by EUID.
	for source, t := range r.realmTrees {
		if t.resolved == nil {
			continue
		}
		for _, c := range t.resolved.Inheritance {
			for _, hop := range append([]inherit.Hop{c.Entity}, c.Ancestors...) {
				if err := v.checkEntity(source, hop.RealmID, hop.Entity); err != nil {
					return fmt.Errorf("checking an inheritance chain: %w", err)
				}
			}
		}
	}
	for source, bySource := range r.extended {
		for key, targets := range bySource {
			// The realm of the note, which an unqualified reference is in.
			from, _, _ := strings.Cut(key, ":")
			for _, t := range targets {
				if err := v.checkRule(source, from, t); err != nil {
					return fmt.Errorf("checking an extends note: %w", err)
				}
			}
		}
	}
	return nil
}

func (v *schemaValidator) checkEntity(source, realm string, f *entity.File) error {
	same := func(_ realmCopy, ok *entity.File) bool { return reflect.DeepEqual(ok, f) }
	if err := v.matchInEveryCopy(source, realm, f.Entity.ID, same); err != nil {
		return fmt.Errorf("entity %s of %s %w", f.Entity.ID, realm, err)
	}
	return nil
}

// checkRule reports an error unless t, named by a note in the realm from, is
// the validated rule of the realm the note means, and still live there: the
// resolver offers no other, and neither state nor supersession shows in t.
func (v *schemaValidator) checkRule(source, from string, t inherit.Extended) error {
	realm, id := from, t.ID
	if q, rest, qualified := strings.Cut(t.ID, ":"); qualified {
		realm, id = q, rest
	}
	i := strings.LastIndex(id, "-")
	if i < 0 {
		return fmt.Errorf("rule %s, named by an extends note, %w", t.ID, errNotValidated)
	}
	same := func(c realmCopy, f *entity.File) bool {
		// The first with the ID, which is the one the resolver reads.
		n := slices.IndexFunc(f.Rules, func(rule entity.Rule) bool { return rule.ID == id })
		return n >= 0 && f.Rules[n].Revision == t.Revision && f.Rules[n].Body == t.Body && c.live(&f.Rules[n])
	}
	if err := v.matchInEveryCopy(source, realm, id[:i], same); err != nil {
		return fmt.Errorf("rule %s, named by an extends note, %w", t.ID, err)
	}
	return nil
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
