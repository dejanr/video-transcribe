set -euo pipefail

mkdir -p bin
go build -o bin/video-transcribe ./cmd/video-transcribe
go build -o bin/yt-transcribe ./cmd/yt-transcribe
go build -o bin/tw-transcribe ./cmd/tw-transcribe
go build -o bin/audio-subtitle-stream ./cmd/audio-subtitle-stream
