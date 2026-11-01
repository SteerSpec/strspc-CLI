package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// latestReleaseTag returns the tag of a repository's latest release. Package-level
// so tests can override it.
var latestReleaseTag = fetchLatestReleaseTag

// githubAPIBase is the GitHub API root. Package-level so tests can point it at a server.
var githubAPIBase = remote.DefaultBaseURL

// validateUpgradedRealm checks a realm the way CI does (strict and recursive).
// Package-level so tests can skip schema and network access.
var validateUpgradedRealm = func(ctx context.Context, dir string) *result.Result {
	vs := newRealmValidator(dir, true, true, false)
	res := vs.realmLinter.Lint(ctx, dir)
	mergeDiagnostics(res, vs.ruleLinter.LintRealm(ctx, dir))
	return res
}

// releaseTag matches a semver release tag such as v1.8.0. Any other source ref
// (commit SHA, branch, floating major) is an immutable or deliberate pin that
// upgrade leaves alone, so unknown ref forms fail closed.
var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// depUpgrade is one planned change to a dependency's pin.
type depUpgrade struct {
	realmID string
	from    string
	to      string
	source  string // rewritten source
}

func fetchLatestReleaseTag(ctx context.Context, ref remote.Ref, token string) (string, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(githubAPIBase, "/"),
		url.PathEscape(ref.Owner), url.PathEscape(ref.Repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "strspc")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("no release found for %s/%s (no releases, or a private repo: set GITHUB_TOKEN and list the owner in %s)", ref.Owner, ref.Repo, remote.TokenOwnersEnv)
	default:
		return "", fmt.Errorf("resolving latest release of %s/%s: HTTP %d", ref.Owner, ref.Repo, resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("decoding latest release of %s/%s: %w", ref.Owner, ref.Repo, err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("latest release of %s/%s has no tag", ref.Owner, ref.Repo)
	}
	return body.TagName, nil
}

// planDepUpgrade decides what to do with one dependency. It returns nil when
// the dependency is already current or cannot be upgraded; note explains why.
func planDepUpgrade(ctx context.Context, dep entity.RealmDep, to string) (up *depUpgrade, note string, err error) {
	if !strings.HasPrefix(dep.Source, remote.Scheme) {
		return nil, "skipped: source is not a github:// reference", nil
	}
	id := printableRealmID(dep.RealmID)
	// Owner and repo reach the release lookup and its error messages.
	if err := rejectControlChars("source", dep.Source); err != nil {
		return nil, "", fmt.Errorf("%s: %w", id, err)
	}
	ref, err := remote.ParseGitHub(dep.Source)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", id, err)
	}
	if ref.Ref == remote.LatestRef {
		return nil, "skipped: source already tracks @latest", nil
	}
	if !releaseTag.MatchString(ref.Ref) {
		return nil, "skipped: source ref is not a release tag (commit SHA, branch or floating ref)", nil
	}
	cur, err := semver.NewVersion(dep.Version)
	if err != nil {
		return nil, "", fmt.Errorf("%s: version %q is not semver: %w", id, dep.Version, err)
	}

	var tag string
	if to != "" {
		tag = "v" + strings.TrimPrefix(to, "v")
	} else if tag, err = latestReleaseTag(ctx, ref, remote.TokenForOwner(ref.Owner)); err != nil {
		return nil, "", fmt.Errorf("%s: %w", id, err)
	}
	next, err := semver.NewVersion(tag)
	if err != nil {
		return nil, "", fmt.Errorf("%s: release tag %q is not semver: %w", id, tag, err)
	}
	if next.Prerelease() != "" && to == "" {
		return nil, "", fmt.Errorf("%s: latest release %s is a prerelease", id, tag)
	}
	switch {
	case next.LessThan(cur):
		return nil, "", fmt.Errorf("%s: %s is older than the pinned %s; refusing to downgrade", id, next, cur)
	case next.Equal(cur):
		return nil, "up to date", nil
	}

	ref.Ref = tag
	return &depUpgrade{realmID: dep.RealmID, from: dep.Version, to: next.String(), source: ref.String()}, "", nil
}

