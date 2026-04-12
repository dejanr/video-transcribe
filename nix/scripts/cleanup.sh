set -euo pipefail

root="${1:-./output}"

if [[ ! -d "$root" ]]; then
  echo "artifacts directory not found: $root"
  exit 0
fi

count=$(find "$root" -type f ! -name '*.md' | wc -l | tr -d '[:space:]')
find "$root" -type f ! -name '*.md' -delete
find "$root" -mindepth 1 -depth -type d -empty -delete

echo "removed $count file(s); preserved *.md files under $root"
