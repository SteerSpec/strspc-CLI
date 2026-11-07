# Configure and diagnose a project

Back to the [README](../README.md).

## Configure a project

`strspc init` writes `.strspc/config.yaml` and a `.strspc/.gitignore` that keeps `cache.db` and
`cache/` out of version control. Pass `--provider null` for an offline setup without an API key, and
`--force` to overwrite. This example adds a local realm to the generated config
([SPEC §2.9](https://github.com/SteerSpec/strspc-manager/blob/v1.44.0/docs/SPEC.md#29-configuration)):

```yaml
rules:
  - source: github://SteerSpec/strspc-rules@v1.21.0/rules/core   # bumped per rules release
    scope: global
  - source: ./rules/
    scope: global
evaluator:
  provider: claude          # requires ANTHROPIC_API_KEY; use "null" to skip AI evaluation
  model: claude-sonnet-5-5  # the default
  effort: medium            # low | medium | high
cache:
  ttl: 24h
evaluate_on: [D, P, I]      # the default
fail_on:
  - I                       # state codes (D, A, P, I, R, T), not words
```

| Field | Meaning |
| ----- | ------- |
| `rules[].source` | A local directory or a [`github://` reference](reference.md#github-sources). At least one is required (SPCFG-004) |
| `rules[].scope` | `global` applies to the whole repository, `local` only to files under the source's own directory |
| `evaluator.provider` | `claude`, or `null` for structural checks only (`static` and `static-only` are aliases). `openai` and `ollama` are reserved and fail with a not-implemented error |
| `evaluator.endpoint` | Provider endpoint, subject to [endpoint safety](check.md#safety-and-configuration-checks) |
| `evaluator.model`, `evaluator.effort` | Model and reasoning effort passed to the provider |
| `cache.ttl` | How long evaluations and `@latest` lookups stay valid |
| `evaluate_on` | Rule states sent to the evaluator; other states are skipped |
| `fail_on` | Rule states whose violations fail the check; other violations are warnings |

> [!TIP]
> Use `scope: global` for a local realm that governs your code, as the CLI's
> [own config](../.strspc/config.yaml) does. `strspc realm init` suggests `scope: local`, which limits
> the rules to files under `rules/`, and those are never evaluated.

## See how a project stands

`strspc status` shows the configuration, each rule source, rule counts by state and the realm. The
output below is a sample:

```text
$ strspc status
SteerSpec CLI 1.36.0

Config: .strspc/config.yaml
  Evaluator: claude
  Fail on: I
  Cache TTL: 24h0m0s

Sources:
  - github://SteerSpec/strspc-rules@v1.21.0/rules/core (global): pinned, cached, 154 rule(s)
  - ./rules/ (global): local, 13 rule(s)

Rules: 167 total
  53 Draft  44 Published  70 Implemented  0 Abandoned  0 Retired  0 Terminated

Realm: dev.steerspec.cli 0.1.0
  Entities: CFG, CMD, CONV
  Sub-realms: none
```

It only reads: it makes no network call and fills no cache, so a source that was never fetched shows
as `not synced`, and `strspc sync` fetches it. Only `@latest` can go stale, once it resolved longer ago
than `cache.ttl`; a pinned source can't. `--json` prints the same report as one document. It exits 0,
also in a project with no configuration, 2 when the configuration can't be parsed, and 4 when the
`--json` report can't be written.

## Diagnose problems

`status` reports; `strspc doctor` judges. Each check passes, warns or fails, and a warning or a
failure comes with the command that fixes it. The output below is a sample:

```text
$ strspc doctor
strspc doctor v1.36.0

install
  ✓ version    v1.36.0
  ⚠ path       /usr/local/bin/strspc comes first on PATH, not this binary (/Users/me/go/bin/strspc)
               → remove /usr/local/bin/strspc, or put /Users/me/go/bin before it on PATH
...
credentials
  ⚠ provider   ANTHROPIC_API_KEY is not set: strspc check exits 3 until it is
               → export ANTHROPIC_API_KEY, or set evaluator.provider: null
...
0 failed, 2 warning(s), 15 passed, 1 skipped, 1 info
```

| Group | Checks |
| ----- | ------ |
| `install` | The version is a release, the `strspc` on `PATH` is this binary, and the platform |
| `tools` | `git` is on `PATH`; missing is a failure inside a git repository |
| `config` | `.strspc/config.yaml` exists, parses, matches the cached `config.v1` schema, and has a valid `cache.ttl`, state codes, scopes and provider |
| `credentials` | `ANTHROPIC_API_KEY` is set when the provider is `claude`, and a GitHub token is sent to the owner of each `github://` source. Values are never printed |
| `sources` | Each source exists, or is cached and not stale |
| `realm` | Each local realm passes `realm validate`, and its core pin accepts the standards it declares |
| `project` | `.strspc/cache/` and `.strspc/cache.db` are git-ignored, and a workflow runs `strspc check` |

Like `status`, doctor only reads: without `--online` it makes no network call, and it never fills a
cache, so what was never fetched (a schema, a source, a realm dependency) is skipped or reported as not cached. Check IDs such as
`credentials.provider` are stable and appear in `--json`, which prints
`{"version", "checks": [{"id", "group", "subject", "status", "message", "remedy"}], "summary"}`.
It exits 0 when no check failed, 1 when one did (or, with `--strict`, when one warned), 2 when the
path can't be read, and 4 when the `--json` report can't be written. A missing API key is a warning,
since lint and realm validation work without it.

`--online` adds the checks that call the GitHub API, and still fills no cache:

| Check | What it checks |
| ----- | -------------- |
| `online.latest` | The version is not older than the latest strspc release (a warning); development builds are skipped, and a failed lookup is a warning |
| `online.sources` | The ref and path of each `github://` source exist. Not found or an invalid token fails; rate limiting is a warning |

The GitHub token goes only to the owners it is allowed for, as when fetching sources.
