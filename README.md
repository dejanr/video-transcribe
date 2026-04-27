# video-transcribe

`video-transcribe` downloads audio from a supported video URL, transcribes the full spoken track locally, and renders a markdown transcript that is easy to summarize or iterate on later.

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
    audio.<ext>
    audio.wav
    transcript.raw.json
    transcript.md
    tmp/
      <unique-run-id>/
        metadata.json
        audio.<ext>
        audio.wav
        transcript.raw.json
        transcript.md
```

## Why this approach

- `yt-dlp` handles source resolution and audio download
- `ffmpeg` normalizes audio for `whisper-cli`, and can also run the Whisper filter directly
- `whisper.cpp` keeps transcription local and reproducible
- markdown stays human-friendly, raw JSON stays machine-friendly

## Quick start

### Load automatically with direnv

```bash
direnv allow
```

That activates the flake dev shell automatically when you enter the project directory. The shell includes the local `video-transcribe`, `yt-transcribe`, and `tw-transcribe` binaries plus small Nix-packaged helper commands for common development tasks.

### Run via Nix app

```bash
nix run .#video-transcribe -- 'https://www.youtube.com/watch?v=JpjXYD2AELg'
```

For Twitch archived broadcasts:

```bash
nix run .#tw-transcribe -- 'https://www.twitch.tv/videos/2097173208'
```

That default app is a portable CPU build. For GPU builds on `x86_64-linux` use one of the accelerator-specific apps instead:

```bash
# AMD ROCm build tuned for Strix Halo (gfx1151)
nix run .#video-transcribe-rocm -- 'https://www.twitch.tv/videos/2097173208'

# NVIDIA CUDA build
nix run .#video-transcribe-cuda -- 'https://www.youtube.com/watch?v=JpjXYD2AELg'
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

### Try the ffmpeg whisper backend

```bash
video-transcribe \
  --backend ffmpeg-whisper \
  --ffmpeg-whisper-queue-seconds 15 \
  'https://www.twitch.tv/videos/2097173208'
```

This backend uses FFmpeg 8's `whisper` filter, which can be faster on long-form inputs. When `--keep` is enabled it stores `transcript.raw.json` in the project's normal JSON shape so the rest of the pipeline stays the same. GPU acceleration is enabled by default when the selected build includes ROCm or CUDA support.

## Accelerator notes

- `video-transcribe` / `video-transcribe-cpu`: portable CPU build
- `yt-transcribe` and `tw-transcribe`: aliases to the same CPU package
- `video-transcribe-rocm`: ROCm build tuned for Strix Halo (`gfx1151`)
- `video-transcribe-cuda`: CUDA build for NVIDIA GPUs on `x86_64-linux`
- `--no-gpu` forces CPU inference even when the selected build supports GPU execution
- `--gpu-device` currently applies to the `ffmpeg-whisper` backend
- default output keeps only `transcript.md`
- `--keep` preserves metadata, audio, raw transcript JSON, and `tmp/`

## Flags

```bash
video-transcribe [flags] <url>

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

## License

GPL-3.0-or-later.
