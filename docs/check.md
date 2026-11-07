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
| `--endpoint <url>` | Provider endpoint; needed to opt in to a non-Anthropic host |
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
[SPEC §8.3](https://github.com/SteerSpec/strspc-manager/blob/v1.43.0/docs/SPEC.md#83-evaluation-flow):

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
config against the published `config.v1` schema, cached under `.strspc/cache/schemas/`. Each mismatch
is warning `CHK002` and never fails the check on its own. Examples are an unknown key, a long state
name such as `implemented` (accepted, but the schema wants the code), and `effort: xhigh` or `max`
(accepted by the Claude provider, but not by the schema). When the schema can't be fetched, this
validation is skipped and `--verbose` says so.

**Endpoint safety.** `.strspc/config.yaml` is repository content, so a pull request could point
`evaluator.endpoint` at a host that would receive your `ANTHROPIC_API_KEY`. The check accepts an
endpoint from the config only when it's an `https://` URL on `anthropic.com`. Any other endpoint must
be opted in from outside the repository, with `STRSPC_EVALUATOR_ENDPOINT=<url>` (highest precedence) or
`--endpoint <url>`. Without that, it exits 2 with a message naming the host.
