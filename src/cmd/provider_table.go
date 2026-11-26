package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
)

// evalProvider describes an AI evaluator provider: everything the CLI says
// or decides about one outside strspc-manager, which builds it. Adding a
// provider once the manager ships it means adding a row to evalProviders.
type evalProvider struct {
	// name is what evaluator.provider and --provider take.
	name string
	// keyEnv is the environment variable holding the API key; "" when the
	// provider takes none.
	keyEnv string
	// defaultModel is the model called when evaluator.model is unset.
	defaultModel string
	// takesEffort reports whether the provider has a reasoning-effort
	// setting. Setting evaluator.effort for one that hasn't is an error.
	takesEffort bool
	// available reports whether strspc-manager implements the provider yet.
	available bool
	// endpointHosts describes, for messages, the hosts configEndpoint
	// accepts.
	endpointHosts string
	// configEndpoint reports whether the repository config may set u as the
	// endpoint on its own: u is parsed, and its host is set.
	configEndpoint func(u *url.URL) bool
}

// defaultEvalProvider is the provider init and ci init write.
const defaultEvalProvider = "claude"

// staticProviderName is the provider name that turns AI evaluation off.
const staticProviderName = "null"

// evalProviders lists the providers the CLI knows, available or not.
// strspc-manager#67 tracks openai, openrouter and ollama.
var evalProviders = []evalProvider{
	{
		name: "claude", keyEnv: claude.EnvAPIKey, defaultModel: claude.DefaultModel, takesEffort: true, available: true,
		endpointHosts:  "anthropic.com",
		configEndpoint: func(u *url.URL) bool { return isHTTPS(u) && hostIsOrUnder(u, "anthropic.com") },
	},
	{
		name: "openai", keyEnv: "OPENAI_API_KEY", takesEffort: true,
		endpointHosts:  "api.openai.com",
		configEndpoint: func(u *url.URL) bool { return isHTTPS(u) && hostIs(u, "api.openai.com") },
	},
	{
		name: "openrouter", keyEnv: "OPENROUTER_API_KEY", takesEffort: true,
		endpointHosts:  "openrouter.ai",
		configEndpoint: func(u *url.URL) bool { return isHTTPS(u) && hostIs(u, "openrouter.ai") },
	},
	{
		// No API key, so no key to leak: any loopback endpoint, over http too.
		name:           "ollama",
		endpointHosts:  "a loopback host",
		configEndpoint: func(u *url.URL) bool { return checkHTTPS(u) == nil && isLoopbackHost(u.Hostname()) },
	},
}

// ciKeySecret is the API key secret the generated check workflow passes:
// the default provider's, the one SteerSpec/.github's strspc-check.yml
// declares. Other providers in ci init are #290.
func ciKeySecret() string { return lookupEvalProvider(defaultEvalProvider).keyEnv }

// lookupEvalProvider returns the row for name, matched as strspc-manager
// matches provider names (trimmed, case-insensitive), or nil.
func lookupEvalProvider(name string) *evalProvider {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range evalProviders {
		if evalProviders[i].name == name {
			return &evalProviders[i]
		}
	}
	return nil
}

// availableProviderNames lists the providers that can be configured today,
// null last: the values evaluator.provider takes.
func availableProviderNames() []string {
	var names []string
	for i := range evalProviders {
		if evalProviders[i].available {
			names = append(names, evalProviders[i].name)
		}
	}
	return append(names, staticProviderName)
}

// providerChoices is availableProviderNames for help text: "claude|null".
func providerChoices() string { return strings.Join(availableProviderNames(), "|") }

// aiProviderNames is availableProviderNames without null.
func aiProviderNames() []string {
	names := availableProviderNames()
	return names[:len(names)-1]
}

// aiProviderChoices names the available AI providers for messages:
// "claude", or "claude or openai".
func aiProviderChoices() string {
	names := aiProviderNames()
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// endpointFlagHelp is the help text of --endpoint, shared by check and rule
// verify.
const endpointFlagHelp = "provider endpoint URL; opts in to a host the repository config may not set (also $" + envEvaluatorEndpoint + ")"

// effortChoices is the help text of --effort, shared by check and rule verify.
const effortChoices = "reasoning effort (overrides evaluator.effort): low|medium|high|xhigh|max"

// checkEffort refuses an effort for a provider that has no such setting,
// rather than ignoring it. An unknown provider is left to strspc-manager,
// which reports it.
func checkEffort(provider, effort string) error {
	p := lookupEvalProvider(provider)
	if p == nil || p.takesEffort || strings.TrimSpace(effort) == "" {
		return nil
	}
	return fmt.Errorf("evaluator.effort %q is set, but provider %q has no reasoning-effort setting: remove it or --effort", effort, p.name)
}

func isHTTPS(u *url.URL) bool { return strings.EqualFold(u.Scheme, "https") }

func hostIs(u *url.URL, host string) bool { return strings.EqualFold(u.Hostname(), host) }

// hostIsOrUnder reports whether u's host is domain or a subdomain of it.
func hostIsOrUnder(u *url.URL, domain string) bool {
	h := strings.ToLower(u.Hostname())
	return h == domain || strings.HasSuffix(h, "."+domain)
}
