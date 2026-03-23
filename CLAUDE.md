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
