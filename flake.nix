{
  description = "A flake for python development environment, using nix for python package management.";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs, ... }:
    let
      # to work with older version of flakes
      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";

      # Generate a user-friendly version number.
      version = builtins.substring 0 8 lastModifiedDate;

      # System types to support.
      supportedSystems = [
        "x86_64-linux"
        "x86_64-darwin"
        "aarch64-linux"
        "aarch64-darwin"
      ];

      # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Nixpkgs instantiated for supported system types.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      # Add dependencies that are only needed for development
      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgsFor.${system};
          # Choose desired Python version
          python39 = pkgs.python39;
          python310 = pkgs.python310;
          python311 = pkgs.python311;
          python312 = pkgs.python3;
          python = pkgs.python313.override {
            packageOverrides =
              py-final: py-prev: {
                pgvector =
                  if pkgs.stdenv.isDarwin then
                    py-prev.pgvector.overridePythonAttrs (_: {
                      # pgvector's nixpkgs test suite pulls postgresql-test-hook,
                      # which is marked unsupported on Darwin.
                      doCheck = false;
                      nativeCheckInputs = [ ];
                      checkInputs = [ ];
                    })
                  else
                    py-prev.pgvector;
              };
          };
        in
        {
          default = pkgs.mkShell {
            shellHook = ''
              export PYTHONPATH="$PWD:$PWD/processor/src:$PWD/agent/src:$PWD/sqlc"
              export DATABASE_URL="postgres://postgres:postgres@localhost:5433/agentdb"
              export AGENT_URL="http://127.0.0.1:8090"
              export LANGSMITH_TRACING=true
              export LANGSMITH_ENDPOINT=https://api.smith.langchain.com
              '';
            buildInputs = with pkgs; [
              ruff
              (python.withPackages (
                python-pkgs: with python-pkgs; [
                  pip
                  # Used for DAP protocol debugging
                  debugpy

                  # Desired Python packages
                  numpy
                  psycopg
                  psycopg-pool
                  flask
                  schedule
                  pydantic
                  pydantic-settings
                  httpx
                  unstructured
                  langchain
                  langchain-core
                  langchain-openai
                  langchain-text-splitters
                  langgraph
                  python-magic
                  pgvector
                  sqlalchemy
                ]
              ))
            ];
          };
        }
      );
    };
}
