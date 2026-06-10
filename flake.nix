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
          govet.enable = true;
          golangci-lint.enable = true;
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
            ++ [pkgs.go pkgs.gopls pkgs.gotools pkgs.golangci-lint pkgs.chafa pkgs.bats];
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

          # The dual-mode toggle with the binary path baked in (no @placeholder@).
          toggle = pkgs.writeShellApplication {
            name = "tmux-claude-images";
            runtimeInputs = [self'.packages.default];
            text = builtins.replaceStrings ["@picker_generate@"] ["agent-carousel"]
              (builtins.readFile ./scripts/tmux-claude-images.sh);
          };
        };

        apps.default = {
          type = "app";
          program = "${self'.packages.default}/bin/agent-carousel";
        };
      };
    };
}
