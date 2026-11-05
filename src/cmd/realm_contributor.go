package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func newRealmContributorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contributor",
		Short: "Manage the contributors a Realm lists",
		Long: "Add, remove or list the entries of the `contributors` array of realm.json (CTRB-004).\n" +
			"When a Realm lists contributors, the added_by of every new rule and note must name one of\n" +
			"them (CTRB-005). A sub-realm uses its parent's list (CTRB-006), so it is edited there.",
	}
	cmd.AddCommand(newRealmContributorAddCmd())
	cmd.AddCommand(newRealmContributorRemoveCmd())
	cmd.AddCommand(newRealmContributorListCmd())
	return cmd
}

func newRealmContributorAddCmd() *cobra.Command {
	var (
		email    string
		githubID int64
		dir      string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add a contributor to a Realm",
		Long: `Append a contributor to the contributors array of realm.json. The id is a
contributor identifier <idp>:<subject> whose provider is one that core lists
(CTRB-001, CTRB-002), such as github:alice, not already listed (GitHub logins match
without regard to case). --email is optional (CTRB-007). For a github id,
--github-id records the account's numeric user ID (CTRB-008), which survives a
rename: ` + "`gh api users/<login> --jq .id`" + ` prints it.

A Realm's contributors are the default for --added-by: rule add, note add and
rule supersede use the entry whose email is your git user.email.

Examples:
  strspc realm contributor add github:alice --github-id 1234567 --email alice@example.com
  strspc realm contributor add github:bob --dir ./specs --json`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := contributorFromFlags(cmd, args[0], email, githubID)
			if err != nil {
				return err
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			// A sub-realm can't be edited whatever its pin, so say so first.
			current, err := entity.LoadRealm(realmPath)
			if err != nil {
				return err
			}
			if err := refuseSubRealmContributors(dir, current); err != nil {
				return err
			}
			if err := checkCoreAccepts(dir, realmPath, "contributors", contributorsMinCore); err != nil {
				return err
			}
			var realmID string
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				if err := refuseSubRealmContributors(dir, rf); err != nil {
					return err
				}
				list := append(slices.Clone(rf.Realm.Contributors), c)
				if errs := entity.ValidateContributors(list); len(errs) > 0 {
					return errors.Join(errs...)
				}
				rf.Realm.Contributors, realmID = list, rf.Realm.ID
				return nil
			})
			if err != nil {
				return err
			}
			if err := writeRealmContributorChange(cmd, jsonOut, "Added", realmID, realmPath, c); err != nil {
				return err
			}
			if !jsonOut && c.GitHubID == 0 && strings.HasPrefix(c.ID, entity.IdentityProviderGitHub+":") {
				login := strings.TrimPrefix(c.ID, entity.IdentityProviderGitHub+":")
				writeln(cmd.OutOrStdout(), descStyle.Render(fmt.Sprintf(
					"  No --github-id: a GitHub login can be renamed or reused, its numeric ID can't (CTRB-008). "+
						"`gh api users/%s --jq .id` prints it; remove the entry and add it again with --github-id.",
					coverageCell(login),
				)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "email address of the contributor (CTRB-007)")
	cmd.Flags().Int64Var(&githubID, "github-id", 0, "numeric GitHub user ID, for a github id (CTRB-008)")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmContributorRemoveCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a contributor from a Realm",
		Long: `Remove the contributor with the given id from realm.json (GitHub logins match
without regard to case). Rules and notes already written keep their added_by:
CTRB-005 applies to new ones. Removing the last entry removes the array, and
added_by is then checked for its format only.

Example:
  strspc realm contributor remove github:alice`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if _, _, err := entity.ParseContributorID(id); err != nil {
				return err
			}
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			var (
				realmID string
				removed entity.Contributor
			)
			err = loadModifyWriteRealm(realmPath, func(rf *entity.RealmFile) error {
				if err := refuseSubRealmContributors(dir, rf); err != nil {
					return err
				}
				list := rf.Realm.Contributors
				found := entity.FindContributor(list, id)
				if found == nil {
					return fmt.Errorf("realm %s lists no contributor %q", printableRealmID(rf.Realm.ID), coverageCell(id))
				}
				removed = *found
				i := slices.IndexFunc(list, func(c entity.Contributor) bool { return c.ID == removed.ID })
				// An empty list is written as no key (omitempty), as the schema
				// requires at least one entry.
				rf.Realm.Contributors, realmID = slices.Delete(slices.Clone(list), i, i+1), rf.Realm.ID
				return nil
			})
			if err != nil {
				return err
			}
			return writeRealmContributorChange(cmd, jsonOut, "Removed", realmID, realmPath, removed)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

func newRealmContributorListCmd() *cobra.Command {
	var (
		dir     string
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the contributors a Realm uses",
		Long: `List the contributors that added_by is checked against for the Realm at --dir:
its own, or for a sub-realm its parent's (CTRB-006).`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			rf, err := entity.LoadRealm(realmPath)
			if err != nil {
				return fmt.Errorf("loading realm.json: %w", err)
			}
			list, err := entity.RealmContributors(dir)
			if err != nil {
				return fmt.Errorf("reading the contributors of the realm in %s: %w", dir, err)
			}
			var from string
			if parent := entity.ParentRealm(dir, rf.Realm.ID); parent != nil {
				from = parent.Realm.ID
			}
			if list == nil {
				list = []entity.Contributor{}
			}
			w := cmd.OutOrStdout()
			if jsonOut {
				out := map[string]any{
					"realm":        rf.Realm.ID,
					"path":         realmPath,
					"contributors": list,
				}
				if from != "" {
					out["inherited_from"] = from
				}
				return writeJSONOutput(w, out)
			}
			writeContributorList(w, rf.Realm.ID, from, list)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")
	return cmd
}

// contributorFromFlags builds the entry `contributor add` appends. The id must
// be a contributor identifier (CTRB-001, CTRB-002); the email a bare address,
// as entity.FindContributor matches it (CTRB-007); the GitHub ID positive, and
// only on a github id (CTRB-008), since 0 is written as no ID.
func contributorFromFlags(cmd *cobra.Command, id, email string, githubID int64) (entity.Contributor, error) {
	idp, _, err := entity.ParseContributorID(id)
	if err != nil {
		return entity.Contributor{}, err
	}
	c := entity.Contributor{ID: id}
	if cmd.Flags().Changed("email") {
		if err := rejectControlChars("email", email); err != nil {
			return entity.Contributor{}, err
		}
		// A bare address is what ValidAddedBy accepts and is neither an
		// identifier nor an @handle.
		if strings.HasPrefix(email, "@") || entity.IsContributorID(email) || !entity.ValidAddedBy(email) {
			return entity.Contributor{}, fmt.Errorf("--email %q is not a bare email address (CTRB-007)", coverageCell(email))
		}
		c.Email = email
	}
	if cmd.Flags().Changed("github-id") {
		if idp != entity.IdentityProviderGitHub {
			return entity.Contributor{}, fmt.Errorf("--github-id applies to a github id, not %q (CTRB-008)", coverageCell(id))
		}
		if githubID <= 0 {
			return entity.Contributor{}, fmt.Errorf("--github-id must be a positive number (CTRB-008)")
		}
		c.GitHubID = githubID
	}
	return c, nil
}

// refuseSubRealmContributors refuses to edit the list of a sub-realm, which
// uses its parent's (CTRB-006), and names the parent's directory. A manifest
// above dir that can't be read is an error: ParentRealm alone would take it
// for no parent, and the sub-realm's list would be edited.
func refuseSubRealmContributors(dir string, rf *entity.RealmFile) error {
	if _, err := entity.RealmContributors(dir); err != nil {
		return fmt.Errorf("reading the contributors of the realm in %s: %w", dir, err)
	}
	parent := entity.ParentRealm(dir, rf.Realm.ID)
	if parent == nil {
		return nil
	}
	parentDir := ".."
	if abs, err := filepath.Abs(dir); err == nil {
		parentDir = filepath.Dir(abs)
	}
	return fmt.Errorf("realm %s is a sub-realm of %s and uses its contributors (CTRB-006): edit them with --dir %s",
		printableRealmID(rf.Realm.ID), printableRealmID(parent.Realm.ID), coverageCell(shellQuote(parentDir)))
}

func writeRealmContributorChange(cmd *cobra.Command, jsonOut bool, verb, realmID, path string, c entity.Contributor) error {
	w := cmd.OutOrStdout()
	if jsonOut {
		return writeJSONOutput(w, map[string]any{
			"realm":       realmID,
			"path":        path,
			"contributor": c,
		})
	}
	writeln(w, brandStyle.Render(fmt.Sprintf("%s contributor %s on Realm %s", verb, describeContributor(c), printableRealmID(realmID))))
	return nil
}

func writeContributorList(w io.Writer, realmID, from string, list []entity.Contributor) {
	heading := "Realm " + printableRealmID(realmID)
	if from != "" {
		heading += ", from its parent " + printableRealmID(from) + " (CTRB-006)"
	}
	writeln(w, brandStyle.Render(heading))
	if len(list) == 0 {
		writeln(w, descStyle.Render("  No contributors listed: added_by is checked for its format only."))
		return
	}
	for _, c := range list {
		writeln(w, "  "+describeContributor(c))
	}
}

// describeContributor renders an entry on one line, safe for a terminal.
func describeContributor(c entity.Contributor) string {
	s := coverageCell(c.ID)
	var extra []string
	if c.Email != "" {
		extra = append(extra, coverageCell(c.Email))
	}
	if c.GitHubID != 0 {
		extra = append(extra, fmt.Sprintf("GitHub ID %d", c.GitHubID))
	}
	if len(extra) > 0 {
		s += " (" + strings.Join(extra, ", ") + ")"
	}
	return s
}
