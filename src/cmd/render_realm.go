package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/render"
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
	return chain[len(chain)-1], nil
}

// realmChain walks up from dir and returns the nearest realm.json followed by
// each enclosing realm it is a sub-realm of (its ID extends the next one's),
// nearest first. Returns an empty chain when no realm.json exists.
func realmChain(dir string) ([]*entity.RealmFile, error) {
	var chain []*entity.RealmFile
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
			chain = append(chain, rf)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return chain, nil
		}
		d = parent
	}
}
