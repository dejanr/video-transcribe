package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionPublishesCaptionAndTimings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"text":"Good evening."}`)
	}))
	defer server.Close()
	var output bytes.Buffer
	session := session{
		config: Config{NoVAD: true},
		server: &inferenceServer{url: server.URL, client: server.Client(), backend: "ROCm0"},
		events: &emitter{encoder: json.NewEncoder(&output)},
	}
	now := time.Now()
	if err := session.process(context.Background(), clip{
		pcm: audioFrame(1000), id: 1, final: true, started: now.Add(-time.Second), queued: now,
	}); err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "subtitle" || event.Text != "Good evening." || event.Runtime != "ROCm0" {
		t.Fatalf("unexpected caption: %+v", event)
	}
	if event.AudioSeconds != 0.25 || event.InferenceSeconds <= 0 || event.LatencySeconds < 1 {
		t.Fatalf("missing caption timings: %+v", event)
	}
}

func TestSessionDiscardsInvalidatedInference(t *testing.T) {
	for _, test := range []struct {
		name  string
		pause bool
	}{{"new generation", false}, {"paused", true}} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			session := session{config: Config{NoVAD: true}, events: &emitter{encoder: json.NewEncoder(&output)}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.pause {
					session.state.paused.Store(true)
				} else {
					session.state.generation.Add(1)
				}
				io.WriteString(w, `{"text":"Stale caption."}`)
			}))
			defer server.Close()
			session.server = &inferenceServer{url: server.URL, client: server.Client()}
			if err := session.process(context.Background(), clip{pcm: audioFrame(1000), id: 1, final: true}); err != nil {
				t.Fatal(err)
			}
			if output.Len() != 0 {
				t.Fatalf("published invalidated caption: %s", output.String())
			}
		})
	}
}

func TestCaptionSequenceDeduplicatesOnlyAdjacentOverlappingWindows(t *testing.T) {
	sequence := captionSequence{}
	sequence.event(clip{id: 1, final: true}, "We are watching a movie.")
	preview := sequence.event(clip{id: 2, overlapped: true}, "A movie today.")
	final := sequence.event(clip{id: 2, overlapped: true, final: true}, "A movie today. Welcome.")
	if preview.Type != "partial" || preview.Text != "today." || final.Text != "today. Welcome." {
		t.Fatalf("overlap revisions were not deduplicated consistently: %+v, %+v", preview, final)
	}
	fresh := sequence.event(clip{id: 3, generation: 1, overlapped: true, final: true}, "Today. Welcome. New speaker.")
	if fresh.Text != "Today. Welcome. New speaker." {
		t.Fatalf("stripped words across a pause/resume generation: %+v", fresh)
	}
	skipped := sequence.event(clip{id: 5, generation: 1, overlapped: true, final: true}, "New speaker. Again.")
	if skipped.Text != "New speaker. Again." {
		t.Fatalf("stripped words after a skipped window: %+v", skipped)
	}
}
