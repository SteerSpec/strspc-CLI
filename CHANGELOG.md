# Changelog

## [1.14.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.14.0...v1.14.1) (2026-03-28)


### Bug Fixes

* bump strspc-manager to v1.14.1 ([4dab185](https://github.com/SteerSpec/strspc-CLI/commit/4dab185693e11548676b9c0bdc6a4be8c1cebd66))

## [1.14.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.13.0...v1.14.0) (2026-03-26)


### Features

* **realm:** add --recursive flag to realm validate ([b51b2fb](https://github.com/SteerSpec/strspc-CLI/commit/b51b2fbf44a11b3f5a7e73819fa93d5a269cb2ff))
* **realm:** add --recursive flag to realm validate ([5ffaef4](https://github.com/SteerSpec/strspc-CLI/commit/5ffaef406cc45463b0a3ab481edc166c2c77d76d)), closes [#44](https://github.com/SteerSpec/strspc-CLI/issues/44)


### Bug Fixes

* **realm:** address PR [#68](https://github.com/SteerSpec/strspc-CLI/issues/68) review comments ([c413b21](https://github.com/SteerSpec/strspc-CLI/commit/c413b21286f3ff598284b8e7703bb666d69852ae))
* **realm:** address PR [#68](https://github.com/SteerSpec/strspc-CLI/issues/68) round-2 review comments ([d52ff84](https://github.com/SteerSpec/strspc-CLI/commit/d52ff842e9cfdf0eb0bc846b1e6b7c3bb070e8d6))
* **realm:** assert err==nil in RL012 cross-ref test ([fb47a66](https://github.com/SteerSpec/strspc-CLI/commit/fb47a66edac7d0793ed8c3a7960f1f9c6f8c62e7))

## [1.13.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.12.0...v1.13.0) (2026-03-25)


### Features

* **check:** add strspc check command — wrap ruleeval (CLI[#17](https://github.com/SteerSpec/strspc-CLI/issues/17)) ([d61a3cb](https://github.com/SteerSpec/strspc-CLI/commit/d61a3cbd82b2a2357ed2471b18f6ffb46943a2c1))
* **check:** add strspc check command — wrap ruleeval (CLI[#17](https://github.com/SteerSpec/strspc-CLI/issues/17)) ([30eac5d](https://github.com/SteerSpec/strspc-CLI/commit/30eac5d96aeaa4eebbefa163b524e0b46401d337))


### Bug Fixes

* **check:** address PR review comments (round 1) ([38e55fb](https://github.com/SteerSpec/strspc-CLI/commit/38e55fb6adf3a4335e1ee366a73aa5e70756a0fc))
* **check:** address round-2 PR review comments ([42f4fc9](https://github.com/SteerSpec/strspc-CLI/commit/42f4fc91c2fdd7114324cd569763ec1d1e1d6263))
* **ci:** use repo root for strspc check (not rules/ subdir) ([a93674e](https://github.com/SteerSpec/strspc-CLI/commit/a93674e3e571e43efc0a928ad2be782792f28920))


### Refactoring

* **check:** simplify via shared config helpers and remove no-op strict flag ([70a3876](https://github.com/SteerSpec/strspc-CLI/commit/70a38766dda536c5607715f68b6a4dfaaed7b5c2))

## [1.12.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.11.0...v1.12.0) (2026-03-25)


### Features

* MVP self-hosting — manager v1.9.0 upgrade, strspc sync, and CLI bootstrap ([52e712d](https://github.com/SteerSpec/strspc-CLI/commit/52e712d0e45cd68968d51f618f1e4683683d0bd1))
* **sync:** add strspc sync command — wrap ruleresolve (CLI[#16](https://github.com/SteerSpec/strspc-CLI/issues/16)) ([210a543](https://github.com/SteerSpec/strspc-CLI/commit/210a5431b086581d9a92dbc385ae7e5bd0fb065b))


### Bug Fixes

* **diff:** use baseRefOid instead of baseRefSha for gh pr view --json ([6c32e86](https://github.com/SteerSpec/strspc-CLI/commit/6c32e86ef24b75aef2e87456319079fb6e76e1d4))
* **sync:** address PR review comments ([35807ab](https://github.com/SteerSpec/strspc-CLI/commit/35807ab85ce3c7ea65f658ce311ff5e22dffae55))
* **sync:** address second round of PR review comments ([270311b](https://github.com/SteerSpec/strspc-CLI/commit/270311b58a0463991f95fb21afac510c76eb8462))
* **sync:** address third round of PR review comments ([f9a524a](https://github.com/SteerSpec/strspc-CLI/commit/f9a524a5ac240edccc1c76f600ca1d4cf274bc58))

## [1.11.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.10.0...v1.11.0) (2026-03-24)


### Features

* **diff:** add strspc diff command — wrap rulediff (CLI[#15](https://github.com/SteerSpec/strspc-CLI/issues/15)) ([687b81f](https://github.com/SteerSpec/strspc-CLI/commit/687b81f5b32bcc2b91b846c377ee4c61d18e23a7))
* **diff:** add strspc diff command — wrap rulediff (CLI[#15](https://github.com/SteerSpec/strspc-CLI/issues/15)) ([6bfa85d](https://github.com/SteerSpec/strspc-CLI/commit/6bfa85d995da110b79e4df30d5a5d509356a328a))


### Bug Fixes

* **diff:** address PR review comments ([501be09](https://github.com/SteerSpec/strspc-CLI/commit/501be095dba4362e94fa79679131eaf2a722d22b))
* **diff:** address second round of PR review comments ([758f50b](https://github.com/SteerSpec/strspc-CLI/commit/758f50b0827668e45953d0f5f9377009602652f4))
* **diff:** surface gh/git stderr and guard empty dir (PR review round 3) ([81047c2](https://github.com/SteerSpec/strspc-CLI/commit/81047c24fccd83fa991fee4ecb0d6dcc8fc023ce))

## [1.10.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.9.0...v1.10.0) (2026-03-24)


### Features

* **realm:** add realm add-subrealm command ([a1eb070](https://github.com/SteerSpec/strspc-CLI/commit/a1eb0704170bb42028015ee5a193fb60e8afb9ea))
* **realm:** add realm add-subrealm command ([583f456](https://github.com/SteerSpec/strspc-CLI/commit/583f456d1b88ff9703dd3c9618c03948168e20e9)), closes [#43](https://github.com/SteerSpec/strspc-CLI/issues/43)


### Bug Fixes

* **realm:** add same-dir guard and parent ID hierarchy check ([eff7935](https://github.com/SteerSpec/strspc-CLI/commit/eff79352c10ca29fa3b1cd1a8e238f4fbd325a47))
* **realm:** address PR review comments on add-subrealm ([baf11af](https://github.com/SteerSpec/strspc-CLI/commit/baf11af0e90502f1aafc3446ec4992be9ce6984b))
* **realm:** error when parent _schema/ exists but is not a directory ([31ec2ef](https://github.com/SteerSpec/strspc-CLI/commit/31ec2ef6d22a349c662d6b5aedfded1adb489e1d))


### Documentation

* **realm:** update add-subrealm help text and copySchemas comment ([2b9cd46](https://github.com/SteerSpec/strspc-CLI/commit/2b9cd46fec7c93a80ea709909595aeb1dbdcc3e5))

## [1.9.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.8.1...v1.9.0) (2026-03-23)


### Features

* **rule:** add rule lifecycle commands ([47b6c46](https://github.com/SteerSpec/strspc-CLI/commit/47b6c46d5ad0df2af4ac2b15cf7beddf1a9a3525))
* **rule:** add rule lifecycle commands (add, update, promote, retire, abandon, supersede) ([113e439](https://github.com/SteerSpec/strspc-CLI/commit/113e439451f06cb788a650fd9df53027db189125))


### Bug Fixes

* **rule:** add revision to add/supersede JSON, fix retire transition display ([5b7c5f0](https://github.com/SteerSpec/strspc-CLI/commit/5b7c5f0f4a96270ad7ec954a05961916a1e1ca6f))
* **rule:** show old → new state transition in promote, retire, abandon ([ff76223](https://github.com/SteerSpec/strspc-CLI/commit/ff76223931ff4160078fd1df03add6f4210283e7))
* **rule:** show version bump transition and fix retire verb ([9c93c7e](https://github.com/SteerSpec/strspc-CLI/commit/9c93c7eb23975552cd46f411a6d3f655b79997df))
* **rule:** use old_state/new_state in transition JSON, add retired supersede test ([9e1c5ba](https://github.com/SteerSpec/strspc-CLI/commit/9e1c5baeead8f80cdbb2741b930d57a9b4d6c553))

## [1.8.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.8.0...v1.8.1) (2026-03-23)


### Documentation

* add Rule Manager Spec reference to CLAUDE.md ([988fc25](https://github.com/SteerSpec/strspc-CLI/commit/988fc259a968d0632e15074948f5beff6889a3a5))
* add Rule Manager Spec reference to CLAUDE.md ([3193622](https://github.com/SteerSpec/strspc-CLI/commit/31936224814ccc61168dac9c462a3ba223e7ea90))

## [1.8.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.7.0...v1.8.0) (2026-03-23)


### Features

* **realm:** add strspc realm dep add/remove/list ([44cf1c3](https://github.com/SteerSpec/strspc-CLI/commit/44cf1c31e711e086c77e6f5fd4defa7ec9d98e12))
* **realm:** add strspc realm dep add/remove/list commands ([213b83c](https://github.com/SteerSpec/strspc-CLI/commit/213b83cf504ed0e832ea6f8f697293ccfd3ed9df)), closes [#38](https://github.com/SteerSpec/strspc-CLI/issues/38)


### Bug Fixes

* **realm:** address second round of PR review comments ([ce08168](https://github.com/SteerSpec/strspc-CLI/commit/ce08168d916060476a805878ed02e00f3c6c3355))
* **realm:** improve error handling in realm dep commands ([0399e11](https://github.com/SteerSpec/strspc-CLI/commit/0399e110f1c119b0447a90076b61530797ab71da))

## [1.7.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.6.0...v1.7.0) (2026-03-23)


### Features

* **realm:** add strspc realm add command ([1fdb0cd](https://github.com/SteerSpec/strspc-CLI/commit/1fdb0cdb56c53c3409fc22e3040e36fc2d88f6d9))
* **realm:** add strspc realm add command ([9b3e9a0](https://github.com/SteerSpec/strspc-CLI/commit/9b3e9a02c5b302cc23f71234473da63f4608c59e)), closes [#10](https://github.com/SteerSpec/strspc-CLI/issues/10)


### Bug Fixes

* **realm:** improve error handling in realm add command ([956708a](https://github.com/SteerSpec/strspc-CLI/commit/956708a9862c695d1c1dfa333ed1fe3b687e8190))

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
