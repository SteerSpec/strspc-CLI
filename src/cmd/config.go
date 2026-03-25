package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// strspcConfig mirrors the fields from .strspc/config.yaml used by multiple commands.
type strspcConfig struct {
	Rules []struct {
		Source string `yaml:"source"`
		Scope  string `yaml:"scope"`
	} `yaml:"rules"`
	Cache struct {
		TTL string `yaml:"ttl"`
	} `yaml:"cache"`
	Evaluator struct {
		Provider string `yaml:"provider"`
		Endpoint string `yaml:"endpoint"`
		Model    string `yaml:"model"`
	} `yaml:"evaluator"`
	FailOn []string `yaml:"fail_on"`
}

// loadStrspcConfig reads and parses .strspc/config.yaml under cwd.
// Returns a user-friendly error if the file is absent (prompting strspc init).
func loadStrspcConfig(cwd string) (*strspcConfig, error) {
	configPath := filepath.Join(cwd, ".strspc", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no .strspc/config.yaml found — run strspc init first")
		}
		return nil, fmt.Errorf("accessing %s: %w", configPath, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config.yaml: %w", err)
	}
	var cfg strspcConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config.yaml: %w", err)
	}
	return &cfg, nil
}

// buildSourceEntries converts the rules section of a config into ruleresolve.SourceEntry
// values, validating each scope value.
func buildSourceEntries(cfg *strspcConfig) ([]ruleresolve.SourceEntry, error) {
	entries := make([]ruleresolve.SourceEntry, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		var scope ruleresolve.Scope
		switch r.Scope {
		case string(ruleresolve.ScopeLocal):
			scope = ruleresolve.ScopeLocal
		case string(ruleresolve.ScopeGlobal):
			scope = ruleresolve.ScopeGlobal
		default:
			return nil, fmt.Errorf("invalid scope %q for source %q: must be %q or %q",
				r.Scope, r.Source, ruleresolve.ScopeLocal, ruleresolve.ScopeGlobal)
		}
		entries = append(entries, ruleresolve.SourceEntry{
			Source: r.Source,
			Scope:  scope,
		})
	}
	return entries, nil
}
