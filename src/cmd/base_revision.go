package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// baseEntityFile returns the entity file at path as it is at ref, read as
// diff reads it (listBase), or nil when ref has no file there. An unknown
// ref, a path outside a repository, a link or submodule on the way, or a
// base file that doesn't parse is an error, never an absent file.
func baseEntityFile(ref, path string) (*entity.File, error) {
	path = filepath.Clean(path)
	repoDir, err := gitRoot(path)
	if err != nil {
		return nil, err
	}
	rel, err := repoPath(path, repoDir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	base, err := listBase(ref, rel, repoDir)
	if err != nil {
		return nil, fmt.Errorf("listing %s at %s: %w", path, ref, err)
	}
	e, ok := base[rel]
	switch {
	case !ok:
		return nil, nil
	case e.kind == "commit":
		return nil, fmt.Errorf("%s at %s %w", rel, ref, errDiffSubmodule)
	case e.kind != "blob" || (e.mode != "100644" && e.mode != "100755"):
		return nil, fmt.Errorf("%s at %s %w", rel, ref, errDiffLink)
	}
	data, err := readBlob(e.sha, repoDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", rel, ref, err)
	}
	f, err := entity.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s at %s: %w", rel, ref, err)
	}
	return f, nil
}

// findNoteInTree returns the note noteID of f or of any of its sub-entities,
// or nil.
func findNoteInTree(f *entity.File, noteID string) *entity.Note {
	for i := range f.Notes {
		if f.Notes[i].ID == noteID {
			return &f.Notes[i]
		}
	}
	for i := range f.SubEntities {
		if n := findNoteInTree(&f.SubEntities[i], noteID); n != nil {
			return n
		}
	}
	return nil
}

// findRuleInTree returns the rule ruleID of f or of any of its sub-entities,
// or nil.
func findRuleInTree(f *entity.File, ruleID string) *entity.Rule {
	if r := findRule(f, ruleID); r != nil {
		return r
	}
	for i := range f.SubEntities {
		if r := findRuleInTree(&f.SubEntities[i], ruleID); r != nil {
			return r
		}
	}
	return nil
}

// revisionFromBase returns the revision diff expects against the base for an
// item whose base revision is baseRev (present false when the base doesn't
// hold it): 0 for an item new since the base, the base revision when it
// reads as at the base, and one more when it changed (RD004, RD013, RD015).
func revisionFromBase(present bool, baseRev int, changed bool) int {
	switch {
	case !present:
		return 0
	case changed:
		return baseRev + 1
	default:
		return baseRev
	}
}

// setNoteRevisionFromBase sets the revision of the note noteID of target
// against base (nil when the base has no such file) and refreshes target's
// hash, which entityops computed with the bumped revision.
func setNoteRevisionFromBase(target, base *entity.File, noteID string) error {
	n := findNoteInTree(target, noteID)
	if n == nil {
		return fmt.Errorf("note %q not found", noteID)
	}
	var b *entity.Note
	if base != nil {
		b = findNoteInTree(base, noteID)
	}
	if b == nil {
		n.Revision = revisionFromBase(false, 0, false)
	} else {
		n.Revision = revisionFromBase(true, b.Revision, n.Content != b.Content || n.Type != b.Type)
	}
	return entityops.UpdateMeta(target)
}

// setRuleRevisionFromBase is setNoteRevisionFromBase for the rule ruleID: it
// changed when any field but its revision differs from the base.
func setRuleRevisionFromBase(target, base *entity.File, ruleID string) error {
	r := findRuleInTree(target, ruleID)
	if r == nil {
		return fmt.Errorf("rule %q not found", ruleID)
	}
	var b *entity.Rule
	if base != nil {
		b = findRuleInTree(base, ruleID)
	}
	if b == nil {
		r.Revision = revisionFromBase(false, 0, false)
	} else {
		same := *b
		same.Revision = r.Revision
		changed, err := rulesDiffer(r, &same)
		if err != nil {
			return err
		}
		r.Revision = revisionFromBase(true, b.Revision, changed)
	}
	return entityops.UpdateMeta(target)
}

// rulesDiffer reports whether a and b differ in any field, compared as JSON.
func rulesDiffer(a, b *entity.Rule) (bool, error) {
	ja, err := marshalJSON(a)
	if err != nil {
		return false, err
	}
	jb, err := marshalJSON(b)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(ja, jb), nil
}
