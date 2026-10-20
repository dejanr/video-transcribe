# Video transcription

[Project overview](../README.md)

`video-transcribe` accepts a supported video URL or a local media file, transcribes the full spoken track locally, and renders a markdown transcript that is easy to summarize or iterate on later.

It works well for normal YouTube videos and Twitch archived broadcasts such as `https://www.twitch.tv/videos/...`. It is not focused on live streams.

By default it uses `large-v3-turbo` for a stronger quality/speed tradeoff on spoken audio. Switch to a smaller model if you prefer faster runs or lower memory use.

By default each run keeps only the final `transcript.md`. Pass `--keep` if you also want downloaded audio, normalized audio, raw transcript JSON, metadata, and the per-run temporary workspace.

The flake and package expose three equivalent binaries:

- `video-transcribe` — primary generic entrypoint
- `yt-transcribe` — YouTube-friendly alias
- `tw-transcribe` — Twitch-friendly alias

## What it produces

By default each source gets a dedicated folder under `./output` containing only the final markdown transcript:

```text
output/
  <media-id>-<slug>/
    transcript.md
```

If you pass `--keep`, the full artifact set is preserved:

```text
output/
  <media-id>-<slug>/
    metadata.json
    audio.<ext>  # source.<ext> for local files
    audio.wav
    transcript.raw.json
    transcript.md
    tmp/
      <unique-run-id>/
        metadata.json
        audio.<ext>  # source.<ext> for local files
        audio.wav
        transcript.raw.json
        transcript.md
```

## Why this approach

- `yt-dlp` handles remote source resolution and audio download; local files bypass it
- `ffmpeg` normalizes audio for `whisper-cli`, and can also run the Whisper filter directly
- `whisper.cpp` keeps transcription local and reproducible
- markdown stays human-friendly, raw JSON stays machine-friendly

## Quick start

### Load automatically with direnv

```bash
direnv allow
```

That activates the flake dev shell automatically when you enter the project directory. The shell includes `audio-transcribe-subtitles`, the `video-transcribe` / `yt-transcribe` / `tw-transcribe` binaries, and small Nix-packaged helper commands for common development tasks.

### Run via Nix app

```bash
nix run .#video-transcribe -- 'https://www.youtube.com/watch?v=JpjXYD2AELg'
```

For Twitch archived broadcasts:

```bash
nix run .#tw-transcribe -- 'https://www.twitch.tv/videos/2097173208'
```

The default app detects accessible GPU devices at runtime and selects CUDA for NVIDIA, ROCm for Strix Halo (`gfx1151`) and Radeon RX 9070 series (`gfx1201`), or Vulkan for other GPUs. CPU is the last fallback. Only the selected variant is built; if its Nix build fails, the launcher tries the next GPU option before CPU. It prints its selection to stderr.

Selection is a GPU-first heuristic, not a benchmark. GPU drivers must already be installed and device nodes accessible to your user. Vulkan also requires a working Vulkan driver. Transcription errors are returned directly, not retried as CPU runs. This is separate from the subtitle launcher's [inference-validated backend selection](subtitles.md#backend-policy).

To bypass detection, use an explicit accelerator app:

```bash
# AMD ROCm build for Strix Halo (gfx1151) and Radeon RX 9070 series (gfx1201)
nix run .#video-transcribe-rocm -- 'https://www.twitch.tv/videos/2097173208'

# NVIDIA CUDA build (x86_64-linux)
nix run .#video-transcribe-cuda -- 'https://www.youtube.com/watch?v=JpjXYD2AELg'

# Vulkan build (AMD, Intel, and other Vulkan-capable GPUs)
nix run .#video-transcribe-vulkan -- --language auto /path/to/recording.wav

# Explicit CPU build
nix run .#video-transcribe-cpu -- --language auto /path/to/recording.wav
```

The CUDA app and `.#cuda` dev shell import nixpkgs with unfree enabled only for that CUDA-specific package set, so no extra `allow-unfree` setup is required for this flake.

### Run inside the dev shell

```bash
nix develop
video-transcribe 'https://www.youtube.com/watch?v=JpjXYD2AELg'
```

Twitch alias example:

```bash
tw-transcribe 'https://www.twitch.tv/videos/2097173208'
```

