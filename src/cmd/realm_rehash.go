package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// realmRehashChange records one realm.json whose manifest hash was added or
// rewritten.
type realmRehashChange struct {
	Realm string `json:"realm"`
	Path  string `json:"path"`
	// OldHash is the hash the manifest declared, or null when it had none.
	OldHash *string `json:"old_hash"`
	NewHash string  `json:"new_hash"`
}

// realmRehashResult is the JSON output of `strspc realm rehash`.
type realmRehashResult struct {
	Updated   []realmRehashChange `json:"updated"`
	Unchanged int                 `json:"unchanged"`
	// Links lists the realm.json files that are symbolic links. They are never
	// written, and the sub-realms they list are not visited: the target may lie
	// outside --dir.
	Links []string `json:"links"`
}

// isSubRealmName reports whether name, from a manifest's sub_realms, names a
// directory directly below the manifest's, so a walk over sub-realms stays
// inside --dir.
func isSubRealmName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !filepath.IsAbs(name)
}

// rehashRealmTree adds or refreshes the manifest hash (RLMMNFST-013) of the
// realm.json in dir, then of each sub-realm it lists, recursively. A manifest
// whose hash already matches is left as it is, byte for byte. seen holds the
// directories visited, so a directory linked back into the tree is visited
// once.
func rehashRealmTree(dir string, res *realmRehashResult, seen map[string]bool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", dir, err)
	}
	if seen[abs] {
		return nil
	}
	seen[abs] = true

	path, err := checkRealmJSON(dir)
	if err != nil {
		return err
	}
	if link, err := isLinkPath(path); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	} else if link {
		res.Links = append(res.Links, path)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	rf, err := entity.ParseRealm(data)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	computed, err := entity.ComputeRealmHash(data)
	if err != nil {
		return fmt.Errorf("computing the manifest hash of %s: %w", path, err)
	}
	if rf.Hash != "" && rf.Hash == computed {
		res.Unchanged++
	} else {
		if rf.Hash == "" {
			if err := checkCoreAccepts(dir, path, "hash", manifestMinCore); err != nil {
				return err
			}
		}
		change := realmRehashChange{Realm: rf.Realm.ID, Path: path}
		if rf.Hash != "" {
			old := rf.Hash
			change.OldHash = &old
		}
		if err := replaceRealmManifest(path, rf, true); err != nil {
			return err
		}
		change.NewHash = rf.Hash
		res.Updated = append(res.Updated, change)
	}

	for _, name := range rf.SubRealms {
		if !isSubRealmName(name) {
			return fmt.Errorf("%s lists sub-realm %q, which is not the name of a directory next to it", path, name)
		}
		if err := rehashRealmTree(filepath.Join(dir, name), res, seen); err != nil {
			return err
		}
	}
	return nil
}

func newRealmRehashCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:   "rehash",
		Short: "Add or refresh the manifest hash of realm.json",
		Long: `Set the hash of the realm.json in --dir, and of each sub-realm it lists, to the
Blake3 hash of the manifest without its hash field (RLMMNFST-013). A manifest
without a hash gets one; one whose hash is stale gets it rewritten; one whose
hash matches is not touched. Run it after editing realm.json by hand or by a
script: the realm commands keep an existing hash current themselves
(RLMMNFST-014), and realm init writes none.

Adding a hash needs a dev.steerspec.core pin of ` + manifestMinCore.String() + ` or later, whose realm
schema accepts it. A realm.json that is a symbolic link is listed and never
followed. Versions are never bumped.

Examples:
  strspc realm rehash
  strspc realm rehash --dir ./specs --json`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res := realmRehashResult{}
			if err := rehashRealmTree(dir, &res, map[string]bool{}); err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if jsonOut {
				if res.Updated == nil {
					res.Updated = []realmRehashChange{}
				}
				if res.Links == nil {
					res.Links = []string{}
				}
				return writeJSONOutput(w, res)
			}

			for _, c := range res.Updated {
				old := "none"
				if c.OldHash != nil {
					old = *c.OldHash
				}
				writeln(w, brandStyle.Render("Rehashed realm "+printableRealmID(c.Realm))+descStyle.Render(fmt.Sprintf(" (%s)", coverageCell(c.Path))))
				writeln(w, descStyle.Render(fmt.Sprintf("  %s → %s", coverageCell(old), c.NewHash)))
			}
			for _, p := range res.Links {
				writeln(w, descStyle.Render(fmt.Sprintf("Skipped %s (a symbolic link, not followed: rehash its target where it lives)", coverageCell(p))))
			}
			writeln(w, descStyle.Render(fmt.Sprintf("%d manifest(s) updated, %d unchanged, %d skipped", len(res.Updated), res.Unchanged, len(res.Links))))
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}
