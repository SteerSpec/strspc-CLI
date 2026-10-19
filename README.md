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
| `strspc init` | Bootstrap `.strspc/config.yaml` (and `.strspc/.gitignore`) in a project |
| `strspc lint` | Validate entity files against schema and rule constraints |
| `strspc diff` | Validate rule lifecycle transitions between a git ref and the working tree |
| `strspc check` | Evaluate a code diff against the rules declared in `.strspc/config.yaml` with Claude (or `--static-only`) |
| `strspc sync` | Resolve and cache the rule sources declared in `.strspc/config.yaml` |
| `strspc render` | Convert entity JSON files to Markdown or JSON |
| `strspc realm init` | Scaffold a new Realm directory |
| `strspc realm add` | Add a new entity to a Realm |
| `strspc realm add-subrealm` | Scaffold a sub-realm inside an existing Realm |
| `strspc realm dep` | Add or remove Realm dependencies |
| `strspc realm validate` | Validate Realm structure and EUID uniqueness |
| `strspc rule add` | Add a Draft rule to an entity or sub-entity |
| `strspc rule update` | Edit the body of a Draft rule |
| `strspc rule promote` | Advance a rule D→P or P→I |
| `strspc rule retire` | Move a rule I→R or R→T |
| `strspc rule abandon` | Abandon a Draft rule (D→A) |
| `strspc rule supersede` | Create a Draft rule that supersedes a P/I/R rule |
| `strspc note add` | Attach a typed note (`rationale`, `example`, ...) to a rule |
| `strspc entity rehash` | Recompute null or stale `rule_set.hash` values |
| `strspc version` | Show version, commit, and build info |

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

### Initialize a project

```bash
strspc init                      # writes .strspc/config.yaml with provider: claude
strspc init --provider null .    # offline: static checks only, no API key needed
```

`init` writes a config that satisfies core rule SPCFG-004 (at least one rule source):

```yaml
rules:
  - source: github://SteerSpec/strspc-rules@v1.6.0/rules/core   # bumped per rules release
    scope: global
evaluator:
  provider: claude          # requires ANTHROPIC_API_KEY; use "null" to skip AI evaluation
cache:
  ttl: 24h
fail_on:
  - I                       # state codes (D, A, P, I, R, T), not words
```

`fail_on` uses rule state codes as defined by `config.v1.schema.json`. It also writes `.strspc/.gitignore` (`cache.db`, `cache/`) so cache artifacts stay out of version control. Until a strspc-manager release ships remote fetching, the resolver rejects any active `github://` entry: replace it with (or comment it out in favour of) a local `./rules/` source before running `check` or `sync`.

### Author rules

```bash
strspc realm init --dir rules --id com.acme.app --title "Acme App"
strspc realm add CFG --title "Configuration" --dir rules
strspc rule add CFG --body "MUST write a config file." --added-by "@alice" --dir rules
strspc rule promote CFG-001 --dir rules                      # D → P
strspc note add CFG-001 --type rationale --body "Why." --added-by "@alice" --dir rules
strspc rule add CFG --sub-entity CFGSUB --body "..." --added-by "@alice"   # target a sub-entity
strspc entity rehash --all --dir rules                       # fix null/stale hashes after a bulk import
```

Rule commands accept a sub-entity EUID anywhere a top-level EUID is accepted: the CLI scans `sub_entities[]` of every entity file under `--dir` and refreshes the parent hash after the change.

### Validate and check

```bash
strspc lint rules/ --strict
strspc realm validate rules/ --recursive
strspc diff rules/ --base origin/main        # lifecycle transitions vs. a git ref
strspc diff rules/ --pr 42                   # ... or vs. a PR base (needs gh auth)
strspc check --static-only                   # resolve sources + structural checks
```

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

### Check code changes against rules

`strspc check` resolves the rule sources declared in `.strspc/config.yaml` (local directories, or
`github://owner/repo@ref/path` references cached under `.strspc/cache/`), collects a unified diff and
asks the configured AI provider for one verdict per rule: `compliant`, `violated` or `not_applicable`.
Violations of rules whose lifecycle state is listed in `fail_on` fail the check; other violations are
reported as warnings. Only Claude is implemented today (`evaluator.provider: claude`, key from
`ANTHROPIC_API_KEY`); `openai` and `ollama` are reserved and fail with a not-implemented error.

