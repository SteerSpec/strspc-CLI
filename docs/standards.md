# Report coverage of a standard

Back to the [README](../README.md).

The standards, rule IDs and clauses on this page (OWASP ASVS, IETF RFC 2119, the `AUTH` rules) are
examples to show the commands. They are not claims that SteerSpec covers any standard.

A realm or entity declares the external standards it cites under `standards`, and a `standard` note
maps a rule to one clause, with content `<standard-id>:<clause>`:

```bash
strspc realm standards add --id owasp.asvs --relation implements --version 4.0.3
strspc entity standards add AUTH --id ietf.rfc2119 --relation derived_from --url https://www.rfc-editor.org/rfc/rfc2119
strspc note add AUTH-004 --type standard --body "owasp.asvs:2.1.1" --added-by github:alice
```

`implements` claims coverage of the standard and `derived_from` only cites it as a source. An id is
declared once per `standards` array. `update` changes its relation, version or url in place
(`--clear-version` and `--clear-url` remove those), so the notes mapping it keep resolving. A new
version names another edition, so `update` lists the notes that now map clauses of it. The entity
commands patch-bump the entity and refresh its hash. `remove` is refused while a `standard` note would
no longer resolve: withdraw the mapping first with `strspc note update <note_id> --type reference`.
The `standards` field needs `dev.steerspec.core` 1.15.0 or later. `add` is refused while the core pin that
validates the file is older: the nearest one among the sub-realm's own, the realm's and its parent's, since a
sub-realm that pins its own core is checked only against that pin. The error names the command that moves it.

`strspc standards coverage` then lists each mapped clause, per standard and entity. The output below
is a sample:

```text
$ strspc standards coverage
Evaluator: claude, runs on Implemented rules

ietf.rfc2119 (derived_from)
  MUST  AUTH  cited  dev.acme:AUTH-004 (Implemented)

owasp.asvs 4.0.3 (implements)
  https://owasp.org/www-project-application-security-verification-standard/
  2.1.1  AUTH  covered  dev.acme:AUTH-004 (Implemented)
  2.1.2  AUTH  mapped   dev.acme:AUTH-005 (Published)
  2.1.3  AUTH  lapsed   dev.acme:AUTH-001 (Retired)
```

| Status | Meaning |
| ------ | ------- |
| `covered` | An Implemented rule maps the clause |
| `mapped` | A rule in another state maps it, and no Implemented rule does |
| `lapsed` | The rule mapping it was retired or replaced, and nothing maps it now |
| `cited` | The standard is a source (`derived_from`), so the rule claims no coverage |

Each entity is measured over its own rules and those it inherits, less those replaced. An overriding
rule doesn't take on the mapping of the rule it replaces, so a clause it drops shows as `lapsed`.
Implemented is a rule's state: the first line shows the evaluator setting, which decides whether
anything checks those rules. SteerSpec holds no clause lists, so a clause no rule maps isn't shown.

The report covers the local sources in `.strspc/config.yaml` that have a `realm.json`, with their
sub-realms, and `--all-sources` adds the remote ones. A source with no `realm.json` can't declare a
standard, so its `standard` notes are left out with a warning. `--json` prints the same report as one
document, or on failure `strspc check`'s document with its `error` field. It exits 0 when the report
is printed, and 2 when the configuration can't be read or a source or dependency doesn't resolve, as a
report would then understate the coverage. A `--json` report that can't be written exits 4; the
failure document is written on a best-effort basis and keeps exit 2.

## Planned: a coverage gate in CI

> **Not yet implemented.** Tracked in [#239](https://github.com/SteerSpec/strspc-CLI/issues/239); the
> flags below may change.

`standards coverage` will be able to fail a build. It exits 1 when the gate fails, and 2 still wins
when resolution fails, so a gate never passes on a report that understates the coverage:

```bash
strspc standards coverage --fail-on lapsed,mapped,unresolved   # no lapsed or unenforced claims
strspc standards coverage --require owasp.asvs@5.0.0            # declared as implements, with a covered clause
strspc standards coverage --require-evaluator                   # covered clauses that something checks
strspc standards coverage --baseline coverage-base.json         # no regression against an earlier --json report
```

The statuses in `--fail-on` (`lapsed`, `mapped`) and `--require` apply to `implements` standards only,
so a `cited` clause never fails. `unresolved` is a separate check, on `standard` notes that resolve to
no declared standard: such a note has no relation, so it fails whatever the standard it meant to name.

`--baseline` compares with the `--json` report of the base branch, kept as a CI artifact. It fails
when:
- a covered clause becomes mapped or lapsed
- a covered or mapped clause is lost
- an `implements` standard is dropped or becomes `derived_from`
- a note that resolved stops resolving

A baseline that can't be read or parsed exits 2. The comparison comes from strspc-manager
([strspc-manager#269](https://github.com/SteerSpec/strspc-manager/issues/269)), so the CLI, the pull
request review and the planned cloud dashboard share one definition of a regression.

In GitHub Actions, a push to `develop` records the baseline, and a pull request into `develop` is
gated against the latest one. A pull request into another branch would need that branch's own
baseline, so the trigger names `develop` for both:

```yaml
on:
  push:
    branches: [develop]
  pull_request:
    branches: [develop]

jobs:
  standards:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      actions: read                    # gh run download reads the develop run's artifact
    steps:
      - uses: actions/checkout@v4
      - uses: SteerSpec/strspc-CLI/.github/actions/setup-strspc@v1.36.0
      - name: Record the baseline
        if: github.event_name == 'push'
        run: strspc standards coverage --json > coverage.json
      - uses: actions/upload-artifact@v4
        if: github.event_name == 'push'
        with:
          name: standards-coverage
          path: coverage.json
      - name: Fetch the baseline from develop
        if: github.event_name == 'pull_request'
        env:
          GH_TOKEN: ${{ github.token }}
          WORKFLOW: ${{ github.workflow }}
        run: |
          run_id=$(gh run list --workflow "$WORKFLOW" --branch develop --event push \
            --status success --limit 1 --json databaseId --jq '.[0].databaseId')
          gh run download "$run_id" --name standards-coverage --dir baseline
      - name: Gate on standards coverage    # exit 1 fails the gate, 2 a report it can't trust
        if: github.event_name == 'pull_request'
        run: >-
          strspc standards coverage
          --fail-on lapsed,unresolved
          --require owasp.asvs@5.0.0
          --baseline baseline/coverage.json
```

Until `develop` has a successful run, there is no artifact to fetch, so leave out the fetch step and
`--baseline` until then.

There is no percentage threshold: without a standard's clause list, a report has nothing to divide by.

## Planned: standards published as realms

> **Not yet implemented.** Proposed in
> [strspc-rules#144](https://github.com/SteerSpec/strspc-rules/issues/144); still a design under
> discussion.

A standard's publisher, or anyone its licence allows, publishes it as a SteerSpec realm with one rule
per clause, for example OWASP ASVS 5.0.0 in a GitHub repository. A project declares it as a
dependency and links its Standard Reference to it, then maps its own rules to the standard's clauses
with `standard` notes, as today. The realm supplies the clause list SteerSpec doesn't hold, so:

- lint rejects a note naming a clause the standard doesn't have
- coverage gains an `unmapped` status and a figure per level, such as covered ÷ the clauses of a
  level, less those excluded
- a project can mark a clause not applicable, with a justification, which removes it from the total
- a new edition is a new realm, published with a crosswalk from the previous one, so a project's
  mappings carry forward instead of all lapsing

A transcription SteerSpec publishes uses its own namespace (`dev.steerspec.std.…`), not the standard
owner's.
