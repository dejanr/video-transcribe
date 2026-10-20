detect_accelerators() {
  local system="$1" sysfs="$2" devices="$3"
  local render vendor device driver has_rocm=false has_vulkan=false

  if [[ "$system" == x86_64-linux && -r "$devices/nvidiactl" && -w "$devices/nvidiactl" ]]; then
    for device in "$devices"/nvidia[0-9]*; do
      if [[ -r "$device" && -w "$device" ]]; then
        printf '%s\n' cuda
        break
      fi
    done
  fi

  for render in "$sysfs"/class/drm/renderD*; do
    [[ -r "$devices/dri/${render##*/}" && -w "$devices/dri/${render##*/}" ]] || continue
    has_vulkan=true
    [[ -r "$render/device/vendor" && -r "$render/device/device" ]] || continue
    vendor=$(< "$render/device/vendor")
    device=$(< "$render/device/device")
    driver=$(readlink "$render/device/driver") || continue
    if [[ "$system" == x86_64-linux && "$vendor" == 0x1002 && ( "$device" == 0x1586 || "$device" == 0x7550 ) && "${driver##*/}" == amdgpu && -r "$devices/kfd" && -w "$devices/kfd" ]]; then
      has_rocm=true
    fi
  done

  if "$has_rocm"; then printf '%s\n' rocm; fi
  if "$has_vulkan"; then printf '%s\n' vulkan; fi
  printf '%s\n' cpu
}

cpu_requested() {
  local arg no_gpu=false skip_value=false
  for arg in "$@"; do
    if "$skip_value"; then
      skip_value=false
      continue
    fi
    case "$arg" in
      -no-gpu|--no-gpu) no_gpu=true ;;
      -no-gpu=*|--no-gpu=*)
        case "${arg#*=}" in
          1|t|T|true|TRUE|True) no_gpu=true ;;
          0|f|F|false|FALSE|False) no_gpu=false ;;
        esac
        ;;
      help|-h|--help) return 0 ;;
      -output-dir|--output-dir|-model|--model|-backend|--backend|-ffmpeg-whisper-queue-seconds|--ffmpeg-whisper-queue-seconds|-language|--language|-gpu-device|--gpu-device|-chunk-seconds|--chunk-seconds) skip_value=true ;;
      --) break ;;
      -*) ;;
      *) break ;;
    esac
  done
  "$no_gpu"
}

run_auto() {
  local flake="$1" system="$2" program="$3"
  shift 3
  local accelerator package
  local -a accelerators
  if [[ $# == 0 ]] || cpu_requested "$@"; then
    accelerators=(cpu)
  else
    mapfile -t accelerators < <(detect_accelerators "$system" /sys /dev)
  fi

  for accelerator in "${accelerators[@]}"; do
    printf '[accelerator] selecting %s\n' "$accelerator" >&2
    if package=$(nix build --no-link --print-out-paths "$flake#packages.$system.video-transcribe-$accelerator"); then
      exec "$package/bin/$program" "$@"
    fi
    printf '[accelerator] %s build unavailable; trying next option\n' "$accelerator" >&2
  done
  printf '[accelerator] no usable build available\n' >&2
  return 1
}
