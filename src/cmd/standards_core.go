package cmd

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// standardsMinCore is the first dev.steerspec.core release whose entity and
// realm schemas accept a `standards` array.
var standardsMinCore = semver.MustParse("1.15.0")

// checkCoreAcceptsStandards refuses to declare a standard in the file at path
// when the core pinned for it predates standardsMinCore: lint would reject the
// file (RL002, RM002). The pin is the one lint validates the file against: that
// of the sub-realm of dir holding it, else the Realm at dir, else that Realm's
// parent. With no manifest, no pin, or a pin that isn't a version, there is
// nothing to compare and the change goes ahead.
func checkCoreAcceptsStandards(dir, path string) error {
	realmID, pin := corePinFor(dir, path)
	if pin == "" {
		return nil
	}
	v, err := semver.NewVersion(pin)
	if err != nil || !v.LessThan(standardsMinCore) {
		return nil
	}
	return fmt.Errorf("realm %s pins %s %s, whose schemas reject `standards` (it needs %s or later): "+
		"upgrade it first with `strspc realm dep upgrade %s --to %s`",
		printableRealmID(realmID), coreRealmID, coverageCell(pin), standardsMinCore, coreRealmID, standardsMinCore)
}

// corePinFor returns the Realm whose core pin applies to the file at path and
// that pin, or "" when none applies.
func corePinFor(dir, path string) (realmID, pin string) {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return "", ""
	}
	if rel, err := filepath.Rel(dir, path); err == nil {
		if first, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && slices.Contains(rf.SubRealms, first) {
			if sub, err := entity.LoadRealm(filepath.Join(dir, first, "realm.json")); err == nil {
				if p := corePin(sub); p != "" {
					return sub.Realm.ID, p
				}
			}
		}
	}
	if p := corePin(rf); p != "" {
		return rf.Realm.ID, p
	}
	if parent := entity.ParentRealm(dir, rf.Realm.ID); parent != nil {
		if p := corePin(parent); p != "" {
			return parent.Realm.ID, p
		}
	}
	return "", ""
}

// corePin returns the version of dev.steerspec.core rf depends on, or "".
func corePin(rf *entity.RealmFile) string {
	for _, d := range rf.Dependencies {
		if d.RealmID == coreRealmID {
			return d.Version
		}
	}
	return ""
}
