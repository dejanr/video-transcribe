{
  lib,
  buildGoModule,
  makeWrapper,
  yt-dlp,
  ffmpeg,
  whisper-cpp,
  subtitleWhisperCPU ? null,
  subtitleWhisperVulkan ? null,
  subtitleWhisperROCm ? null,
  coreutils,
  gnugrep,
  pulseaudio,
}:
let
  subtitleRuntimeArgs =
    lib.optionals (subtitleWhisperCPU != null) [
      "--set"
      "AUDIO_SUBTITLE_WHISPER_CPU"
      "${subtitleWhisperCPU}/bin/whisper-server"
      "--set"
      "AUDIO_SUBTITLE_VAD"
      "${subtitleWhisperCPU}/bin/whisper-vad-speech-segments"
    ]
    ++ lib.optionals (subtitleWhisperROCm != null) [
      "--set"
      "AUDIO_SUBTITLE_WHISPER_ROCM"
      "${subtitleWhisperROCm}/bin/whisper-server"
    ]
    ++ lib.optionals (subtitleWhisperVulkan != null) [
      "--set"
      "AUDIO_SUBTITLE_WHISPER_VULKAN"
      "${subtitleWhisperVulkan}/bin/whisper-server"
    ];
in
buildGoModule {
  pname = "video-transcribe";
  version = "0.1.0";

  src = lib.cleanSource ../.;

  vendorHash = null;
  subPackages = [
    "cmd/video-transcribe"
    "cmd/yt-transcribe"
    "cmd/tw-transcribe"
    "cmd/audio-subtitle-stream"
  ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    for program in video-transcribe yt-transcribe tw-transcribe audio-subtitle-stream; do
      wrapProgram $out/bin/$program \
        --set PATH ${
          lib.makeBinPath [
            yt-dlp
            ffmpeg
            whisper-cpp
            coreutils
            gnugrep
            pulseaudio
          ]
        } \
        ${lib.escapeShellArgs subtitleRuntimeArgs}
    done
  '';

  meta = {
    license = lib.licenses.gpl3Plus;
    mainProgram = "video-transcribe";
  };
}
