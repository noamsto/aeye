# Changelog

## [0.9.1](https://github.com/noamsto/aeye/compare/v0.9.0...v0.9.1) (2026-07-12)


### Bug Fixes

* **carousel:** macOS hook portability + bleed/empty-box hardening ([#121](https://github.com/noamsto/aeye/issues/121)) ([86b110c](https://github.com/noamsto/aeye/commit/86b110cec95d68408a8c5690ced6e266317c3b3a))
* **claude-code:** don't leak GC sweep exit status from session-reset hook ([#119](https://github.com/noamsto/aeye/issues/119)) ([221209e](https://github.com/noamsto/aeye/commit/221209e3e4b6443f4e9588c63b8afd5d38d99b5e))

## [0.9.0](https://github.com/noamsto/aeye/compare/v0.8.0...v0.9.0) (2026-07-07)


### Features

* carousel follows tmux focus in kitty-pane mode (--reconcile) ([#101](https://github.com/noamsto/aeye/issues/101)) ([f982a0b](https://github.com/noamsto/aeye/commit/f982a0be5da1a34882989a89f6a5dadf1c7267cb))


### Bug Fixes

* **adapter:** engage carousel reconcile via KITTY_LISTEN_ON fallback ([#102](https://github.com/noamsto/aeye/issues/102)) ([9099ea6](https://github.com/noamsto/aeye/commit/9099ea69624502a073ea97fb0a8bb3a893546d0c))
* **adapter:** rebuild resumed manifest from transcript to stop session ghosting ([#108](https://github.com/noamsto/aeye/issues/108)) ([#109](https://github.com/noamsto/aeye/issues/109)) ([4cdd43d](https://github.com/noamsto/aeye/commit/4cdd43d29b2be31cfe3ededc3534cd20fed373d4))
* **adapter:** stop carousel bleed on reused tmux pane ids + GC stale manifests ([#95](https://github.com/noamsto/aeye/issues/95)) ([4a7ff8f](https://github.com/noamsto/aeye/commit/4a7ff8f75adab022abd947e49399d5baedb88d5d)), closes [#94](https://github.com/noamsto/aeye/issues/94)
* **adapter:** tolerate a stale KITTY_WINDOW_ID in kitty vsplit placement ([#105](https://github.com/noamsto/aeye/issues/105)) ([57418d1](https://github.com/noamsto/aeye/commit/57418d18c38eb15b5ed74b21392e8516a44b9882))
* **carousel:** launch hidden off-screen + seamless ctrl+hjkl ([#103](https://github.com/noamsto/aeye/issues/103)) ([#104](https://github.com/noamsto/aeye/issues/104)) ([1e16ab5](https://github.com/noamsto/aeye/commit/1e16ab5779e71c17cb91bebf8795357655d5ac3e))
* **diagrams:** suppress blank |md renders and prune superseded ones ([#93](https://github.com/noamsto/aeye/issues/93)) ([013d842](https://github.com/noamsto/aeye/commit/013d842ba42074e1d6310e240428589f8141782c)), closes [#92](https://github.com/noamsto/aeye/issues/92)
* **macos:** live d2 sharp render honors AEYE_D2_FONT_DIR ([#113](https://github.com/noamsto/aeye/issues/113)) ([f3b3298](https://github.com/noamsto/aeye/commit/f3b329848af53f2e273b996db6bedd26ff254412)), closes [#111](https://github.com/noamsto/aeye/issues/111)
* **macos:** open/open-folder/copy/drag use native macOS tools ([#112](https://github.com/noamsto/aeye/issues/112)) ([3726801](https://github.com/noamsto/aeye/commit/3726801a5b1921229da605445f7efb093298bb7c))

## [0.8.0](https://github.com/noamsto/aeye/compare/v0.7.1...v0.8.0) (2026-06-22)


### Features

* configurable kitty-pane launch mode (AEYE_HOST) ([#90](https://github.com/noamsto/aeye/issues/90)) ([#91](https://github.com/noamsto/aeye/issues/91)) ([999178e](https://github.com/noamsto/aeye/commit/999178e1275a1ade92ac97eada5bdebe6296ffec))
* drag the selected image out — native kitty OSC 72 + helper/clipboard fallback ([#86](https://github.com/noamsto/aeye/issues/86)) ([#87](https://github.com/noamsto/aeye/issues/87)) ([2740711](https://github.com/noamsto/aeye/commit/27407113810db5fb6ac1a8acda47471c75bb9eda))
* OSC 1337 rendering + iTerm2 launch mode ([#60](https://github.com/noamsto/aeye/issues/60)) ([#88](https://github.com/noamsto/aeye/issues/88)) ([729552d](https://github.com/noamsto/aeye/commit/729552db3ca8b4402c1b52c5ce88050c7d56d96c))

## [0.7.1](https://github.com/noamsto/aeye/compare/v0.7.0...v0.7.1) (2026-06-18)


### Bug Fixes

* derive version from the release-please manifest ([#84](https://github.com/noamsto/aeye/issues/84)) ([46ae772](https://github.com/noamsto/aeye/commit/46ae772f1779b85084173d5238e3d4b5f8d84077))

## [0.7.0](https://github.com/noamsto/aeye/compare/v0.6.0...v0.7.0) (2026-06-18)


### Features

* render diagrams in both themes, carousel picks per live theme ([#83](https://github.com/noamsto/aeye/issues/83)) ([c1016e7](https://github.com/noamsto/aeye/commit/c1016e72d10050897f4a630ba68908d040659b68))
* semantic role palette injected per theme ([#80](https://github.com/noamsto/aeye/issues/80)) ([d78a3fb](https://github.com/noamsto/aeye/commit/d78a3fb8c7e0c17469b3580d7f3f292b9553639c))

## [0.6.0](https://github.com/noamsto/aeye/compare/v0.5.0...v0.6.0) (2026-06-18)


### Features

* copy the current image to the clipboard with y ([#75](https://github.com/noamsto/aeye/issues/75)) ([5137c70](https://github.com/noamsto/aeye/commit/5137c70dae0a8bfd4b247a29684c77a3e49bf122))
* default diagram theme from detected terminal mode ([#77](https://github.com/noamsto/aeye/issues/77)) ([2f0457b](https://github.com/noamsto/aeye/commit/2f0457b5a9d527a549944e92d922a799bc4be665))
* warn at SessionStart when the diagram render deps are missing ([#72](https://github.com/noamsto/aeye/issues/72)) ([4822769](https://github.com/noamsto/aeye/commit/4822769e0c87761e217921b02b206f90768f3e08)), closes [#71](https://github.com/noamsto/aeye/issues/71)


### Bug Fixes

* contrast diagram labels at the graph level (shapes + edge labels) ([#79](https://github.com/noamsto/aeye/issues/79)) ([ecc5842](https://github.com/noamsto/aeye/commit/ecc5842391fdba35b4abfe2dd95c85f191c68274))
* keep theme text on themed child labels in contrast pass ([#74](https://github.com/noamsto/aeye/issues/74)) ([eceb1af](https://github.com/noamsto/aeye/commit/eceb1af2d4d8cb52f93c706a26a55f75e0f495bd))

## [0.5.0](https://github.com/noamsto/aeye/compare/v0.4.0...v0.5.0) (2026-06-17)


### Features

* **carousel:** caption diagrams by their .d2 name, not the hash ([#64](https://github.com/noamsto/aeye/issues/64)) ([d874bad](https://github.com/noamsto/aeye/commit/d874badd3150c0ad20e096ec8cc5f8fbee37a5fe))
* crisp sixel rendering on non-kitty terminals ([#60](https://github.com/noamsto/aeye/issues/60)) ([#66](https://github.com/noamsto/aeye/issues/66)) ([8fc9d61](https://github.com/noamsto/aeye/commit/8fc9d61a338e6baa78ecc9cbc82e9a02dd71466a))
* embed d2 as a library + kong CLI (drop the external d2 dep) ([#67](https://github.com/noamsto/aeye/issues/67)) ([fce2e10](https://github.com/noamsto/aeye/commit/fce2e10b6c61b17579267dddea50c04a34958b53))
* open the carousel under ghostty and wezterm (no tmux) ([#59](https://github.com/noamsto/aeye/issues/59)) ([05c5cb1](https://github.com/noamsto/aeye/commit/05c5cb1ac1878f2045cf4f1efb2bbc3d89dbcc22))


### Bug Fixes

* **diagrams:** make svg-contrast actually run; tighten the diagram skill ([#63](https://github.com/noamsto/aeye/issues/63)) ([df2dc9c](https://github.com/noamsto/aeye/commit/df2dc9c6f5f12b200dc6dcec3cd46fee88c784fd))
* **hooks:** make session-backfill.sh executable ([#56](https://github.com/noamsto/aeye/issues/56)) ([437594f](https://github.com/noamsto/aeye/commit/437594f2fc0828bc9dc38db4f9c92504e6179bb2))
* **viewer:** re-store images on settle so the carousel paints on launch ([#61](https://github.com/noamsto/aeye/issues/61)) ([#62](https://github.com/noamsto/aeye/issues/62)) ([c79349e](https://github.com/noamsto/aeye/commit/c79349ea63679b25734ba7315b49dcb03dc9d52a))

## [0.4.0](https://github.com/noamsto/aeye/compare/v0.3.0...v0.4.0) (2026-06-16)


### Features

* **hooks:** backfill image/diagram manifest on resume ([#44](https://github.com/noamsto/aeye/issues/44)) ([#52](https://github.com/noamsto/aeye/issues/52)) ([a4c1a6c](https://github.com/noamsto/aeye/commit/a4c1a6c7d51e50f48b68f3244e8859701d7dca40))

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
