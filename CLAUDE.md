# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rule Manager Specification

This CLI wraps strspc-manager modules. The specification that defines how rules are
validated and enforced lives at:
[`strspc-manager/docs/SPEC.md`](https://github.com/SteerSpec/strspc-manager/blob/main/docs/SPEC.md).

Key sections for CLI work: §6 (Module Breakdown), §8 (Enforcement Architecture).

## Project

strspc-CLI — CLI tool for SteerSpec (part of the steerspec org).

## Language

Go

## Build & Run

```bash
make build        # build binary → ./strspc
make test         # run tests with -race
make lint         # golangci-lint
make fmt          # gofumpt formatting
make clean        # remove build artifacts
```

## Setup

```bash
make install-hooks   # install conventional commits git hook
```

## Conventions

### Commits

Use [Conventional Commits](https://www.conventionalcommits.org/). Format: `<type>(<scope>): <description>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

Breaking changes: add `!` after type/scope (e.g. `feat!: remove legacy auth`) or a `BREAKING CHANGE:` footer.

### Versioning

Semantic Versioning (semver). Commit types drive version bumps:
- `fix` → patch (0.0.X)
- `feat` → minor (0.X.0)
- breaking change (`!` or `BREAKING CHANGE:` footer) → major (X.0.0)

### Linting

golangci-lint v2 with: errcheck, gocritic, govet, ineffassign, staticcheck. Formatter: gofumpt.

### Releases

Automated via release-please (version bumps + changelog) and goreleaser (cross-platform binaries). Pushing to main triggers a release PR; merging it creates a tag; the tag triggers goreleaser.

<!-- STEERSPEC:BEGIN:org-standards -->
## SteerSpec org standards

Managed by [SteerSpec/.claude](https://github.com/SteerSpec/.claude) through
[strspc-sync](https://github.com/SteerSpec/strspc-sync). Change
`templates/CLAUDE.md.mustache` there; edits made here are overwritten by the
next sync.

- **Tiers.** strspc-rules (rule definitions) → strspc-manager (Go engine) →
  strspc-CLI and the other consumers of strspc-manager. Dependencies point
  downward only. Release and tag upstream before bumping a downstream `go.mod`
  or core pin.
- **Pull requests only.** Org rulesets reject direct pushes to the default
  branch and require verified commit signatures; Copilot review and
  pr-auto-approve gate the merge. Agents open PRs and never merge them.
- **Conventional commits** (`type(scope): subject`). Where a repo uses
  release-please, it derives versions and changelogs from them; other repos
  follow the release process in their own README or AGENTS.md.
- **Rules live next to the code.** Each repo owns a realm under `rules/` that
  depends on `dev.steerspec.core`. Author rules through `strspc rule …` so the
  content hashes stay valid; never hand-edit rule JSON.
- **Rule states.** Draft text may still change. Published text is final while
  the code may still violate it. Implemented is enforced in CI (`fail_on: [I]`);
  promote to Implemented only once the code complies.
- **Work tracking.** Multi-session work is tracked in beads (`bd`), not in
  markdown todo files.
- **Org agents.** Architect, Agile PM, Code Review and Release come from the
  `strspc-agents` plugin: `/plugin marketplace add SteerSpec/.claude`, then
  `/plugin install strspc-agents@steerspec`.
<!-- STEERSPEC:END:org-standards -->