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

**Link a reference to a Standard Realm.** Core 1.21.0 adds the Draft rules for standards published as
realms. `--realm` sets the `realm_id` of the Standard Realm the reference links to (STDREF-017). That
Realm must be a dependency of the Realm tree, and its edition must equal the reference's `version`
(STDREF-018). `--profile` limits the reference to one of that Realm's profiles (STDREF-019,
STDREF-027), and needs a realm. `update` takes `--realm`, `--profile`, `--clear-realm` and
`--clear-profile`. The CLI checks the realm ID, that a profile has a realm, and that a Realm of the
tree declares the realm as a dependency: the Realm at `--dir`, its parent if it's a sub-realm, or one
of its sub-realms, as `realm validate` on the same `--dir` reads it (RM026). Declare the dependency
first. `realm dep remove` keeps a dependency a reference still links to, unless another Realm of the
tree declares it too. Lint checks the rest of the link against the Realm, which it fetches. Both
fields need `dev.steerspec.core` 1.21.0 or later.

```bash
strspc realm dep add dev.steerspec.std.owasp.asvs.v5@5.0.0
strspc realm standards add --id owasp.asvs --relation implements --version 5.0.0 \
  --realm dev.steerspec.std.owasp.asvs.v5 --profile L1
```

**Exclude a clause.** A Realm Note sits in the `notes` array of `realm.json`, not next to a rule
(NTE-029). Its ID is `REALM/NNN`, numbered from `REALM/001` in each manifest (NTE-030). It is one of
four types: `exclusion`, `rationale`, `clarification` or `reference` (NTE-031). An `exclusion`
declares one clause not applicable to the realm, and its content is the justification (NTE-033). Its
`--ref` is `<id>:<clause>`, and the `<id>` must name a reference of the same manifest that is linked
with `--realm` (NTE-034). An exclusion is never deleted. To withdraw it, retype it to `reference`,
which keeps its ref and content (NTE-035). `--added-by` is checked as for rule notes, against the
realm's contributors, or for a sub-realm its parent's. A sub-realm holds its own notes, for its own
linked references. Removing a reference, or clearing its `realm`, is refused while an exclusion names
it. The notes are Draft rules of core 1.21.0 and need that core pin. Every write recomputes the
manifest hash when the manifest has one.

```bash
strspc realm note add --type exclusion --ref owasp.asvs:V3.1.1 --body "The CLI renders no HTML." --added-by github:alice
strspc realm note list
strspc realm note update REALM/001 --type reference   # withdraw the exclusion
```

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
| `excluded` | An `exclusion` Realm Note names the clause of a linked standard, whatever maps it |
| `unmapped` | A clause of a linked standard's list that no rule maps |

Each entity is measured over its own rules and those it inherits, less those replaced. An overriding
rule doesn't take on the mapping of the rule it replaces, so a clause it drops shows as `lapsed`.
Implemented is a rule's state: the first line shows the evaluator setting, which decides whether
anything checks those rules. Without a linked Standard Realm, a clause no rule maps isn't shown:
SteerSpec holds no clause lists.

### Against a linked Standard Realm

A reference linked with `--realm` is measured against that realm's clause list, narrowed to its
`--profile` (STDREF-020). The clauses no rule maps show as `unmapped`, and those an exclusion names as
`excluded`, with the justification (STDREF-022, STDREF-023). Each entity, and the Realm as a whole,
gets a figure: the clauses covered over those of the list less the excluded (STDREF-024,
STDREF-025). A clause that several rules of the Standard Realm transcribe counts once. The Realm figure
counts a clause covered when any entity of the tree covers it. The figure is `undefined` when no
clause is left to count, because every clause is excluded or the profile lists none (STDREF-028),
never 0 % or 100 %, and the percentage is truncated, so 100 % means
every clause. Only an `implements` reference gets one (STDREF-026).

```text
$ strspc standards coverage
Evaluator: claude, runs on Implemented rules

owasp.asvs 5.0.0 (implements)
  Standard Realm dev.steerspec.std.owasp.asvs.v5, profile L1: 70 clause(s), 1 excluded
  Realm dev.acme: 12 of 69 covered (17.3 %)
    AUTH: 9 of 69 covered (13.0 %)
    BILL: 4 of 69 covered (5.7 %)
  V1.2.1  AUTH  covered   dev.acme:AUTH-004 (Implemented)
  V1.2.2  AUTH  unmapped
  V3.1.1  AUTH  excluded  excluded by REALM/001 (dev.acme)
  ...
  Exclusions:
    owasp.asvs:V3.1.1  REALM/001 (dev.acme): The CLI renders no HTML.
```

