---
name: strspc
description: Use when working with the strspc CLI (SteerSpec) - setting up a project, authoring or promoting rules in a realm, validating entity JSON, or checking code changes against rules. Covers the command workflow, rule lifecycle, exit codes and machine-readable output.
---

# Using the strspc CLI

strspc manages SteerSpec steering rules: JSON entity files grouped into realms, validated by `lint`
and enforced against code changes by `check`. Run `strspc <command> --help` for any command's flags;
this skill is the map, `--help` is the detail.

## Golden rule

Never hand-edit rule or entity JSON. Author through `strspc rule`, `strspc note`, `strspc entity`
and `strspc realm` so versions and content hashes stay valid. After a bulk import, run
`strspc entity rehash --all`.

## Set up a project

```bash
strspc doctor                  # check the installation and project; each warning names a fix
strspc init                    # write .strspc/config.yaml (core rules as a global source)
strspc init --provider null    # offline: static checks only, no API key
strspc status                  # show rule sources and how they stand
```

## Author rules in your own realm

```bash
strspc realm init --dir rules --id com.acme.app --title "Acme App"
strspc realm add CFG --title "Configuration"
strspc rule add CFG --body "The CLI MUST write a config file." --added-by github:alice
strspc rule update CFG-001 --body "The CLI MUST write a YAML config file."
strspc rule promote CFG-001
strspc rule show CFG-001
strspc note add CFG-001 --type rationale --body "Why." --added-by github:alice
```

`--added-by` takes `github:<login>`; an `@handle` or an email address is refused.

Rule lifecycle is forward-only, and only a superseded Published rule may skip a state:

| State | Meaning | Move on with |
| ----- | ------- | ------------ |
| D Draft | text may still change (`rule update`) | `rule promote` to P, or `rule abandon` |
| P Published | text is final, code may still violate it | `rule promote` to I, which verifies the code first, or `rule terminate` to T once superseded |
| I Implemented | enforced in CI when listed in `fail_on` | `rule retire` to R |
| R Retired | no longer applies | `rule terminate` to T |

Only promote a rule to Implemented once the code complies. A rule that has left Draft is immutable:
use `strspc rule supersede` to replace it. A superseded Published rule skips I and R: once its
successor is Published, `strspc rule terminate` ends it, in the same change or later (RULST-012).

## Validate

```bash
strspc lint rules/ --strict                     # entity JSON against schema and business rules
strspc realm validate rules/ --recursive --strict
strspc check --static-only                      # resolve rule sources, structural checks, no API key
strspc check                                    # evaluate the diff against rules with the AI provider
strspc diff rules/                              # validate rule lifecycle transitions against a git ref
```

Diagnostics carry codes such as `RL032`. `strspc explain <code>` says what a code means and how to
fix it, and `strspc explain --list` lists them all. To evaluate your own realm, add `./rules/` as a
source in `.strspc/config.yaml`.

`strspc check` picks the diff from `--diff-file`, `--staged`, `--pr`, `--base`, or the working tree
against HEAD. It never evaluates files under `.strspc/` or `rules/`.

## Exit codes and output

`check` exits 0 when compliant, 1 on violations in `fail_on` states, 2 on a configuration or rule
resolution error, 3 when the AI provider is unavailable and 4 when the JSON report could not be
written. `doctor` exits 1 when a check failed.

Most reporting commands take `--json` (`check` takes `--format json`). Prefer it when parsing
output, and read the exit code rather than the text.

## More commands

- `strspc sync` fetches and caches rule sources from the configuration.
- `strspc history <EUID>` and `strspc rule history <RULE-ID>` show how an entity's or a rule's text changed, from git.
- `strspc refs` validates rule citations in text files such as agent prompts.
- `strspc render rules/` renders entity JSON to Markdown.
- `strspc ci init` writes GitHub workflows for CI.
- `strspc standards` reports on the external standards a project's rules map to.
