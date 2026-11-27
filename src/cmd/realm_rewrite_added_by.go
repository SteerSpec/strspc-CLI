package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// addedByChange records one entity file whose added_by values were rewritten.
type addedByChange struct {
	Entity     string `json:"entity"`
	Path       string `json:"path"`
	Replaced   int    `json:"replaced"`
	OldVersion string `json:"old_version"`
	Version    string `json:"version"`
}

// addedByManifest records one realm.json whose Realm Notes had their
// added_by rewritten.
type addedByManifest struct {
	Realm    string `json:"realm"`
	Path     string `json:"path"`
	Replaced int    `json:"replaced"`
}

// addedByKept records one override_ack note whose added_by matched but was
// left as it is: such a note doesn't change once added (NTE-044).
type addedByKept struct {
	Entity  string `json:"entity"`
	Note    string `json:"note"`
	AddedBy string `json:"added_by"`
	Path    string `json:"path"`
}

// addedByResult is the JSON output of `strspc realm rewrite-added-by`.
type addedByResult struct {
	DryRun    bool            `json:"dry_run"`
	Updated   []addedByChange `json:"updated"`
	Unchanged int             `json:"unchanged"`
	// Manifests lists the realm.json files, of the realm in --dir and its
	// sub-realms, whose Realm Notes (NTE-029) had an added_by rewritten.
	Manifests []addedByManifest `json:"manifests"`
	// Kept lists the override_ack notes whose added_by matched and that were
	// left as they are (NTE-044), sub-entities before parents in each file.
	Kept []addedByKept `json:"kept"`
	// Skipped lists .json files under --dir that are not entity files (no
	// valid entity.id). They are never rewritten.
	Skipped []string `json:"skipped"`
	// Links lists .json files under --dir that are symbolic links, and
	// sub-realm directories that are. They are never followed, so no link can
	// make the command write outside --dir; a target inside --dir is rewritten
	// under its own path.
	Links []string `json:"links"`
}

// parseAddedByMap reads --map values of the form OLD=NEW. Each NEW must be a
// contributor identifier (CTRB-001, CTRB-002), and an OLD may be mapped once.
// GitHub logins ignore case, so two @handles that differ only in case may not
// map to different identifiers.
func parseAddedByMap(pairs []string) (map[string]string, error) {
	m := make(map[string]string, len(pairs))
	handles := make(map[string]string) // lower-cased @handle → the OLD first given
	for _, pair := range pairs {
		old, repl, ok := strings.Cut(pair, "=")
		old, repl = strings.TrimSpace(old), strings.TrimSpace(repl)
		if !ok || old == "" || repl == "" {
			return nil, fmt.Errorf("--map %q: want OLD=NEW, such as @alice=github:alice", pair)
		}
		if _, _, err := entity.ParseContributorID(repl); err != nil {
			return nil, fmt.Errorf("--map %q: %w", pair, err)
		}
		if prev, dup := m[old]; dup && prev != repl {
			return nil, fmt.Errorf("--map maps %q to both %q and %q", old, prev, repl)
		}
		if strings.HasPrefix(old, "@") {
			key := strings.ToLower(old)
			if first, seen := handles[key]; seen && first != old && m[first] != repl {
				return nil, fmt.Errorf("--map maps %q and %q, the same GitHub handle, to both %q and %q", first, old, m[first], repl)
			} else if !seen {
				handles[key] = old
			}
		}
		m[old] = repl
	}
	return m, nil
}

// mappedHandle returns what m maps addedBy to, matching an @handle without
// regard to case, as GitHub logins do. An exact key wins.
func mappedHandle(m map[string]string, addedBy string) (string, bool) {
	if v, ok := m[addedBy]; ok {
		return v, true
	}
	if !strings.HasPrefix(addedBy, "@") {
		return "", false
	}
	for k, v := range m {
		if strings.HasPrefix(k, "@") && strings.EqualFold(k, addedBy) {
			return v, true
		}
	}
	return "", false
}

// githubHandleMap adds to m, for every @handle written as an added_by in f or
// its sub-entities, the contributor identifier github:<handle>, unless the
// result would not be a valid identifier. When m maps the same handle in
// another case, that mapping applies instead, so a --map wins for every
// spelling of the login. It returns a new map; m is not changed.
func githubHandleMap(f *entity.File, m map[string]string) map[string]string {
	var addedBy []string
	var walk func(*entity.File)
	walk = func(e *entity.File) {
		for i := range e.Rules {
			addedBy = append(addedBy, e.Rules[i].AddedBy)
		}
		for _, n := range e.Notes {
			addedBy = append(addedBy, n.AddedBy)
		}
		for i := range e.SubEntities {
			walk(&e.SubEntities[i])
		}
	}
	walk(f)
	return githubHandleMapFor(addedBy, m)
}

