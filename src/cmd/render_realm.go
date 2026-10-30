package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/render"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// ruleIDFormatResolver finds the rule_identifier_format that applies to an
// entity file and caches the resulting formatter per directory.
type ruleIDFormatResolver struct {
	cache map[string]render.RuleIDFormatter
}

func newRuleIDFormatResolver() *ruleIDFormatResolver {
	return &ruleIDFormatResolver{cache: map[string]render.RuleIDFormatter{}}
}

// formatterFor returns the formatter for the realm that owns entityPath. A
// file outside any realm keeps the RLIFRMT defaults (default formatter).
func (r *ruleIDFormatResolver) formatterFor(entityPath string) (render.RuleIDFormatter, error) {
	dir, err := filepath.Abs(filepath.Dir(entityPath))
	if err != nil {
		return render.RuleIDFormatter{}, fmt.Errorf("resolving %s: %w", entityPath, err)
	}
	if f, ok := r.cache[dir]; ok {
		return f, nil
	}

	rf, err := owningRealm(dir)
	if err != nil {
		return render.RuleIDFormatter{}, err
	}
	var fmtSpec *entity.RuleIdentifierFormat
	if rf != nil {
		fmtSpec = rf.RuleIdentifierFormat
	}
	f, err := render.NewRuleIDFormatter(fmtSpec)
	if err != nil {
		return render.RuleIDFormatter{}, fmt.Errorf("rule_identifier_format of realm %s: %w", realmLabel(rf), err)
	}
	r.cache[dir] = f
	return f, nil
}

func realmLabel(rf *entity.RealmFile) string {
	if rf == nil {
		return "(none)"
	}
	return rf.Realm.ID
}

// owningRealm walks up from dir to the nearest realm.json. When that realm is a
// sub-realm (its ID extends the ID of a realm.json further up), the outermost
// such ancestor is returned: a sub-realm leaves the format null and its
// entities use the parent's (RLM-015). Returns nil when no realm.json exists.
func owningRealm(dir string) (*entity.RealmFile, error) {
	chain, err := realmChain(dir)
	if err != nil || len(chain) == 0 {
		return nil, err
	}
	return chain[len(chain)-1].RealmFile, nil
}

// locatedRealm is a realm.json with the directory holding it.
type locatedRealm struct {
	*entity.RealmFile
	dir string
}

// realmChain walks up from dir and returns the nearest realm.json followed by
// each enclosing realm it is a sub-realm of (its ID extends the next one's),
// nearest first. Returns an empty chain when no realm.json exists.
func realmChain(dir string) ([]locatedRealm, error) {
	var chain []locatedRealm
	for d := dir; ; {
		p := filepath.Join(d, "realm.json")
		info, statErr := os.Stat(p)
		switch {
		case statErr != nil && !os.IsNotExist(statErr):
			return nil, fmt.Errorf("accessing %s: %w", p, statErr)
		case statErr == nil && !info.Mode().IsRegular():
			return nil, fmt.Errorf("%s is not a regular file", p)
		case statErr == nil:
			rf, loadErr := entity.LoadRealm(p)
			if loadErr != nil {
				return nil, fmt.Errorf("loading %s: %w", p, loadErr)
			}
			if n := len(chain); n > 0 && !strings.HasPrefix(chain[n-1].Realm.ID, rf.Realm.ID+".") {
				return chain, nil
			}
			chain = append(chain, locatedRealm{RealmFile: rf, dir: d})
		}
		parent := filepath.Dir(d)
		if parent == d {
			return chain, nil
		}
		d = parent
	}
}

// inheritedRule is one entry of the `inherited` array of `render --json`.
type inheritedRule struct {
	ID           string `json:"id"`
	Body         string `json:"body"`
	State        string `json:"state"`
	Owner        string `json:"owner"`
	OwnerRealm   string `json:"owner_realm"`
	InheritedBy  string `json:"inherited_by"`
	OverriddenBy string `json:"overridden_by,omitempty"`
}

