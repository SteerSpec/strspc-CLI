package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-CLI/src/internal/testutil"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
)

// A config endpoint is the PR-editable case: each provider accepts only its
// own hosts, and any other needs the env or the flag.
func TestResolveEndpointPerProvider(t *testing.T) {
	cases := []struct {
		provider, cfg string
		ok            bool
	}{
		{"claude", "https://api.anthropic.com", true},
		{" Claude ", "https://eu.api.anthropic.com/v1", true}, // matched as the manager matches names
		{"claude", "https://api.openai.com/v1", false},        // another provider's host
		{"claude", "https://anthropic.com.evil.example", false},

		{"openai", "https://api.openai.com/v1", true},
		{"openai", "https://API.OpenAI.com/v1", true},
		{"openai", "http://api.openai.com/v1", false}, // plain http leaks the key in transit
		{"openai", "https://api.openai.com.evil.com/v1", false},
		{"openai", "https://eu.api.openai.com/v1", false}, // exact host, no subdomains
		{"openai", "https://mycorp.openai.azure.com", false},
		{"openai", "https://api.anthropic.com", false},

		{"openrouter", "https://openrouter.ai/api/v1", true},
		{"openrouter", "https://openrouter.ai@evil.com/api/v1", false}, // userinfo, host is evil.com
		{"openrouter", "https://api.openrouter.ai/v1", false},
		{"openrouter", "http://openrouter.ai/api/v1", false},

		{"ollama", "http://localhost:11434", true},
		{"ollama", "http://127.0.0.1:11434", true},
		{"ollama", "http://[::1]:11434", true},
		{"ollama", "https://localhost:11434", true},
		{"ollama", "http://localhost.evil.com:11434", false},
		{"ollama", "http://192.168.1.5:11434", false},
		{"ollama", "https://ollama.example.com", false},
		{"ollama", "ftp://localhost", false},

		{"claud", "https://api.anthropic.com", false}, // no row, no policy
	}
	for _, c := range cases {
		got, err := resolveEndpoint(c.provider, c.cfg, "", "")
		if c.ok && (err != nil || got != c.cfg) {
			t.Errorf("%s: config endpoint %q refused: %v", c.provider, c.cfg, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: config endpoint %q accepted", c.provider, c.cfg)
		}
		// The env and the flag opt in from outside the repository.
		if strings.HasPrefix(c.cfg, "https://") {
			if got, err := resolveEndpoint(c.provider, "", c.cfg, ""); err != nil || got != c.cfg {
				t.Errorf("%s: --endpoint %q = %q, %v", c.provider, c.cfg, got, err)
			}
		}
	}
}

// The error names what the provider accepts, and for claude reads as before.
func TestResolveEndpointErrors(t *testing.T) {
	for _, c := range []struct{ provider, cfg, want string }{
		{"claude", "https://other.example", `evaluator.endpoint "https://other.example" points outside anthropic.com: ` +
			`the API key would be sent to that host; opt in explicitly from outside the repository with ` +
			envEvaluatorEndpoint + `=<url> or --endpoint <url>`},
		{"claude", "http://api.anthropic.com", `evaluator.endpoint "http://api.anthropic.com" is not an https URL`},
		{"openai", "https://other.example", "points outside api.openai.com: the API key"},
		{"ollama", "http://gpu.example:11434", "points outside a loopback host: the code under evaluation"},
		{"claud", "https://api.anthropic.com", `evaluator.provider "claud" is not a known provider (claude|null)`},
	} {
		_, err := resolveEndpoint(c.provider, c.cfg, "", "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: err = %v, want %q", c.provider, c.cfg, err, c.want)
		}
	}
}

func TestEvalProviderTable(t *testing.T) {
	if got := providerChoices(); got != "claude|null" {
		t.Errorf("providerChoices = %q", got)
	}
	if got := aiProviderChoices(); got != "claude" {
		t.Errorf("aiProviderChoices = %q", got)
	}
	p := lookupEvalProvider(defaultEvalProvider)
	if p == nil || !p.available || p.keyEnv != claude.EnvAPIKey || p.defaultModel != claude.DefaultModel {
		t.Errorf("default provider row = %+v", p)
	}
	if lookupEvalProvider("nope") != nil || lookupEvalProvider(staticProviderName) != nil {
		t.Error("only AI providers have rows")
	}

	saved := evalProviders
	t.Cleanup(func() { evalProviders = saved })
	evalProviders = append([]evalProvider(nil), saved...)
	for i := range evalProviders {
		evalProviders[i].available = evalProviders[i].name != "ollama"
	}
	if got := aiProviderChoices(); got != "claude, openai or openrouter" {
		t.Errorf("aiProviderChoices with three = %q", got)
	}
	if got := providerChoices(); got != "claude|openai|openrouter|null" {
		t.Errorf("providerChoices with three = %q", got)
	}
}

