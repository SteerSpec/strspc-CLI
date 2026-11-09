package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/spf13/cobra"
)

// schemaBaseURL is the raw-content root schemas are fetched from, as
// <schemaBaseURL>/<core tag>/<schemaPath>/<file>. Package-level so tests can override it.
var schemaBaseURL = "https://raw.githubusercontent.com/SteerSpec/strspc-rules"

// schemaPath is the directory holding the schemas inside the strspc-rules repository.
const schemaPath = "rules/core/_schema"

// schemaFiles lists the schemas vendored under _schema/ (same names upstream).
var schemaFiles = []struct {
	local string
}{
	{"bootstrap.schema.json"},
	{"entity.v1.schema.json"},
	{"realm.v1.schema.json"},
}

// realmJSON is the structure written to realm.json.
type realmJSON struct {
	Schema            string                       `json:"$schema"`
	Realm             realmMeta                    `json:"realm"`
	Dependencies      []entity.RealmDep            `json:"dependencies"`
	RuleIdentifierFmt *entity.RuleIdentifierFormat `json:"rule_identifier_format"`
}

type realmMeta struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version string `json:"version"`
	// The fields of a Standard Realm (STDRLM-001), in the order of the
	// manager's entity.RealmMeta, omitted from a code Realm's manifest.
	Kind        string              `json:"kind,omitempty"`
	License     string              `json:"license,omitempty"`
	Attribution *entity.Attribution `json:"attribution,omitempty"`
	Edition     string              `json:"edition,omitempty"`
	Profiles    []string            `json:"profiles,omitempty"`
}

// standardRealmFlags holds the flags of realm init that make a Standard Realm,
// or give a code Realm a license and an attribution.
type standardRealmFlags struct {
	kind, edition, license          string
	attributionText, attributionURL string
	profiles                        []string
	// editionSet and profilesSet record that the flag was given, so an empty
	// value is refused rather than read as no flag.
	editionSet, profilesSet bool
}

// attributionURLPattern is the attribution url pattern of realm.v1.schema.json
// (RLMMNFST-011), `^https?://[^\s/?#]+[^\s]*$`.
var attributionURLPattern = regexp.MustCompile(`^https?://[^\s/?#]+\S*$`)

// apply checks the flags and sets their fields on m. A Standard Realm needs an
// edition, a license and an attribution (STDRLM-002, STDRLM-003, STDRLM-015);
// an edition and profiles belong to a Standard Realm only (STDRLM-002,
// STDRLM-004), while any Realm may have a license and an attribution
// (RLMMNFST-010, RLMMNFST-011).
func (f *standardRealmFlags) apply(m *realmMeta) error {
	for _, v := range []struct{ flag, value string }{
		{"--kind", f.kind},
		{"--edition", f.edition},
		{"--license", f.license},
		{"--attribution-text", f.attributionText},
		{"--attribution-url", f.attributionURL},
	} {
		if err := rejectControlChars(v.flag, v.value); err != nil {
			return err
		}
	}
	standard := f.kind == entity.KindStandard
	switch {
	case f.kind != "" && !standard:
		return fmt.Errorf("invalid --kind %q: the only kind is %q (RLMMNFST-008); leave it out for a code Realm", f.kind, entity.KindStandard)
	case !standard && (f.edition != "" || f.editionSet || len(f.profiles) > 0 || f.profilesSet):
		return fmt.Errorf("--edition and --profiles describe a Standard Realm: pass --kind %s", entity.KindStandard)
	case f.profilesSet && len(f.profiles) == 0:
		return errors.New("--profiles: want one or more profile names, or leave it out (STDRLM-004)")
	case (f.attributionText == "") != (f.attributionURL == ""):
		return errors.New("--attribution-text and --attribution-url go together: an attribution has a text and a url (RLMMNFST-011)")
	case f.attributionURL != "" && !attributionURLPattern.MatchString(f.attributionURL):
		return fmt.Errorf("invalid --attribution-url %q: want an absolute http or https URL (RLMMNFST-011)", f.attributionURL)
	}
	if standard {
		var missing []string
		if strings.TrimSpace(f.edition) == "" {
			missing = append(missing, "--edition (STDRLM-002)")
		}
		if strings.TrimSpace(f.license) == "" {
			missing = append(missing, "--license (STDRLM-003)")
		}
		if f.attributionText == "" {
			missing = append(missing, "--attribution-text and --attribution-url (STDRLM-015)")
		}
		if len(missing) > 0 {
			return fmt.Errorf("a Standard Realm needs %s", strings.Join(missing, ", "))
		}
	}
	if f.license != "" && strings.TrimSpace(f.license) != f.license {
		return fmt.Errorf("invalid --license %q: want an SPDX license identifier such as CC-BY-SA-4.0 (RLMMNFST-010)", f.license)
	}
	seen := make(map[string]bool)
	for _, p := range f.profiles {
		if err := rejectControlChars("--profiles", p); err != nil {
			return err
		}
		switch {
		case strings.TrimSpace(p) == "" || strings.TrimSpace(p) != p:
			return fmt.Errorf("invalid --profiles entry %q: a profile is a non-empty name without surrounding spaces (STDRLM-004)", p)
		case seen[p]:
			return fmt.Errorf("--profiles lists %q twice: profiles are unique (STDRLM-004)", p)
		}
		seen[p] = true
	}
	m.Kind, m.License, m.Edition = f.kind, f.license, f.edition
	m.Profiles = f.profiles
	if f.attributionText != "" {
		m.Attribution = &entity.Attribution{Text: f.attributionText, URL: f.attributionURL}
	}
	return nil
}

