{
  description = "Flake for imgcull";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    nix2container.url = "github:nlewo/nix2container";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, nix2container, flake-utils}:
    flake-utils.lib.eachDefaultSystem 
    (system:
      let
        pkgs = import nixpkgs { inherit system; };
        n2c = nix2container.packages.${system};
        lib = pkgs.lib;

        version = "0.0.1";
        useUpx = false;

        buildGo126Module = pkgs.buildGoModule.override { 
          go = pkgs.go_1_26; 
        };

        # build Go binary
        imgcull = buildGo126Module {
          pname = "imgcull";
          inherit version;
          src = ./.;
          # If the binary is identical, downstream steps won't re-run.
          __contentAddressed = true; 

          vendorHash = "sha256-TZidEyy/dCaYw6HYEYp0me/bKfbaTp9LCGRN04O7jhM=";
          
          env = {
            CGO_ENABLED = 0;
          };

          ldflags = [
            "-s" 
            "-w"
            "-X main.version=${self.shortRev or self.dirtyRev}"
            "-X main.commit=${self.rev or "unknown"}"
            "-X main.date=1970-01-01"
            "-extldflags=-static"
          ];

          # optional UPX compression
          nativeBuildInputs = lib.optional useUpx pkgs.upx;
          postInstall = lib.optionalString useUpx ''
            upx --best --lzma $out/imgcull
          '';

          meta = {
            description = "imgcull, an LRU based OCI image pruning tool";
            homepage = "https://github.com/Pinguladora/imgcull";
            license = lib.licenses.asl20;
            maintainers = with lib.maintainers; [ pinguladora ];
          };
        };

        # builds image from scratch
        containerImage = n2c.nix2container.buildImage {
          name = "ghcr.io/pinguladora/imgcull";
          tag = version;
          copyToRoot = pkgs.buildEnv {
            name = "image-root";
            paths = [ imgcull pkgs.cacert pkgs.tzdata ];
            pathsToLink = [ "/bin" "/etc" "/usr" ];
          };
          config = {
            Entrypoint = [ "/bin/imgcull" ];
            User = "65532:65532";
            Env = [ "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
          };
        };

      in
      {
        packages = {
          default = imgcull;
          container = containerImage;
        };

        # local development environment
        devShells.default = pkgs.mkShell {
          buildInputs = builtins.attrValues {
            inherit (pkgs) go_1_26 cosign upx;
          };
        };
      });
}