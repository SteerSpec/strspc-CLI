package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// standardsMinCore is the first dev.steerspec.core release whose entity and
// realm schemas accept a `standards` array.
var standardsMinCore = semver.MustParse("1.15.0")

// coreReleasePin matches a pin that selects a core release's schemas, as the
// manager's schema.WithCoreVersion reads it. Lint ignores any other form and
// uses the latest schemas.
var coreReleasePin = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// realmPin is one Realm's dev.steerspec.core pin.
type realmPin struct {
	realmID string
	dir     string
	version string
	source  string
}

// checkCoreAcceptsStandards refuses to declare a standard in the file at path
// when a core pin that validates it predates standardsMinCore: lint would
// reject the file (RL002, RM002). Every pin that applies is checked: that of
// the sub-realm of dir holding the file, the Realm at dir's, and that Realm's
// parent's, since a recursive `realm validate` lints a sub-realm's entities
// against the parent's pin too. A pin that isn't an X.Y.Z release selects no
// schemas and is skipped, as is a Realm with no manifest or no pin.
func checkCoreAcceptsStandards(dir, path string) error {
	for _, p := range corePinsFor(dir, path) {
		if !coreReleasePin.MatchString(p.version) {
			continue
		}
		v, err := semver.StrictNewVersion(p.version)
		if err != nil || !v.LessThan(standardsMinCore) {
			continue
		}
		return fmt.Errorf("realm %s pins %s %s, whose schemas reject `standards` (it needs %s or later): %s",
			printableRealmID(p.realmID), coreRealmID, p.version, standardsMinCore, corePinRemedy(p))
	}
	return nil
}

// corePinRemedy tells how to move p to standardsMinCore. `realm dep upgrade`
// moves only a github:// source pinned to a release tag (planDepUpgrade); any
// other source is re-pinned with `realm dep remove` and `realm dep add`,
// keeping the source.
func corePinRemedy(p realmPin) string {
	dir := coverageCell(shellQuote(p.dir))
	if depUpgradable(p.source) {
		return fmt.Sprintf("upgrade it first with `strspc realm dep upgrade %s --to %s --dir %s`", coreRealmID, standardsMinCore, dir)
	}
	spec := fmt.Sprintf("%s@%s", coreRealmID, standardsMinCore)
	if p.source != "" {
		spec += "=" + p.source
	}
	return fmt.Sprintf("`realm dep upgrade` can't move its source, so re-pin it first with "+
		"`strspc realm dep remove %s --dir %s` then `strspc realm dep add %s --dir %s`",
		coreRealmID, dir, coverageCell(shellQuote(spec)), dir)
}

// depUpgradable reports whether `realm dep upgrade` can move a dependency with
// this source: a github:// reference pinned to a release tag.
func depUpgradable(source string) bool {
	if !strings.HasPrefix(source, remote.Scheme) {
		return false
	}
	ref, err := remote.ParseGitHub(source)
	return err == nil && ref.Ref != remote.LatestRef && releaseTag.MatchString(ref.Ref)
}

// corePinsFor returns the core pins that apply to the file at path, nearest
// first: the sub-realm of dir holding it, the Realm at dir, and its parent.
func corePinsFor(dir, path string) []realmPin {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return nil
	}
	var pins []realmPin
	add := func(r *entity.RealmFile, d string) {
		if dep := corePin(r); dep != nil {
			pins = append(pins, realmPin{realmID: r.Realm.ID, dir: d, version: dep.Version, source: dep.Source})
		}
	}
	if rel, err := filepath.Rel(dir, path); err == nil {
		if first, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && slices.Contains(rf.SubRealms, first) {
			subDir := filepath.Join(dir, first)
			if sub, err := entity.LoadRealm(filepath.Join(subDir, "realm.json")); err == nil {
				add(sub, subDir)
			}
		}
	}
	add(rf, dir)
	if parent := entity.ParentRealm(dir, rf.Realm.ID); parent != nil {
		if abs, err := filepath.Abs(dir); err == nil {
			add(parent, filepath.Dir(abs))
		}
	}
	return pins
}

// corePin returns rf's dependency on dev.steerspec.core, or nil.
func corePin(rf *entity.RealmFile) *entity.RealmDep {
	for i := range rf.Dependencies {
		if rf.Dependencies[i].RealmID == coreRealmID {
			return &rf.Dependencies[i]
		}
	}
	return nil
}

// shellQuote returns s as one POSIX shell word: unchanged when it holds only
// characters no shell treats specially, single-quoted otherwise.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, unsafeShellRune) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// unsafeShellRune reports whether r needs quoting in a POSIX shell word.
func unsafeShellRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return !strings.ContainsRune("_-./+:@,=", r)
	}
}
