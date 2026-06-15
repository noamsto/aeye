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
          AEYE_D2_FONT = "Source Sans 3";
          # truetype/ = static Regular/Bold/Italic faces only; the parent dir also
          # ships variable/ (same family name), which makes resvg's bold/italic
          # face selection ambiguous. Keep it narrow.
          AEYE_D2_FONT_DIR = "${pkgs.source-sans}/share/fonts/truetype";
          packages =
            config.pre-commit.settings.enabledPackages
            ++ [pkgs.go pkgs.gopls pkgs.gotools pkgs.golangci-lint pkgs.chafa pkgs.bats pkgs.goreleaser pkgs.gh pkgs.d2 pkgs.resvg pkgs.source-sans pkgs.source-code-pro pkgs.just];
        };

        packages = {
          default = pkgs.buildGoModule {
            pname = "aeye";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-G0x4z/zFDK578yJBLUD555wBQ9quUyLeO5bKEZewCC4=";
            doCheck = true;
            ldflags = ["-X main.version=0.1.0-${rev}"];
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

          # The diagram-render hook with the d2 -> svg -> resvg -> png toolchain
          # baked onto PATH (plus jq/coreutils, and the toggle for --ensure-open),
          # so nix consumers get diagrams without d2/resvg in their environment.
          # Non-nix users run the plugin's scripts/diagrams.sh with their own
          # d2/resvg on PATH (or via AEYE_D2 / AEYE_RESVG).
          diagrams = pkgs.writeShellApplication {
            name = "aeye-diagrams";
            runtimeInputs = [self'.packages.toggle pkgs.d2 pkgs.resvg pkgs.jq pkgs.coreutils];
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