// chainResolver finds the inheritance chains of rendered entities, running
// realm-resolve once per realm. Only a realm with a rendered entity that
// declares `inherits` is resolved, so that rendering any other file stays
// offline. A chain that cannot be resolved is a warning: rendering is
// documentation, the gate is `strspc check`.
type chainResolver struct {
	ctx   context.Context
	errW  io.Writer
	realm map[string]map[string]render.Chain // by realm directory
}

func newChainResolver(ctx context.Context, errW io.Writer) *chainResolver {
	return &chainResolver{ctx: ctx, errW: errW, realm: map[string]map[string]render.Chain{}}
}

// chainsFor returns the chains of the realm that owns the entity file at
// path, keyed by EUID, and the directory of that realm. Both are empty when
// the file declares no `inherits`.
func (c *chainResolver) chainsFor(path string, ef *entity.File) (realmDir string, chains map[string]render.Chain, err error) {
	if !usesInherits(ef) {
		return "", nil, nil
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return "", nil, fmt.Errorf("resolving %s: %w", path, err)
	}
	realms, err := realmChain(dir)
	if err != nil {
		return "", nil, err
	}
	if len(realms) > 0 {
		root := realms[len(realms)-1]
		realmDir = root.dir
		if chains, err = c.resolve(root); err != nil {
			return "", nil, err
		}
	}
	c.warnUnresolved(path, ef, chains)
	return realmDir, chains, nil
}

func (c *chainResolver) resolve(root locatedRealm) (map[string]render.Chain, error) {
	if chains, ok := c.realm[root.dir]; ok {
		return chains, nil
	}
	cacheDir, err := filepath.Abs(filepath.Join(".strspc", "cache"))
	if err != nil {
		return nil, fmt.Errorf("resolving cache directory: %w", err)
	}
	out, res := realmresolve.New(realmresolve.WithCacheDir(cacheDir)).Resolve(c.ctx, root.RealmFile, root.dir)
	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			_, _ = fmt.Fprintf(c.errW, "warning: realm %s: %s: %s\n", root.Realm.ID, d.Code, d.Message)
		}
	}
	chains := make(map[string]render.Chain, len(out.Inheritance))
	for _, in := range out.Inheritance {
		chains[in.Entity.Entity.Entity.ID] = render.Chain{Entity: in.Entity, Ancestors: in.Ancestors}
	}
	c.realm[root.dir] = chains
	return chains, nil
}

func (c *chainResolver) warnUnresolved(path string, f *entity.File, chains map[string]render.Chain) {
	if _, ok := chains[f.Entity.ID]; !ok && f.Entity.Inherits != "" {
		_, _ = fmt.Fprintf(c.errW, "warning: %s: rendering %s without the rules it inherits: %q did not resolve\n", path, f.Entity.ID, f.Entity.Inherits)
	}
	for i := range f.SubEntities {
		c.warnUnresolved(path, &f.SubEntities[i], chains)
	}
}

// inheritedRows lists the rules that f and its sub-entities inherit. The
// result is nil only when none of them has a chain.
func inheritedRows(f *entity.File, chains map[string]render.Chain) []inheritedRule {
	var rows []inheritedRule
	if c, ok := chains[f.Entity.ID]; ok {
		replacedBy := make(map[string]string)
		effective := inherit.Effective(c.Entity, c.Ancestors, nil)
		for i := range effective {
			if r := &effective[i]; r.InheritedBy == "" && r.Overrides != "" {
				replacedBy[r.Overrides] = r.Rule.ID
			}
		}
		inherited := inherit.Inherited(c.Ancestors)
		rows = make([]inheritedRule, 0, len(inherited))
		for i := range inherited {
			r := &inherited[i]
			rows = append(rows, inheritedRule{
				ID:           r.QualifiedID(),
				Body:         r.Rule.Body,
				State:        r.Rule.State,
				Owner:        r.Owner,
				OwnerRealm:   r.OwnerRealm,
				InheritedBy:  f.Entity.ID,
				OverriddenBy: replacedBy[r.QualifiedID()],
			})
		}
	}
	for i := range f.SubEntities {
		if sub := inheritedRows(&f.SubEntities[i], chains); rows == nil {
			rows = sub
		} else {
			rows = append(rows, sub...)
		}
	}
	return rows
}
