{ pkgs }:
let
  mkScript =
    name: runtimeInputs: path:
    pkgs.writeShellApplication {
      inherit name runtimeInputs;
      text = builtins.readFile path;
    };
in
{
  cleanup = mkScript "cleanup" [
    pkgs.coreutils
    pkgs.findutils
  ] ./cleanup.sh;

  fmt = mkScript "fmt" [
    pkgs.coreutils
    pkgs.findutils
    pkgs.go
    pkgs.nixfmt
  ] ./fmt.sh;

  check = mkScript "check" [ pkgs.go ] ./check.sh;

  build = mkScript "build" [
    pkgs.coreutils
    pkgs.go
  ] ./build.sh;

  ci = mkScript "ci" [
    pkgs.coreutils
    pkgs.go
  ] ./ci.sh;
}
