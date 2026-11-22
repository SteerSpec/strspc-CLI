# Check code changes

Back to the [README](../README.md).

`strspc check` resolves the configured rule sources, collects a unified diff, and asks the AI provider
for one verdict per rule: `compliant`, `violated` or `not_applicable`. Violations of rules in a
`fail_on` state fail the check; other violations are warnings.

```bash
strspc check                                 # working tree vs HEAD
strspc check --staged                        # index vs HEAD (pre-commit)
strspc check --base origin/develop           # merge base of origin/develop and HEAD vs working tree
strspc check --pr 42                         # same, with the PR base resolved through gh
strspc check --diff-file change.diff         # evaluate a diff produced elsewhere
strspc check --static-only                   # no AI call (RE003)
strspc check --format github                 # ::error/::warning annotations + $GITHUB_STEP_SUMMARY
```

| Flag | Description |
| ---- | ----------- |
| `--base <ref>` | Diff the working tree against the merge base of `<ref>` and HEAD |
| `--staged` | Diff the index against HEAD |
| `--pr <n>` | Diff against the base of GitHub PR `n` (needs the `gh` CLI) |
| `--diff-file <path>` | Read a unified diff from a file, with repository-relative paths as `git diff` prints them |
| `--format human\|json\|github` | Output style; `github` emits annotations and appends a table to `$GITHUB_STEP_SUMMARY` |
| `--provider`, `--model` | Override `evaluator.provider` and `evaluator.model` |
| `--endpoint <url>` | Provider endpoint; needed to opt in to a non-Anthropic host. `https://`, or `http://` to a loopback host |
| `--static-only` | Skip AI evaluation (also implied by `evaluator.provider: null`) |
| `--no-cache` | Don't read or write `.strspc/cache.db` |
| `--verbose` | Print resolution, diff, cache and token-usage details to stderr |

Diff selection precedence: `--diff-file` > `--staged` > `--pr` > `--base` > `$GITHUB_BASE_REF`
(`origin/<ref>`, fetched when missing) > working tree vs HEAD. Files under `.strspc/` and `rules/` are
never evaluated. The check only writes inside `.strspc/` (`cache/`, `cache.db`).

| Exit code | Meaning |
| --------- | ------- |
| `0` | No violation of a rule in a `fail_on` state (warnings may have been reported) |
| `1` | At least one rule in a `fail_on` state was violated |
| `2` | Configuration (empty `rules:`, unknown state code or provider, unsafe endpoint), rule resolution or diff collection failed |
| `3` | AI provider unavailable: no `ANTHROPIC_API_KEY`, rejected credentials (401/403), unreachable endpoint, or every request failed (a key without workspace scope, a rejected schema) |

## How evaluation works

