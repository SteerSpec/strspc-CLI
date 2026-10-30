# Changelog

## [1.25.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.24.0...v1.25.0) (2026-10-30)


### Features

* **ci:** add --cli-version to ci init ([4a8e5d4](https://github.com/SteerSpec/strspc-CLI/commit/4a8e5d437a5f163f491e031153f6f6f206e8c313))
* **ci:** add --cli-version to ci init ([64a3075](https://github.com/SteerSpec/strspc-CLI/commit/64a30754bcd25024e20245a34964d6f9ce970c1a))
* **realm:** add 'realm dep upgrade' to bump github:// dependencies ([8be69b1](https://github.com/SteerSpec/strspc-CLI/commit/8be69b12b8fa6b5477d6c048e022e065924e4687))
* **realm:** add 'realm dep upgrade' to bump github:// dependencies ([762fa76](https://github.com/SteerSpec/strspc-CLI/commit/762fa760e0d85c7d62aedee94e21c0a4d36033c4))
* **realm:** compare a realm with its pinned dependencies ([3a5bb32](https://github.com/SteerSpec/strspc-CLI/commit/3a5bb32911a4850b85d9582982c4910f34adda14))
* **realm:** compare a realm with its pinned dependencies ([3265ff3](https://github.com/SteerSpec/strspc-CLI/commit/3265ff3e561eee3f1360f4f832fbc605875ffe43))


### Bug Fixes

* **check:** refuse invalid UTF-8 with control characters ([fed9d1a](https://github.com/SteerSpec/strspc-CLI/commit/fed9d1a48675b869fe0d40af63a4d2c6009f9b6f))
* **check:** treat C1 characters as control characters ([2c488e3](https://github.com/SteerSpec/strspc-CLI/commit/2c488e35fcd995853cfffd9e144edc7021e03bd4))
* **realm:** keep control characters out of dep output ([1a06e75](https://github.com/SteerSpec/strspc-CLI/commit/1a06e75b21f2f45401a98192ff46937cf2b85e28))
* **realm:** keep control characters out of dependency output ([abe80c2](https://github.com/SteerSpec/strspc-CLI/commit/abe80c2eb962018d043efe3085cf070fa8b317e7))
* **realm:** keep sub_realms when rewriting realm.json; skip SHA-pinned sources ([4a6511f](https://github.com/SteerSpec/strspc-CLI/commit/4a6511f76286d6d351575ef36193d8969b18e56c))
* **realm:** quote an invalid realm ID in dep list ([4a109d1](https://github.com/SteerSpec/strspc-CLI/commit/4a109d13e372a7e4d23f51c69e6c501311ad565a))
* **realm:** refuse control characters in a dependency source ([d9c76cc](https://github.com/SteerSpec/strspc-CLI/commit/d9c76cc2f64274d7c655ab0531acec9a87562fda))
* **realm:** upgrade only sources pinned to a release tag ([c36b967](https://github.com/SteerSpec/strspc-CLI/commit/c36b967df3ad703a5b6d0b1d90c22a1c40672e2b))

## [1.24.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.23.0...v1.24.0) (2026-10-30)


### Features

* **check:** evaluate the effective rule set of inheriting entities ([e01f359](https://github.com/SteerSpec/strspc-CLI/commit/e01f359d6b974226f1ecf7803b8db757ae2ad65b)), closes [#122](https://github.com/SteerSpec/strspc-CLI/issues/122)
* **render:** list inherited rules in Markdown and --json ([5839f57](https://github.com/SteerSpec/strspc-CLI/commit/5839f57ac1a256614af263224cca97eb07df367c)), closes [#122](https://github.com/SteerSpec/strspc-CLI/issues/122)
* wire entity inheritance into check and render ([96c1168](https://github.com/SteerSpec/strspc-CLI/commit/96c1168b99c43f0a33adaa6bab1e7a684cc8280b))


### Bug Fixes

* **check:** resolve dependency-free realms that only declare overrides ([a59014f](https://github.com/SteerSpec/strspc-CLI/commit/a59014f1e840e3b259dcfb8f24dbc6189a590796))
* **render:** keep an empty inherited array for a resolved chain ([3210199](https://github.com/SteerSpec/strspc-CLI/commit/32101995633a3201ac96fbb78ea82fccffebd4a3))


### Documentation

* describe inheritance in check and render ([3619d4f](https://github.com/SteerSpec/strspc-CLI/commit/3619d4f4a43604b3a43db88a0111670dd4f94258))

## [1.23.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.22.0...v1.23.0) (2026-10-30)


### Features

* **cli:** add ci init ([1ba7b94](https://github.com/SteerSpec/strspc-CLI/commit/1ba7b94e5f7a3eade34d876232328eb3db7c30f0))
* **cli:** add ci init to wire SteerSpec rule workflows into CI ([8dac793](https://github.com/SteerSpec/strspc-CLI/commit/8dac7937ee5619c25831c5b3a0aaa5a594d128d0))
* **cmd:** add --inherits and --overrides flags ([1cab555](https://github.com/SteerSpec/strspc-CLI/commit/1cab555f7ce6dd98798b1cd278bb3b450c645b0c))
* **cmd:** add --inherits and --overrides flags ([8d9981d](https://github.com/SteerSpec/strspc-CLI/commit/8d9981d5f72f946bdbfa79da6e93606ed78838ba))


### Bug Fixes

* **cli:** reject unsafe values interpolated into ci init YAML ([6541edf](https://github.com/SteerSpec/strspc-CLI/commit/6541edfd153787bf8d2fcfa5669f5943b78b8a97))
* **cli:** reject YAML indicator-leading values in ci init ([5ee05c0](https://github.com/SteerSpec/strspc-CLI/commit/5ee05c0a5ad0f1173482044868133647a67bff4d))
* **cli:** reword ci init validation error for staticcheck ([1a588f0](https://github.com/SteerSpec/strspc-CLI/commit/1a588f032ee7d5b72005f460da2f98ed5373f92a))
* **cli:** target the default branch and forward STRSPC_GITHUB_TOKEN in ci init ([4e95bc4](https://github.com/SteerSpec/strspc-CLI/commit/4e95bc4402f6a13145b9218c21e49403ad2f4708))
* **cli:** write .strspc/.gitignore and refuse symlinked destinations in ci init ([182542c](https://github.com/SteerSpec/strspc-CLI/commit/182542cef276477a4be31e9207097db3f7e939cc))
* **cli:** write ci init files through os.Root ([c71e888](https://github.com/SteerSpec/strspc-CLI/commit/c71e888574438d3736b4e5f7300dabff137c452d))
* **cmd:** reject realm-qualified self-inheritance ([e9c4326](https://github.com/SteerSpec/strspc-CLI/commit/e9c4326c0227f680cb252fa60d2e919a5cf34958))


### Documentation

* **cli:** document --branch and STRSPC_GITHUB_TOKEN for ci init ([ac43f22](https://github.com/SteerSpec/strspc-CLI/commit/ac43f22e978da8cb4f23eba819e1836bd48ca080))
* **cmd:** name the source of the realm-id grammar for inheritance flags ([6dcbee3](https://github.com/SteerSpec/strspc-CLI/commit/6dcbee3d21da5ec60b6e25913b693e7ffb018a71))

## [1.22.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.21.0...v1.22.0) (2026-10-29)


### Features

* **cli:** add --version flag and document brew trust ([f01ed21](https://github.com/SteerSpec/strspc-CLI/commit/f01ed21f6e4f6e7c0e1f90b7eaecf861cc30008e))
* **cli:** add --version flag and document brew trust ([db73bdd](https://github.com/SteerSpec/strspc-CLI/commit/db73bdd086cf77bc4f36c8f5c2f2cdf58ea59678))
* **render:** honour the realm's rule_identifier_format ([92ebbce](https://github.com/SteerSpec/strspc-CLI/commit/92ebbce57876514519a6af87e49d57af513c3e5b))
* **render:** honour the realm's rule_identifier_format ([d520777](https://github.com/SteerSpec/strspc-CLI/commit/d5207772a72f9539ea457afac5c37631dfa9cf37)), closes [#133](https://github.com/SteerSpec/strspc-CLI/issues/133)


### Bug Fixes

* **realm:** validate dependency version as SemVer ([eb18b79](https://github.com/SteerSpec/strspc-CLI/commit/eb18b790437a082f619c594ab0ce46a4f6d427ce))
* **realm:** validate dependency version as SemVer ([3b131d0](https://github.com/SteerSpec/strspc-CLI/commit/3b131d00c88cd7afbcbe62194e8df17fcef3bd01)), closes [#121](https://github.com/SteerSpec/strspc-CLI/issues/121)
* **render:** cache renderers per format and reject invalid realm.json ([28c383d](https://github.com/SteerSpec/strspc-CLI/commit/28c383d3681cab22c1bff16b8b8cc485b14cffb3))

## [1.21.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.20.2...v1.21.0) (2026-10-28)


### Features

* **note:** add note update; note add stamps revision 0 ([6fa7f8a](https://github.com/SteerSpec/strspc-CLI/commit/6fa7f8a5f3decd25aad1315cea5bdf6573042b47))
* **note:** add note update; note add stamps revision 0 ([5318516](https://github.com/SteerSpec/strspc-CLI/commit/53185161fc02a06b7d79c427611f34975f562e84)), closes [#126](https://github.com/SteerSpec/strspc-CLI/issues/126)


### Bug Fixes

* **note:** reject non-numeric note ID suffix in note update ([64b5a1e](https://github.com/SteerSpec/strspc-CLI/commit/64b5a1e5a45bdb73cb6363ed3d995d0e603a6380))

## [1.20.2](https://github.com/SteerSpec/strspc-CLI/compare/v1.20.1...v1.20.2) (2026-10-27)


### Bug Fixes

* **release:** require all five MACOS_* secrets before signing ([74a1945](https://github.com/SteerSpec/strspc-CLI/commit/74a19456e906fca3e4a2187ccac5cdfbd9ef2b77))

## [1.20.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.20.0...v1.20.1) (2026-10-27)


### Bug Fixes

* **release:** stop auto-removing macOS quarantine from cask ([48d5afc](https://github.com/SteerSpec/strspc-CLI/commit/48d5afce3676de9e31e3fdd67d202d4bbe403303))
* **release:** use install_steps for cask quarantine hook ([c22dfa1](https://github.com/SteerSpec/strspc-CLI/commit/c22dfa1e8d27e5dbbfee0927af48e12c26541a2f))

## [1.20.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.19.1...v1.20.0) (2026-10-27)


### Features

* **entity:** add entity add command for sub-entities ([321ba7c](https://github.com/SteerSpec/strspc-CLI/commit/321ba7ca39554b8e77b4698a3bc547c43ae42196))
* **entity:** add entity add command for sub-entities ([b6389de](https://github.com/SteerSpec/strspc-CLI/commit/b6389de26eef1205bf8a1cb01584a4d2cb6a1e62)), closes [#115](https://github.com/SteerSpec/strspc-CLI/issues/115)


### Bug Fixes

* **entity:** validate --parent and abort uniqueness scan on parse errors ([e09ca95](https://github.com/SteerSpec/strspc-CLI/commit/e09ca956d2a5f43796ceacb323ba7c4cc174e5d7))

## [1.19.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.19.0...v1.19.1) (2026-10-24)


### Bug Fixes

* **deps:** bump strspc-manager to v1.19.0 ([752cfce](https://github.com/SteerSpec/strspc-CLI/commit/752cfce11cacbbcc5d71a30efec4ef0a21c8e84d))
* **deps:** bump strspc-manager to v1.19.0 ([c81f9e5](https://github.com/SteerSpec/strspc-CLI/commit/c81f9e54a34bf9de5e7172b0c13934b9b7b060c7))

## [1.19.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.18.2...v1.19.0) (2026-10-24)


### Features

* **check:** validate config.yaml structure before resolving sources ([e9f035a](https://github.com/SteerSpec/strspc-CLI/commit/e9f035ae93a1c313aef9a2c0a2518ad9006d0192))
* **check:** validate config.yaml structure before resolving sources ([3207e79](https://github.com/SteerSpec/strspc-CLI/commit/3207e799caf0245c3d5810611a2d37bf10666ce4)), closes [#94](https://github.com/SteerSpec/strspc-CLI/issues/94)

## [1.18.2](https://github.com/SteerSpec/strspc-CLI/compare/v1.18.1...v1.18.2) (2026-10-22)


### Bug Fixes

* **init:** pin the default core source to strspc-rules v1.7.0 ([2d0b5e9](https://github.com/SteerSpec/strspc-CLI/commit/2d0b5e9c5c01209cbd816112a3facd3cbd500164))
* **init:** pin the default core source to strspc-rules v1.7.0 ([27b835c](https://github.com/SteerSpec/strspc-CLI/commit/27b835c623642d1383e0ea9e78c31dbf4bca3158))

## [1.18.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.18.0...v1.18.1) (2026-10-21)


### Bug Fixes

* **deps:** bump strspc-manager to v1.18.0 and pass the check context to evalcache.Open ([d5081e4](https://github.com/SteerSpec/strspc-CLI/commit/d5081e42065837657cde7831bea8cb5e946a3123))
* **deps:** bump strspc-manager to v1.18.0 and pass the check context to evalcache.Open ([34d3f7f](https://github.com/SteerSpec/strspc-CLI/commit/34d3f7fc468a7a24e11f714b6acb5b05f146ecf0))

## [1.18.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.17.3...v1.18.0) (2026-10-21)


### Features

* **deps:** bump strspc-manager to v1.17.0 and pass the command context to the linters ([4a36f95](https://github.com/SteerSpec/strspc-CLI/commit/4a36f95be7559eb2724e1782692a0ed0b185a3a2))
* **deps:** bump strspc-manager to v1.17.0 and pass the command context to the linters ([0bdfb74](https://github.com/SteerSpec/strspc-CLI/commit/0bdfb74372985a01f97babe89ef6729113dcf601))

## [1.17.3](https://github.com/SteerSpec/strspc-CLI/compare/v1.17.2...v1.17.3) (2026-10-21)


### Bug Fixes

* **deps:** bump strspc-manager to v1.16.5 for the corrupt-cache recovery fix ([22cb1dd](https://github.com/SteerSpec/strspc-CLI/commit/22cb1ddd32a334250f83602bd022e4c5100cbe93))
* **deps:** bump strspc-manager to v1.16.5 for the corrupt-cache recovery fix ([4eec04f](https://github.com/SteerSpec/strspc-CLI/commit/4eec04f987b2a42893e0324c0c8ec0df3a21f626))

## [1.17.2](https://github.com/SteerSpec/strspc-CLI/compare/v1.17.1...v1.17.2) (2026-10-20)


### Bug Fixes

* **deps:** bump strspc-manager to v1.16.2 so the evaluator stops failing PRs on fixtures ([e33dc73](https://github.com/SteerSpec/strspc-CLI/commit/e33dc73f0cc40f48d5f4fe7e618cb516f8771948))
* **deps:** bump strspc-manager to v1.16.2 so the evaluator stops failing PRs on fixtures ([f97a7c2](https://github.com/SteerSpec/strspc-CLI/commit/f97a7c2776768a0ef57cb7defa82733491f55d30))

## [1.17.1](https://github.com/SteerSpec/strspc-CLI/compare/v1.17.0...v1.17.1) (2026-10-20)


### Bug Fixes

* **setup-strspc:** tolerate a release whose assets are still uploading ([#92](https://github.com/SteerSpec/strspc-CLI/issues/92)) ([1ce43e4](https://github.com/SteerSpec/strspc-CLI/commit/1ce43e4484d9451083c0774a9926178889d684f2))

## [1.17.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.16.0...v1.17.0) (2026-10-20)


### Features

* **rules:** promote CMD-001 and CMD-002 to Implemented ([#90](https://github.com/SteerSpec/strspc-CLI/issues/90)) ([a5713d5](https://github.com/SteerSpec/strspc-CLI/commit/a5713d5f8ac5eda757ffa541a6d28cd53274cfd8))

## [1.16.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.15.0...v1.16.0) (2026-10-20)


### Features

* **rules:** core dependency, new conventions, publish command and config rules ([#86](https://github.com/SteerSpec/strspc-CLI/issues/86)) ([4e1c0ab](https://github.com/SteerSpec/strspc-CLI/commit/4e1c0abd515ddf2dc24333ea40da02aeb9d8cc79))


### Bug Fixes

* **check:** exit 3 when every provider request failed ([#88](https://github.com/SteerSpec/strspc-CLI/issues/88)) ([5ab6953](https://github.com/SteerSpec/strspc-CLI/commit/5ab6953d1f5ec956de106704c814e85261305c74))

## [1.15.0](https://github.com/SteerSpec/strspc-CLI/compare/v1.14.2...v1.15.0) (2026-10-19)


### Features

* **check:** evaluate diffs against resolved rules with Claude ([#80](https://github.com/SteerSpec/strspc-CLI/issues/80)) ([756dea2](https://github.com/SteerSpec/strspc-CLI/commit/756dea209bd01506765f1647aff6ff0381e91ede))
* **cli:** valid init defaults, note add, entity rehash, sub-entity rule ops ([#79](https://github.com/SteerSpec/strspc-CLI/issues/79)) ([502c389](https://github.com/SteerSpec/strspc-CLI/commit/502c389c389574d88dd4cf73c5ed93acf6b9c7b5))


### Bug Fixes

* **check:** resolve sibling dependencies of remote realms through realmresolve origin ([#82](https://github.com/SteerSpec/strspc-CLI/issues/82)) ([16a623e](https://github.com/SteerSpec/strspc-CLI/commit/16a623e0142614c8639b1a87afc58e5f1f917658))

## [1.14.2](https://github.com/SteerSpec/strspc-CLI/compare/v1.14.1...v1.14.2) (2026-08-20)


### Documentation

* add the LICENSE the README already advertises ([#76](https://github.com/SteerSpec/strspc-CLI/issues/76)) ([e194b6c](https://github.com/SteerSpec/strspc-CLI/commit/e194b6c997353cdb0ab5ce242beae6b773ab3220))

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
