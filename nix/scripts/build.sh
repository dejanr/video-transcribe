set -euo pipefail

mkdir -p bin
go build -o bin/yt-transcribe ./cmd/yt-transcribe