```bash
strspc check                                 # working tree vs HEAD
strspc check --staged                        # index vs HEAD (pre-commit)
strspc check --base origin/main              # merge-base of origin/main and HEAD vs working tree
strspc check --pr 42                         # same, with the PR base resolved through gh
strspc check --diff-file change.diff         # evaluate a diff produced elsewhere
strspc check --static-only                   # resolve sources only, no AI call (RE003)
strspc check --format github                 # ::error/::warning annotations + $GITHUB_STEP_SUMMARY
```

| Flag | Description |
|------|-------------|
| `--base <ref>` | Diff the working tree against the merge base of `<ref>` and HEAD |
| `--staged` | Diff the index against HEAD |
| `--pr <n>` | Diff against the base of GitHub PR `n` (needs the `gh` CLI) |
| `--diff-file <path>` | Read a unified diff from a file instead of running git |
| `--format human\|json\|github` | Output style; `github` emits workflow-command annotations and appends a table to `$GITHUB_STEP_SUMMARY` |
| `--provider`, `--model` | Override `evaluator.provider` / `evaluator.model` |
| `--endpoint <url>` | Provider endpoint; required (or `STRSPC_EVALUATOR_ENDPOINT`) to opt in to a non-Anthropic host |
| `--static-only` | Skip AI evaluation (also implied by `evaluator.provider: null`) |
| `--no-cache` | Ignore `.strspc/cache.db` (verdicts are otherwise cached for `cache.ttl`) |
| `--verbose` | Print resolution, diff, cache and token-usage details to stderr |

Diff selection precedence: `--diff-file` > `--staged` > `--pr` > `--base` > `$GITHUB_BASE_REF`
(`origin/<ref>`, fetched when missing) > working tree vs HEAD. Files under `.strspc/` and `rules/`
are never part of the evaluated diff. Rules in states listed in `evaluate_on` (default `D`, `P`, `I`)
are evaluated; others are skipped. The check only ever writes inside `.strspc/` (`cache/`, `cache.db`).

**Endpoint safety.** `.strspc/config.yaml` is repository content, so a pull request could point
`evaluator.endpoint` at a host that would receive your `ANTHROPIC_API_KEY`. The check therefore accepts an
`evaluator.endpoint` from the config only when it is an `https://` URL on `anthropic.com` (for example
`https://api.anthropic.com`). Any other endpoint (a proxy, a gateway) must be opted in from outside the
repository: `STRSPC_EVALUATOR_ENDPOINT=<url>` (highest precedence) or `--endpoint <url>`. A non-Anthropic
endpoint in the config without that opt-in exits 2 with a message naming the host.

| Exit code | Meaning |
|-----------|---------|
| `0` | No violation of a rule in a `fail_on` state (warnings may have been reported) |
| `1` | At least one rule in a `fail_on` state was violated |
| `2` | Configuration, rule resolution or diff collection failed |
| `3` | AI provider unavailable: no `ANTHROPIC_API_KEY`, rejected credentials (401/403), unreachable endpoint |

### CI usage

```yaml
- run: strspc lint rules/ --strict
- run: strspc realm validate rules/ --recursive
- run: strspc diff rules/ --pr ${{ github.event.pull_request.number }}
  env:
    GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

```yaml
# .github/workflows/strspc.yml (excerpt)
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0                     # --base needs the merge base
      - name: Detect evaluator key          # secrets.* cannot be used in if:
        id: key
        run: echo "has_key=${{ secrets.ANTHROPIC_API_KEY != '' }}" >> "$GITHUB_OUTPUT"
      - name: Check code changes (Claude)
        if: steps.key.outputs.has_key == 'true'
        run: strspc check --base origin/main --format github
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}   # this step only
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}             # github:// rule sources
      - name: Check rules (static fallback)
        if: steps.key.outputs.has_key != 'true'
        run: strspc check --static-only
```

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
