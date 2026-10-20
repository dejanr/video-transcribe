{
  lib,
  stdenvNoCC,
  makeWrapper,
  python3,
  qt6,
  backend,
}:
let
  python = python3.withPackages (ps: [ ps.pyside6 ]);
in
stdenvNoCC.mkDerivation {
  pname = "audio-transcribe-subtitles";
  version = "0.1.0";
  src = lib.cleanSource ../.;

  nativeBuildInputs = [
    makeWrapper
    qt6.wrapQtAppsHook
  ];
  buildInputs = [
    python
    qt6.qtbase
  ];
  dontWrapQtApps = true;
  dontBuild = true;

  doCheck = true;
  checkPhase = ''
    runHook preCheck
    QT_STYLE_OVERRIDE=Fusion QT_QPA_PLATFORM=offscreen ${python}/bin/python -m unittest discover -s tests -p 'test_*.py'
    runHook postCheck
  '';

  installPhase = ''
    runHook preInstall
    mkdir -p $out/share/audio-transcribe-subtitles $out/bin
    cp ui/audio_subtitle.py $out/share/audio-transcribe-subtitles/
    makeWrapper ${python}/bin/python $out/bin/audio-transcribe-subtitles \
      --add-flags "$out/share/audio-transcribe-subtitles/audio_subtitle.py" \
      --set AUDIO_SUBTITLE_BACKEND ${backend}/bin/audio-subtitle-stream \
      --set QT_STYLE_OVERRIDE Fusion \
      "''${qtWrapperArgs[@]}"
    runHook postInstall
  '';

  meta = {
    description = "Transparent local German-to-English system-audio subtitle overlay";
    license = lib.licenses.gpl3Plus;
    platforms = lib.platforms.linux;
    mainProgram = "audio-transcribe-subtitles";
  };
}
