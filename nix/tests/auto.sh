#!/usr/bin/env bash
set -euo pipefail
source "$1"

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
sysfs="$workspace/sys"
devices="$workspace/dev"
mkdir -p "$sysfs/class/drm" "$devices/dri"

assert_equal() {
  if [[ "$1" != "$2" ]]; then
    printf 'expected %q, got %q\n' "$1" "$2" >&2
    exit 1
  fi
}

assert_equal cpu "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
mkdir -p "$sysfs/class/drm/renderD128/device"
touch "$devices/dri/renderD128"
printf '0x1002\n' > "$sysfs/class/drm/renderD128/device/vendor"
printf '0x1586\n' > "$sysfs/class/drm/renderD128/device/device"
ln -s /drivers/amdgpu "$sysfs/class/drm/renderD128/device/driver"
assert_equal $'vulkan\ncpu' "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
touch "$devices/kfd"
assert_equal $'rocm\nvulkan\ncpu' "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
assert_equal $'vulkan\ncpu' "$(detect_accelerators aarch64-linux "$sysfs" "$devices")"
printf '0x7550\n' > "$sysfs/class/drm/renderD128/device/device"
assert_equal $'rocm\nvulkan\ncpu' "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
assert_equal $'vulkan\ncpu' "$(detect_accelerators aarch64-linux "$sysfs" "$devices")"
printf '0x744c\n' > "$sysfs/class/drm/renderD128/device/device"
assert_equal $'vulkan\ncpu' "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
printf '0x1586\n' > "$sysfs/class/drm/renderD128/device/device"
touch "$devices/nvidiactl" "$devices/nvidia0"
assert_equal $'cuda\nrocm\nvulkan\ncpu' "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
rm "$devices/dri/renderD128"
assert_equal $'cuda\ncpu' "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"
rm "$devices/nvidia0"
assert_equal cpu "$(detect_accelerators x86_64-linux "$sysfs" "$devices")"

cpu_requested --no-gpu --language auto recording.wav
cpu_requested -no-gpu=true recording.wav
cpu_requested --help
cpu_requested help
if cpu_requested --no-gpu=false recording.wav; then exit 1; fi
if cpu_requested --no-gpu --no-gpu=false recording.wav; then exit 1; fi
if cpu_requested --model --no-gpu recording.wav; then exit 1; fi
if cpu_requested recording.wav --no-gpu; then exit 1; fi
if cpu_requested -- --no-gpu; then exit 1; fi

mkdir -p "$workspace/package/bin"
printf '#!%s\nprintf "%%s\\n" "${0##*/}" "$@"\nexit "${APP_STATUS:-0}"\n' "$BASH" > "$workspace/package/bin/video-transcribe"
chmod +x "$workspace/package/bin/video-transcribe"
ln -s video-transcribe "$workspace/package/bin/yt-transcribe"
ln -s video-transcribe "$workspace/package/bin/tw-transcribe"

detect_accelerators() {
  printf '%s\n' cuda rocm vulkan cpu
}

nix() {
  assert_equal build "$1"
  assert_equal --no-link "$2"
  assert_equal --print-out-paths "$3"
  printf '%s\n' "$4" >> "$workspace/builds"
  case "$4" in
    *-cuda|*-rocm) return 1 ;;
    *) printf '%s\n' "$workspace/package" ;;
  esac
}

assert_equal $'tw-transcribe\n--language\nauto\nfile:///a recording.wav' "$(run_auto /flake x86_64-linux tw-transcribe --language auto 'file:///a recording.wav')"
assert_equal $'/flake#packages.x86_64-linux.video-transcribe-cuda\n/flake#packages.x86_64-linux.video-transcribe-rocm\n/flake#packages.x86_64-linux.video-transcribe-vulkan' "$(< "$workspace/builds")"
: > "$workspace/builds"
assert_equal $'video-transcribe\n--no-gpu\nrecording.wav' "$(run_auto /flake x86_64-linux video-transcribe --no-gpu recording.wav)"
assert_equal /flake#packages.x86_64-linux.video-transcribe-cpu "$(< "$workspace/builds")"
: > "$workspace/builds"
if (export APP_STATUS=7; run_auto /flake x86_64-linux video-transcribe recording.wav); then
  exit 1
else
  assert_equal 7 "$?"
fi
assert_equal $'/flake#packages.x86_64-linux.video-transcribe-cuda\n/flake#packages.x86_64-linux.video-transcribe-rocm\n/flake#packages.x86_64-linux.video-transcribe-vulkan' "$(< "$workspace/builds")"

nix() {
  printf '%s\n' "$4" >> "$workspace/builds"
  printf '%s\n' "$workspace/package"
}
: > "$workspace/builds"
assert_equal $'yt-transcribe\n--no-gpu=false\nrecording.wav' "$(run_auto /flake x86_64-linux yt-transcribe --no-gpu=false recording.wav)"
assert_equal /flake#packages.x86_64-linux.video-transcribe-cuda "$(< "$workspace/builds")"
: > "$workspace/builds"
assert_equal $'tw-transcribe\n--help' "$(run_auto /flake x86_64-linux tw-transcribe --help)"
assert_equal /flake#packages.x86_64-linux.video-transcribe-cpu "$(< "$workspace/builds")"

nix() {
  printf '%s\n' "$4" >> "$workspace/builds"
  return 1
}
: > "$workspace/builds"
if (run_auto /flake x86_64-linux video-transcribe recording.wav); then exit 1; fi
assert_equal $'/flake#packages.x86_64-linux.video-transcribe-cuda\n/flake#packages.x86_64-linux.video-transcribe-rocm\n/flake#packages.x86_64-linux.video-transcribe-vulkan\n/flake#packages.x86_64-linux.video-transcribe-cpu' "$(< "$workspace/builds")"
printf 'auto launcher tests passed\n'