GPU-focused shells are also available:

```bash
nix develop .#rocm
nix develop .#cuda
```

### Transcribe a local file

Pass a plain file path or a `file://` URL:

```bash
nix run .#video-transcribe -- --language auto 'file:///path/to/recording.wav'

# Plain paths work too
video-transcribe --language auto /path/to/recording.wav
```

Local audio and video files bypass `yt-dlp`. `ffprobe` reads the duration and checks for an audio stream; FFmpeg normalizes a copy for either transcription backend. The original file is never modified. Use `--keep` to preserve the source copy as `source.<ext>` alongside the normalized `audio.wav`. Local output folders use a file-identity hash and filename; changing the file size or modification time creates a new workspace rather than reusing stale transcripts.

The default language is English; use `--language auto` or specify the recording's language.

### Try the ffmpeg whisper backend

```bash
video-transcribe \
  --backend ffmpeg-whisper \
  --ffmpeg-whisper-queue-seconds 15 \
  'https://www.twitch.tv/videos/2097173208'
```

This backend uses FFmpeg 8's `whisper` filter, which can be faster on long-form inputs. When `--keep` is enabled it stores `transcript.raw.json` in the project's normal JSON shape so the rest of the pipeline stays the same. GPU acceleration is enabled by default when the selected build includes ROCm or CUDA support.

## Accelerator notes

- `video-transcribe`, `yt-transcribe`, and `tw-transcribe`: automatic GPU-first selection, including in the default dev shell
- `video-transcribe-cpu`: portable CPU build
- `video-transcribe-vulkan`: Vulkan GPU build on both supported Linux architectures
- `video-transcribe-rocm`: ROCm build for Strix Halo (`gfx1151`) and Radeon RX 9070 series (`gfx1201`)
- `video-transcribe-cuda`: CUDA build for NVIDIA GPUs on `x86_64-linux`
- `--no-gpu` selects the CPU build in the automatic launcher, or disables GPU inference in an explicit accelerator build
- the automatic launcher requires Nix at runtime and builds from its bundled flake snapshot, regardless of the current working directory
- the `build` helper still produces an unwrapped Go binary; its acceleration depends on the `whisper-cli` / FFmpeg available on `PATH`
- `--gpu-device` currently applies to the `ffmpeg-whisper` backend
- default output keeps only `transcript.md`
- `--keep` preserves metadata, audio, raw transcript JSON, and `tmp/`

## Flags

```bash
video-transcribe [flags] <source>

  -output-dir string
        Directory where artifacts are written. (default "./output")
  -model string
        whisper.cpp model to use. (default "large-v3-turbo")
  -backend string
        Transcription backend: whisper-cli or ffmpeg-whisper. (default "whisper-cli")
  -ffmpeg-whisper-queue-seconds int
        Queue size in seconds for the ffmpeg whisper filter. Higher values trade latency for throughput. (default 15)
  -language string
        Spoken language for whisper.cpp, or auto to detect. (default "en")
  -no-gpu
        Disable GPU acceleration even when the selected backend was built with GPU support.
  -gpu-device int
        GPU device index for the ffmpeg whisper backend. (default 0)
  -keep
        Keep downloaded and intermediate artifacts instead of only transcript.md.
  -force
        Re-run every stage even if artifacts already exist.
  -chunk-seconds int
        Target markdown chunk size in seconds. (default 45)
  -trace
        Print stage timings and external command timings.
```

## Model cache

Models are downloaded on first use into:

```text
$XDG_CACHE_HOME/video-transcribe/models
# or
~/.cache/video-transcribe/models
```

Override it with:

```bash
export VIDEO_TRANSCRIBE_MODEL_DIR=/path/to/models
```

`YT_TRANSCRIBE_MODEL_DIR` is still accepted as a compatibility fallback.

## Cleanup

To remove everything under `output/` except markdown files:

```bash
direnv allow
cleanup
```

Pass a different root directory if needed:

```bash
cleanup ./some/other/artifacts
```

This deletes non-`.md` files and then removes empty directories.

## Development

With direnv enabled:

```bash
direnv allow
check
build
```

Without direnv:

```bash
nix develop --command check
nix develop --command build
```
