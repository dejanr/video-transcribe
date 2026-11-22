{
  description = "Video audio downloader and local markdown transcription CLI";

  inputs = {
    flake-parts.url = "github:hercules-ci/flake-parts";
    flake-parts.inputs.nixpkgs-lib.follows = "nixpkgs";
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    devshell.url = "github:numtide/devshell";
    devshell.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    inputs@{
      flake-parts,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [ inputs.devshell.flakeModule ];

      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];

      perSystem =
        {
          pkgs,
          config,
          system,
          ...
        }:
        let
          lib = pkgs.lib;
          inherit (pkgs.stdenv.hostPlatform) isLinux isDarwin;
          isLinuxX86 = isLinux && pkgs.stdenv.hostPlatform.isx86_64;
          macCapture = if isDarwin then pkgs.callPackage ./nix/mac-capture.nix { } else null;
          pkgsCuda =
            if isLinuxX86 then
              import inputs.nixpkgs {
                system = pkgs.stdenv.hostPlatform.system;
                config.allowUnfree = true;
              }
            else
              pkgs;

          mkWhisperCpp =
            packageSet: accelerator:
            let
              package = packageSet.whisper-cpp.override (
                {
                  cudaSupport = accelerator == "cuda";
                  rocmSupport = accelerator == "rocm";
                  vulkanSupport = accelerator == "vulkan";
                  metalSupport = accelerator == "metal";
                  coreMLSupport = false;
                  withSDL = false;
                }
                // packageSet.lib.optionalAttrs (accelerator == "rocm") {
                  rocmGpuTargets = "gfx1151;gfx1201";
                }
              );
            in
            if accelerator == "cpu" && isDarwin then
              package.overrideAttrs (old: {
                cmakeFlags = old.cmakeFlags ++ [ "-DGGML_METAL=OFF" ];
              })
            else if accelerator == "rocm" then
              package.overrideAttrs (old: {
                cmakeFlags =
                  (builtins.filter (flag: !(packageSet.lib.hasInfix "GGML_HIPBLAS" flag)) old.cmakeFlags)
                  ++ [
                    "-DGGML_HIP=ON"
                  ];
              })
            else
              package;

          mkRuntime =
            packageSet: accelerator:
            let
              whisperPackage = mkWhisperCpp packageSet accelerator;
              ffmpegPackage = packageSet.ffmpeg.override {
                withWhisper = true;
                whisper-cpp = whisperPackage;
              };
              subtitleBackend = packageSet.callPackage ./nix/package.nix {
                ffmpeg = packageSet.ffmpeg;
                whisper-cpp = whisperPackage;
                subtitleWhisperCPU = mkWhisperCpp packageSet "cpu";
                subtitleWhisperVulkan =
                  if isLinux && accelerator != "cuda" then mkWhisperCpp packageSet "vulkan" else null;
                subtitleWhisperMetal = if accelerator == "metal" then whisperPackage else null;
                inherit macCapture;
                subtitleWhisperROCm =
                  if
                    isLinuxX86
                    && builtins.elem accelerator [
                      "cpu"
                      "rocm"
                    ]
                  then
                    mkWhisperCpp packageSet "rocm"
                  else
                    null;
              };
              package = packageSet.callPackage ./nix/package.nix {
                ffmpeg = ffmpegPackage;
                whisper-cpp = whisperPackage;
              };
            in
            {
              inherit whisperPackage ffmpegPackage package;
              subtitlePackage = packageSet.callPackage ./nix/subtitle.nix {
                backend = subtitleBackend;
              };
            };

          mkDevShell =
            {
              runtime,
              acceleratorName,
              extraPackages ? [ ],
            }:
            {
              devshell.motd = ''
                {202}🔨 project {reset}

                  cleanup <root>  — Cleanup output artifacts while preserving markdown files
                  fmt             — Format Go and Nix files
                  check           — Run the Go test suite
                  build           — Build the local CLI binary
                  ci              — Run the local CI task set

                {202}▶ video transcripts{reset}

                  video-transcribe  — Transcribe a video URL or local file to Markdown
                  yt-transcribe / tw-transcribe  — Aliases

                {202}▶ live subtitles{reset}

                  audio-transcribe-subtitles         — German → English overlay
                  audio-transcribe-subtitles --demo  — Preview without audio or models
                  Choose System audio or Microphone in the toolbar.

                {202}▶ accelerator{reset}

                  ${acceleratorName}
              '';

              devshell.startup.project-env.text = lib.optionalString isLinux ''
                export VIDEO_TRANSCRIBE_MODEL_DIR="''${VIDEO_TRANSCRIBE_MODEL_DIR:-''${YT_TRANSCRIBE_MODEL_DIR:-$HOME/.cache/video-transcribe/models}}"
                export YT_TRANSCRIBE_MODEL_DIR="$VIDEO_TRANSCRIBE_MODEL_DIR"
              '';

              packages = [
                runtime.package
                runtime.subtitlePackage
                config.packages.cleanup
                config.packages.fmt
                config.packages.check
                config.packages.build
                config.packages.ci
              ]
              ++ (with pkgs; [
                go
                gopls
                gotools
                yt-dlp
                runtime.ffmpegPackage
                runtime.whisperPackage
                coreutils
                gnugrep
                config.formatter
              ])
              ++ lib.optional isDarwin macCapture
              ++ extraPackages;
            };

          scriptPackages = import ./nix/scripts { inherit pkgs; };
          cpuRuntime = mkRuntime pkgs "cpu";
          platformAccelerator = if isDarwin then "metal" else "vulkan";
          acceleratorRuntime = mkRuntime pkgs platformAccelerator;
          defaultRuntime = if isDarwin then acceleratorRuntime else cpuRuntime;
          cliNames = [
            "video-transcribe"
            "yt-transcribe"
            "tw-transcribe"
          ];
          platformVariantNames = map (name: "${name}-${platformAccelerator}") (
            cliNames ++ [ "audio-transcribe-subtitles" ]
          );
          autoPackage = import ./nix/auto.nix {
            inherit pkgs system;
            flake = inputs.self.outPath;
          };

          rocmVariantPackages = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime pkgs "rocm";
            in
            {
              video-transcribe-rocm = runtime.package;
              yt-transcribe-rocm = runtime.package;
              tw-transcribe-rocm = runtime.package;
              audio-transcribe-subtitles-rocm = runtime.subtitlePackage;
            }
          );

          cudaVariantPackages = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime pkgsCuda "cuda";
            in
            {
              video-transcribe-cuda = runtime.package;
              yt-transcribe-cuda = runtime.package;
              tw-transcribe-cuda = runtime.package;
              audio-transcribe-subtitles-cuda = runtime.subtitlePackage;
            }
          );

          rocmApps = lib.optionalAttrs isLinuxX86 {
            audio-transcribe-subtitles-rocm = {
              type = "app";
              program = "${config.packages.audio-transcribe-subtitles-rocm}/bin/audio-transcribe-subtitles";
            };
            video-transcribe-rocm = {
              type = "app";
              program = "${config.packages.video-transcribe-rocm}/bin/video-transcribe";
            };
            yt-transcribe-rocm = {
              type = "app";
              program = "${config.packages.yt-transcribe-rocm}/bin/yt-transcribe";
            };
            tw-transcribe-rocm = {
              type = "app";
              program = "${config.packages.tw-transcribe-rocm}/bin/tw-transcribe";
            };
          };

          cudaApps = lib.optionalAttrs isLinuxX86 {
            audio-transcribe-subtitles-cuda = {
              type = "app";
              program = "${config.packages.audio-transcribe-subtitles-cuda}/bin/audio-transcribe-subtitles";
            };
            video-transcribe-cuda = {
              type = "app";
              program = "${config.packages.video-transcribe-cuda}/bin/video-transcribe";
            };
            yt-transcribe-cuda = {
              type = "app";
              program = "${config.packages.yt-transcribe-cuda}/bin/yt-transcribe";
            };
            tw-transcribe-cuda = {
              type = "app";
              program = "${config.packages.tw-transcribe-cuda}/bin/tw-transcribe";
            };
          };

          rocmShells = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime pkgs "rocm";
            in
            {
              rocm = mkDevShell {
                inherit runtime;
                acceleratorName = "ROCm build for Strix Halo (gfx1151) and Radeon RX 9070 series (gfx1201)";
                extraPackages = [
                  pkgs.rocmPackages.clr
                  pkgs.rocmPackages.rocm-smi
                ];
              };
            }
          );

          cudaShells = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime pkgsCuda "cuda";
            in
            {
              cuda = mkDevShell {
                inherit runtime;
                acceleratorName = "CUDA build for NVIDIA GPUs";
              };
            }
          );
        in
        {
          formatter = pkgs.nixfmt;

          checks.auto-launcher = pkgs.runCommand "video-transcribe-auto-tests" { } ''
            ${pkgs.bash}/bin/bash ${./nix/tests/auto.sh} ${./nix/scripts/auto.sh}
            touch "$out"
          '';

          packages =
            scriptPackages
            // {
              audio-transcribe-subtitles = defaultRuntime.subtitlePackage;
              "audio-transcribe-subtitles-${platformAccelerator}" = acceleratorRuntime.subtitlePackage;
              video-transcribe = autoPackage;
              video-transcribe-cpu = cpuRuntime.package;
              yt-transcribe = autoPackage;
              yt-transcribe-cpu = cpuRuntime.package;
              tw-transcribe = autoPackage;
              tw-transcribe-cpu = cpuRuntime.package;
              default = config.packages.video-transcribe;
            }
            // lib.genAttrs (map (name: "${name}-${platformAccelerator}") cliNames) (
              _: acceleratorRuntime.package
            )
            // rocmVariantPackages
            // cudaVariantPackages;

          apps = {
            audio-transcribe-subtitles = {
              type = "app";
              program = "${config.packages.audio-transcribe-subtitles}/bin/audio-transcribe-subtitles";
            };
            video-transcribe = {
              type = "app";
              program = "${config.packages.video-transcribe}/bin/video-transcribe";
            };
            yt-transcribe = {
              type = "app";
              program = "${config.packages.yt-transcribe}/bin/yt-transcribe";
            };
            tw-transcribe = {
              type = "app";
              program = "${config.packages.tw-transcribe}/bin/tw-transcribe";
            };
            default = config.apps.video-transcribe;
          }
          // lib.genAttrs (map (name: "${name}-cpu") cliNames ++ platformVariantNames) (name: {
            type = "app";
            program = "${config.packages.${name}}/bin/${lib.removeSuffix "-cpu" (lib.removeSuffix "-${platformAccelerator}" name)}";
          })
          // rocmApps
          // cudaApps;

          devshells = {
            default = mkDevShell {
              runtime = defaultRuntime // {
                package = autoPackage;
              };
              acceleratorName =
                if isDarwin then "Apple Metal with CPU fallback" else "Automatic GPU selection with CPU fallback";
            };
          }
          // rocmShells
          // cudaShells;
        };
    };
}