// An available row has what init and the endpoint policy need: a default
// model for the config's comment and a policy. Making a row available
// before its default model is filled fails here.
func TestEvalProviderRowsComplete(t *testing.T) {
	for i := range evalProviders {
		p := &evalProviders[i]
		if p.configEndpoint == nil || p.endpointHosts == "" {
			t.Errorf("%s: no endpoint policy", p.name)
		}
		if p.available && p.defaultModel == "" {
			t.Errorf("%s is available without a default model: init would write a blank model comment", p.name)
		}
		if p.available && p.keyEnv == "" && strings.Contains(renderConfig("x", p.name), "requires") {
			t.Errorf("%s takes no key, but the config says it requires one", p.name)
		}
	}
}

func TestCheckEffort(t *testing.T) {
	for _, c := range []struct {
		provider, effort string
		ok               bool
	}{
		{"claude", "high", true},
		{"ollama", "", true},
		{"ollama", "high", false},
		{" OLLAMA ", "low", false},
		{"unknown", "high", true}, // left to strspc-manager
	} {
		if err := checkEffort(c.provider, c.effort); (err == nil) != c.ok {
			t.Errorf("checkEffort(%q, %q) = %v", c.provider, c.effort, err)
		}
	}
}

// check --effort overrides evaluator.effort (low in the test config) as
// --model overrides evaluator.model, and the JSON output reports it.
func TestCheckEffortFlag(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "low"},
		{[]string{"--effort", " high "}, "high"},
	} {
		dir, diff := setupCheckProject(t, "claude", []string{"I"})
		opts := stubEvalProvider(t, &stubProvider{}, nil)
		args := append([]string{dir, "--diff-file", diff, "--no-cache", "--format", "json"}, c.args...)
		output, err := runCheckCmd(t, args...)
		assertExit(t, err, exitOK)
		if opts.Effort != c.want {
			t.Errorf("%v: effort passed to provider = %q, want %q", c.args, opts.Effort, c.want)
		}
		var out checkOutput
		if err := json.Unmarshal([]byte(output), &out); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, output)
		}
		if out.Effort != c.want {
			t.Errorf("%v: JSON effort = %q, want %q", c.args, out.Effort, c.want)
		}
	}
}

// Effort for a provider without the setting is a configuration error, and
// static checks ignore it.
func TestCheckEffortUnsupported(t *testing.T) {
	dir, diff := setupCheckProject(t, "ollama", []string{"I"})
	stubEvalProvider(t, &stubProvider{}, nil)
	_, err := runCheckCmd(t, dir, "--diff-file", diff, "--no-cache")
	assertExit(t, err, exitConfig)
	testutil.AssertContains(t, err.Error(), `provider "ollama" has no reasoning-effort setting`)

	_, err = runCheckCmd(t, dir, "--diff-file", diff, "--no-cache", "--static-only")
	assertExit(t, err, exitOK)
}

// doctor reports the same effort error, and a provider row's key variable.
func TestDoctorProviderFromTable(t *testing.T) {
	env := &doctorEnv{getenv: func(string) string { return "" }}
	cfg := &strspcConfig{}
	cfg.Evaluator.Provider = "ollama"
	cfg.Evaluator.Effort = "high"
	var project doctorProject
	if r := doctorProviderCheck(env, cfg, &project); r.Status != doctorFail || !strings.Contains(r.Message, "reasoning-effort") {
		t.Errorf("ollama with effort: %+v", r)
	}

	project = doctorProject{cfg: cfg, ai: lookupEvalProvider("ollama")}
	if r := doctorCredentialChecks(env, &project)[0]; r.Status != doctorSkip || r.Message != "ollama takes no API key" {
		t.Errorf("keyless provider: %+v", r)
	}
	project.ai = lookupEvalProvider("claude")
	if r := doctorCredentialChecks(env, &project)[0]; r.Status != doctorWarn || !strings.Contains(r.Message, claude.EnvAPIKey+" is not set") {
		t.Errorf("claude without a key: %+v", r)
	}
}
