{
  description = "YouTube audio downloader and local markdown transcription CLI";

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

          mkWhisperCpp =
            accelerator:
            let
              package = pkgs.whisper-cpp.override (
                {
                  cudaSupport = accelerator == "cuda";
                  rocmSupport = accelerator == "rocm";
                  withSDL = false;
                }
                // lib.optionalAttrs (accelerator == "rocm") {
                  rocmGpuTargets = "gfx1151";
                }
              );
            in
            if accelerator != "rocm" then
              package
            else
              package.overrideAttrs (old: {
                cmakeFlags = (builtins.filter (flag: !(lib.hasInfix "GGML_HIPBLAS" flag)) old.cmakeFlags) ++ [
                  "-DGGML_HIP=ON"
                ];
              });

          mkRuntime =
            accelerator:
            let
              whisperPackage = mkWhisperCpp accelerator;
              ffmpegPackage = pkgs.ffmpeg.override {
                withWhisper = true;
                whisper-cpp = whisperPackage;
              };
            in
            {
              inherit whisperPackage ffmpegPackage;
              package = pkgs.callPackage ./nix/package.nix {
                ffmpeg = ffmpegPackage;
                whisper-cpp = whisperPackage;
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

                  yt-transcribe

                {202}▶ accelerator{reset}

                  ${acceleratorName}
              '';

              devshell.startup.project-env.text = ''
                export YT_TRANSCRIBE_MODEL_DIR="''${YT_TRANSCRIBE_MODEL_DIR:-$HOME/.cache/yt-transcribe/models}"
              '';

              packages = [
                runtime.package
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
          cpuRuntime = mkRuntime "cpu";

          rocmVariantPackages = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime "rocm";
            in
            {
              yt-transcribe-rocm = runtime.package;
            }
          );

          cudaVariantPackages = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime "cuda";
            in
            {
              yt-transcribe-cuda = runtime.package;
            }
          );

          rocmApps = lib.optionalAttrs isLinuxX86 {
            yt-transcribe-rocm = {
              type = "app";
              program = "${config.packages.yt-transcribe-rocm}/bin/yt-transcribe";
            };
          };

          cudaApps = lib.optionalAttrs isLinuxX86 {
            yt-transcribe-cuda = {
              type = "app";
              program = "${config.packages.yt-transcribe-cuda}/bin/yt-transcribe";
            };
          };

          rocmShells = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime "rocm";
            in
            {
              rocm = mkDevShell {
                inherit runtime;
                acceleratorName = "ROCm build optimized for Strix Halo (gfx1151)";
                extraPackages = [
                  pkgs.rocmPackages.clr
                  pkgs.rocmPackages.rocm-smi
                ];
              };
            }
          );

          cudaShells = lib.optionalAttrs isLinuxX86 (
            let
              runtime = mkRuntime "cuda";
            in
            {
              cuda = mkDevShell {
                inherit runtime;
                acceleratorName = "CUDA build for NVIDIA GPUs (requires allow-unfree)";
              };
            }
          );
        in
        {
          formatter = pkgs.nixfmt;

          packages =
            scriptPackages
            // {
              yt-transcribe = cpuRuntime.package;
              yt-transcribe-cpu = cpuRuntime.package;
              default = config.packages.yt-transcribe;
            }
            // rocmVariantPackages
            // cudaVariantPackages;

          apps = {
            yt-transcribe = {
              type = "app";
              program = "${config.packages.yt-transcribe}/bin/yt-transcribe";
            };
            default = config.apps.yt-transcribe;
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
