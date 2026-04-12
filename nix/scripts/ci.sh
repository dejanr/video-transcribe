set -euo pipefail

go test ./...
mkdir -p bin
go build -o bin/yt-transcribe ./cmd/yt-transcribe
