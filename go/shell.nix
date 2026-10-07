# Explicit opt-in: nix-shell go/shell.nix. Default Haskell shells are unchanged.
{ system ? builtins.currentSystem }:
let
  lock = builtins.fromJSON (builtins.readFile ../flake.lock);
  nixpkgs = lock.nodes.nixpkgs.locked;
  pkgs = import
    (builtins.fetchTarball {
      url = "https://github.com/${nixpkgs.owner}/${nixpkgs.repo}/archive/${nixpkgs.rev}.tar.gz";
      sha256 = nixpkgs.narHash;
    })
    { inherit system; };
  pins = builtins.fromJSON (builtins.readFile ./toolchain.json);
  archive = pins.archives.${system} or (throw "Unsupported Go development system: ${system}");
  go = pkgs.stdenvNoCC.mkDerivation {
    pname = "wire-go";
    version = pins.version;
    src = pkgs.fetchurl {
      url = "https://go.dev/dl/${archive.filename}";
      sha256 = archive.sha256;
    };
    dontBuild = true;
    dontStrip = true;
    dontFixup = true;
    installPhase = ''
      runHook preInstall
      mkdir -p "$out"
      cp -R . "$out/"
      runHook postInstall
    '';
  };
in
pkgs.mkShell {
  packages = [ go pkgs.git pkgs.python3 pkgs.stdenv.cc pkgs.nixpkgs-fmt ];
  GOTOOLCHAIN = "local";
  GOWORK = "off";
}
