package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/spf13/cobra"
)

// schemaBaseURL is the base URL for fetching schemas. Package-level so tests can override it.
var schemaBaseURL = "https://steerspec.dev/schemas"

// schemaFiles maps remote schema paths to local filenames under _schema/.
var schemaFiles = []struct {
	remote string // path under schemaBaseURL
	local  string // filename under _schema/
}{
	{"entity/bootstrap.json", "bootstrap.schema.json"},
	{"entity/v1.json", "entity.v1.schema.json"},
	{"realm/v1.json", "realm.v1.schema.json"},
}

// realmJSON is the structure written to realm.json.
type realmJSON struct {
	Schema            string            `json:"$schema"`
	Realm             realmMeta         `json:"realm"`
	Dependencies      []entity.RealmDep `json:"dependencies"`
	RuleIdentifierFmt *string           `json:"rule_identifier_format"`
}

type realmMeta struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// realmIDPattern validates realm IDs: lowercase alphanumeric, dots, and hyphens.
// Must not start or end with a dot or hyphen.
var realmIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.\-]*[a-z0-9])?$`)

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

	// Split off optional source.
	idVersion, source, hasSource := strings.Cut(s, "=")
	if hasSource {
		dep.Source = source
	}

	// Split realm_id@version.
	parts := strings.SplitN(idVersion, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return dep, fmt.Errorf("invalid dependency format %q: expected <realm_id>@<version>[=<source>]", s)
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
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new Realm directory",
		Long:  "Create a Realm directory with realm.json and vendored schemas for rule authoring.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			realmPath := filepath.Join(dir, "realm.json")

			if !force {
				if _, err := os.Stat(realmPath); err == nil {
					return fmt.Errorf("realm already initialized in %s — use --force to overwrite", dir)
				}
			}

			// Default realm ID to directory name (no validation for defaults).
			idExplicit := cmd.Flags().Changed("id")
			if realmID == "" {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return fmt.Errorf("resolving directory path: %w", err)
				}
				realmID = filepath.Base(abs)
			}

			// Validate explicitly provided realm IDs.
			if idExplicit {
				if !realmIDPattern.MatchString(realmID) {
					return fmt.Errorf("invalid realm ID %q: must be lowercase alphanumeric with dots and hyphens (e.g. com.acme.myproject)", realmID)
				}
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

			if err := fetchSchemas(schemaDir); err != nil {
				return err
			}

			// Parse dependencies.
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

			data, err := json.MarshalIndent(realm, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling realm.json: %w", err)
			}

			if err := os.WriteFile(realmPath, append(data, '\n'), 0o644); err != nil {
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

			writeln(w, descStyle.Render("Docs: https://steerspec.dev/docs/realm"))

			return nil
		},
	}

	cmd.Flags().StringVar(&realmID, "id", "", "realm ID (reverse domain notation, e.g. com.acme.myproject)")
	cmd.Flags().StringVar(&title, "title", "", "realm title")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "target directory")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing realm")
	cmd.Flags().StringArrayVar(&deps, "dependency", nil, "realm dependency (format: realm_id@version[=source])")

	return cmd
}

var schemaHTTPClient = &http.Client{Timeout: 30 * time.Second}

func fetchSchemas(schemaDir string) error {
	for _, sf := range schemaFiles {
		if err := fetchSchema(schemaDir, sf.remote, sf.local); err != nil {
			return err
		}
	}
	return nil
}

func fetchSchema(schemaDir, remote, local string) error {
	url := schemaBaseURL + "/" + remote
	outPath := filepath.Join(schemaDir, local)

	resp, err := schemaHTTPClient.Get(url) //nolint:gosec // URL is a compile-time constant
	if err != nil {
		return fmt.Errorf("fetching schema %s: %w", remote, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching schema %s: HTTP %d", remote, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading schema %s: %w", remote, err)
	}

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

	writeln(w, descStyle.Render("Tip: Add this to the rules section in .strspc/config.yaml:"))
	writeln(w)
	writeln(w, cmdStyle.Render("  rules:"))
	writeln(w, cmdStyle.Render(fmt.Sprintf("    - source: %s", realmDir)))
	writeln(w, cmdStyle.Render("      scope: local"))
	writeln(w)
}
