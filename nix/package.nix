{
  lib,
  buildGoModule,
  makeWrapper,
  yt-dlp,
  ffmpeg,
  whisper-cpp,
  coreutils,
  gnugrep,
}:
buildGoModule {
  pname = "yt-transcribe";
  version = "0.1.0";

  src = lib.cleanSource ../.;

  vendorHash = null;
  subPackages = [ "cmd/yt-transcribe" ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    wrapProgram $out/bin/yt-transcribe \
      --set PATH ${
        lib.makeBinPath [
          yt-dlp
          ffmpeg
          whisper-cpp
          coreutils
          gnugrep
        ]
      }
  '';

  meta = {
    license = lib.licenses.gpl3Plus;
    mainProgram = "yt-transcribe";
  };
}
