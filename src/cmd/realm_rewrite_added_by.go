package cmd

import (
	"fmt"
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

// addedByResult is the JSON output of `strspc realm rewrite-added-by`.
type addedByResult struct {
	DryRun    bool            `json:"dry_run"`
	Updated   []addedByChange `json:"updated"`
	Unchanged int             `json:"unchanged"`
	// Skipped lists .json files under --dir that are not entity files (no
	// valid entity.id). They are never rewritten.
	Skipped []string `json:"skipped"`
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
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	var walk func(*entity.File)
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
	walk = func(e *entity.File) {
		for _, r := range e.Rules {
			add(r.AddedBy)
		}
		for _, n := range e.Notes {
			add(n.AddedBy)
		}
		for i := range e.SubEntities {
			walk(&e.SubEntities[i])
		}
	}
	walk(f)
	return out
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
sub-entities and sub-realms included, to a contributor identifier such as
github:<login> (CTRB-003).

--map OLD=NEW replaces each added_by equal to OLD with NEW, and may be given
more than once. --github-handles replaces each @handle with github:<handle>;
a --map for the same @handle wins, in any case, as GitHub logins ignore case. Each changed file gets a patch version
bump and a fresh hash; revisions stay as they are, since added_by is not part
of what a revision counts. Files with nothing to replace are not touched.
Use --dry-run to see what would change without writing.

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
			err = entity.WalkEntityFiles(dir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
				if parseErr != nil {
					return fmt.Errorf("reading %s: %w", path, parseErr)
				}
				if entityops.ValidateEUID(ef.Entity.ID) != nil {
					res.Skipped = append(res.Skipped, path)
					return nil
				}
				fileMap := m
				if githubHandles {
					fileMap = githubHandleMap(ef, m)
				}
				oldVersion := ef.RuleSet.Version
				n, err := entityops.RewriteAddedBy(ef, fileMap)
				if err != nil {
					return fmt.Errorf("rewriting %s: %w", path, err)
				}
				if n == 0 {
					res.Unchanged++
					return nil
				}
				if !dryRun {
					if err := saveEntity(path, ef); err != nil {
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
			sort.Slice(res.Updated, func(i, j int) bool { return res.Updated[i].Path < res.Updated[j].Path })

			w := cmd.OutOrStdout()
			if jsonOut {
				if res.Updated == nil {
					res.Updated = []addedByChange{}
				}
				if res.Skipped == nil {
					res.Skipped = []string{}
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
			for _, p := range res.Skipped {
				writeln(w, descStyle.Render(fmt.Sprintf("Skipped %s (not an entity file)", p)))
			}
			done := "rewritten"
			if dryRun {
				done = "to rewrite (dry run, nothing written)"
			}
			summary := fmt.Sprintf("%d added_by in %d entity file(s) %s, %d unchanged, %d skipped", total, len(res.Updated), done, res.Unchanged, len(res.Skipped))
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
