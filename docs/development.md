# Development

Back to the [README](../README.md).

Requires Go 1.26+ and [golangci-lint](https://golangci-lint.run/) v2.

```bash
make build          # build binary → ./strspc
make test           # run tests with -race
make cover          # run tests with coverage; fails below 95% (CONV-005)
make lint           # golangci-lint
make fmt            # formatting via golangci-lint fmt
make strspc-lint    # lint this repository's rules/
make clean          # remove build artifacts
make install-hooks  # install conventional commits git hook
```

```text
strspc-CLI/
├── src/cmd/                   # command implementations
├── src/internal/              # UI and test helpers
├── rules/                     # the dev.steerspec.cli realm
├── docs/                      # reference pages linked from the README
├── .strspc/config.yaml        # strspc check configuration
└── .github/actions/setup-strspc/
```

See the [SteerSpec contributing guide](https://github.com/SteerSpec/.github/blob/v1.2.0/CONTRIBUTING.md).
Commits follow [Conventional Commits](https://www.conventionalcommits.org/), as
`<type>(<scope>): <description>`. The types are `feat`, `fix`, `docs`, `style`, `refactor`, `perf`,
`test`, `build`, `ci`, `chore` and `revert`.
