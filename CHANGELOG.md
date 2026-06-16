# Changelog

## [0.3.0](https://github.com/noamsto/aeye/compare/v0.2.0...v0.3.0) (2026-06-16)


### Features

* mouse support for the carousel viewer ([#48](https://github.com/noamsto/aeye/issues/48)) ([722a9b9](https://github.com/noamsto/aeye/commit/722a9b910ab785aa44bc2eeb60840276008f2101))


### Bug Fixes

* **adapter:** self-heal manifest on tmux pane-id reuse ([#37](https://github.com/noamsto/aeye/issues/37)) ([945c334](https://github.com/noamsto/aeye/commit/945c33417a7280e3ed5a25a880c9d1583bd88f82)), closes [#31](https://github.com/noamsto/aeye/issues/31)
* **diagrams:** warn on |md blocks that render blank in the carousel ([#46](https://github.com/noamsto/aeye/issues/46)) ([a21b87a](https://github.com/noamsto/aeye/commit/a21b87a1c1be5acab45050719dc45465186a9a7c))
* **regions:** stop attributing trailing &lt;mask&gt;/&lt;defs&gt; geometry to last object ([#50](https://github.com/noamsto/aeye/issues/50)) ([9c3c91c](https://github.com/noamsto/aeye/commit/9c3c91cbd691b2b647fc93c9ef5b3faec9d88803)), closes [#49](https://github.com/noamsto/aeye/issues/49)

## [0.2.0](https://github.com/noamsto/aeye/compare/v0.1.0...v0.2.0) (2026-06-15)


### Features

* **adapter:** reset image manifest on fresh SessionStart ([#25](https://github.com/noamsto/aeye/issues/25)) ([6654a57](https://github.com/noamsto/aeye/commit/6654a57154609099c43955d327e78f084c6cc20c))
* crisp vector zoom for D2 diagrams ([#10](https://github.com/noamsto/aeye/issues/10)) ([8b4fc59](https://github.com/noamsto/aeye/commit/8b4fc596a3f118473c84126ab5607690e39ca17c))
* **diagrams:** add on-demand d2 cheatsheet skill ([25b8dc0](https://github.com/noamsto/aeye/commit/25b8dc01eb95dfca8e589a43936b01725b09fe39))
* **diagrams:** auto-contrast label text against node fill ([#23](https://github.com/noamsto/aeye/issues/23)) ([a3a1293](https://github.com/noamsto/aeye/commit/a3a129364ce1a33c26a8c0df487fcb8175d0b962))
* **diagrams:** dedup, malformed-d2 logging, no-op when renderers absent ([d2255c1](https://github.com/noamsto/aeye/commit/d2255c1444d40228612a458c46b8c417901979eb))
* **diagrams:** host-gated SessionStart diagram guidance ([19e6e18](https://github.com/noamsto/aeye/commit/19e6e186b953acdc247fc8e110782c616b2e0cbd))
* **diagrams:** legible D2 text rendering — Phase 0 ([#8](https://github.com/noamsto/aeye/issues/8)) ([267cbff](https://github.com/noamsto/aeye/commit/267cbff529c8b287d9e1278e2ada21a1a21a31fc))
* **diagrams:** once-per-session carousel auto-open ([3bff2a0](https://github.com/noamsto/aeye/commit/3bff2a077b7f1a9a890562e8a6b4cbcbf82e365e))
* **diagrams:** render .d2 writes to png in the manifest ([73bb37d](https://github.com/noamsto/aeye/commit/73bb37dbbd58d7554fede185a215b75549f12092))
* **diagrams:** rich, beautiful, render-tested D2 authoring skill ([1977c53](https://github.com/noamsto/aeye/commit/1977c53c830ebb779ee900b809f1b0767c80506e))
* focus carousel pane on manual toggle ([#27](https://github.com/noamsto/aeye/issues/27)) ([c5c8c25](https://github.com/noamsto/aeye/commit/c5c8c2578bd13bc849522fe6c638c78f20d58aec))
* **gallery:** context-aware key legend ([#12](https://github.com/noamsto/aeye/issues/12)) ([2bed03f](https://github.com/noamsto/aeye/commit/2bed03fa21b9430426c2702eb5f7a74684bda7af))
* **gallery:** open at newest image, pin until first navigation ([d8be178](https://github.com/noamsto/aeye/commit/d8be1787200788c52032ce82417f0002249c0747))
* **gallery:** proactive carousel open + themed empty state ([#19](https://github.com/noamsto/aeye/issues/19)) ([da8f66d](https://github.com/noamsto/aeye/commit/da8f66d25f64ad23fc3c5588279224f4df83cfa0))
* **nix:** package the diagram hook with d2 + resvg bundled ([efd3005](https://github.com/noamsto/aeye/commit/efd30054776183dcb456ce4516e63f33b3881be6))
* **plugin:** register diagrams PostToolUse + SessionStart hooks ([2efcafe](https://github.com/noamsto/aeye/commit/2efcafe2ec503b01b77cc0c74eef55122b115fc6))
* **regions:** shift+tab from first group backs out to whole diagram ([0763660](https://github.com/noamsto/aeye/commit/07636601a8da3e634fade24680fc6a48a132b0b8))
* stamp git rev into build for version visibility ([e471297](https://github.com/noamsto/aeye/commit/e4712974dc9380b85f3d7f23f79a72ffc76d6503))
* step-group navigation for D2 diagrams ([#11](https://github.com/noamsto/aeye/issues/11)) ([62bd5f7](https://github.com/noamsto/aeye/commit/62bd5f7785fb8aae027ec2e9c5e36f88bf3c5d90))
* **toggle:** add --ensure-open (open-if-closed) mode ([e590726](https://github.com/noamsto/aeye/commit/e590726bc4b191634fcf861a3659f866445a362a))
* zoom + pan in the carousel (kitty) ([#9](https://github.com/noamsto/aeye/issues/9)) ([c9f6aa5](https://github.com/noamsto/aeye/commit/c9f6aa5b67e888edeca4116f028848b237b2ac2d))


### Bug Fixes

* **diagrams:** correct example labels, sketch-via-hook framing, ELK claim ([b93d9be](https://github.com/noamsto/aeye/commit/b93d9be551ecb10f8156d19409729b96d04272d8))
* **diagrams:** drill-down into styled nodes + flat-cost zoom re-render ([#26](https://github.com/noamsto/aeye/issues/26)) ([7a96b34](https://github.com/noamsto/aeye/commit/7a96b346e8846b1b235c867e5a020640661c3ade))
* **diagrams:** make renderer absence test deterministic ([5cd23e6](https://github.com/noamsto/aeye/commit/5cd23e60ed4c67f213055c19f63b429e4f578197))
* **gallery:** drop manifest entries that don't decode ([2687a38](https://github.com/noamsto/aeye/commit/2687a380dfa9e703f046743ac6604541107bb2d5))
* **gallery:** repaint after first transmit so images aren't blank until move ([#18](https://github.com/noamsto/aeye/issues/18)) ([49acf38](https://github.com/noamsto/aeye/commit/49acf38fee8e7926619a84e01257cd8974c8c36a))
* **plugin:** drop redundant hooks key from manifest ([#4](https://github.com/noamsto/aeye/issues/4)) ([7cc0464](https://github.com/noamsto/aeye/commit/7cc0464738c1a5dbff22cddfd9ef020d37760186))
* **readme:** commit demo video as a normal blob so it embeds ([#34](https://github.com/noamsto/aeye/issues/34)) ([7c7f7b7](https://github.com/noamsto/aeye/commit/7c7f7b7a2265507baf56fc9053703b64ca4e3f60))
* **toggle:** guard empty kitty placement array under set -u ([a3eb835](https://github.com/noamsto/aeye/commit/a3eb8355889ffcc6cbc00d62823d96e2bbf8e004))
* **toggle:** open carousel in Claude's window, not the active one ([ca869ae](https://github.com/noamsto/aeye/commit/ca869ae2fbc24108d945abf1437640046be633b2))
* **toggle:** open kitty carousel in Claude's window, not the active one ([28b60cf](https://github.com/noamsto/aeye/commit/28b60cf91dde17e55477abde2fd0711cb613a164))
* **viewer:** dedup manifest on read; make capture append-only ([#32](https://github.com/noamsto/aeye/issues/32)) ([0f645f0](https://github.com/noamsto/aeye/commit/0f645f0c5ce4ba9041c6a00a8dcef78e9d6da835))
* **zoom:** keep tall region tight instead of letterboxing off-center ([#17](https://github.com/noamsto/aeye/issues/17)) ([8c2f070](https://github.com/noamsto/aeye/commit/8c2f070213afc326039b0725008d5e9155791acd))
* **zoom:** magnify framed region in place instead of resetting to full diagram ([#21](https://github.com/noamsto/aeye/issues/21)) ([52f4617](https://github.com/noamsto/aeye/commit/52f4617fa31643004a9c9a3bcea929ea7f53d7f0))
