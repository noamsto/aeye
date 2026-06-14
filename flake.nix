{
  description = "agent-carousel — a tmux/kitty image carousel for coding agents.";

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
      }: {
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
          packages =
            config.pre-commit.settings.enabledPackages
            ++ [pkgs.go pkgs.gopls pkgs.gotools pkgs.golangci-lint pkgs.chafa pkgs.bats pkgs.goreleaser pkgs.gh pkgs.d2 pkgs.resvg];
        };

        packages = {
          default = pkgs.buildGoModule {
            pname = "agent-carousel";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-G0x4z/zFDK578yJBLUD555wBQ9quUyLeO5bKEZewCC4=";
            doCheck = true;
            meta = {
              description = "tmux/kitty image carousel for coding agents";
              mainProgram = "agent-carousel";
              license = lib.licenses.mit;
            };
          };

          # The dual-mode toggle. runtimeInputs puts `agent-carousel` on PATH,
          # which the script invokes by default (AGENT_CAROUSEL_BIN override).
          toggle = pkgs.writeShellApplication {
            name = "tmux-claude-images";
            runtimeInputs = [self'.packages.default];
            text = builtins.readFile ./scripts/tmux-claude-images.sh;
          };

          # The diagram-render hook with the d2 -> svg -> resvg -> png toolchain
          # baked onto PATH (plus jq/coreutils, and the toggle for --ensure-open),
          # so nix consumers get diagrams without d2/resvg in their environment.
          # Non-nix users run the plugin's scripts/diagrams.sh with their own
          # d2/resvg on PATH (or via AGENT_CAROUSEL_D2 / AGENT_CAROUSEL_RESVG).
          diagrams = pkgs.writeShellApplication {
            name = "agent-carousel-diagrams";
            runtimeInputs = [self'.packages.toggle pkgs.d2 pkgs.resvg pkgs.jq pkgs.coreutils];
            text = builtins.readFile ./adapters/claude-code/plugin/scripts/diagrams.sh;
          };
        };

        apps.default = {
          type = "app";
          program = "${self'.packages.default}/bin/agent-carousel";
        };
      };
    };
}
