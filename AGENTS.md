# Repository guide

## Read for the change

- **Subtitles:** before changing capture, VAD, window processing, the overlay, or
  runtime packaging, read [subtitle development](docs/subtitles.md#development).
  For backend-selection changes, also read its **Backend policy** section.
- **Video transcripts:** before changing URL downloads, normalization, or markdown
  rendering, read the [video guide](docs/video-transcribe.md).

## Verification

- Go changes: run `go test -race ./...` and `go vet ./...`.
- Overlay or packaging changes: run `nix build path:.#audio-transcribe-subtitles`;
  its check phase runs the headless Qt tests.
- Use `path:.` for local Nix checks so new, untracked files are included without
  changing the Git index.
