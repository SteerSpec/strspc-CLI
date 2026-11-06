# Render rules and run in CI

Back to the [README](../README.md).

## Show rules

`strspc rule show` prints rules looked up by ID or by entity, with their state, body and notes, so a
prompt or a doc can name a rule instead of copying it. Like `refs`, it reads `.strspc/config.yaml` in
the current directory and includes the realm dependencies it pins:

```bash
strspc rule show CMD-001                      # one rule
strspc rule show CFG --state I,P              # an entity's own rules, narrowed by state
strspc rule show dev.steerspec.core:ENT-001   # only in that realm
strspc rule show CMD-001 CFG --json           # a JSON list
```

An unqualified ID resolves to the first source that holds it, the way `refs` resolves citations.
`--state` narrows what an EUID selects; a rule named by its ID is shown whatever its state. A
sub-entity is selected by its own EUID. If any argument matches nothing, nothing is printed and the
command exits 1; a configuration or resolution error exits 2.

## Render rules

```bash
strspc render rules/CFG.json                        # single file to stdout
strspc render rules/ -o docs/                       # directory to docs/ (default: alongside the source)
strspc render rules/CFG.json --json                 # normalized JSON for tooling pipelines
strspc render rules/CFG.json --template my.md.tmpl  # custom Go template
```

Rule identifiers follow the `rule_identifier_format` of the realm that owns the entity file (the
nearest `realm.json`; for a sub-realm, its parent's format). Without one, the RLIFRMT defaults apply,
as in `[ENT-001.0/D]`. Only the rendered form changes, never the stored ID
([SPEC §4](https://github.com/SteerSpec/strspc-manager/blob/v1.42.0/docs/SPEC.md#4-rule-identifier-format)).

An entity that declares `inherits` gets an "Inherited rules" section listing the rules it inherits,
with their owner and state, and which of its own rules overrides them. `--json` adds them as an
`inherited` array, each entry naming the inheriting entity in `inherited_by`, also for sub-entities;
the array is empty when the chain resolved but nothing is inherited yet, and absent without a chain.
Only such a file makes `render` run realm-resolve, which can download `github://` dependencies into
`.strspc/cache/`. A chain that doesn't resolve is a warning and the entity renders without the section;
`strspc check` is what fails on it.

## Continuous integration

`strspc ci init` writes the workflow for the repository it runs in:

```bash
strspc ci init            # realm repo → lint workflow; other repos → check workflow + config
strspc ci init --dry-run  # print the files instead of writing them
```

- **Realm repo** (has `rules/realm.json`): `.github/workflows/strspc-lint.yml` validates the realm,
  lints entities and checks lifecycle transitions on pull requests, in strict mode.
- **Consumer repo**: `.strspc/config.yaml` plus `.github/workflows/strspc-check.yml`. Without an
  `ANTHROPIC_API_KEY` secret the job runs structural checks only.
- Workflows run for the repository's default branch (from `origin/HEAD`, else `main`); pass `--branch`
  to override, or `--kind realm|consumer` to override the detection.
- `--rules-source` points a consumer at your realm. If it's a private GitHub repository, add a
  `STRSPC_GITHUB_TOKEN` repository secret with read access to it.
- Workflows install the latest `strspc`. Pass `--cli-version X.Y.Z` to pin one, and `--workflow-ref` to
  pin another ref of `SteerSpec/.github`.
- If a file exists, including a config from `strspc init`, the command exits 1, also with `--dry-run`.
  `--force` regenerates every file from the flags given, so repeat any pins and re-add any rule
  sources you added by hand.

### Reusable workflows

The generated files call the reusable workflows in
[SteerSpec/.github](https://github.com/SteerSpec/.github), pinned to `@v1`:

```yaml
jobs:
  rules:
    uses: SteerSpec/.github/.github/workflows/strspc-lint.yml@v1
    with:
      path: rules/
  check:
    uses: SteerSpec/.github/.github/workflows/strspc-check.yml@v1
    secrets:
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

| Workflow | Runs | Notable inputs |
| -------- | ---- | -------------- |
| `strspc-lint.yml` | `realm validate`, `lint` and `diff`, and optionally `refs` | `diff: false` for release PRs that aggregate promotions; `core: true` for the core realm; `refs: true` to check rule citations, which needs `.strspc/config.yaml` and strspc 1.30.0 or later; `refs-allow-state` lists the rule states a citation may name, `D` by default and none when empty; `STRSPC_GITHUB_TOKEN` for private rule sources |
| `strspc-check.yml` | `check`, or `check --static-only` without a key | `cache: true` only where every rule source is public: the cache stores explanations that fork PRs can restore |
| `strspc-deps-bump.yml` | `realm dep upgrade`, then opens one pull request | Run it on a schedule; it never merges |

### Manual steps

Outside those workflows, run the CLI through the [setup action](../README.md#installation):

```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0                     # --base needs the merge base
- uses: SteerSpec/strspc-CLI/.github/actions/setup-strspc@v1.36.0
- run: strspc lint rules/ --strict
- run: strspc diff rules/ --pr ${{ github.event.pull_request.number }}
  env:
    GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
- run: strspc check --base origin/develop --format github
  env:
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}   # this step only
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}             # github:// rule sources
- name: Check that rendered Markdown has no drift
  run: |
    strspc render rules/core/ -o rules/core/ --format markdown
    git diff --exit-code rules/core/*.md
```
