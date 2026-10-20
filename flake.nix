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
      ];

      perSystem =
        {
          pkgs,
          config,
          ...
        }:
        let
          lib = pkgs.lib;
          isLinuxX86 = pkgs.stdenv.hostPlatform.isLinux && pkgs.stdenv.hostPlatform.isx86_64;
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
                  withSDL = false;
                }
                // packageSet.lib.optionalAttrs (accelerator == "rocm") {
                  rocmGpuTargets = "gfx1151;gfx1201";
                }
              );
            in
            if accelerator != "rocm" then
              package
            else
              package.overrideAttrs (old: {
                cmakeFlags =
                  (builtins.filter (flag: !(packageSet.lib.hasInfix "GGML_HIPBLAS" flag)) old.cmakeFlags)
                  ++ [
                    "-DGGML_HIP=ON"
                  ];
              });

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
                subtitleWhisperVulkan = if accelerator != "cuda" then mkWhisperCpp packageSet "vulkan" else null;
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

                {202}▶ app{reset}

                  video-transcribe
                  yt-transcribe
                  tw-transcribe
                  audio-transcribe-subtitles

                {202}▶ accelerator{reset}

                  ${acceleratorName}
              '';

              devshell.startup.project-env.text = ''
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
              ++ extraPackages;
            };

          scriptPackages = import ./nix/scripts { inherit pkgs; };
          cpuRuntime = mkRuntime pkgs "cpu";
          vulkanRuntime = mkRuntime pkgs "vulkan";

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

          packages =
            scriptPackages
            // {
              audio-transcribe-subtitles = cpuRuntime.subtitlePackage;
              audio-transcribe-subtitles-vulkan = vulkanRuntime.subtitlePackage;
              video-transcribe = cpuRuntime.package;
              video-transcribe-cpu = cpuRuntime.package;
              yt-transcribe = cpuRuntime.package;
              yt-transcribe-cpu = cpuRuntime.package;
              tw-transcribe = cpuRuntime.package;
              tw-transcribe-cpu = cpuRuntime.package;
              default = config.packages.video-transcribe;
            }
            // rocmVariantPackages
            // cudaVariantPackages;

          apps = {
            audio-transcribe-subtitles-vulkan = {
              type = "app";
              program = "${config.packages.audio-transcribe-subtitles-vulkan}/bin/audio-transcribe-subtitles";
            };
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
          // rocmApps
          // cudaApps;

          devshells = {
            default = mkDevShell {
              runtime = cpuRuntime;
              acceleratorName = "Portable CPU build (use .#rocm or .#cuda for GPU shells)";
            };
          }
          // rocmShells
          // cudaShells;
        };
    };
}