// githubHandleMapFor is githubHandleMap for the added_by values given.
func githubHandleMapFor(addedBys []string, m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	add := func(addedBy string) {
		handle, ok := strings.CutPrefix(addedBy, "@")
		if !ok {
			return
		}
		if _, mapped := out[addedBy]; mapped {
			return
		}
		if v, mapped := mappedHandle(m, addedBy); mapped {
			out[addedBy] = v
			return
		}
		id := entity.IdentityProviderGitHub + ":" + handle
		if _, _, err := entity.ParseContributorID(id); err == nil {
			out[addedBy] = id
		}
	}
	for _, a := range addedBys {
		add(a)
	}
	return out
}

// rewriteRealmNotes rewrites the added_by of the Realm Notes (NTE-029) of the
// realm.json in dir and of its sub-realms, with the guards of walkRealmTree,
// and records each manifest changed in res. A manifest with a hash gets it
// recomputed (RLMMNFST-014); its version stays as it is, as with any other
// manifest edit. A manifest with nothing to replace is not touched. When dir
// holds no realm.json there are no Realm Notes, and nothing is done.
func rewriteRealmNotes(v *schemaValidator, dir string, m map[string]string, githubHandles, dryRun bool, res *addedByResult) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("opening %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	if _, err := root.Lstat("realm.json"); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return walkRealmTree(root, dir, ".", &res.Links, &[]os.FileInfo{}, func(manifest, path string, data []byte) (*entity.RealmFile, error) {
		// SEC-004: the manifest is checked against its schema before it is changed.
		if err := v.validateRealmManifest(path, data); err != nil {
			return nil, fmt.Errorf("validating before rewriting: %w", err)
		}
		rf, err := entity.ParseRealm(data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		noteMap := m
		if githubHandles {
			addedBy := make([]string, len(rf.Notes))
			for i := range rf.Notes {
				addedBy[i] = rf.Notes[i].AddedBy
			}
			noteMap = githubHandleMapFor(addedBy, m)
		}
		n := 0
		for i := range rf.Notes {
			if to, ok := noteMap[rf.Notes[i].AddedBy]; ok && to != rf.Notes[i].AddedBy {
				rf.Notes[i].AddedBy = to
				n++
			}
		}
		if n == 0 {
			return rf, nil
		}
		if !dryRun {
			out, err := marshalRealmManifest(rf, rf.Hash != "")
			if err != nil {
				return nil, err
			}
			if err := v.ctx.Err(); err != nil {
				return nil, fmt.Errorf("interrupted before writing %s: %w", path, err)
			}
			if err := replaceFileIn(root, manifest, out, "realm.json"); err != nil {
				return nil, err
			}
		}
		res.Manifests = append(res.Manifests, addedByManifest{Realm: rf.Realm.ID, Path: path, Replaced: n})
		return rf, nil
	})
}

