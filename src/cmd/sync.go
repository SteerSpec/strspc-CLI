package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// syncConfig mirrors the relevant fields from .strspc/config.yaml.
type syncConfig struct {
	Rules []struct {
		Source string `yaml:"source"`
		Scope  string `yaml:"scope"`
	} `yaml:"rules"`
	Cache struct {
		TTL string `yaml:"ttl"`
	} `yaml:"cache"`
}

// syncOutput is the JSON representation of a sync result.
type syncOutput struct {
	OK          bool                `json:"ok"`
	RuleCount   int                 `json:"rule_count"`
	Sources     []sourceSummary     `json:"sources"`
	Diagnostics []result.Diagnostic `json:"diagnostics"`
}

type sourceSummary struct {
	Source    string `json:"source"`
	Scope     string `json:"scope"`
	RuleCount int    `json:"rule_count"`
}

func newSyncCmd() *cobra.Command {
	var (
		force   bool
		verbose bool
		jsonOut bool
	)

	cmd := &cobra.Command{
		Use:           "sync [path]",
		Short:         "Fetch and cache rules from configured sources",
		Long:          "Resolve and cache rule sets declared in .strspc/config.yaml. Defaults to the current directory.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd := "."
			if len(args) > 0 {
				cwd = args[0]
			}

			configPath := filepath.Join(cwd, ".strspc", "config.yaml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				return fmt.Errorf("no .strspc/config.yaml found — run strspc init first")
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("reading config.yaml: %w", err)
			}

			var cfg syncConfig
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return fmt.Errorf("parsing config.yaml: %w", err)
			}

			entries := make([]ruleresolve.SourceEntry, 0, len(cfg.Rules))
			for _, r := range cfg.Rules {
				scope := ruleresolve.ScopeLocal
				if r.Scope == string(ruleresolve.ScopeGlobal) {
					scope = ruleresolve.ScopeGlobal
				}
				entries = append(entries, ruleresolve.SourceEntry{
					Source: r.Source,
					Scope:  scope,
				})
			}

			var ttl time.Duration
			if cfg.Cache.TTL != "" {
				ttl, err = time.ParseDuration(cfg.Cache.TTL)
				if err != nil {
					return fmt.Errorf("parsing cache.ttl %q: %w", cfg.Cache.TTL, err)
				}
			}

			opts := []ruleresolve.Option{
				ruleresolve.WithBaseDir(cwd),
				ruleresolve.WithCacheDir(filepath.Join(cwd, ".strspc")),
				ruleresolve.WithForceSync(force),
			}
			if ttl > 0 {
				opts = append(opts, ruleresolve.WithCacheTTL(ttl))
			}

			resolver, err := ruleresolve.New(entries, opts...)
			if err != nil {
				return fmt.Errorf("initializing resolver: %w", err)
			}

			resolved, res := resolver.Resolve(context.Background())

			w := cmd.OutOrStdout()

			if jsonOut {
				counts := make(map[string]int)
				for _, rf := range resolved {
					counts[rf.Origin.Source]++
				}
				sources := make([]sourceSummary, 0, len(entries))
				for _, e := range entries {
					sources = append(sources, sourceSummary{
						Source:    e.Source,
						Scope:     string(e.Scope),
						RuleCount: counts[e.Source],
					})
				}
				diags := res.Diagnostics
				if diags == nil {
					diags = []result.Diagnostic{}
				}
				out := syncOutput{
					OK:          res.OK(),
					RuleCount:   len(resolved),
					Sources:     sources,
					Diagnostics: diags,
				}
				enc, marshalErr := json.MarshalIndent(out, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("marshaling output: %w", marshalErr)
				}
				_, _ = w.Write(append(enc, '\n'))
				if !res.OK() {
					return fmt.Errorf("sync found %d error(s)", len(res.Errors()))
				}
				return nil
			}

			if len(res.Diagnostics) > 0 {
				outputText(w, res)
			}

			if !res.OK() {
				return fmt.Errorf("sync found %d error(s)", len(res.Errors()))
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Synced %d rule(s) from %d source(s)", len(resolved), len(entries))))

			if verbose {
				for _, rf := range resolved {
					writeln(w, descStyle.Render("  "+rf.Path))
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "bypass cache and re-fetch all sources")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "list each resolved rule file")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output result as JSON")

	return cmd
}
