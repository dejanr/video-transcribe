{ stdenv, darwinMinVersionHook }:
stdenv.mkDerivation {
  pname = "audio-subtitle-mac-capture";
  version = "0.1.0";
  src = ../native/macos;
  buildInputs = [ (darwinMinVersionHook "13.0") ];
  buildPhase = ''
    runHook preBuild
    $CC -fobjc-arc -Wall -Wextra -Werror -framework Foundation -framework CoreMedia \
      -framework ScreenCaptureKit capture.m -o audio-subtitle-mac-capture
    runHook postBuild
  '';
  installPhase = ''
    runHook preInstall
    mkdir -p "$out/bin"
    cp audio-subtitle-mac-capture "$out/bin/"
    runHook postInstall
  '';
}
