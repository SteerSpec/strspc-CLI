# Security Policy

This file replaces the [SteerSpec organization policy](https://github.com/SteerSpec/.github/blob/main/SECURITY.md)
for strspc-CLI. Reporting works the same way. This file adds the time frames for strspc's dependencies.

## Supported versions

Only the latest release receives fixes. Older releases are not backported, so update with
`brew upgrade --cask strspc` or download the latest binary from the
[releases page](https://github.com/SteerSpec/strspc-CLI/releases).

## Reporting a vulnerability

**Do not open a public issue, discussion, or pull request for a security problem.**

Report it privately: on this repository, go to **Security → Report a vulnerability**, or use
[`/security/advisories/new`](https://github.com/SteerSpec/strspc-CLI/security/advisories/new). We
aim to acknowledge a report within five working days. We coordinate disclosure through a GitHub
Security Advisory published with the release that contains the fix, and credit the reporter unless
they ask us not to.

## Dependency time frames

strspc is a Go binary, so its dependencies are the Go modules it is built from and the Go standard
library of the toolchain that builds it. Releases build with the Go version that `go.mod` names.
The time frames depend on the risk, and CI enforces each one through the
[Security workflow](.github/workflows/security.yml):

| Risk | Time frame | Enforced by |
|---|---|---|
| [govulncheck](https://go.dev/doc/security/vuln/) reports a vulnerability that affects strspc's code: its call graph reaches the vulnerable symbol, in a module or in the standard library | Fixed before the next merge to `develop` | `make vulncheck` fails the `govulncheck` check on every pull request. It also runs daily on `develop`, so a new disclosure fails the scheduled run. |
| A direct dependency has a newer release, with or without a vulnerability that strspc doesn't call | Updated within **30 days** of its newest release | `make depcheck` runs daily on `develop` and fails when a direct dependency is not on its newest release and that release is more than 30 days old. |

The 30 days is `DEP_MAX_AGE_DAYS` in the [Makefile](Makefile), and the govulncheck version is
`GOVULNCHECK_VERSION`.

### What the checks don't cover

- **Each new release restarts the 30 days.** The age gate measures from the newest release, as `go
  list -m -u` reports it. A dependency that publishes often can stay behind for longer than 30 days.
- **A new major version** (a new module path such as `/v4`) is not tracked. Neither is a
  pre-release.
- **An indirect module** is covered only by govulncheck, which fails when the code calls the
  vulnerable symbol. Any other indirect update is made when a direct dependency or the toolchain
  moves.
- **GitHub Actions** used by the workflows are updated weekly by Dependabot. No time frame applies.
