{
  pkgs,
  flake,
  system,
}:
let
  launcher = pkgs.writeShellApplication {
    name = "video-transcribe";
    runtimeInputs = [
      pkgs.nix
      pkgs.coreutils
    ];
    text = builtins.readFile ./scripts/auto.sh + ''
      run_auto ${pkgs.lib.escapeShellArg (toString flake)} ${pkgs.lib.escapeShellArg system} "$(basename "$0")" "$@"
    '';
  };
in
pkgs.symlinkJoin {
  name = "video-transcribe-auto";
  paths = [ launcher ];
  postBuild = ''
    ln -s video-transcribe "$out/bin/yt-transcribe"
    ln -s video-transcribe "$out/bin/tw-transcribe"
  '';
  meta = {
    license = pkgs.lib.licenses.gpl3Plus;
    mainProgram = "video-transcribe";
  };
}
