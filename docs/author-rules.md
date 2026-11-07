# Author rules

Back to the [README](../README.md).

Author rules through the CLI, which keeps versions and hashes valid. Don't edit entity JSON by hand.

```mermaid
flowchart LR
    D["<b>D</b>raft<br/>text may change"]
    A["<b>A</b>bandoned"]
    P["<b>P</b>ublished<br/>text is final"]
    I["<b>I</b>mplemented<br/>enforced"]
    R["<b>R</b>etired"]
    T["<b>T</b>erminated"]

    D -->|rule promote| P
    P -->|rule promote| I
    I -->|rule retire| R
    R -->|rule retire| T
    D -->|rule abandon| A

    classDef draft fill:#161B22,stroke:#6E7681,color:#A0A8B1
    classDef published fill:#161B22,stroke:#A0A8B1,color:#E6EDF3
    classDef implemented fill:#3FB950,stroke:#3FB950,color:#0D1117
    class D,A,R,T draft
    class P published
    class I implemented
```

Transitions are forward-only and no state may be skipped
([SPEC §3](https://github.com/SteerSpec/strspc-manager/blob/v1.42.0/docs/SPEC.md#3-rule-lifecycle)).
Every change bumps the rule set version: minor when a rule is promoted or retired, patch otherwise.

```bash
strspc rule add CFG --body "The CLI MUST write a config file." --added-by github:alice
strspc rule update CFG-001 --body "The CLI MUST write a YAML config file."     # Draft only
strspc rule promote CFG-001                                                    # D → P
strspc note add CFG-001 --type rationale --body "Why." --added-by github:alice
strspc note update CFG-001/01 --body "Corrected."
strspc entity add CFGIO --parent CFG --title "Config I/O"
strspc rule add CFG --sub-entity CFGIO --body "Writes MUST be atomic." --added-by github:alice
strspc entity rehash --all                                     # fix hashes after a bulk import
```

**Sub-entities.** Rule commands accept a sub-entity EUID anywhere a top-level EUID is accepted: the
CLI scans `sub_entities[]` of every entity file under `--dir` and refreshes the parent hash.

**Symbolic links.** Commands never write through an entity file that is a symbolic link, since its
target may lie outside `--dir`: rule, note and entity commands refuse it, and `entity rehash --all`
and `realm rewrite-added-by` list it as skipped. Edit the target where it lives. Commands that only
read, such as `entity standards list`, follow the link.

**Superseding.** Rules that have left Draft are immutable, so `rule supersede` creates a Draft that
replaces them. It leaves the original unchanged: when the new rule reaches Implemented, the old one
must be Retired, or `strspc diff` warns (`RD006`). `rule retire` moves a rule from Implemented to
Retired, so promote a Published rule first. A rule that's already Retired needs nothing.

**Notes.** Types are `rationale`, `example`, `counter_example`, `reference`, `applies_to`, `changelog`,
`clarification`, `deprecation_notice`, `supersedes`, `extends`, `related` and `standard`. A note can be
revised but never removed. `changelog`, `supersedes`, `extends` and `related` notes can't change once added.
A `standard` note maps its rule to one clause of a declared standard (`<standard-id>:<clause>`). It can be
added, revised, or retyped to `reference` and back in any rule state, without changing the rule's revision
(NTE-017, NTE-018); retyping it to `reference` is how a mapping is withdrawn (NTE-019).

**Contributors.** `--added-by` takes a contributor identifier, `<idp>:<login>` with `github` as the
only provider (CTRB-001, CTRB-002). When the Realm lists `realm.contributors`, the value must name
one of them (CTRB-005), and a sub-realm uses its parent's list (CTRB-006). An `@handle` or an email
address is refused; `realm rewrite-added-by` migrates files that still hold them, and `strspc lint`
reports any left (`RL032`, an error). `strspc diff` fails on a new rule or note by someone the Realm
doesn't list (`RD023`, an error).

`realm contributor` edits the list, so realm.json is never edited by hand. A sub-realm's list is its
parent's, so it is edited there:

```bash
strspc realm contributor add github:alice --github-id 1234567 --email alice@example.com
strspc realm contributor list
strspc realm contributor remove github:alice
```

`--github-id` records the account's numeric ID, which survives a rename (CTRB-008):
`gh api users/<login> --jq .id` prints it. `add` needs `dev.steerspec.core` 1.16.0 or later.

Without `--added-by`, `rule add`, `note add` and `rule supersede` use `$STRSPC_ADDED_BY`, then the
listed contributor whose email is your `git config user.email`, and print which one they used. The
value is checked like the flag's, before anything is written.

`realm rewrite-added-by` migrates existing values, with a patch bump for each changed file:

```bash
strspc realm rewrite-added-by --github-handles --dry-run         # preview @alice → github:alice
strspc realm rewrite-added-by --github-handles --map alice@example.com=github:alice
```

**Inheritance.** An entity can inherit another entity's rule set, in the same Realm or in a declared
dependency, and a rule can override one inherited rule for that entity:

```bash
strspc realm add SAVACCT --title "Savings Account" --inherits dev.globalbank.core:ACCT
strspc rule add SAVACCT --body "Withdrawals MUST be capped." --added-by github:alice \
  --overrides dev.globalbank.core:ACCT-005 # strspc-refs: ignore
```

`entity add` also takes `--inherits`, and `rule supersede` takes `--overrides` to keep an override in
force on the replacement rule.

## Realms and dependencies

`strspc realm init` creates `realm.json` and vendors the schemas in `_schema/`
([SPEC §2.8](https://github.com/SteerSpec/strspc-manager/blob/v1.42.0/docs/SPEC.md#28-realm)). Non-core
realms should depend on `dev.steerspec.core`, as in the [quick start](../README.md#quick-start).

The schemas are copied unchanged from the `dev.steerspec.core` release tag in `strspc-rules`: the
`--core-version` you pass, else the core dependency's version, else the latest release. Since core
release 1.20.0 each file carries its release as a top-level `x-steerspec-core-version`. Copies of
older releases have no such field. `realm init` refuses a core dependency whose `github://` source tag
differs from its version, and a `--core-version` that differs from the dependency.

```bash
strspc realm dep add com.acme.shared@1.0.0=../shared
strspc realm dep list
strspc realm dep remove com.acme.shared
strspc realm add-subrealm --parent-dir rules --dir rules/sync --id com.acme.app.sync --title "Sync"
```

A sub-realm inherits its parent's dependencies unless you pass `--no-inherit-deps`. `add-subrealm`
doesn't declare it in the parent: add the directory name to `sub_realms` in the parent's `realm.json`,
or `realm validate --recursive` skips it.

**Keep dependencies current.** Dependabot and Renovate can't read `realm.json`, so a pinned dependency
drifts silently. `strspc realm dep upgrade` moves each `github://` dependency pinned to a release tag
to the latest release of its repository, rewrites `version` and the `@vX.Y.Z` in `source`, then
validates the realm (strict, recursive).

```bash
strspc realm dep upgrade --check    # exit 1 if an upgrade is available; writes nothing
strspc realm dep upgrade            # upgrade every dependency
strspc realm dep upgrade dev.steerspec.core --to X.Y.Z
```

When `dev.steerspec.core` is upgraded, the `_schema/*.json` files that differ from the new release's
copies are replaced by them, so the realm doesn't trip RM015 (schema drift). This covers sub-realms
that inherit the pin; one that pins its own core is left alone. Files the core release doesn't hold
are left alone too. If the new version breaks the realm, `realm.json` and the schema files are
restored and the command exits non-zero.
Dependencies are never downgraded. Sources that aren't `github://` references, and those on `@latest`,
a branch or a commit SHA, are skipped. See [GitHub sources](reference.md#github-sources) for private
repositories.