The flow follows
[SPEC §8.3](https://github.com/SteerSpec/strspc-manager/blob/v1.47.1/docs/SPEC.md#83-evaluation-flow):

1. Resolve the rule sources and collect the rules in `evaluate_on` states.
2. Collect the diff. An empty diff ends evaluation with `RE004`.
3. Look each rule up in the cache, and send only the misses, one request per owning entity.
4. Get one verdict per rule, then re-read the diff for each violated rule to confirm it or withdraw it.
5. Report the results. A provider failure for a rule is warning `RE002`, which doesn't block on its own.

Only Claude is implemented. With provider `claude` and no key the check exits 3 rather than falling
back, so pass `--static-only` for structural checks. Cached verdicts expire after `cache.ttl`, and
changing a rule, its notes, the diff or the model invalidates them.

## Inheritance and references

An entity with `inherits` is evaluated on its effective rule set: the rules it inherits, minus those
replaced by its own rules in `evaluate_on`, plus its own, so `rule_count` includes inherited rules.
The check resolves the chain of every entity of a source that has a `realm.json`. These fail with
exit 2, also with `--static-only`:

| Code | Failure |
| ---- | ------- |
| `RR013` | An `inherits` target can't be resolved |
| `RR014` | An inheritance chain is cyclic or over 3 hops |
| `RR015` | An `overrides` target isn't inherited |
| `RR017` | A `supersedes`, `extends` or `related` note of a live rule names a Realm that isn't declared |
| `RR018` | Such a note names a known Realm that holds no such rule |

These are warnings:

| Code | Warning |
| ---- | ------- |
| `RR016` | An overridden rule isn't Published or Implemented, so the override has no effect |
| `RR019` | A note or a rule body names a rule that isn't live |
| `RR020` | A rule body names a Realm that isn't declared |
| `RR021` | A rule body names a rule its Realm doesn't hold |
| `CHK003` | An entity declares `inherits` in a source without a `realm.json`, so its inherited rules aren't evaluated |

`strspc realm validate` doesn't run these checks. Rules named in an `extends` note are sent to the
evaluator with the rule that extends them, one hop only, so that rule is read in full; the verdict is
still for that rule alone.

## Safety and configuration checks

**Configuration.** Before resolving any source, the check exits 2 on an empty `rules:` list, an unknown
lifecycle state in `fail_on` or `evaluate_on`, or an unknown `evaluator.provider`. It then validates the
config against the published `config.v1` schema, kept in the [schema cache](reference.md#schema-cache). Each mismatch
is warning `CHK002` and never fails the check on its own. Examples are an unknown key, a long state
name such as `implemented` (accepted, but the schema wants the code), and `effort: xhigh` or `max`
(accepted by the Claude provider, but not by the schema). When the schema can't be fetched, this
validation is skipped and `--verbose` says so.

**Rule files.** Once the sources are resolved, and before anything is evaluated, the check validates
what it reads against the entity and realm schemas. That covers four things:

- every entity file of each source, and each `realm.json` that chose a schema: the nearest one above
  a file, listed as a sub-realm or not, and the ones it inherits a core pin from;
- the `realm.json` of each source realm and of its sub-realms;
- the whole tree of each realm a source or one of its sub-realms depends on, where a listed sub-realm
  without a readable `realm.json` is an error;
- the whole tree of each further dependency that an `inherits` or an `extends` note in those trees
  names.

The rules evaluated must then be the ones validated, source by source and realm by realm: if a file
changes during the run, the check exits 2 and asks you to run it again. It also exits 2 when one source
reaches two versions of a realm that differ in an entity it inherits or extends (or of which one lacks it), as it can't then tell
which was evaluated; two sources may use different versions. A file with a key twice in one object, in any case, is
refused as well, as the schema and the parser would read it differently.

`strspc rule verify`, and so `strspc rule promote` to Implemented, run the same validation. Each realm
is checked against the schemas of the core release it pins, and core against its own release. A
sub-realm that pins none takes those of the realm that lists it, and a pin that isn't a release
selects the latest schemas. A file that doesn't match exits 2, naming the file and the first violations;
`strspc lint` lists them all. A schema that isn't in the [schema cache](reference.md#schema-cache)
and can't be fetched also exits 2: run the `strspc schema fetch` command the error gives, which names
the core release when there is one, once while online to check offline afterwards.

**Endpoint safety.** `.strspc/config.yaml` is repository content, so a pull request could point
`evaluator.endpoint` at a host that would receive your `ANTHROPIC_API_KEY`. The check accepts an
endpoint from the config only when it's an `https://` URL on `anthropic.com`. Any other endpoint must
be opted in from outside the repository, with `STRSPC_EVALUATOR_ENDPOINT=<url>` (highest precedence) or
`--endpoint <url>`. Without that, it exits 2 with a message naming the host.
An opted-in endpoint must still be an `https://` URL, or `http://` to a loopback host (`localhost`,
`127.0.0.0/8` or `::1`), so the key never crosses the network in clear text. A plain-http endpoint
anywhere else exits 2.