func newRealmDepUpgradeCmd() *cobra.Command {
	var (
		dir   string
		to    string
		check bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade [realm_id]",
		Short: "Upgrade Realm dependencies to their latest release",
		Long: "Resolve the latest release of each dependency's github:// source, rewrite its version and\n" +
			"source in realm.json, and validate the realm (strict, recursive). When dev.steerspec.core is\n" +
			"upgraded, _schema/*.json files that differ from the new release's copies are replaced by\n" +
			"them (RM015). If validation fails, every file is restored and the command exits non-zero.\n\n" +
			"Without realm_id every dependency is considered. Dependencies are never downgraded,\n" +
			"and sources that are not github:// references are skipped. Private sources need GITHUB_TOKEN.\n\n" +
			"With --check nothing is written; the command exits non-zero when an upgrade is available.\n\n" +
			"Examples:\n" +
			"  strspc realm dep upgrade --dir rules\n" +
			"  strspc realm dep upgrade dev.steerspec.core --to 1.11.0\n" +
			"  strspc realm dep upgrade --check",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if to != "" && len(args) == 0 {
				return errors.New("--to requires a realm_id")
			}
			if to != "" && check {
				return errors.New("--to and --check cannot be combined")
			}

			realmPath, err := checkRealmJSON(dir)
			if err != nil {
				return err
			}
			rf, err := entity.LoadRealm(realmPath)
			if err != nil {
				return fmt.Errorf("loading realm.json: %w", err)
			}

			deps := rf.Dependencies
			if len(args) == 1 {
				deps = nil
				for _, d := range rf.Dependencies {
					if d.RealmID == args[0] {
						deps = []entity.RealmDep{d}
					}
				}
				if deps == nil {
					return fmt.Errorf("dependency %q not found in realm.json", args[0])
				}
			}

			w := cmd.OutOrStdout()
			var ups []depUpgrade
			for _, d := range deps {
				up, note, err := planDepUpgrade(cmd.Context(), d, to)
				if err != nil {
					return err
				}
				// As in realm dep list, realm.json isn't validated yet.
				id := printableRealmID(d.RealmID)
				if up == nil {
					writeln(w, descStyle.Render(sanitizeLine(fmt.Sprintf("  %s@%s: %s", id, d.Version, note))))
					continue
				}
				writeln(w, cmdStyle.Render(sanitizeLine(fmt.Sprintf("  %s: %s -> %s", id, up.from, up.to))))
				ups = append(ups, *up)
			}

			if len(ups) == 0 {
				writeln(w, brandStyle.Render("Dependencies are up to date"))
				return nil
			}
			if check {
				return fmt.Errorf("%d dependency upgrade(s) available", len(ups))
			}

			original, err := os.ReadFile(realmPath)
			if err != nil {
				return fmt.Errorf("reading realm.json: %w", err)
			}
			err = loadAndWriteRealm(realmPath, func(cur []entity.RealmDep) ([]entity.RealmDep, error) {
				for _, up := range ups {
					for i := range cur {
						if cur[i].RealmID == up.realmID {
							cur[i].Version = up.to
							cur[i].Source = up.source
						}
					}
				}
				return cur, nil
			})
			if err != nil {
				return err
			}

			var synced []schemaSync
			rollback := func() error {
				if rerr := os.WriteFile(realmPath, original, 0o644); rerr != nil {
					return rerr
				}
				return restoreSchemas(dir, synced)
			}
			for _, up := range ups {
				s, serr := syncCoreSchemas(cmd.Context(), dir, up)
				synced = append(synced, s...)
				if serr != nil {
					if rerr := rollback(); rerr != nil {
						return fmt.Errorf("syncing schemas failed (%v) and restoring files failed: %w", serr, rerr)
					}
					return fmt.Errorf("%w; files left unchanged", serr)
				}
			}
			for _, s := range synced {
				writeln(w, cmdStyle.Render(sanitizeLine("  synced "+filepath.Join(dir, s.path))))
			}

			res := validateUpgradedRealm(cmd.Context(), dir)
			if !res.OK() {
				outputText(w, res)
				if rerr := rollback(); rerr != nil {
					return fmt.Errorf("upgrade breaks the realm and restoring files failed: %w", rerr)
				}
				return fmt.Errorf("upgrade breaks the realm (%d error(s)); files left unchanged", len(res.Errors()))
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Upgraded %d dependency(ies); realm validates", len(ups))))
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&to, "to", "", "upgrade the named dependency to this version instead of the latest")
	cmd.Flags().BoolVar(&check, "check", false, "only report available upgrades; exit non-zero if there are any")

	return cmd
}
