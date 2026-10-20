# video-transcribe

Local Whisper-powered tools for desktop subtitles and video transcripts.

## Start

### Live German → English subtitles

```bash
nix run .#audio-transcribe-subtitles
```

A transparent, always-on-top overlay for meetings, movies, and other desktop audio.
Linux with PulseAudio or PipeWire-Pulse is required. Models download on first use;
subsequent runs work offline.

Read the [subtitle guide](docs/subtitles.md) for controls, audio selection,
automatic hardware acceleration, speech detection, tuning, and troubleshooting.

### Video URL or local file → markdown transcript

```bash
nix run .#video-transcribe -- 'https://www.youtube.com/watch?v=JpjXYD2AELg'
nix run .#video-transcribe -- --language auto /path/to/recording.wav
```

Downloads or copies source audio and transcribes it locally, with automatic GPU
selection. `yt-transcribe` and `tw-transcribe` are aliases; `nix run .` also starts
this CLI. The default output is `transcript.md`.

Read the [video guide](docs/video-transcribe.md) for Twitch and local-file examples,
accelerator builds, artifacts, and cleanup. Both apps share its [model cache](docs/video-transcribe.md#model-cache).

## Development

`direnv allow` loads the Nix dev shell. Task-specific code guidance and validation
commands live in [AGENTS.md](AGENTS.md).

## License

GPL-3.0-or-later. See [LICENSE](LICENSE).
