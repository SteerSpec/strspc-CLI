# Reference

Back to the [README](../README.md).

## GitHub sources

Rule sources and realm dependencies can point at a directory of a GitHub repository
([SPEC §6.4](https://github.com/SteerSpec/strspc-manager/blob/v1.47.1/docs/SPEC.md#64-remote-sources-github)):

```text
github://<owner>/<repo>@<ref>[/<path>]
```

`<ref>` is required. An exact tag or commit SHA is cached indefinitely, and `strspc sync --force`
re-downloads it. `latest` follows the newest release, re-resolved after `cache.ttl`. Branch names
resolve but are discouraged, because they need a forced sync to pick up commits. Sources are fetched
through the GitHub tarball API and cached under `.strspc/cache/`.

`strspc sync` fetches and verifies the sources ahead of a check. It takes `--force`, `--json` and
`--verbose`.

## Schema cache

Rule files, realm manifests and `.strspc/config.yaml` are validated against JSON schemas published on
steerspec.dev. Every command keeps them in one cache, shared by all projects:
`$STRSPC_SCHEMA_CACHE`, or `strspc/schemas` under the user cache directory (`~/Library/Caches` on
macOS, `$XDG_CACHE_HOME` or `~/.cache` on Linux, `%LocalAppData%` on Windows). A cached schema is
used offline. A core release's schemas never change and are kept for good, below `versions/`; a
latest schema is refreshed after an hour, when the network can be reached.

| Command | Does |
| ------- | ---- |
| `strspc schema` | List the cached schemas, with their core release and when they were fetched |
| `strspc schema fetch [path]` | Download the latest config, entity and realm schemas, and the entity, realm and bootstrap schemas of the core release each local realm in the project pins (with its sub-realms). `--dir <realm>` and `--core-version <X.Y.Z>` add more |
| `strspc schema update` | Download the latest schemas again; a copy is replaced only once its download succeeds |
| `strspc schema clear` | Delete the cached schemas |

Run `strspc schema fetch` once while online to validate offline afterwards, for example before a
pre-commit hook runs `strspc check`. In CI, cache the directory or set `STRSPC_SCHEMA_CACHE` to a
path the workflow caches.

`strspc realm init` keeps the schemas it vendors in the same cache, below `versions/`, and reads
them from there. A pinned release that is cached, from an earlier init or from
`schema fetch --core-version`, needs no network. Without a pin, when the latest release can't be looked
up, init uses the newest release cached in full and prints a warning naming it and why the lookup
failed.

On Windows, `schema fetch` refuses a source in `.strspc/config.yaml` that is a network or device path
(`\\host\share`), and any symbolic link or junction in the paths it reads from a project. Pass a share,
or a linked realm, that you trust with `--dir`.

A download that isn't a JSON schema, such as a proxy's sign-in page, is never cached: the copy already
cached stays in use.

`schema fetch`, `schema update` and `realm init` mark the cache with a `.strspc-schema-cache` file.
`schema clear` runs only on the default location or a marked directory. It deletes only the schema
files strspc writes (`config/v1.json`, `entity/v1.json`, `realm/v1.json`, `entity/bootstrap.json`,
the last three below `versions/X.Y.Z/` too, and their temporary files), also inside the `.update-N` folders an interrupted update leaves, and
then the empty folders there. Other files and symbolic links are kept.

> [!WARNING]
> Point `STRSPC_SCHEMA_CACHE` at a directory used only for schemas. In any other directory, a
> fetch overwrites a file that has the path of a schema, such as `config/v1.json`, marks the
> directory as a cache, and `schema clear` then deletes that file.
>
> The cache is trusted like any other file you own. Anyone who can write to it can replace the
> schemas. `schema update` refuses a folder in the cache that is a symbolic link, but someone who can
> write to the cache could swap a folder for a link between that check and the move.

## Environment variables

| Variable | Used for |
| -------- | -------- |
| `ANTHROPIC_API_KEY` | The Claude evaluator |
| `STRSPC_EVALUATOR_ENDPOINT` | Opt in to an endpoint the config may not set ([endpoint safety](check.md#safety-and-configuration-checks)): `https://`, or `http://` to a loopback host |
| `STRSPC_SCHEMA_CACHE` | Directory of the [schema cache](#schema-cache) |
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
[SPEC §6.3](https://github.com/SteerSpec/strspc-manager/blob/v1.47.1/docs/SPEC.md#63-diagnostic-codes).
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

## Usage data

The CLI collects no telemetry. For maintainers, `scripts/usage-report.sh` reads two things GitHub
already keeps:

- The **download counts** of release archives, per version and platform. `setup-strspc` downloads one
  archive per CI run, so they mostly count CI runs. They are cumulative: two snapshots give the
  downloads in between.
- The **pinned version** of each public SteerSpec repository workflow that calls the strspc workflows
  or `setup-strspc`. `--include-private` adds private repositories, for local use only: a snapshot of
  this public repository must not name them.

The **Usage snapshot** workflow runs it weekly and keeps each snapshot as an artifact for 90 days.
`scripts/usage-report.sh --from <snapshot>.json` renders one again. It can't see `go install`, or a
binary downloaded once and run many times.

## Work in progress

The specification already covers these, but this CLI doesn't do them yet:

| Item | Status |
| ---- | ------ |
| **`refs.coverage`** | `config.v1` accepts it, but `strspc refs` doesn't read it yet, so it reports no rule that no file cites |
| **OpenAI and Ollama providers** | Reserved names that fail with a not-implemented error |
| **Fallback without an LLM** | SPEC §8.4 falls back to structural checks; today `strspc check` exits 3 with provider `claude` and no key, so pass `--static-only` |
| **Cloud mode** | SPEC §8.5 plans a CI action that calls a hosted SteerSpec evaluator, with shared caching and a compliance dashboard. The hosted service is planned, and the CLI has no cloud commands |
