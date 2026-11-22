# Live translated subtitles

`audio-transcribe-subtitles` captures desktop audio or microphone input and translates German speech
into English in a transparent, always-on-top overlay. Start it with the command
in [README](../README.md#start).

```bash
nix run .#audio-transcribe-subtitles -- --demo    # UI preview; no capture or models
nix run .#audio-transcribe-subtitles -- --no-gpu  # Skip GPU startup
nix run .#audio-transcribe-subtitles -- --help    # Available options
```

## Requirements and privacy

- **Apple Silicon, macOS 14 or later:** ScreenCaptureKit captures system audio
  without a virtual audio device. Allow the terminal you launch from under
  **System Settings → Privacy & Security → Screen & System Audio Recording** and
  relaunch it if macOS asks. The app consumes only audio, not screen frames.
- **Linux:** Use PulseAudio or PipeWire-Pulse. X11 transparency requires a
  compositor such as Picom. Wayland transparency and placement are
  compositor-dependent.

If fullscreen video covers the overlay, use windowed or borderless playback.

The first run downloads Whisper `small` (~466 MB) and Silero VAD (~865 KB) into
the shared [model cache](video-transcribe.md#model-cache). Silero is pinned to an
upstream revision and SHA-256 verified. Cached models work offline: no audio is
sent to a cloud service. Audio and subtitle history stay in memory; appearance
settings persist. Terminal diagnostics may contain transcribed text.

## Controls

The top bar contains the audio input dropdown, Pause/Resume, Live, Clear, font
Size, Background opacity, and close. Drag the title to move; drag the bottom-right grip to resize.

- **Pause** discards incoming audio; Resume starts from live audio.
- Scroll back to review history; **Live** returns to new captions.
- **Clear** removes history and the current preview.
- Cyan text is provisional and may be revised before moving into history.
- Hover over the status for audio-window, VAD, inference, and queue timings.

Closing the overlay stops capture and inference. Geometry and appearance are
remembered between runs.

## Backend policy

On Apple Silicon, the normal launcher tries **Metal → CPU**. On Linux, it tries
**ROCm → Vulkan → CPU**. ROCm is bundled on `x86_64-linux` for AMD
`gfx1151` / `gfx1201`, including the RX 9070 series, and adds AMD runtime
dependencies. On `aarch64-linux`, startup tries Vulkan → CPU.

Each GPU attempt loads the model, verifies an actual GPU selection, and runs
synthetic inference. Failure advances to the next backend; each attempt has a
30-second startup/validation limit. `--no-gpu` goes directly to CPU. This is a
fixed preference order, not a performance benchmark.

The status reports the selected runtime, such as `MTL0`, `ROCm0`, `Vulkan0`, or `CPU`.
Silero always runs on CPU. Accelerator-specific app names remain available for
explicit configurations; normal startup needs no suffix.

## Audio selection

Choose **System audio** or **Microphone** in the overlay's input dropdown.
Changing input restarts capture and inference; completed captions stay visible.
The app starts on system audio unless you pass `--mic` or `--source`. An explicit
`--source` is also available as **Custom source** in the dropdown.

### Microphone

**Microphone** uses your system's default input instead of desktop audio. On macOS,
choose the device in **System Settings → Sound → Input** before launching.
Microphone and screen recording are separate permissions. If capture doesn't
start and no prompt appears, check **System Settings → Privacy & Security →
Microphone** for your terminal app. If you launched from an editor, try
**Terminal.app**. Restart the launcher after changing permission or audio input.

On Linux, `--mic` uses the default PulseAudio/PipeWire-Pulse input. Use
`--source` instead to select a specific PulseAudio source.

### Desktop audio

On macOS, the default captures system audio via the first available display, not
the microphone. Run `--list-sources` to check permission and list display IDs.
Optionally pass `--source DISPLAY_ID`; this selects a display, not an audio device.

On Linux, the default source is the current output monitor, not the microphone.
All apps playing through that output are captured.

```bash
nix run .#audio-transcribe-subtitles -- --list-sources
nix run .#audio-transcribe-subtitles -- --source alsa_output.YOUR_DEVICE.monitor
```

Choose a source ending in `.monitor`. Restart after changing speakers/headphones,
or provide an explicit source. To isolate one app, route it to a dedicated
PulseAudio sink and capture that sink's monitor.

## Quality and latency

`small` is the default translation model. `base` (~142 MB) was less accurate in
our speech checks; `medium` and `large-v3` need more memory and compute.
English-only models and `large-v3-turbo` are rejected for German translation.

```bash
# Smaller model, with a translation-quality tradeoff
nix run .#audio-transcribe-subtitles -- --model base

# More frequent previews; increases inference work
nix run .#audio-transcribe-subtitles -- --step-seconds 1

# Quiet dialogue; may admit more noise
nix run .#audio-transcribe-subtitles -- --silence-threshold 0.004 --vad-threshold 0.4

# Wait for completed windows and use the full encoder context
nix run .#audio-transcribe-subtitles -- --step-seconds 0 --full-context
```

The energy gate skips quiet input; Silero checks candidate windows before Whisper.
Previews reuse the current phrase context. `--chunk-seconds` bounds the phrase
window, `--step-seconds` controls preview frequency, and `--overlap-seconds` retains
ending audio across window limits to reduce cut-off words. Matching translated
suffixes/prefixes are trimmed; reworded translations can still repeat text.
Overlap is discarded at silence or pause/resume. `--no-vad` keeps energy gating
only; `--vad-model` accepts a custom Silero GGML model.

This is near-real-time, not word-by-word streaming. The reduced encoder context
favors speed; `--full-context` may improve accuracy. Only one inference window
is queued: slow inference skips old updates with a warning. If a completed update
is skipped, the last preview is retained when the next phrase arrives.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| No captions | For microphone capture, check the default input and microphone permission. For desktop audio on macOS, check recording permission with `--list-sources`. On Linux, play audio through the selected monitor. |
| Quiet speech missed | Lower the energy/VAD thresholds, accepting more false positives. |
| Unexpected CPU fallback | Inspect terminal diagnostics for driver, device-permission, or inference failures. |
| Skipped/delayed captions | Check the runtime; try a smaller model or longer preview interval. |
| Opaque/hidden overlay | Check compositor/window-manager settings and fullscreen playback mode. |
| Wording changes or repeats | Previews are revised; VAD and overlap cannot eliminate all recognition errors. |

## Development

`internal/subtitle.Run` owns setup. The private session owns capture shutdown and
serial window processing; `captionSequence` owns adjacent-window deduplication.
The PySide6 overlay in `ui/audio_subtitle.py` consumes the Go process stream.

The process contract is newline-delimited JSON on stdout, diagnostics on stderr,
and `pause` / `resume` lines on stdin. `partial` replaces a preview; `subtitle`
finalizes it. Both carry a phrase `id`; events also report runtime and timings.
Pause/resume generations invalidate queued and in-flight captions. Deduplication
applies only to adjacent overlapping windows in the same generation.

Each failed startup attempt must stop before the next backend starts. Capture
and inference processes must terminate when the overlay closes. On Linux, exercise
audio checks through an isolated PulseAudio null sink with synthetic audio: nonexistent
source names may be redirected to a real output and are not reliable isolation.
On macOS, the helper in `native/macos/capture.m` emits 16 kHz mono signed 16-bit
PCM to the same capture loop. Test it from a logged-in desktop session using
synthetic playback.

Packaged executable paths come from `AUDIO_SUBTITLE_WHISPER_ROCM`,
`AUDIO_SUBTITLE_WHISPER_VULKAN`, `AUDIO_SUBTITLE_WHISPER_METAL`,
`AUDIO_SUBTITLE_WHISPER_CPU`, and `AUDIO_SUBTITLE_VAD`. Local backend builds
without them use tools on `PATH`. On macOS, put `audio-subtitle-mac-capture` on
`PATH` too; it is included in the dev shell.

```bash
# With PySide6 and Whisper tools available, use a locally built backend
go build -o bin/audio-subtitle-stream ./cmd/audio-subtitle-stream
python ui/audio_subtitle.py --backend ./bin/audio-subtitle-stream
```

Follow the change-specific [verification commands](../AGENTS.md#verification).
