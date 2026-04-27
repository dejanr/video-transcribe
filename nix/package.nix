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
  pname = "video-transcribe";
  version = "0.1.0";

  src = lib.cleanSource ../.;

  vendorHash = null;
  subPackages = [
    "cmd/video-transcribe"
    "cmd/yt-transcribe"
    "cmd/tw-transcribe"
  ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    for program in video-transcribe yt-transcribe tw-transcribe; do
      wrapProgram $out/bin/$program \
        --set PATH ${
          lib.makeBinPath [
            yt-dlp
            ffmpeg
            whisper-cpp
            coreutils
            gnugrep
          ]
        }
    done
  '';

  meta = {
    license = lib.licenses.gpl3Plus;
    mainProgram = "video-transcribe";
  };
}
