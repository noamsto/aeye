{
  description = "aeye — a tmux/kitty image carousel for coding agents.";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    git-hooks-nix.url = "github:cachix/git-hooks.nix";
    git-hooks-nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      imports = [inputs.git-hooks-nix.flakeModule];

      systems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"];

      perSystem = {
        config,
        pkgs,
        lib,
        self',
        ...
      }: let
        # Short commit the binary was built from; "unknown" when the tree is dirty
        # in a non-git build. Stamped into the binary so `aeye --version` and the
        # viewer footer reveal which build is running (see the version skew this
        # made hard to spot).
        rev = inputs.self.shortRev or inputs.self.dirtyShortRev or "unknown";
        # Single source of truth, bumped by release-please. The Go binary embeds
        # the same file (see main.go), so the derivation and the runtime agree.
        releaseVersion = (builtins.fromJSON (builtins.readFile ./.release-please-manifest.json)).".";
        # d2 text rendering: fixFonts flattens d2's @font-face to this family
        # name, which resvg then resolves from AEYE_D2_FONT_DIR. Shared by the
        # devShell and the packaged binary's wrapper so a plain install renders
        # diagram text without a matching system font (macOS ships neither).
        # truetype/ = static Regular/Bold/Italic faces only; the parent dir also
        # ships variable/ (same family name), which makes resvg's bold/italic
        # face selection ambiguous. Keep it narrow.
        d2FontName = "Source Sans 3";
        d2FontDir = "${pkgs.source-sans}/share/fonts/truetype";
        # Runtime tools the binary execs: resvg rasterizes d2 SVGs (render-diagram
        # hook + the live sharp re-render); chafa paints the raster backend on
        # non-kitty terminals.
        aeyeRuntimeDeps = [pkgs.resvg pkgs.chafa];
      in {
        pre-commit.settings.hooks = {
          gofmt.enable = true;
          # govet and golangci-lint require network access (to resolve Go module
          # deps) which is unavailable in the Nix build sandbox. Go correctness
          # is still enforced by the buildGoModule doCheck = true check.
          govet.enable = false;
          golangci-lint.enable = false;
          shellcheck.enable = true;
          shfmt.enable = true;
          typos.enable = true;
          check-merge-conflicts.enable = true;
          trim-trailing-whitespace.enable = true;
        };

        devShells.default = pkgs.mkShell {
          inherit (config.pre-commit) shellHook;
          AEYE_D2_FONT = d2FontName;
          AEYE_D2_FONT_DIR = d2FontDir;
          packages =
            config.pre-commit.settings.enabledPackages
            ++ [pkgs.go pkgs.gopls pkgs.gotools pkgs.golangci-lint pkgs.chafa pkgs.bats pkgs.goreleaser pkgs.gh pkgs.d2 pkgs.resvg pkgs.source-sans pkgs.source-code-pro pkgs.just];
        };

        # `nix develop .#verify` — the real terminal hosts for manually checking the
        # ghostty/wezterm launch paths, which bats can only stub (they need a live
        # GUI/D-Bus/mux session). Kept out of the default shell so contributors don't
        # build these heavy emulators on every `nix develop`. ghostty is Linux-only
        # in nixpkgs (macOS uses the official app); wezterm is cross-platform.
        devShells.verify = pkgs.mkShell {
          packages =
            [self'.packages.default self'.packages.toggle pkgs.chafa pkgs.wezterm]
            ++ lib.optionals pkgs.stdenv.isLinux [pkgs.ghostty];
        };

        packages = {
          default = pkgs.buildGoModule {
            pname = "aeye";
            version = releaseVersion;
            src = ./.;
            vendorHash = "sha256-1eHo6vUxQWLkzQ8d1msYEWXAD1RtUBykuLKFJ1kthFk=";
            doCheck = true;
            ldflags = ["-X main.buildSuffix=${rev}"];
            nativeBuildInputs = [pkgs.makeWrapper];
            # Self-contained: pin the diagram font and carry resvg/chafa so the
            # binary renders d2 text and rasters without a system font or PATH
            # setup. --set-default leaves an explicit user env override the winner.
            postInstall = ''
              wrapProgram $out/bin/aeye \
                --set-default AEYE_D2_FONT ${lib.escapeShellArg d2FontName} \
                --set-default AEYE_D2_FONT_DIR ${d2FontDir} \
                --prefix PATH : ${lib.makeBinPath aeyeRuntimeDeps}
            '';
            meta = {
              description = "tmux/kitty image carousel for coding agents";
              mainProgram = "aeye";
              license = lib.licenses.mit;
            };
          };

          # The dual-mode toggle. runtimeInputs puts `aeye` on PATH,
          # which the script invokes by default (AEYE_BIN override).
          toggle = pkgs.writeShellApplication {
            name = "tmux-claude-images";
            runtimeInputs = [self'.packages.default];
            text = builtins.readFile ./scripts/tmux-claude-images.sh;
          };

          # The diagram-render hook. aeye now embeds d2 (compile + render happen
          # in the binary via `aeye render-diagram`, which also contrasts labels
          # against their fills during compile), so the wrapper needs aeye itself
          # (toggle only carries it internally and does not re-export it), resvg for
          # rasterizing, jq/coreutils, and the toggle for --ensure-open. Non-nix
          # users run the plugin's scripts/diagrams.sh with aeye + resvg on PATH
          # (or via AEYE_BIN / AEYE_RESVG).
          diagrams = pkgs.writeShellApplication {
            name = "aeye-diagrams";
            runtimeInputs = [self'.packages.default self'.packages.toggle pkgs.resvg pkgs.jq pkgs.coreutils];
            text = builtins.readFile ./adapters/claude-code/plugin/scripts/diagrams.sh;
          };
        };

        apps.default = {
          type = "app";
          program = "${self'.packages.default}/bin/aeye";
        };
      };
    };
}
