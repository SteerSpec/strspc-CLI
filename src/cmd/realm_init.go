package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// schemaBaseURL is the base URL for fetching schemas. Exported for testing.
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
	Schema            string        `json:"$schema"`
	Realm             realmMeta     `json:"realm"`
	Dependencies      []interface{} `json:"dependencies"`
	RuleIdentifierFmt *string       `json:"rule_identifier_format"`
}

type realmMeta struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

func newRealmInitCmd() *cobra.Command {
	var (
		realmID string
		title   string
		dir     string
		force   bool
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

			// Default realm ID to directory name.
			if realmID == "" {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return fmt.Errorf("resolving directory path: %w", err)
				}
				realmID = filepath.Base(abs)
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

			// Write realm.json.
			realm := realmJSON{
				Schema: "./_schema/realm.v1.schema.json",
				Realm: realmMeta{
					ID:      realmID,
					Title:   title,
					Version: "0.1.0",
				},
				Dependencies:      []interface{}{},
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
			w := cmd.OutOrStdout()
			writeln(w, brandStyle.Render(fmt.Sprintf("Initialized Realm %q in %s/", realmID, dir)))
			writeln(w)
			writeln(w, descStyle.Render("Next steps:"))
			writeln(w, cmdStyle.Render("  1.")+descStyle.Render(" Add entities: ")+cmdStyle.Render("strspc realm add MYENTITY --title \"My Entity\""))
			writeln(w, cmdStyle.Render("  2.")+descStyle.Render(" Validate: ")+cmdStyle.Render("strspc realm validate"))
			writeln(w, cmdStyle.Render("  3.")+descStyle.Render(" Reference from config: ")+descStyle.Render("source: ./rules/ in .strspc/config.yaml"))
			writeln(w)

			// Suggest linking to .strspc/config.yaml if it exists.
			printConfigSuggestion(w, dir)

			writeln(w, descStyle.Render("Docs: https://steerspec.dev/docs/realm"))

			return nil
		},
	}

	cmd.Flags().StringVar(&realmID, "id", "", "realm ID (reverse domain notation, e.g. com.acme.myproject)")
	cmd.Flags().StringVar(&title, "title", "", "realm title")
	cmd.Flags().StringVar(&dir, "dir", "./rules", "target directory")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing realm")

	return cmd
}

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

	resp, err := http.Get(url) //nolint:gosec // URL is a compile-time constant
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
	configPath := filepath.Join(".strspc", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return // no config — skip suggestion
	}

	if strings.Contains(string(data), realmDir) {
		return // already referenced
	}

	writeln(w, descStyle.Render("Tip: Add this source to .strspc/config.yaml:"))
	writeln(w, cmdStyle.Render(fmt.Sprintf("  - source: %s/", realmDir)))
	writeln(w, cmdStyle.Render("    scope: local"))
	writeln(w)
}
