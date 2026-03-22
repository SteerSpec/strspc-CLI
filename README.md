# strspc-CLI

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/SteerSpec/strspc-CLI)](https://github.com/SteerSpec/strspc-CLI/releases)

User-facing command-line tool for the [SteerSpec](https://steerspec.dev) ecosystem.

SteerSpec manages behavioral rules as structured JSON with deterministic enforcement. The CLI renders, validates, and manages rule files locally — no server required.

## Architecture

```
Layer 1: strspc-rules    — rule definitions + JSON schemas (Python)
    ↓ consumed by
Layer 2: strspc-manager   — enforcement engine: lint, diff, eval (Go library)
    ↓ wrapped by
Layer 3: strspc-CLI       — this repo: user-facing CLI (Go)
```

- [strspc-rules](https://github.com/SteerSpec/strspc-rules) — canonical rule format specification
- [strspc-manager](https://github.com/SteerSpec/strspc-manager) — core enforcement engine

## Commands

### Available

| Command | Description |
|---------|-------------|
| `strspc render` | Convert entity JSON files to Markdown or JSON |
| `strspc version` | Show version, commit, and build info |

### Planned

| Command | Description |
|---------|-------------|
| `strspc lint` | Validate entity files against schema and rule constraints |
| `strspc diff` | Validate rule lifecycle transitions in PRs |
| `strspc eval` | Evaluate code against applicable rules (AI-powered) |
| `strspc realm init` | Scaffold a new Realm directory |
| `strspc realm add` | Add a new entity to a Realm |
| `strspc realm validate` | Validate Realm structure and EUID uniqueness |

## Installation

### From GitHub Releases

Download the latest binary for your platform from [Releases](https://github.com/SteerSpec/strspc-CLI/releases).

Available for Linux and macOS (amd64, arm64).

### From source

```bash
go install github.com/SteerSpec/strspc-CLI/src@latest
```

Or clone and build:

```bash
git clone https://github.com/SteerSpec/strspc-CLI.git
cd strspc-CLI
make build    # produces ./strspc
```

## Usage

### Render entity files to Markdown

```bash
# Single file to stdout
strspc render rules/core/ENT.json

# Directory to output dir
strspc render rules/core/ -o docs/

# Explicit format (default: markdown)
strspc render rules/core/ --format markdown

# JSON identity output (for tooling pipelines)
strspc render rules/core/ENT.json --json

# Custom Go template
strspc render rules/core/ENT.json --template my-template.md.tmpl
```

### CI usage

```yaml
- name: Render rules to Markdown
  run: strspc render rules/core/ -o rules/core/ --format markdown
- name: Check no drift
  run: git diff --exit-code rules/core/*.md
```

## Rule sources

The CLI consumes rules and schemas published at [steerspec.dev](https://steerspec.dev) from [strspc-rules](https://github.com/SteerSpec/strspc-rules):

| Resource | URL |
|----------|-----|
| Entity schema | `https://steerspec.dev/schemas/entity/v1.json` |
| Realm schema | `https://steerspec.dev/schemas/realm/v1.json` |
| Bootstrap schema | `https://steerspec.dev/schemas/entity/bootstrap.json` |
| Rules manifest | `https://steerspec.dev/rules/latest/index.json` |
| Versioned rules | `https://steerspec.dev/rules/v<version>/` |

## Development

### Prerequisites

- Go 1.26+
- [golangci-lint](https://golangci-lint.run/) v2

### Build and test

```bash
make build          # build binary → ./strspc
make test           # run tests with -race
make lint           # golangci-lint
make fmt            # formatting via golangci-lint fmt
make install-hooks  # install conventional commits git hook
```

### Commits

[Conventional Commits](https://www.conventionalcommits.org/) required. Format: `<type>(<scope>): <description>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

### Releases

Automated via [release-please](https://github.com/googleapis/release-please) (version bumps + changelog) and [goreleaser](https://goreleaser.com/) (cross-platform binaries). Pushing to `main` triggers a release PR; merging it creates a tag; the tag triggers goreleaser.

## License

[Apache License 2.0](LICENSE)
