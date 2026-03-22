# Changelog

## [1.2.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.2.0...v1.2.1) (2026-03-22)


### Bug Fixes

* **ci:** use PAT in release-please to trigger GoReleaser ([2248dcd](https://github.com/SteerSpec/strspc-CLI/commit/2248dcddb5cb99480fd0dacfce288b43f4e08017))
* **ci:** use PAT in release-please to trigger GoReleaser ([83850c8](https://github.com/SteerSpec/strspc-CLI/commit/83850c8d84d2fce152c271b4ccfd4f083193e2fe)), closes [#21](https://github.com/SteerSpec/strspc-CLI/issues/21)

## [1.2.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.1.0...v1.2.0) (2026-03-22)


### Features

* **render:** fix schema validation for realm files and add --json flag ([acddd87](https://github.com/SteerSpec/strspc-CLI/commit/acddd87f52b01b4b8691c50313d4d256ae136870))
* **render:** fix schema validation for realm files and add --json flag ([7e629ae](https://github.com/SteerSpec/strspc-CLI/commit/7e629aeb60b612ed9b195dfd4564bbc95506afc5)), closes [#18](https://github.com/SteerSpec/strspc-CLI/issues/18)


### Bug Fixes

* **render:** address PR review feedback ([7b2b6d4](https://github.com/SteerSpec/strspc-CLI/commit/7b2b6d4394232f1945aa1f15cbcdbf06fd98eb5f))
* **render:** address round 2 PR review feedback ([3736041](https://github.com/SteerSpec/strspc-CLI/commit/3736041a196973542b964962035c76486c58c436))
* **render:** address round 3 PR review feedback ([75fcdb8](https://github.com/SteerSpec/strspc-CLI/commit/75fcdb86d13cf5d2074222aadeb47f3dccc6fa51))


### Documentation

* overhaul README with install, usage, and architecture ([127800b](https://github.com/SteerSpec/strspc-CLI/commit/127800bff88059d669c176d9fd969e8c457121e7))

## [1.1.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.0.0...v1.1.0) (2026-03-22)


### Features

* **render:** implement rule-render module (JSON → Markdown) ([dde7067](https://github.com/SteerSpec/strspc-CLI/commit/dde7067793609f504feab52e49f59230603f5401))
* **render:** implement rule-render module (JSON → Markdown) ([1e8e8b7](https://github.com/SteerSpec/strspc-CLI/commit/1e8e8b760dcdfd88bc99b146dbdce4c5dbf4dc12)), closes [#1](https://github.com/SteerSpec/strspc-CLI/issues/1)


### Bug Fixes

* **render:** address PR review feedback ([f986674](https://github.com/SteerSpec/strspc-CLI/commit/f9866744f1808f5c59c7757a07345da7f6aac93d))
* **render:** address round 2 PR review feedback ([f5d9dca](https://github.com/SteerSpec/strspc-CLI/commit/f5d9dca3c32015233e86e345288874487d419f32))
* **render:** skip underscore-prefixed directories in directory walk ([35f9cc3](https://github.com/SteerSpec/strspc-CLI/commit/35f9cc3ed5dfccff54ede456174059ee69f9ebe3))

## 1.0.0 (2026-03-21)


### Features

* initialize CLI with Cobra and lipgloss styling ([9f1908f](https://github.com/SteerSpec/strspc-CLI/commit/9f1908f10d1229676d66b4310c6405901a47a233))
* initialize CLI with Cobra and lipgloss styling ([bb2948c](https://github.com/SteerSpec/strspc-CLI/commit/bb2948c4e38982c5844d360182bcd8762cfbbf8c))


### Bug Fixes

* address PR review comments ([7eaa656](https://github.com/SteerSpec/strspc-CLI/commit/7eaa65677212620b8d5cd94f72eec04e346e4160))
* context-aware help and remove committed credential key ([40f9c32](https://github.com/SteerSpec/strspc-CLI/commit/40f9c323113f2250d1e2eb9f6a9dfb0983a60878))
* restore versionInfo state in tests with t.Cleanup ([7cbd5bf](https://github.com/SteerSpec/strspc-CLI/commit/7cbd5bfa0252262eb4b752eee919072a6e8531c6))


### Refactoring

* use command factories for test isolation ([e471740](https://github.com/SteerSpec/strspc-CLI/commit/e471740fefe59a92b1bdc385bea76846bc4a192a))


### Documentation

* add rule sources section with steerspec.dev URLs ([8c881a3](https://github.com/SteerSpec/strspc-CLI/commit/8c881a3c9d73dfd77e26bee88b010975c852fa7f))
* add rule sources with steerspec.dev URLs ([30b0b63](https://github.com/SteerSpec/strspc-CLI/commit/30b0b63f6e9436651ea430751bd1cbf8a1496f64))
