package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	)

	cmd := &cobra.Command{
		Use:           "init",
		Short:         "Scaffold a new Realm directory",
		Long:          "Create a Realm directory with realm.json and vendored schemas for rule authoring.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			// Parse and validate dependencies before any filesystem/network operations.
			var realmDeps []entity.RealmDep
			for _, d := range deps {
				dep, err := parseDependency(d)
				if err != nil {
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
				Schema: "./_schema/realm.v1.schema.json",
				Realm: realmMeta{
					ID:      realmID,
					Title:   title,
					Version: "0.1.0",
				},
				Dependencies:      realmDeps,
				RuleIdentifierFmt: nil,
			}

			data, err := marshalJSON(realm)
			if err != nil {
				return fmt.Errorf("marshaling realm.json: %w", err)
			}

			if err := os.WriteFile(realmPath, data, 0o644); err != nil {
				return fmt.Errorf("writing realm.json: %w", err)
			}

			// Print success.
			cleanDir := filepath.Clean(dir)
			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Initialized Realm %q in %s", realmID, cleanDir)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add entities: ")+cmdStyle.Render("strspc realm add MYENTITY --title \"My Entity\""))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Validate: ")+cmdStyle.Render("strspc realm validate"))
			writeln(w, cmdStyle.Render("  3.")+descStyle.Render(" Reference from config: ")+descStyle.Render("source: ./rules/ in .strspc/config.yaml"))
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
