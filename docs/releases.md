# Branching and releases

Back to the [README](../README.md).

Releases are automated with
[release-please](https://github.com/googleapis/release-please) (version bumps and changelog) and
[goreleaser](https://goreleaser.com/) (cross-platform binaries):

1. Pull requests target `develop` and are squashed, so the title becomes the commit release-please
   reads.
2. Merging `develop` into `main`, with a merge commit to keep the conventional commits, triggers a
   release pull request.
3. Merging that pull request creates a tag, and the tag triggers goreleaser.

> [!IMPORTANT]
> Promote `develop` before merging a release pull request. release-please stops reading at the last
> release commit, by date, so a `feat:` or `fix:` left on `develop` while a release merges is
> dropped from the next release.

The `Release changelog` workflow catches a dropped commit. After every Release Please run, and on
each release pull request, it lists the commits `main` gained since the last release by ancestry,
keeps the types the changelog shows, and checks that `CHANGELOG.md` names each one. Its verdict is
the `release-changelog` status on the release pull request; with no release pull request open, the
workflow run itself fails. Every other pull request into `main`, such as a promotion, gets a
passing `release-changelog` status, so the status can be required on `main`. To restore a dropped
commit, add it to the body of the latest promotion pull request and re-run Release Please:

```text
BEGIN_COMMIT_OVERRIDE
fix(eval): example subject of the skipped commit (#123)
END_COMMIT_OVERRIDE
```

release-please reads that block in place of the promotion's merge commit, so list each skipped
commit, one per paragraph, ending in its `(#<pr>)`.
