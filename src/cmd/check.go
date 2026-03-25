package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

func newCheckCmd() *cobra.Command {
	var (
		provider   string
		staticOnly bool
		jsonOut    bool
	)

	cmd := &cobra.Command{
		Use:   "check [path]",
		Short: "Evaluate code changes against resolved rules",
		Long: "Evaluate rule compliance for the rule set declared in .strspc/config.yaml. " +
			"Defaults to the current directory.\n\n" +
			"Use --static-only to skip AI evaluation and run structural checks only.",
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

			resolveOpts := []ruleresolve.Option{
				ruleresolve.WithBaseDir(cwd),
				ruleresolve.WithCacheDir(filepath.Join(cwd, ".strspc")),
			}
			if ttl > 0 {
				resolveOpts = append(resolveOpts, ruleresolve.WithCacheTTL(ttl))
			}

			resolver, err := ruleresolve.New(entries, resolveOpts...)
			if err != nil {
				return fmt.Errorf("initializing resolver: %w", err)
			}

			resolved, resolveRes := resolver.Resolve(cmd.Context())
			w := cmd.OutOrStdout()
			if !resolveRes.OK() {
				if jsonOut {
					_ = writeJSON(w, resolveRes)
				} else if len(resolveRes.Diagnostics) > 0 {
					outputText(w, resolveRes)
				}
				return fmt.Errorf("check: rule resolution failed with %d error(s)", len(resolveRes.Errors()))
			}

			var inputs []ruleeval.RuleInput
			for _, rf := range resolved {
				inputs = append(inputs, ruleeval.RuleInputsFromFile(rf.File)...)
			}

			effectiveProvider := cfg.Evaluator.Provider
			if provider != "" {
				effectiveProvider = provider
			}
			effectiveStatic := staticOnly ||
				effectiveProvider == "" ||
				strings.EqualFold(effectiveProvider, "null") ||
				strings.EqualFold(effectiveProvider, "static")

			if !effectiveStatic {
				return fmt.Errorf("AI evaluation not yet available — use --static-only or set evaluator.provider: null in config")
			}

			evalOpts := []ruleeval.Option{ruleeval.WithStaticOnly(true)}
			if len(cfg.FailOn) > 0 {
				evalOpts = append(evalOpts, ruleeval.WithFailOn(normalizeStateCodes(cfg.FailOn)))
			}

			evaluator, err := ruleeval.New(nil, evalOpts...)
			if err != nil {
				return fmt.Errorf("initializing evaluator: %w", err)
			}

			res := evaluator.Evaluate(cmd.Context(), inputs, "")

			if jsonOut {
				if writeErr := writeJSON(w, res); writeErr != nil {
					return writeErr
				}
				if !res.OK() {
					return fmt.Errorf("check found %d error(s)", len(res.Errors()))
				}
				return nil
			}

			if len(res.Diagnostics) > 0 {
				outputText(w, res)
			}

			if !res.OK() {
				return fmt.Errorf("check found %d error(s)", len(res.Errors()))
			}

			writeln(w, brandStyle.Render(fmt.Sprintf("Checked %d rule(s) from %d source(s)", len(inputs), len(entries))))
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "AI provider: claude|openai|ollama|static (overrides config)")
	cmd.Flags().BoolVar(&staticOnly, "static-only", false, "skip AI evaluation; structural checks only")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output diagnostics as JSON")

	return cmd
}

// normalizeStateCodes converts human-readable state names (e.g. "implemented")
// to the single-letter codes expected by ruleeval (e.g. "I"). Short codes are
// passed through unchanged so both forms are accepted in config.yaml.
func normalizeStateCodes(states []string) []string {
	nameToCode := map[string]string{
		"draft":       "D",
		"abandoned":   "A",
		"published":   "P",
		"implemented": "I",
		"retired":     "R",
		"terminated":  "T",
	}
	out := make([]string, len(states))
	for i, s := range states {
		if code, ok := nameToCode[strings.ToLower(s)]; ok {
			out[i] = code
		} else if len(s) == 1 {
			out[i] = strings.ToUpper(s)
		} else {
			out[i] = s
		}
	}
	return out
}
