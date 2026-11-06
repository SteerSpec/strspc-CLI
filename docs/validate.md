# Validate rules

Back to the [README](../README.md).

## Validate rules

```bash
strspc lint rules/ --strict                    # schema and structure of each entity file
strspc realm validate rules/ --recursive       # manifest, EUID uniqueness, sub-realms, pins
strspc diff rules/ --base origin/develop       # lifecycle transitions vs. a git ref
strspc diff rules/ --pr 42                     # ... or vs. a PR base (needs gh auth)
strspc check --static-only                     # resolve sources, verify hashes and references
```

These implement the checks of
[SPEC §7](https://github.com/SteerSpec/strspc-manager/blob/v1.42.0/docs/SPEC.md#7-cicd-workflows). `lint`,
`realm validate` and `diff` take `--strict`, which treats warnings as errors, and `--json`; `check`
takes `--format json`. `lint --cross-ref` adds
cross-file reference checks in directory mode. `diff` exits 0 when all checks pass and 1 on a
violation.

**Schemas.** `lint` and `realm validate` use the schemas of the `dev.steerspec.core` version the realm
pins, so a schema change reaches a realm when it bumps its pin. A realm with no pin uses the latest
published schemas, and a realm's own `_schema/` never decides validation. `lint` reads the pin from
`realm.json` in the directory it's given, or in the file's directory. A sub-realm that pins its own core
is checked only against that pin, also when linting or validating its parent; one with no pin uses its
parent's.

**Dependencies.** `realm validate` compares the realm with the dependencies it pins, so it fetches them
and needs network access unless they're cached. It reports a pin that disagrees with its source tag or
the dependency's manifest, and a `_schema/` file that differs from the pinned `dev.steerspec.core`.

**Core realm.** The core realm defines the schemas, so it has nothing to pin. Pass `--core` to validate
it against its own `_schema/`:

```bash
strspc realm validate rules/core --recursive --strict --core
strspc lint rules/core --strict --core
```

## Validate rule citations

`strspc refs` checks the rule identifiers that text files cite, such as agent prompts, READMEs and
docs, against the rules in `.strspc/config.yaml` and the realm dependencies they pin:

```bash
strspc refs                                    # Markdown files, ignoring what .gitignore ignores
strspc refs docs .claude --strict              # only these directories; warnings fail
strspc refs --include '*.yml' --exclude 'docs/archive/**'
strspc refs --ignore SHA-256 --allow-state R,T # strspc-refs: ignore
strspc refs --format github                    # annotations; --json for diagnostics as JSON
```

| Code | Cited rule | Default | `--strict` |
| ---- | ---------- | ------- | ---------- |
| RF001 | Doesn't exist, or names a realm that isn't a dependency | error | error |
| RF002 | Abandoned | error | error |
| RF003 | Retired or Terminated, whether or not a rule replaces it | warning | error |
| RF004 | Draft, Published or Implemented, replaced by a rule in I, R or T | error | error |
| RF005 | Draft, Published or Implemented, replaced by a Published rule | warning | error |
| RF006 | Draft | warning | error |
| RF007 | No ID is cited: the text holds the whole body of a rule of five words or more, in any state | error | error |

Text that copies the body of a rule instead of citing it, five words or more, is an error (RF007),
whatever the rule's state. Cite the ID and look the rule up with `strspc rule show`. Committed
`strspc render` output copies every body, so exclude it, for example with `--exclude 'docs/rules/**'`.

The stored `<EUID>-<NNN>` form always matches, in text or code spans, and so does the realm's own
rendered form. An unknown prefix is reported only when it is uppercase and starts with a letter, so
<!-- strspc-refs: ignore -->
`CLAMS-001` is reported and `feat-123` isn't. `SHA-256` has that shape too, so pass it to `--ignore`.
URLs are skipped.

A line holding `strspc-refs: ignore` exempts itself and the next line, for a historical mention. Any
comment syntax can carry it:

```markdown
<!-- strspc-refs: ignore -->
RUL-004 was retired in 2026.
```

`.strspc/` and `CHANGELOG.md` are always skipped. A pattern with no slash matches a file name in any
directory, and `dir/**` matches everything under `dir`. A file named as an argument is scanned
whatever its extension. `refs` exits 0 when clean, 1 on findings and 2 on a configuration or
resolution error.

## Understand a diagnostic

```bash
strspc explain RM006                         # what a code means and how to fix it
strspc explain --list                        # every code, with its title
strspc lint rules/ --explain                 # explain findings as they are reported
```

`--explain` works on `lint`, `diff`, `realm validate` and `check`. Text output explains each code once,
under its first occurrence. JSON output adds an `explanation` object to every diagnostic.
`strspc check --format github` rejects the flag, because annotations cannot carry the text.