// newRealmRewriteAddedByCmd returns `strspc realm rewrite-added-by`, which
// migrates added_by values with entityops.RewriteAddedBy (#248).
func newRealmRewriteAddedByCmd() *cobra.Command {
	var (
		pairs         []string
		githubHandles bool
		dir           string
		dryRun        bool
		jsonOut       bool
	)

	cmd := &cobra.Command{
		Use:   "rewrite-added-by",
		Short: "Rewrite the added_by of rules and notes to contributor identifiers",
		Long: `Rewrite the added_by of every rule and note in the entity files under --dir,
sub-entities and sub-realms included, and of the Realm Notes in the realm.json
of --dir and of each sub-realm it lists, to a contributor identifier such as
github:<login> (CTRB-003).

--map OLD=NEW replaces each added_by equal to OLD with NEW, and may be given
more than once. --github-handles replaces each @handle with github:<handle>;
a --map for the same @handle wins, in any case, as GitHub logins ignore case.
Each changed file gets a patch version bump and a fresh hash; revisions stay
as they are, since added_by is not part of what a revision counts. A changed
realm.json keeps its version and gets its hash recomputed, if it has one. Files
with nothing to replace are not touched. A note of type override_ack never
changes once added (NTE-044): its added_by stays, and the command lists it as
kept; a file whose only matches are such notes is not touched. A file that is a symbolic link is listed
and never followed, as is a sub-realm directory that is one: rewrite its target
where it lives. Use --dry-run to see
what would change without writing.

Examples:
  strspc realm rewrite-added-by --github-handles --dry-run
  strspc realm rewrite-added-by --map @alice=github:alice-gh --map bob@example.com=github:bob
  strspc realm rewrite-added-by --github-handles --dir ./specs --json`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(pairs) == 0 && !githubHandles {
				return fmt.Errorf("specify --map OLD=NEW or --github-handles")
			}
			m, err := parseAddedByMap(pairs)
			if err != nil {
				return err
			}

			res := addedByResult{DryRun: dryRun}
			validator := newSchemaValidator(cmd.Context())
			err = entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
				// The walk has already read through a link; writing back
				// through it could change a file outside --dir.
				if link, err := isLinkPath(path); err != nil {
					return fmt.Errorf("reading %s: %w", path, err)
				} else if link {
					res.Links = append(res.Links, path)
					return nil
				}
				if parseErr != nil {
					return fmt.Errorf("reading %s: %w", path, parseErr)
				}
				if entityops.ValidateEUID(ef.Entity.ID) != nil {
					res.Skipped = append(res.Skipped, path)
					return nil
				}
				// SEC-004: the file is checked against its schema before it is changed.
				if err := validator.validateEntityFile(path, data); err != nil {
					return fmt.Errorf("validating before rewriting: %w", err)
				}
				fileMap := m
				if githubHandles {
					fileMap = githubHandleMap(ef, m)
				}
				oldVersion := ef.RuleSet.Version
				rw, err := entityops.RewriteAddedByReport(ef, fileMap)
				if err != nil {
					return fmt.Errorf("rewriting %s: %w", path, err)
				}
				for _, k := range rw.Kept {
					res.Kept = append(res.Kept, addedByKept{Entity: k.EntityID, Note: k.NoteID, AddedBy: k.AddedBy, Path: path})
				}
				n := rw.Replaced
				if n == 0 {
					res.Unchanged++
					return nil
				}
				if !dryRun {
					if err := validator.ctx.Err(); err != nil {
						return fmt.Errorf("interrupted before writing %s: %w", path, err)
					}
					if err := replaceEntityFile(path, ef); err != nil {
						return err
					}
				}
				res.Updated = append(res.Updated, addedByChange{
					Entity: ef.Entity.ID, Path: path, Replaced: n,
					OldVersion: oldVersion, Version: ef.RuleSet.Version,
				})
				return nil
			})
			if err != nil {
				return err
			}
			if err := rewriteRealmNotes(validator, dir, m, githubHandles, dryRun, &res); err != nil {
				return err
			}
			sort.Slice(res.Updated, func(i, j int) bool { return res.Updated[i].Path < res.Updated[j].Path })
			sort.SliceStable(res.Kept, func(i, j int) bool { return res.Kept[i].Path < res.Kept[j].Path })

			w := cmd.OutOrStdout()
			if jsonOut {
				if res.Updated == nil {
					res.Updated = []addedByChange{}
				}
				if res.Manifests == nil {
					res.Manifests = []addedByManifest{}
				}
				if res.Kept == nil {
					res.Kept = []addedByKept{}
				}
				if res.Skipped == nil {
					res.Skipped = []string{}
				}
				if res.Links == nil {
					res.Links = []string{}
				}
				return writeJSONOutput(w, res)
			}

			verb, total := "Rewrote", 0
			if dryRun {
				verb = "Would rewrite"
			}
			for _, c := range res.Updated {
				total += c.Replaced
				writeln(w, brandStyle.Render(fmt.Sprintf("%s %d added_by in %s", verb, c.Replaced, c.Entity))+descStyle.Render(fmt.Sprintf(" (%s)", c.Path)))
				writeln(w, descStyle.Render(fmt.Sprintf("  Entity version: %s → %s", c.OldVersion, c.Version)))
			}
			for _, c := range res.Manifests {
				total += c.Replaced
				writeln(w, brandStyle.Render(fmt.Sprintf("%s %d added_by in the Realm Notes of %s", verb, c.Replaced, printableRealmID(c.Realm)))+descStyle.Render(fmt.Sprintf(" (%s)", c.Path)))
			}
			for _, k := range res.Kept {
				writeln(w, descStyle.Render(fmt.Sprintf("Kept %s in %s as it is: an override_ack note doesn't change once added (NTE-044); added_by %s (%s)",
					printableText(k.Note), printableText(k.Entity), printableText(k.AddedBy), k.Path)))
			}
			for _, p := range res.Skipped {
				writeln(w, descStyle.Render(fmt.Sprintf("Skipped %s (not an entity file)", p)))
			}
			for _, p := range res.Links {
				writeln(w, descStyle.Render(fmt.Sprintf("Skipped %s (a symbolic link, not followed: rewrite its target where it lives)", p)))
			}
			done := "rewritten"
			if dryRun {
				done = "to rewrite (dry run, nothing written)"
			}
			files := fmt.Sprintf("%d entity file(s)", len(res.Updated))
			if len(res.Manifests) > 0 {
				files += fmt.Sprintf(" and %d realm.json", len(res.Manifests))
			}
			summary := fmt.Sprintf("%d added_by in %s %s, %d unchanged, %d skipped", total, files, done, res.Unchanged, len(res.Skipped)+len(res.Links))
			if len(res.Kept) > 0 {
				summary += fmt.Sprintf(", %d override_ack note(s) kept", len(res.Kept))
			}
			writeln(w, descStyle.Render(summary))
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&pairs, "map", nil, "replace added_by OLD with the contributor identifier NEW, as OLD=NEW (repeatable)")
	cmd.Flags().BoolVar(&githubHandles, "github-handles", false, "replace each @handle with github:<handle>")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would change without writing")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