`--json` adds a `figures` array, one entry per Realm figure and entity figure (`entity` left out for
the Realm's). Each has `total`, the clauses of the profile's list; `excluded`; `covered`, `mapped`,
`lapsed` and `unmapped`, which count the clauses not excluded and add up to `total` less `excluded`;
`declared_by`, the manifest declaring the reference; and `figure`, the ratio, or `null` when
undefined. An excluded clause carries its `exclusion`: the manifest, the `REALM/NNN` note and its
content. With no linked reference, `figures` is empty and the report is as before.

The report covers the local sources in `.strspc/config.yaml` that have a `realm.json`, with their
sub-realms, and `--all-sources` adds the remote ones. A source with no `realm.json` can't declare a
standard, so its `standard` notes are left out with a warning. `--json` prints the same report as one
document, or on failure `strspc check`'s document with its `error` field. It exits 0 when the report
is printed, 1 when a [gate](#gate-ci-on-coverage) fails, and 2 when the configuration can't be read or a source or dependency doesn't resolve, as a
report would then understate the coverage. A `--json` report that can't be written exits 4; the
failure document is written on a best-effort basis and keeps exit 2.

## Gate CI on coverage

`standards coverage` can fail a build when coverage weakens. With no gate flag it exits 0 once the
report is printed, as above. With one, the report ends with the gate's verdict, and the command
exits 1 when the gate fails:

```bash
strspc standards coverage --fail-on lapsed,mapped,unresolved   # no lapsed or unenforced claims
strspc standards coverage --require owasp.asvs@5.0.0            # declared as implements, with a covered clause
strspc standards coverage --require-evaluator                   # covered clauses that something checks
strspc standards coverage --min-coverage 60                     # every linked Realm figure at 60 % or more
strspc standards coverage --min-coverage owasp.asvs=80          # owasp.asvs at 80 % or more
```

- `--fail-on` fails on each clause `lapsed` or only `mapped` in an entity, and with `unresolved` on
  each `standard` note that resolves to no declared standard. Such a note has no relation, so it
  fails whatever the standard it meant to name. Values are comma-separated, and the flag can be
  repeated. A `cited` clause claims no coverage (STDREF-006) and never fails. `unmapped` isn't
  accepted: a clause is unmapped in each entity that doesn't map it, though another may cover it,
  so gate the Realm figure with `--min-coverage` instead.
- `--require <id>[@<version>]`, repeatable, fails unless the standard, or that edition of it, is
  declared with relation `implements` and one of its clauses is covered. The failure says why: not
  declared, declared under another edition, declared only as `derived_from`, or no clause covered.
- `--require-evaluator` fails when clauses are covered but the evaluator is `null`, can't be run,
  or isn't run on Implemented rules (`evaluate_on`): a covered clause is then a claim that nothing
  checks.
- `--min-coverage`, repeatable, fails a Realm figure of a linked Standard Realm under the
  percentage. A bare `<percent>` applies to every standard, and `<id>[@<version>]=<percent>` to one,
  overriding the bare value; the edition's overrides the standard's. The percentage is 0 to 100
  with at most one decimal, and compared exactly, so `66.6` passes 2 of 3 and `66.7` doesn't.
  Entity figures are shown but not gated. A threshold no figure is there to check fails, so the
  gate never passes on nothing. An undefined figure (STDREF-028) is not checked: it is a warning.

The STDREF rules defining the statuses and figures are Draft in `dev.steerspec.core`.

Exit 2 still wins over 1: a gate flag that can't be read, a configuration that can't be read or a
source or dependency that doesn't resolve exits 2 before the gate runs, so a gate never passes on a
report that understates the coverage. A `--json` report that can't be written exits 4.

`--json` adds a `gate` object, absent when no gate flag is set:

```json
"gate": {
  "passed": false,
  "failures": [
    {
      "kind": "min_coverage",
      "message": "Realm dev.acme: owasp.asvs@5.0.0 covers 12 of 69 clause(s) (17.3 %), under the minimum of 60.0 %",
      "standard": "owasp.asvs",
      "version": "5.0.0",
      "tree": "dev.acme",
      "figure": 0.17391304347826086,
      "threshold": 60
    }
  ]
}
```

`kind` is `lapsed`, `mapped`, `unresolved`, `required`, `evaluator` or `min_coverage`. The other
fields say what failed, where they apply: `standard`, `version`, `clause`, `entity`, `tree` (the Realm
of a figure), `rules` (the mappings behind a clause or the unresolved note), `figure` and `threshold`.

In GitHub Actions:

```yaml
jobs:
  standards:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: SteerSpec/strspc-CLI/.github/actions/setup-strspc@v1.36.0
      - name: Gate on standards coverage    # exit 1 fails the gate, 2 a report it can't trust
        run: >-
          strspc standards coverage
          --fail-on lapsed,unresolved
          --require owasp.asvs@5.0.0
          --min-coverage owasp.asvs=60
```

### Planned: regression against a baseline

> **Not yet implemented.** Phase 2 of [#239](https://github.com/SteerSpec/strspc-CLI/issues/239);
> the flag below may change.

```bash
strspc standards coverage --baseline coverage-base.json         # no regression against an earlier --json report
```

`--baseline` compares with the `--json` report of the base branch, kept as a CI artifact. It fails
when:
- a covered clause becomes mapped or lapsed
- a covered or mapped clause is lost
- an `implements` standard is dropped or becomes `derived_from`
- a note that resolved stops resolving

A baseline that can't be read or parsed exits 2. The comparison comes from strspc-manager
([strspc-manager#269](https://github.com/SteerSpec/strspc-manager/issues/269)), so the CLI, the pull
request review and the planned cloud dashboard share one definition of a regression.

In GitHub Actions, a push to `develop` would record the baseline, and a pull request into `develop`
would be gated against the latest one. A pull request into another branch would need that branch's own
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
      - name: Gate on standards coverage
        if: github.event_name == 'pull_request'
        run: >-
          strspc standards coverage
          --fail-on lapsed,unresolved
          --require owasp.asvs@5.0.0
          --baseline baseline/coverage.json
```

Until `develop` has a successful run, there is no artifact to fetch, so leave out the fetch step and
`--baseline` until then.

## Planned: standards published as realms

> **Mostly implemented.** Designed in
> [strspc-rules#144](https://github.com/SteerSpec/strspc-rules/issues/144). The rules are Draft in core
> 1.21.0. Linking a reference to a Standard Realm (`--realm`), excluding a clause (`realm note`), lint
> of a note's clause against the linked realm, and coverage against its clause list all work today, as
> above. The crosswalk between editions is not yet implemented.

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
