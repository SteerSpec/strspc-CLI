package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

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

			cfg, err := loadStrspcConfig(cwd)
			if err != nil {
				return err
			}

			entries, err := buildSourceEntries(cfg)
			if err != nil {
				return err
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

			resolved, res := resolver.Resolve(cmd.Context())

			w := cmd.OutOrStdout()

			if jsonOut {
				counts := make(map[string]int)
				for _, rf := range resolved {
					key := rf.Origin.Source + "\x00" + string(rf.Origin.Scope)
					counts[key]++
				}
				sources := make([]sourceSummary, 0, len(entries))
				for _, e := range entries {
					sources = append(sources, sourceSummary{
						Source:    e.Source,
						Scope:     string(e.Scope),
						RuleCount: counts[e.Source+"\x00"+string(e.Scope)],
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
				if _, err := w.Write(append(enc, '\n')); err != nil {
					return fmt.Errorf("writing JSON output: %w", err)
				}
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
