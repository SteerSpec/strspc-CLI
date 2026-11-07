# Reference

Back to the [README](../README.md).

## GitHub sources

Rule sources and realm dependencies can point at a directory of a GitHub repository
([SPEC §6.4](https://github.com/SteerSpec/strspc-manager/blob/v1.45.0/docs/SPEC.md#64-remote-sources-github)):

```text
github://<owner>/<repo>@<ref>[/<path>]
```

`<ref>` is required. An exact tag or commit SHA is cached indefinitely, and `strspc sync --force`
re-downloads it. `latest` follows the newest release, re-resolved after `cache.ttl`. Branch names
resolve but are discouraged, because they need a forced sync to pick up commits. Sources are fetched
through the GitHub tarball API and cached under `.strspc/cache/`.

`strspc sync` fetches and verifies the sources ahead of a check. It takes `--force`, `--json` and
`--verbose`.

## Environment variables

| Variable | Used for |
| -------- | -------- |
| `ANTHROPIC_API_KEY` | The Claude evaluator |
| `STRSPC_EVALUATOR_ENDPOINT` | Opt in to a non-Anthropic endpoint |
| `STRSPC_ADDED_BY` | Default for `--added-by` in `rule add`, `note add` and `rule supersede` |
| `GITHUB_TOKEN`, then `GH_TOKEN` | Private `github://` sources and higher rate limits |
| `STRSPC_TOKEN_OWNERS` | Comma-separated owners who may also receive the GitHub token |
| `GITHUB_REPOSITORY_OWNER` | Owner who may receive the GitHub token; set in GitHub Actions |
| `GITHUB_BASE_REF` | Default diff base of `strspc check`; set on pull request events |
| `GITHUB_STEP_SUMMARY` | Where `--format github` appends its table |

> [!IMPORTANT]
> The GitHub token is sent only for sources owned by `SteerSpec`, by `GITHUB_REPOSITORY_OWNER`, or by
> an owner in `STRSPC_TOKEN_OWNERS`, because a source can come from a `realm.json` you only cloned.
> Other sources are fetched anonymously, so a private one returns 404 until you list its owner.

## Diagnostic codes

Every code the manager emits (`RL`, `RD`, `RM`, `RR`, `RSV`, `RE`, `EC`, `RF`) is listed in
[SPEC §6.3](https://github.com/SteerSpec/strspc-manager/blob/v1.45.0/docs/SPEC.md#63-diagnostic-codes).
The CLI adds two warnings of its own, `CHK002` and `CHK003`, described under
[check](check.md#safety-and-configuration-checks). `strspc explain` covers all of them.

## Published files

The SteerSpec rules repository publishes these at [steerspec.dev](https://steerspec.dev). The CLI
fetches rules through `github://` sources. `strspc realm init` and `realm add-subrealm` don't use the
schema URLs below: they copy `_schema/` unchanged from a `dev.steerspec.core` release tag in the
`strspc-rules` repository (see [Realms and dependencies](author-rules.md#realms-and-dependencies)).

| Resource | URL |
| -------- | --- |
| Entity schema | `https://steerspec.dev/schemas/entity/v1.json` |
| Realm schema | `https://steerspec.dev/schemas/realm/v1.json` |
| Config schema | `https://steerspec.dev/schemas/config/v1.json` |
| Bootstrap schema | `https://steerspec.dev/schemas/entity/bootstrap.json` |
| Rules manifest | `https://steerspec.dev/rules/latest/index.json` |
| Versioned rules | `https://steerspec.dev/rules/v<version>/` |
| Schemas of a core release | `https://steerspec.dev/rules/v<version>/_schema/` |

## Work in progress

The specification already covers these, but this CLI doesn't do them yet:

| Item | Status |
| ---- | ------ |
| **`effort: xhigh` and `max`** | The spec and the Claude provider accept them; the published `config.v1` schema doesn't yet, so they give warning `CHK002` |
| **OpenAI and Ollama providers** | Reserved names that fail with a not-implemented error |
| **Fallback without an LLM** | SPEC §8.4 falls back to structural checks; today `strspc check` exits 3 with provider `claude` and no key, so pass `--static-only` |
| **Cloud mode** | SPEC §8.5 plans a CI action that calls a hosted SteerSpec evaluator, with shared caching and a compliance dashboard. The hosted service is planned, and the CLI has no cloud commands |
