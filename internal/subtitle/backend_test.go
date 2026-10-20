package subtitle

import "testing"

func TestBackendReflectsSelectedDeviceNotAvailableDevices(t *testing.T) {
	logs := &backendLog{}
	logs.Write([]byte("ggml_vulkan: Found 1 Vulkan devices:\n"))
	if logs.name() != "CPU" {
		t.Fatal("available GPU was mistaken for an active inference backend")
	}
	logs.Write([]byte("whisper_backend_init_gpu: us"))
	logs.Write([]byte("ing Vulkan0 backend\n"))
	if logs.name() != "Vulkan0" {
		t.Fatal("selected backend was not detected across writes")
	}
}
