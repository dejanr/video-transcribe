package subtitle

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMicrophoneCaptureTimesOutWithoutAudio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input := captureInput{program: "sleep", args: []string{"30"}, startupTimeout: 50 * time.Millisecond}
	err := capture(ctx, Config{}, input, &playback{}, make(chan clip, 1), &emitter{})
	if err == nil || !strings.Contains(err.Error(), "Privacy & Security → Microphone") {
		t.Fatalf("expected a permission hint, got %v", err)
	}
}
