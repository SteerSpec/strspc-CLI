# Changelog

## [1.6.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.5.0...v1.6.0) (2026-03-22)


### Features

* **realm:** add strspc realm validate command ([aa55c32](https://github.com/SteerSpec/strspc-CLI/commit/aa55c32cb244a23a54595fdeeed5fbcc0bad7378))
* **realm:** add strspc realm validate command ([f247c4e](https://github.com/SteerSpec/strspc-CLI/commit/f247c4e38b46fe072391edd9801c52047726b26b)), closes [#11](https://github.com/SteerSpec/strspc-CLI/issues/11)


### Bug Fixes

* **lint:** use command-specific error messages in JSON output ([3b4e923](https://github.com/SteerSpec/strspc-CLI/commit/3b4e923b7aefc36124e582904bdc4bee690611be))

## [1.5.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.4.0...v1.5.0) (2026-03-22)


### Features

* **lint:** add strspc lint command wrapping rulelint ([ec8bf7e](https://github.com/SteerSpec/strspc-CLI/commit/ec8bf7ecbf9cd41b417f67fa6f6b0a74082d2a2c))
* **lint:** add strspc lint command wrapping rulelint ([7d125ba](https://github.com/SteerSpec/strspc-CLI/commit/7d125ba7c123cd68284aeff67a12642bc17bd941)), closes [#14](https://github.com/SteerSpec/strspc-CLI/issues/14)


### Bug Fixes

* **lint:** address PR review comments ([65df826](https://github.com/SteerSpec/strspc-CLI/commit/65df826925f2f7f22644ae228752f7d8c4e82f64))
* **lint:** address second round of PR review comments ([5f12561](https://github.com/SteerSpec/strspc-CLI/commit/5f125619c5b958ef3381498c0e5cce9950e96217))
* **lint:** error when directory contains no entity files ([318727d](https://github.com/SteerSpec/strspc-CLI/commit/318727d0d32094fa9b59869ec5c1fdfe2fd6387d))

## [1.4.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.3.0...v1.4.0) (2026-03-22)


### Features

* **realm:** add --dependency flag to realm init ([554c27b](https://github.com/SteerSpec/strspc-CLI/commit/554c27b8bf64be87a184de4dd0ad7ca02a970bcc)), closes [#37](https://github.com/SteerSpec/strspc-CLI/issues/37) [#36](https://github.com/SteerSpec/strspc-CLI/issues/36)


### Bug Fixes

* address PR [#39](https://github.com/SteerSpec/strspc-CLI/issues/39) review comments ([c6781ae](https://github.com/SteerSpec/strspc-CLI/commit/c6781ae498c646fdd8678b2177a349dc9d76b120))
* address second round of PR [#39](https://github.com/SteerSpec/strspc-CLI/issues/39) review comments ([a7e4665](https://github.com/SteerSpec/strspc-CLI/commit/a7e466588ae124b1d20a5b395af2f81dc4686987))
* **init:** bridge fixes for init and realm init ([ab5700d](https://github.com/SteerSpec/strspc-CLI/commit/ab5700d44bd9cdaed1e84242520ade449cc97757))
* **init:** comment out unimplemented github:// source in default config ([554c27b](https://github.com/SteerSpec/strspc-CLI/commit/554c27b8bf64be87a184de4dd0ad7ca02a970bcc))
* trim idVersion after Cut, rephrase coming soon text ([8b06907](https://github.com/SteerSpec/strspc-CLI/commit/8b06907f7017b144dfa66dc312c9e386eebbfeff))


### Refactoring

* **render:** replace local schema validation with manager imports ([935f0d6](https://github.com/SteerSpec/strspc-CLI/commit/935f0d6c7c84a244b3e2109a391fd438a2136519))
* **render:** replace local schema validation with manager imports ([f9ef012](https://github.com/SteerSpec/strspc-CLI/commit/f9ef0125475f9d4c0458e345b7e133012c46ff00)), closes [#27](https://github.com/SteerSpec/strspc-CLI/issues/27)

## [1.3.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.2.2...v1.3.0) (2026-03-22)


### Features

* implement strspc init and strspc realm init commands ([9a1480e](https://github.com/SteerSpec/strspc-CLI/commit/9a1480e49978d81a74fb1b1b02527e7f8d158ec3))
* **init:** implement strspc init command ([bbbd362](https://github.com/SteerSpec/strspc-CLI/commit/bbbd36263d52eeb6a84c851beb51e4dff1c471f2)), closes [#4](https://github.com/SteerSpec/strspc-CLI/issues/4)
* **realm:** implement strspc realm init command ([095cd77](https://github.com/SteerSpec/strspc-CLI/commit/095cd77447fefcfc80987bc9a2bdd5fb9f232c4c)), closes [#9](https://github.com/SteerSpec/strspc-CLI/issues/9)


### Bug Fixes

* address PR [#28](https://github.com/SteerSpec/strspc-CLI/issues/28) review comments ([bdf2f16](https://github.com/SteerSpec/strspc-CLI/commit/bdf2f163a644259a1c353039edee37f2a2a94738))

## [1.2.2](https://github.com/SteerSpec/strspc-CLI/compare/v1.2.1...v1.2.2) (2026-03-22)


### Refactoring

* import entity + render from strspc-manager ([7f19f96](https://github.com/SteerSpec/strspc-CLI/commit/7f19f967ba64bb636266e14e2cf196990a9a5042))
* import entity + render from strspc-manager, delete internal duplicates ([e5fe93c](https://github.com/SteerSpec/strspc-CLI/commit/e5fe93cc3774363a5cd5d002699059efa8ea0dce)), closes [#24](https://github.com/SteerSpec/strspc-CLI/issues/24)

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
