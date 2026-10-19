package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
)

// rehashChange records one entity file whose rule_set.hash was rewritten.
type rehashChange struct {
	Entity  string  `json:"entity"`
	Path    string  `json:"path"`
	OldHash *string `json:"old_hash"`
	NewHash string  `json:"new_hash"`
}

// rehashResult is the JSON output of `strspc entity rehash`.
type rehashResult struct {
	Updated   []rehashChange `json:"updated"`
	Unchanged int            `json:"unchanged"`
	// Skipped lists .json files under --dir that are not entity files (no valid
	// entity.id). They are never rewritten.
	Skipped []string `json:"skipped"`
}

// notEntityError reports a JSON file that parsed but carries no valid entity.id.
// entity.Parse only unmarshals, so an unrelated file such as metadata.json would
// otherwise be rewritten as an empty entity.
type notEntityError struct {
	path string
	err  error
}

func (e *notEntityError) Error() string {
	return fmt.Sprintf("%s is not an entity file: %v", e.path, e.err)
}

func (e *notEntityError) Unwrap() error { return e.err }

// rehashEntity compares the stored rule_set.hash with the blake3 hash of the
// file's raw bytes (what lint RL011 verifies), and each sub-entity's hash with
// the hash of its own JSON (what entityops stamps on sub-entity operations).
// When any differ, or are null, it refreshes timestamp and hash via
// entityops.UpdateMeta and rewrites the file. Fresh files are left untouched.
func rehashEntity(path string, data []byte, ef *entity.File) (*rehashChange, error) {
	if err := entityops.ValidateEUID(ef.Entity.ID); err != nil {
		return nil, &notEntityError{path: path, err: err}
	}
	subsChanged, err := rehashSubEntities(ef)
	if err != nil {
		return nil, fmt.Errorf("rehashing sub-entities of %s: %w", path, err)
	}
	computed, err := entity.ComputeHash(data)
	if err != nil {
		return nil, fmt.Errorf("computing hash for %s: %w", path, err)
	}
	if !subsChanged && ef.RuleSet.Hash != nil && *ef.RuleSet.Hash == computed {
		return nil, nil
	}

	change := &rehashChange{Entity: ef.Entity.ID, Path: path, OldHash: ef.RuleSet.Hash}
	if err := entityops.UpdateMeta(ef); err != nil {
		return nil, fmt.Errorf("updating metadata for %s: %w", path, err)
	}
	if err := saveEntity(path, ef); err != nil {
		return nil, err
	}
	change.NewHash = *ef.RuleSet.Hash
	return change, nil
}

// rehashSubEntities refreshes the hash of every sub-entity (recursively) whose
// stored hash is null or stale. It reports whether anything changed.
func rehashSubEntities(ef *entity.File) (bool, error) {
	changed := false
	for i := range ef.SubEntities {
		sub := &ef.SubEntities[i]
		nestedChanged, err := rehashSubEntities(sub)
		if err != nil {
			return false, err
		}
		data, err := json.Marshal(sub)
		if err != nil {
			return false, fmt.Errorf("marshaling %s: %w", sub.Entity.ID, err)
		}
		computed, err := entity.ComputeHash(data)
		if err != nil {
			return false, fmt.Errorf("computing hash for %s: %w", sub.Entity.ID, err)
		}
		if !nestedChanged && sub.RuleSet.Hash != nil && *sub.RuleSet.Hash == computed {
			continue
		}
		if err := entityops.UpdateMeta(sub); err != nil {
			return false, fmt.Errorf("updating metadata for %s: %w", sub.Entity.ID, err)
		}
		changed = true
	}
	return changed, nil
}

func newEntityRehashCmd() *cobra.Command {
	var (
		all     bool
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "rehash [EUID]",
		Short: "Recompute stale or missing rule_set hashes",
		Long: `Recompute rule_set.hash (and timestamp) for entity files whose stored hash is
null or does not match the file contents. Sub-entities are repaired the same way,
each over its own JSON, and a file is rewritten when it or any sub-entity was
stale. Files whose hashes are all consistent are not touched. Versions are never
bumped. A .json file without a valid entity.id is never rewritten: --all reports
it as skipped and the single-file form fails. Useful after bulk imports or hand
edits.

Examples:
  strspc entity rehash MYE
  strspc entity rehash --all --dir ./specs --json`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if all == (len(args) == 1) {
				return fmt.Errorf("specify exactly one of <EUID> or --all")
			}

			var res rehashResult

			if all {
				err := entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
					if parseErr != nil {
						return fmt.Errorf("reading %s: %w", path, parseErr)
					}
					change, err := rehashEntity(path, data, ef)
					var notEntity *notEntityError
					if errors.As(err, &notEntity) {
						res.Skipped = append(res.Skipped, path)
						return nil
					}
					if err != nil {
						return err
					}
					if change == nil {
						res.Unchanged++
					} else {
						res.Updated = append(res.Updated, *change)
					}
					return nil
				})
				if err != nil {
					return err
				}
			} else {
				path := resolveEntityPath(dir, args[0])
				data, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("reading entity file: %w", err)
				}
				ef, err := entity.Parse(data)
				if err != nil {
					return fmt.Errorf("parsing %s: %w", path, err)
				}
				change, err := rehashEntity(path, data, ef)
				if err != nil {
					return err
				}
				if change == nil {
					res.Unchanged++
				} else {
					res.Updated = append(res.Updated, *change)
				}
			}

			w := cmd.OutOrStdout()

			if jsonOut {
				if res.Updated == nil {
					res.Updated = []rehashChange{}
				}
				if res.Skipped == nil {
					res.Skipped = []string{}
				}
				return writeJSONOutput(w, res)
			}

			for _, c := range res.Updated {
				old := "null"
				if c.OldHash != nil {
					old = *c.OldHash
				}
				writeln(w, brandStyle.Render(fmt.Sprintf("Rehashed %s", c.Entity))+descStyle.Render(fmt.Sprintf(" (%s)", c.Path)))
				writeln(w, descStyle.Render(fmt.Sprintf("  %s → %s", old, c.NewHash)))
			}
			for _, p := range res.Skipped {
				writeln(w, descStyle.Render(fmt.Sprintf("Skipped %s (not an entity file)", p)))
			}
			writeln(w, descStyle.Render(fmt.Sprintf("%d entity file(s) updated, %d unchanged, %d skipped", len(res.Updated), res.Unchanged, len(res.Skipped))))

			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "rehash every entity file under --dir (recursive)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
