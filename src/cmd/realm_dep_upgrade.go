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
	// oldSource is the source before the upgrade, which .strspc/config.yaml
	// may repeat.
	oldSource string
}

// getGitHubAPI sends a GET for path below githubAPIBase, with token as the
// bearer token unless it is empty. The URL must be https, or http to a
// loopback host (SEC-001). The caller closes the body.
func getGitHubAPI(ctx context.Context, client *http.Client, path, accept, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(githubAPIBase, "/")+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	if err := checkHTTPS(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "strspc")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

// releasesLatestPath is the API path of a repository's latest release.
func releasesLatestPath(ref remote.Ref) string {
	return fmt.Sprintf("/repos/%s/%s/releases/latest", url.PathEscape(ref.Owner), url.PathEscape(ref.Repo))
}

// decodeReleaseTag reads the tag_name from a 200 response for a release.
func decodeReleaseTag(resp *http.Response, ref remote.Ref) (string, error) {
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

func fetchLatestReleaseTag(ctx context.Context, ref remote.Ref, token string) (string, error) {
	resp, err := getGitHubAPI(ctx, newHTTPSOnlyClient(30*time.Second), releasesLatestPath(ref), "application/vnd.github+json", token)
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
	return decodeReleaseTag(resp, ref)
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
	return &depUpgrade{realmID: dep.RealmID, from: dep.Version, to: next.String(), source: ref.String(), oldSource: dep.Source}, "", nil
}

// depUpgradeReport is the --json output of realm dep upgrade.
type depUpgradeReport struct {
	// Check is true when nothing was written because --check was given.
	Check bool `json:"check"`
	// Applied is true when realm.json was rewritten and the realm validates.
	Applied  bool                `json:"applied"`
	Upgrades []depUpgradeEntry   `json:"upgrades"`
	Skipped  []depUpgradeSkipped `json:"skipped"`
	Synced   []string            `json:"synced"`
	// Config lists the .strspc/config.yaml files whose rule sources follow the
	// upgrade (planned when Check is set, rewritten otherwise).
	Config []string `json:"config"`
	// Errors lists the validation errors of an upgrade that was rolled back.
	Errors []string `json:"errors"`
}

type depUpgradeEntry struct {
	RealmID string `json:"realm_id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Source  string `json:"source"`
}

type depUpgradeSkipped struct {
	RealmID string `json:"realm_id"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

func newRealmDepUpgradeCmd() *cobra.Command {
	var (
		dir     string
		to      string
		check   bool
		jsonOut bool
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
			"The repository's .strspc/config.yaml (the nearest one at or above --dir) follows the upgrade:\n" +
			"a rule source that is exactly a dependency's old source is rewritten to the new one.\n\n" +
			"With --check nothing is written; the command exits non-zero when an upgrade is available.\n" +
			"With --json one JSON document is written to stdout (exit codes are unchanged).\n\n" +
			"Examples:\n" +
			"  strspc realm dep upgrade --dir rules\n" +
			"  strspc realm dep upgrade dev.steerspec.core --to 1.11.0\n" +
			"  strspc realm dep upgrade --check\n" +
			"  strspc realm dep upgrade --check --json",
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
			manifest, err := os.ReadFile(realmPath)
			if err != nil {
				return fmt.Errorf("loading realm.json: %w", err)
			}
			// SEC-004: before anything is planned or looked up from it. --check
			// only reports, like realm dep list, so it doesn't validate.
			if !check {
				if err := newSchemaValidator(cmd.Context()).validateRealmManifest(realmPath, manifest); err != nil {
					return fmt.Errorf("validating before planning: %w", err)
				}
			}
			rf, err := entity.ParseRealm(manifest)
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

			// In JSON mode the progress lines are dropped and one report is
			// written instead; the exit code stays the same.
			w := cmd.OutOrStdout()
			text := w
			if jsonOut {
				text = io.Discard
			}
			report := depUpgradeReport{
				Check:    check,
				Upgrades: []depUpgradeEntry{},
				Skipped:  []depUpgradeSkipped{},
				Synced:   []string{},
				Config:   []string{},
				Errors:   []string{},
			}
			emit := func() error {
				if !jsonOut {
					return nil
				}
				return writeJSONOutput(w, report)
			}
			var ups []depUpgrade
			for _, d := range deps {
				up, note, err := planDepUpgrade(cmd.Context(), d, to)
				if err != nil {
					// Nothing is written yet; report what was planned so far.
					report.Errors = append(report.Errors, sanitizeLine(err.Error()))
					return errors.Join(err, emit())
				}
				// As in realm dep list, realm.json isn't validated yet.
				id := printableRealmID(d.RealmID)
				if up == nil {
					writeln(text, descStyle.Render(sanitizeLine(fmt.Sprintf("  %s@%s: %s", id, d.Version, note))))
					report.Skipped = append(report.Skipped, depUpgradeSkipped{RealmID: sanitizeLine(id), Version: sanitizeLine(d.Version), Reason: note})
					continue
				}
				writeln(text, cmdStyle.Render(sanitizeLine(fmt.Sprintf("  %s: %s -> %s", id, up.from, up.to))))
				report.Upgrades = append(report.Upgrades, depUpgradeEntry{RealmID: sanitizeLine(id), From: sanitizeLine(up.from), To: up.to, Source: sanitizeLine(up.source)})
				ups = append(ups, *up)
			}

			if len(ups) == 0 {
				writeln(text, brandStyle.Render("Dependencies are up to date"))
				return emit()
			}
			if check {
				if cfg, err := planConfigPin(dir, ups); err != nil {
					report.Errors = append(report.Errors, sanitizeLine(err.Error()))
					return errors.Join(err, emit())
				} else if cfg != nil {
					writeln(text, cmdStyle.Render(sanitizeLine("  would update "+cfg.path)))
					report.Config = append(report.Config, sanitizeLine(cfg.path))
				}
				if err := emit(); err != nil {
					return err
				}
				return fmt.Errorf("%d dependency upgrade(s) available", len(ups))
			}

			original, err := os.ReadFile(realmPath)
			if err != nil {
				return fmt.Errorf("reading realm.json: %w", err)
			}
			err = loadAndWriteRealm(cmd.Context(), realmPath, func(cur []entity.RealmDep) ([]entity.RealmDep, error) {
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
			var cfgPin *configPin
			rollback := func() error {
				// Every step is attempted, so one that fails cannot leave the
				// rest upgraded. Through replaceFile, as the upgrade wrote it:
				// atomic, keeping the file's mode, and never through a link.
				errs := []error{replaceFile(realmPath, original, "realm.json")}
				if cfgPin != nil {
					errs = append(errs, cfgPin.write(cfgPin.original))
				}
				errs = append(errs, restoreSchemas(dir, synced))
				return errors.Join(errs...)
			}
			for _, up := range ups {
				s, serr := syncCoreSchemas(cmd.Context(), dir, &up, rf.SubRealms)
				synced = append(synced, s...)
				if serr != nil {
					// Roll back first, then report: an output failure must not
					// leave the upgraded files behind.
					rerr := rollback()
					report.Errors = append(report.Errors, sanitizeLine(serr.Error()))
					eerr := emit()
					if rerr != nil {
						return errors.Join(fmt.Errorf("syncing schemas failed (%w) and restoring files failed: %w", serr, rerr), eerr)
					}
					return errors.Join(fmt.Errorf("%w; files left unchanged", serr), eerr)
				}
			}
			for _, s := range synced {
				writeln(text, cmdStyle.Render(sanitizeLine("  synced "+filepath.Join(dir, s.path))))
				report.Synced = append(report.Synced, sanitizeLine(filepath.Join(dir, s.path)))
			}

			// The repository's own config pins the same dependency; leaving it
			// would validate against the old release. Written after the schema
			// sync so a rollback below restores it too.
			cfgPin, err = planConfigPin(dir, ups)
			if err == nil && cfgPin != nil {
				err = cfgPin.write(cfgPin.updated)
			}
			if err != nil {
				rerr := rollback()
				report.Errors = append(report.Errors, sanitizeLine(err.Error()))
				eerr := emit()
				if rerr != nil {
					return errors.Join(fmt.Errorf("updating config.yaml failed (%w) and restoring files failed: %w", err, rerr), eerr)
				}
				return errors.Join(fmt.Errorf("%w; files left unchanged", err), eerr)
			}
			if cfgPin != nil {
				writeln(text, cmdStyle.Render(sanitizeLine("  updated "+cfgPin.path)))
				report.Config = append(report.Config, sanitizeLine(cfgPin.path))
			}

			res := validateUpgradedRealm(cmd.Context(), dir)
			if !res.OK() {
				if jsonOut {
					for _, d := range res.Errors() {
						report.Errors = append(report.Errors, sanitizeLine(d.Message))
					}
				} else {
					outputText(w, res)
				}
				// Roll back before reporting, so a failing output stream cannot
				// bypass the all-or-nothing guarantee.
				rerr := rollback()
				eerr := emit()
				if rerr != nil {
					return errors.Join(fmt.Errorf("upgrade breaks the realm and restoring files failed: %w", rerr), eerr)
				}
				return errors.Join(fmt.Errorf("upgrade breaks the realm (%d error(s)); files left unchanged", len(res.Errors())), eerr)
			}

			writeln(text, brandStyle.Render(fmt.Sprintf("Upgraded %d dependency(ies); realm validates", len(ups))))
			report.Applied = true
			return emit()
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./rules", "realm directory")
	cmd.Flags().StringVar(&to, "to", "", "upgrade the named dependency to this version instead of the latest")
	cmd.Flags().BoolVar(&check, "check", false, "only report available upgrades; exit non-zero if there are any")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}
