<div align="center">

<img src="https://steerspec.dev/assets/logo.svg" alt="SteerSpec logo" width="96" height="96">

# SteerSpec CLI

**Author, validate and enforce behavioral rules from the command line.**

[![CI](https://img.shields.io/github/actions/workflow/status/SteerSpec/strspc-CLI/ci.yml?label=CI&labelColor=161B22)](https://github.com/SteerSpec/strspc-CLI/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/SteerSpec/strspc-CLI?labelColor=161B22&color=161B22)](https://github.com/SteerSpec/strspc-CLI/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/SteerSpec/strspc-CLI?labelColor=161B22&color=161B22)](go.mod)
[![License](https://img.shields.io/badge/License-Apache_2.0-161B22?labelColor=161B22)](LICENSE)

[Install](#installation) · [Quick start](#quick-start) · [Where to go](#where-to-go) ·
[Governance](#governance) · [Security](#security) · [Community](#community)

</div>

`strspc` is the command-line tool, built on the [strspc-manager](https://github.com/SteerSpec/strspc-manager)
engine, for engineers who keep behavioral rules as structured JSON next to the code they govern.
It writes rule files with valid versions and hashes, validates them and their lifecycle, and asks an
AI evaluator whether a code change complies; authoring and validation run locally, with no backend
service of ours (remote rule sources, dependencies and schemas are fetched over the network).

**Status.** The [latest release](https://github.com/SteerSpec/strspc-CLI/releases/latest) is under the
[Apache 2.0 licence](LICENSE). `strspc check` sends the diff to an AI provider: only Claude
is implemented, and it needs an `ANTHROPIC_API_KEY` (see [Work in progress](docs/reference.md#work-in-progress)). The
hosted service is planned and not available.

## Installation

**Homebrew (macOS)**

```bash
brew install --cask SteerSpec/tap/strspc
```

Homebrew can refuse to load a cask from a third-party tap until you trust it. If brew refuses, run
`brew trust --cask SteerSpec/tap/strspc` (or `brew trust SteerSpec/tap`) and retry.

**GitHub Releases**

Download an archive from [Releases](https://github.com/SteerSpec/strspc-CLI/releases). Linux and
macOS builds (amd64, arm64) are `.tar.gz` files. Windows builds (amd64, arm64) are `.zip` files with
`strspc.exe`, which you can put in any directory on your `PATH`. Only the macOS binaries are
notarized. The Linux and Windows binaries are not code-signed, so SmartScreen may warn on the first
run of the Windows build.

**GitHub Actions**

The composite action installs a release for the runner and verifies its checksum:

```yaml
- uses: SteerSpec/strspc-CLI/.github/actions/setup-strspc@v1.36.0
  with:
    version: "latest"       # the default, or a release such as "1.36.0"
- run: strspc version
```

**From source** (Go 1.26+)

```bash
git clone https://github.com/SteerSpec/strspc-CLI.git
cd strspc-CLI
make build    # produces ./strspc
```

Use `make build`: `go install` names the binary `src` after the package directory, and leaves out
the version information.

## Quick start

```bash
# Consume the core rules
strspc init

# Author rules in your own realm, pinned to the core realm
strspc realm init --dir rules --id com.acme.app --title "Acme App" \
  --dependency dev.steerspec.core@1.17.1=github://SteerSpec/strspc-rules@v1.17.1/rules/core
strspc realm add CFG --title "Configuration"
strspc rule add CFG --body "The CLI MUST write a config file." --added-by github:alice

# Validate them
strspc lint rules/ --strict
strspc realm validate rules/ --recursive --strict

# Evaluate your code: structural checks first, then Claude
strspc check --static-only
ANTHROPIC_API_KEY=... strspc check
```

Add `./rules/` to `.strspc/config.yaml` to
[evaluate your own realm](docs/configuration.md#configure-a-project).

## Where to go

| I want to | Go to |
| --------- | ----- |
| Install `strspc` | [Installation](#installation) |
| Write and promote my first rule | [Quick start](#quick-start), [Author rules](docs/author-rules.md) |
| Understand entities, rules and realms | [Concepts](docs/architecture.md#concepts) |
| See how the CLI, the manager and the rules fit together | [Architecture](docs/architecture.md#architecture) |
| Find a command | [Commands](docs/architecture.md#commands) |
| Configure a project | [Configure a project](docs/configuration.md#configure-a-project) |
| See how a project stands, or diagnose my setup | [Status and doctor](docs/configuration.md#see-how-a-project-stands) |
| Validate rules and their lifecycle | [Validate rules](docs/validate.md#validate-rules) |
| Check that docs cite live rules | [Validate rule citations](docs/validate.md#validate-rule-citations) |
| Understand a diagnostic code | [Understand a diagnostic](docs/validate.md#understand-a-diagnostic) |
| Check a code change against my rules | [Check code changes](docs/check.md) |
| Render rules to Markdown | [Render rules](docs/render-and-ci.md#render-rules) |
| Run SteerSpec in CI | [Continuous integration](docs/render-and-ci.md#continuous-integration) |
| Map rules to an external standard | [Report coverage of a standard](docs/standards.md) |
| Look up an environment variable or schema URL | [Reference](docs/reference.md) |
| See what is not implemented yet | [Work in progress](docs/reference.md#work-in-progress) |
| Build and contribute | [Development](docs/development.md) |
| Release | [Branching and releases](docs/releases.md) |
| Report a vulnerability | [Security](#security) |

## Governance

Open source software by Consulare.

Consulare is the copyright holder named in the [NOTICE](NOTICE) file, and the code is licensed under
the [Apache 2.0 licence](LICENSE).

### Development

Build, test and commit conventions are in [Development](docs/development.md), and the
[SteerSpec contributing guide](https://github.com/SteerSpec/.github/blob/v1.2.0/CONTRIBUTING.md)
applies to every contribution.

### Branching and releases

The branch flow, the release steps and what to do when a commit is dropped from a changelog are in
[Branching and releases](docs/releases.md).

## Security

Report a vulnerability privately: on this repository, go to **Security**, then **Report a
vulnerability**, or open the [advisory form](https://github.com/SteerSpec/strspc-CLI/security/advisories/new).
Do not open a public issue, discussion or pull request for a security problem. The
[SteerSpec security policy](https://github.com/SteerSpec/.github/blob/v1.2.0/SECURITY.md) covers
supported versions and what happens after a report.

## Community

Participation follows the [SteerSpec code of conduct](https://github.com/SteerSpec/.github/blob/v1.2.0/CODE_OF_CONDUCT.md).
To contribute, read the [contributing guide](https://github.com/SteerSpec/.github/blob/v1.2.0/CONTRIBUTING.md).

## License

Apache 2.0, see [LICENSE](LICENSE).
