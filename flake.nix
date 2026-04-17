{
  description = "gguf-manager — local web UI for managing GGUF models with llama-server";

  inputs = {
    nixpkgs.url     = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages.default = pkgs.buildGoModule {
          pname   = "gguf-manager";
          version = "0.1.0";
          src     = ./.;

          vendorHash = "";

          meta = {
            description = "Local web UI for managing GGUF models with llama-server";
            license     = pkgs.lib.licenses.mit;
            maintainers = [];
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
          ];
        };
      })
    // {
      nixosModules.default = import ./module.nix;
    };
}
