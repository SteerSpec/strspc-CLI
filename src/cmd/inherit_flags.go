package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// realmPrefix is an optional `<realm-id>:` qualifier (RLM-009). The realm-id
// grammar is the one realm.v1.schema.json sets for realm.id and strspc-manager
// realm-lint enforces, and the entity.v1 schema uses for rules[].overrides.
const realmPrefix = `([a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*:)?`

var (
	// inheritsPattern accepts a short EUID or <realm-id>:<EUID> (ENT-006/007).
	inheritsPattern = regexp.MustCompile(`^` + realmPrefix + `[a-zA-Z0-9]{3,18}$`)
	// overridesPattern mirrors the entity.v1 schema for rules[].overrides.
	overridesPattern = regexp.MustCompile(`^` + realmPrefix + `[A-Z0-9]+-\d{3}$`)
)

// validateInherits checks an --inherits value for the entity euid, created in
// the realm at dir. An empty value means the flag was not given.
func validateInherits(dir, euid, inherits string) (string, error) {
	inherits = strings.TrimSpace(inherits)
	if inherits == "" {
		return "", nil
	}
	if !inheritsPattern.MatchString(inherits) {
		return "", fmt.Errorf("invalid --inherits %q: must be an EUID or <realm-id>:<EUID>", inherits)
	}
	if self, err := namesSelf(dir, euid, inherits); err != nil {
		return "", err
	} else if self {
		return "", fmt.Errorf("invalid --inherits %q: an entity cannot inherit itself", inherits)
	}
	return inherits, nil
}

// namesSelf reports whether inherits names euid itself: the short EUID, or
// the EUID qualified with the ID of the realm at dir or of a realm it is a
// sub-realm of, since EUIDs are unique across a realm and its sub-realms
// (RLM-013). The same EUID qualified with another realm is a different entity.
func namesSelf(dir, euid, inherits string) (bool, error) {
	realmID, target, qualified := strings.Cut(inherits, ":")
	if !qualified {
		return inherits == euid, nil
	}
	if target != euid {
		return false, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false, fmt.Errorf("resolving %s: %w", dir, err)
	}
	chain, err := realmChain(abs)
	if err != nil {
		return false, err
	}
	for _, rf := range chain {
		if rf.Realm.ID == realmID {
			return true, nil
		}
	}
	return false, nil
}

// usesInherits reports whether f or one of its sub-entities declares
// `inherits`.
func usesInherits(f *entity.File) bool {
	if f.Entity.Inherits != "" {
		return true
	}
	for i := range f.SubEntities {
		if usesInherits(&f.SubEntities[i]) {
			return true
		}
	}
	return false
}

// usesOverrides reports whether a rule of f or of one of its sub-entities
// declares `overrides`.
func usesOverrides(f *entity.File) bool {
	for i := range f.Rules {
		if f.Rules[i].Overrides != nil {
			return true
		}
	}
	for i := range f.SubEntities {
		if usesOverrides(&f.SubEntities[i]) {
			return true
		}
	}
	return false
}

// validateOverrides checks an --overrides value. An empty value means the
// flag was not given.
func validateOverrides(overrides string) (string, error) {
	overrides = strings.TrimSpace(overrides)
	if overrides == "" {
		return "", nil
	}
	if !overridesPattern.MatchString(overrides) {
		return "", fmt.Errorf("invalid --overrides %q: must be a rule ID <EUID>-NNN or <realm-id>:<EUID>-NNN", overrides)
	}
	return overrides, nil
}

// setOverrides records overrides on the rule ruleID of f and refreshes the
// file's hash. It is a no-op when overrides is empty.
func setOverrides(f *entity.File, ruleID, overrides string) error {
	if overrides == "" {
		return nil
	}
	for i := range f.Rules {
		if f.Rules[i].ID == ruleID {
			f.Rules[i].Overrides = &overrides
			return entityops.UpdateMeta(f)
		}
	}
	return fmt.Errorf("rule %s not found after it was added", ruleID)
}
