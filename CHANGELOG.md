# Changelog

## [0.3.0](https://github.com/rudderlabs/rudder-load/compare/v0.2.0...v0.3.0) (2026-10-15)


### Features

* **helm:** add a Pulsar soak test profile and optional pod annotations ([#62](https://github.com/rudderlabs/rudder-load/issues/62)) ([20a4300](https://github.com/rudderlabs/rudder-load/commit/20a430000efaab040f9fb9334a4da0205939a9a2))
* **producer:** stop generating load at RUN_DEADLINE ([#64](https://github.com/rudderlabs/rudder-load/issues/64)) ([f003c16](https://github.com/rudderlabs/rudder-load/commit/f003c1653db214bad61cbeadfb14f76954f5c558))


### Miscellaneous

* **deps:** bump github.com/rudderlabs/rudder-go-kit ([#60](https://github.com/rudderlabs/rudder-load/issues/60)) ([1b8bfa8](https://github.com/rudderlabs/rudder-load/commit/1b8bfa809ee18b99e466bdac6dc6bf2eeedb5772))
* **deps:** bump step-security/harden-runner ([#61](https://github.com/rudderlabs/rudder-load/issues/61)) ([68d4eb7](https://github.com/rudderlabs/rudder-load/commit/68d4eb7a36bb1cfc982607e009919fb5a472da82))

## [0.2.0](https://github.com/rudderlabs/rudder-load/compare/v0.1.1...v0.2.0) (2026-10-14)


### Features

* **producer:** retry HTTP requests after connection errors ([#58](https://github.com/rudderlabs/rudder-load/issues/58)) ([62b61e1](https://github.com/rudderlabs/rudder-load/commit/62b61e161bed4f1a2cf6a6281b4a42fb25d09513))

## [0.1.1](https://github.com/rudderlabs/rudder-load/compare/v0.1.0...v0.1.1) (2026-10-13)


### Miscellaneous

* fix the release build race and let make deploy-% pick the image ([#56](https://github.com/rudderlabs/rudder-load/issues/56)) ([e57e760](https://github.com/rudderlabs/rudder-load/commit/e57e760b8f9ca2c2ffb2bd326d4fd71febd01b2a))

## 0.1.0 (2026-10-12)


### Features

* add initial workflow file for test execution on K8s ([#32](https://github.com/rudderlabs/rudder-load/issues/32)) ([aef0b7d](https://github.com/rudderlabs/rudder-load/commit/aef0b7dd0f8e71e662683a224fd3d9872de66206))
* add pulsar producer support to enable message publishing ([#36](https://github.com/rudderlabs/rudder-load/issues/36)) ([edb0692](https://github.com/rudderlabs/rudder-load/commit/edb0692f2bb3dbbee355918a66ebd76b637bc357))
* add ramping load test ([#21](https://github.com/rudderlabs/rudder-load/issues/21)) ([9b9b7c2](https://github.com/rudderlabs/rudder-load/commit/9b9b7c2b09c527e10ca3f101e99fc44129f8e2a8))
* add spike load test ([#23](https://github.com/rudderlabs/rudder-load/issues/23)) ([6674c5b](https://github.com/rudderlabs/rudder-load/commit/6674c5b3e642a0fda3bf27f51fa6e042d7bfdc1c))
* add support for more endpoint types ([#28](https://github.com/rudderlabs/rudder-load/issues/28)) ([1a10a4f](https://github.com/rudderlabs/rudder-load/commit/1a10a4f4e9dc388ab0fc27a43cfa527b7e828825))
* add test  for delivery to two destinations ([#24](https://github.com/rudderlabs/rudder-load/issues/24)) ([c1ca643](https://github.com/rudderlabs/rudder-load/commit/c1ca64395714cff87c12aa5bc0a04b00dbf3fdd6))
* create mechanisms that automatically calculates test resources ([#31](https://github.com/rudderlabs/rudder-load/issues/31)) ([9e0fdd7](https://github.com/rudderlabs/rudder-load/commit/9e0fdd74b57a6424f37673590cf871d3db1ea14b))
* execute tests using Docker compose ([#35](https://github.com/rudderlabs/rudder-load/issues/35)) ([9cd685c](https://github.com/rudderlabs/rudder-load/commit/9cd685cc537a7d1e99cfbab135e2492182ac7682))
* improve validation logic ([#34](https://github.com/rudderlabs/rudder-load/issues/34)) ([868ad1b](https://github.com/rudderlabs/rudder-load/commit/868ad1b4196e4238bba197db530804fea7de26cb))
* initial load runner implementation ([#6](https://github.com/rudderlabs/rudder-load/issues/6)) ([3659482](https://github.com/rudderlabs/rudder-load/commit/3659482472bd749aa93ba4f19ebf64d4075fbdfd))
* keydb support ([#43](https://github.com/rudderlabs/rudder-load/issues/43)) ([41580c8](https://github.com/rudderlabs/rudder-load/commit/41580c87d5cc2f794ea553fee98843b731117f0f))
* load test delivery ([#26](https://github.com/rudderlabs/rudder-load/issues/26)) ([193684c](https://github.com/rudderlabs/rudder-load/commit/193684cd602dc1634a421ed2e98bb46f317035ce))
* max data for producer ([#39](https://github.com/rudderlabs/rudder-load/issues/39)) ([a340e6f](https://github.com/rudderlabs/rudder-load/commit/a340e6f796a8db839bdbcaa20b3fea08389d2bfa))
* ok responder ([#44](https://github.com/rudderlabs/rudder-load/issues/44)) ([597d76c](https://github.com/rudderlabs/rudder-load/commit/597d76ccd4a7a3ec04514db38023939c98dad39f))
* report base metrics on test end ([#33](https://github.com/rudderlabs/rudder-load/issues/33)) ([d6589e0](https://github.com/rudderlabs/rudder-load/commit/d6589e0a5c6e4e61a637b17ab026dfbc5c3d4c0a))
* test functions and main files ([#19](https://github.com/rudderlabs/rudder-load/issues/19)) ([637f4ab](https://github.com/rudderlabs/rudder-load/commit/637f4ab9e09be3d658bcb43772bd05e7dcad2cf3))
* test user-transformer + validations ([#30](https://github.com/rudderlabs/rudder-load/issues/30)) ([e7c697d](https://github.com/rudderlabs/rudder-load/commit/e7c697dab7c6af632d4e304505f182febd7654c5))


### Bug Fixes

* fix load runner ([#38](https://github.com/rudderlabs/rudder-load/issues/38)) ([b65fc8b](https://github.com/rudderlabs/rudder-load/commit/b65fc8b0af6d3517fa828aa2486a9910066d0a4d))
* handle sigterm ([#18](https://github.com/rudderlabs/rudder-load/issues/18)) ([cf6d60b](https://github.com/rudderlabs/rudder-load/commit/cf6d60b11cba7a83ba3fb95ae7a4380554aaf5c9))
* support event types with hyphens ([#12](https://github.com/rudderlabs/rudder-load/issues/12)) ([3c0465e](https://github.com/rudderlabs/rudder-load/commit/3c0465eb8eec5f21d0ed7b69eec30f38eac22d51))
* unit tests ([#17](https://github.com/rudderlabs/rudder-load/issues/17)) ([d1c1157](https://github.com/rudderlabs/rudder-load/commit/d1c11575b2a0efa6847c34c117891617141aa376))
* **vuln:** pin and bump action refs (SEC-171) ([#48](https://github.com/rudderlabs/rudder-load/issues/48)) ([d4582ad](https://github.com/rudderlabs/rudder-load/commit/d4582adc20b8fcade9888f25d4637ed8ed0eb312))


### Miscellaneous

* add tests and auto copy values file ([#15](https://github.com/rudderlabs/rudder-load/issues/15)) ([3ecb594](https://github.com/rudderlabs/rudder-load/commit/3ecb59471f654d0c7d9061e184f33ef779d1be2c))
* add validations for sources and http endpoint ([#25](https://github.com/rudderlabs/rudder-load/issues/25)) ([f9f10f1](https://github.com/rudderlabs/rudder-load/commit/f9f10f1da072a9f938db03497f49e0fd8064d507))
* apply security best practices from step security ([#40](https://github.com/rudderlabs/rudder-load/issues/40)) ([2f60cdf](https://github.com/rudderlabs/rudder-load/commit/2f60cdf2462af1c1402b6c9da45222e22893967d))
* apply security best practices from step security ([#42](https://github.com/rudderlabs/rudder-load/issues/42)) ([c321423](https://github.com/rudderlabs/rudder-load/commit/c3214234c97aab16efd53f6d5f1d3a291c5b5405))
* bump Go 1.27.2 and dependencies, harden CI, faster gzip, smaller-track event type ([#51](https://github.com/rudderlabs/rudder-load/issues/51)) ([320c496](https://github.com/rudderlabs/rudder-load/commit/320c49648b9bd07b05dd16bd2923855b85187dfe))
* custom double payload size ([#20](https://github.com/rudderlabs/rudder-load/issues/20)) ([fc6b185](https://github.com/rudderlabs/rudder-load/commit/fc6b185d8ba4070a6d7e2b326098085704334668))
* custom events payload ([#10](https://github.com/rudderlabs/rudder-load/issues/10)) ([ffa70ae](https://github.com/rudderlabs/rudder-load/commit/ffa70aee580db8e359b1502d7b70127d57192499))
* **deps:** bump gitleaks/gitleaks-action ([#53](https://github.com/rudderlabs/rudder-load/issues/53)) ([e0279ca](https://github.com/rudderlabs/rudder-load/commit/e0279ca65cb17ce2de620b27df56fb64abbc4db8))
* enable gosec security linter ([#45](https://github.com/rudderlabs/rudder-load/issues/45)) ([b5c2f73](https://github.com/rudderlabs/rudder-load/commit/b5c2f731c98971b0f52fd580ce567c8355a283b1))
* fix tests and run tests as part of pr ([#13](https://github.com/rudderlabs/rudder-load/issues/13)) ([4e35aae](https://github.com/rudderlabs/rudder-load/commit/4e35aae822fc6d6690dafd368f7f98234061e94e))
* gcra with burst ([#3](https://github.com/rudderlabs/rudder-load/issues/3)) ([04b267f](https://github.com/rudderlabs/rudder-load/commit/04b267fa017aa3e1c17f0dbec9164226e7a51fd1))
* migrate self-hosted arm64 build to gha-runner-scale-set ([#52](https://github.com/rudderlabs/rudder-load/issues/52)) ([4260423](https://github.com/rudderlabs/rudder-load/commit/42604238b74a70f59702479142767c7ebfe8a52d))
* monitoring init ([#29](https://github.com/rudderlabs/rudder-load/issues/29)) ([c51ddb2](https://github.com/rudderlabs/rudder-load/commit/c51ddb2577af92267399b612a36574fcf0ee7433))
* publish images to ECR, pull test containers through the ECR cache, automate releases ([#54](https://github.com/rudderlabs/rudder-load/issues/54)) ([886eeed](https://github.com/rudderlabs/rudder-load/commit/886eeed9db9761821c90a0c89078f36a37d5188f))
* read data from .env files ([#14](https://github.com/rudderlabs/rudder-load/issues/14)) ([7c187d7](https://github.com/rudderlabs/rudder-load/commit/7c187d73950df4126f2733355689418fb7bc71c7))
* remove default sources and writekeys ([#16](https://github.com/rudderlabs/rudder-load/issues/16)) ([2f9a2b6](https://github.com/rudderlabs/rudder-load/commit/2f9a2b686db246f9e8544c9a9c2fa236af18ba9c))
* support hot sources list ([#8](https://github.com/rudderlabs/rudder-load/issues/8)) ([3a0befe](https://github.com/rudderlabs/rudder-load/commit/3a0befe6d72ea60f6d08e0ea5da0890b2fe57208))
* support passing env variables from load runner ([#11](https://github.com/rudderlabs/rudder-load/issues/11)) ([bb92bc8](https://github.com/rudderlabs/rudder-load/commit/bb92bc824d39854ea6768ab9f214a26fe2eb8a62))
* support spike test scenario ([#9](https://github.com/rudderlabs/rudder-load/issues/9)) ([a8d2a03](https://github.com/rudderlabs/rudder-load/commit/a8d2a03d40581d1b01cb693853406143c80f9479))
* update README ([#27](https://github.com/rudderlabs/rudder-load/issues/27)) ([a9b6ca6](https://github.com/rudderlabs/rudder-load/commit/a9b6ca6a4a2f76e6cb58f8af69c243f1cd29a2ad))
* update README format ([#5](https://github.com/rudderlabs/rudder-load/issues/5)) ([97ed7ce](https://github.com/rudderlabs/rudder-load/commit/97ed7ce91c0e862ff3fc482aef2b5611bb770d8a))
* updating README ([7b4bbe9](https://github.com/rudderlabs/rudder-load/commit/7b4bbe9b34f058ae9ffeec971d63a282b04f3ee7))
* **vuln:** scope workflow permissions to least privilege (SEC-167) ([#50](https://github.com/rudderlabs/rudder-load/issues/50)) ([790a329](https://github.com/rudderlabs/rudder-load/commit/790a329698ac9c60c1964c1e697dd8d4f1cc2bbc))
* **vuln:** zizmor --fix=all findings (SEC-199) ([#49](https://github.com/rudderlabs/rudder-load/issues/49)) ([1266578](https://github.com/rudderlabs/rudder-load/commit/126657861b42de71ee831e480ad8edee63fae329))
