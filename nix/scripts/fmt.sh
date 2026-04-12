set -euo pipefail

find . -name '*.go' -print0 | xargs -0 -r gofmt -w
find . -name '*.nix' -print0 | xargs -0 -r nixfmt