// checkStandardRealmCore refuses to vendor, for a Standard Realm, the schemas
// of a core release older than manifestMinCore: they have no kind.
func checkStandardRealmCore(kind, tag string) error {
	if kind != entity.KindStandard {
		return nil
	}
	v, err := semver.StrictNewVersion(strings.TrimPrefix(tag, "v"))
	if err != nil || !v.LessThan(manifestMinCore) {
		return nil
	}
	return fmt.Errorf("a Standard Realm needs %s %s or later, whose schemas accept `kind`: the schemas would come from %s",
		coreRealmID, manifestMinCore, tag)
}

// realmIDPattern is the realm ID grammar of realm.v1.schema.json (realm.id),
// which realm-lint also checks: dot-separated segments, each a lowercase letter
// followed by lowercase letters and digits.
var realmIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`)

// realmIDGrammar describes realmIDPattern in error messages.
const realmIDGrammar = "dot-separated segments, each a lowercase letter followed by lowercase letters or digits"

// semverPattern matches dependencies[].version in realm.v1.schema.json.
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// parseDependency parses a dependency string in the format:
//
//	<realm_id>@<version>[=<source>]
//
// Examples:
//
//	dev.steerspec.core@0.1.0
//	dev.steerspec.core@0.1.0=github://SteerSpec/strspc-rules@latest/rules/core
func parseDependency(s string) (entity.RealmDep, error) {
	var dep entity.RealmDep
	s = strings.TrimSpace(s)

	// Split off optional source.
	idVersion, source, hasSource := strings.Cut(s, "=")
	if hasSource {
		source = strings.TrimSpace(source)
		if source == "" {
			return dep, fmt.Errorf("invalid dependency %q: source must not be empty when '=' is present", s)
		}
		if err := rejectControlChars("source", source); err != nil {
			return dep, err
		}
		dep.Source = source
	}

	// Split realm_id@version.
	idVersion = strings.TrimSpace(idVersion)
	parts := strings.SplitN(idVersion, "@", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return dep, fmt.Errorf("invalid dependency format %q: expected <realm_id>@<version>[=<source>]", s)
	}

	// Validate realm ID against the established pattern.
	if !realmIDPattern.MatchString(parts[0]) {
		return dep, fmt.Errorf("invalid realm ID %q in dependency %q: must be %s", parts[0], s, realmIDGrammar)
	}

	// Validate version against the realm schema's SemVer pattern.
	if !semverPattern.MatchString(parts[1]) {
		hint := ""
		if strings.HasPrefix(parts[1], "v") {
			hint = fmt.Sprintf(" (drop the leading %q)", "v")
		}
		return dep, fmt.Errorf("invalid version %q in dependency %q: must be SemVer, e.g. 1.2.3%s", parts[1], s, hint)
	}

	dep.RealmID = parts[0]
	dep.Version = parts[1]
	return dep, nil
}

func newRealmInitCmd() *cobra.Command {
	var (
		realmID string
		title   string
		dir     string
		force   bool
		deps    []string
		core    string
		std     standardRealmFlags
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new Realm directory",
		Long: "Create a Realm directory with realm.json and vendored schemas for rule authoring.\n\n" +
			"--kind standard makes a Standard Realm, which transcribes an external standard, one rule per\n" +
			"clause sentence (STDRLM-001). It needs the --edition of the standard (STDRLM-002), the SPDX\n" +
			"--license of its text (STDRLM-003) and an attribution, --attribution-text and\n" +
			"--attribution-url (STDRLM-015). --profiles lists, in order, the profiles its rules may\n" +
			"declare, such as the levels of the standard (STDRLM-004). Any Realm may have a license and an\n" +
			"attribution. A Standard Realm needs " + coreRealmID + " " + manifestMinCore.String() + " or later.\n\n" +
			"Examples:\n" +
			"  strspc realm init --id com.acme.myproject --title \"My Project\"\n" +
			"  strspc realm init --id dev.steerspec.std.owasp.asvs.v5 --title \"OWASP ASVS\" --kind standard \\\n" +
			"    --edition 5.0.0 --profiles L1,L2,L3 --license CC-BY-SA-4.0 \\\n" +
			"    --attribution-text \"OWASP ASVS 5.0.0, by the OWASP Foundation\" --attribution-url https://owasp.org/",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			std.editionSet, std.profilesSet = cmd.Flags().Changed("edition"), cmd.Flags().Changed("profiles")
			realmPath := filepath.Join(dir, "realm.json")

			if !force {
				if _, err := os.Stat(realmPath); err == nil {
					return fmt.Errorf("realm already initialized in %s — use --force to overwrite", dir)
				}
			}

			// The realm ID defaults to the directory name, which must then fit
			// the grammar too: realm validate would reject the realm otherwise.
			if realmID == "" {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return fmt.Errorf("resolving directory path: %w", err)
				}
				name := filepath.Base(abs)
				if !realmIDPattern.MatchString(name) {
					return fmt.Errorf("directory name %q is not a valid realm ID (%s): pass one with --id (e.g. com.acme.myproject)", name, realmIDGrammar)
				}
				realmID = name
			}
			if !realmIDPattern.MatchString(realmID) {
				return fmt.Errorf("invalid realm ID %q: must be %s (e.g. com.acme.myproject)", realmID, realmIDGrammar)
			}
			meta := realmMeta{ID: realmID, Title: title, Version: "0.1.0"}
			if err := std.apply(&meta); err != nil {
				return err
			}

			// Parse and validate dependencies before any filesystem/network operations.
			var realmDeps []entity.RealmDep
			for _, d := range deps {
				dep, err := parseDependency(d)
				if err != nil {
					return err
				}
				// One core pin: the schemas are vendored from it, so a second would
				// leave the manifest contradicting them (realm dep add refuses too).
				if dep.RealmID == coreRealmID && slices.ContainsFunc(realmDeps, func(d entity.RealmDep) bool { return d.RealmID == coreRealmID }) {
					return fmt.Errorf("duplicate dependency on %s: pass it once", coreRealmID)
				}
				if err := checkCoreSourceTag(dep); err != nil {
					return err
				}
				realmDeps = append(realmDeps, dep)
			}
			if realmDeps == nil {
				realmDeps = []entity.RealmDep{}
			}

			// Resolve the schema release before touching the filesystem, so a
			// conflicting or invalid --core-version leaves nothing behind.
			tag, err := resolveSchemaTag(cmd.Context(), core, realmDeps)
			if err != nil {
				return err
			}
			if err := checkStandardRealmCore(meta.Kind, tag); err != nil {
				return err
			}

			// Create target directory.
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dir, err)
			}

			// Fetch and write schemas.
			schemaDir := filepath.Join(dir, "_schema")
			if err := os.MkdirAll(schemaDir, 0o755); err != nil {
				return fmt.Errorf("creating _schema/: %w", err)
			}

			if err := fetchSchemas(schemaDir, tag); err != nil {
				return err
			}

			// Write realm.json.
			realm := realmJSON{
				Schema:            "./_schema/realm.v1.schema.json",
				Realm:             meta,
				Dependencies:      realmDeps,
				RuleIdentifierFmt: nil,
			}

			data, err := marshalJSON(realm)
			if err != nil {
				return fmt.Errorf("marshaling realm.json: %w", err)
			}

			// Through replaceFile, so a realm.json link left by --force is replaced,
			// not written through.
			if err := replaceFile(realmPath, data, "realm.json"); err != nil {
				return err
			}

			// Print success.
			cleanDir := filepath.Clean(dir)
			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Initialized Realm %q in %s", realmID, cleanDir)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add entities: ")+cmdStyle.Render("strspc realm add MYENTITY --title \"My Entity\""))
			step := 2
			if meta.Kind == entity.KindStandard {
				add := "strspc rule add MYENTITY --body \"...\" --clause <clause>"
				if len(meta.Profiles) > 0 {
					add += " --profiles " + meta.Profiles[0]
				}
				writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Transcribe each clause: ")+cmdStyle.Render(add))
				step++
			}
			writeln(w, cmdStyle.Render(fmt.Sprintf("  %d.", step))+descStyle.Render(" Validate: ")+cmdStyle.Render("strspc realm validate"))
			writeln(w, cmdStyle.Render(fmt.Sprintf("  %d.", step+1))+descStyle.Render(" Reference from config: ")+descStyle.Render("source: ./rules/ in .strspc/config.yaml"))
			writeln(w)

			// Suggest linking to .strspc/config.yaml if it exists.
			printConfigSuggestion(w, cleanDir)

			writeln(w, descStyle.Render("Reference: "+readmeURL))

			return nil
		},
	}

	cmd.Flags().StringVar(&realmID, "id", "", "realm ID (reverse domain notation, e.g. com.acme.myproject)")
	cmd.Flags().StringVar(&title, "title", "", "realm title")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "target directory")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing realm")
	cmd.Flags().StringVar(&core, "core-version", "", "dev.steerspec.core release to vendor schemas from (default: the dev.steerspec.core dependency, else the latest release)")
	cmd.Flags().StringArrayVar(&deps, "dependency", nil, "realm dependency (format: realm_id@version[=source])")
	cmd.Flags().StringVar(&std.kind, "kind", "", "the Realm's kind: standard for a Standard Realm; leave out for a code Realm")
	cmd.Flags().StringVar(&std.edition, "edition", "", "with --kind standard, the edition of the standard, such as 5.0.0")
	cmd.Flags().StringSliceVar(&std.profiles, "profiles", nil, "with --kind standard, the profiles its rules may declare, in order, comma-separated, such as L1,L2,L3")
	cmd.Flags().StringVar(&std.license, "license", "", "SPDX license identifier of the Realm's content, such as CC-BY-SA-4.0 (required with --kind standard)")
	cmd.Flags().StringVar(&std.attributionText, "attribution-text", "", "who the Realm's content is credited to (required with --kind standard, with --attribution-url)")
	cmd.Flags().StringVar(&std.attributionURL, "attribution-url", "", "absolute http or https URL of the content's source (required with --kind standard, with --attribution-text)")

	return cmd
}

var schemaHTTPClient = &http.Client{Timeout: 30 * time.Second}

// coreTag turns a core version ("1.17.1" or "v1.17.1") into its release tag.
func coreTag(version string) (string, error) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	// The pattern mirrors the realm schema's dependency version; the strict parse
	// also refuses forms it lets through (leading zeros in a prerelease), since
	// the value is interpolated into a URL.
	if _, err := semver.StrictNewVersion(v); err != nil || !semverPattern.MatchString(v) {
		return "", fmt.Errorf("invalid core version %q: must be SemVer, e.g. 1.17.1", version)
	}
	return "v" + v, nil
}

// isReleaseRef reports whether a source ref is a SemVer release tag (vX.Y.Z,
// with an optional prerelease or build metadata), as opposed to @latest, a
// commit SHA or a branch.
func isReleaseRef(ref string) bool {
	v, ok := strings.CutPrefix(ref, "v")
	if !ok {
		return false
	}
	_, err := semver.StrictNewVersion(v)
	return err == nil
}

// checkCoreSourceTag refuses a core dependency whose github:// source is pinned
// to a release tag other than its version: init would vendor the schemas of
// the version while the source (what validation resolves) serves another
// release, and RM015 would flag the drift. Only a release-tag ref can be
// compared; @latest, a commit SHA or a branch can't, so they pass.
func checkCoreSourceTag(dep entity.RealmDep) error {
	if dep.RealmID != coreRealmID || !strings.HasPrefix(dep.Source, remote.Scheme) {
		return nil
	}
	ref, err := remote.ParseGitHub(dep.Source)
	if err != nil || !isReleaseRef(ref.Ref) {
		return nil
	}
	want, err := coreTag(dep.Version)
	if err != nil || ref.Ref == want {
		return nil
	}
	return fmt.Errorf("%s version %s does not match its source tag %s: pin both to the same release", coreRealmID, dep.Version, ref.Ref)
}

// resolveSchemaTag picks the strspc-rules release tag the schemas are fetched
// from: the explicit version if given, else the realm's dev.steerspec.core
// dependency, else the latest release. An explicit version that disagrees with
// the core dependency is an error: the realm would pin one release and vendor
// the schemas of another, and RM015 would flag the drift at once.
func resolveSchemaTag(ctx context.Context, explicit string, deps []entity.RealmDep) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	depTag := ""
	for _, d := range deps {
		if d.RealmID == coreRealmID {
			t, err := coreTag(d.Version)
			if err != nil {
				return "", err
			}
			depTag = t
			break
		}
	}
	if explicit != "" {
		tag, err := coreTag(explicit)
		if err != nil {
			return "", err
		}
		if depTag != "" && depTag != tag {
			return "", fmt.Errorf("--core-version %s conflicts with the %s dependency at %s: drop one or make them match", tag, coreRealmID, depTag)
		}
		return tag, nil
	}
	if depTag != "" {
		return depTag, nil
	}
	ref := remote.Ref{Owner: "SteerSpec", Repo: "strspc-rules"}
	tag, err := latestReleaseTag(ctx, ref, remote.TokenForOwner(ref.Owner))
	if err != nil {
		return "", fmt.Errorf("resolving core version for schemas (pass --core-version to pin one): %w", err)
	}
	return coreTag(tag)
}

func fetchSchemas(schemaDir, tag string) error {
	for _, sf := range schemaFiles {
		if err := fetchSchema(schemaDir, tag, sf.local); err != nil {
			return err
		}
	}
	return nil
}

func fetchSchema(schemaDir, tag, local string) error {
	url := schemaBaseURL + "/" + tag + "/" + schemaPath + "/" + local
	outPath := filepath.Join(schemaDir, local)

	resp, err := schemaHTTPClient.Get(url) //nolint:gosec // tag is validated SemVer; host is a constant
	if err != nil {
		return fmt.Errorf("fetching schema %s@%s: %w", local, tag, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching schema %s@%s: HTTP %d", local, tag, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading schema %s: %w", local, err)
	}

	// Written verbatim: RM015 requires _schema/ to match core's copies, so any
	// version marker has to come from the core release itself.
	return os.WriteFile(outPath, body, 0o644)
}

func printConfigSuggestion(w io.Writer, realmDir string) {
	realmDir = filepath.Clean(realmDir)

	configPath := filepath.Join(".strspc", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return // no config — skip suggestion
	}

	if strings.Contains(string(data), realmDir) {
		return // already referenced
	}

	writeln(w, descStyle.Render("Tip: Add this under `rules:` in .strspc/config.yaml:"))
	writeln(w)
	writeln(w, cmdStyle.Render(fmt.Sprintf("  - source: %s", realmDir)))
	writeln(w, cmdStyle.Render("    scope: local"))
	writeln(w)
}
