package subtitle

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranslateSendsGermanToEnglishRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		for key, expected := range map[string]string{"language": "de", "translate": "true", "response_format": "json", "audio_ctx": "768"} {
			if r.FormValue(key) != expected {
				t.Errorf("%s = %q, expected %q", key, r.FormValue(key), expected)
			}
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body[:4]) != "RIFF" || len(body) != 44+frameBytes {
			t.Error("invalid audio attachment")
		}
		io.WriteString(w, `{"text":"  Hello\n world.  "}`)
	}))
	defer server.Close()
	client := inferenceServer{url: server.URL, client: server.Client()}
	text, err := client.translate(context.Background(), audioFrame(1000))
	if err != nil || text != "Hello world." {
		t.Fatalf("translation = %q, %v", text, err)
	}
}

func TestTranslateReportsHTTPAndJSONErrors(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
	}{
		{500, "model failed"},
		{200, "invalid json"},
	} {
		t.Run(test.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer server.Close()
			client := inferenceServer{url: server.URL, client: server.Client()}
			if _, err := client.translate(context.Background(), audioFrame(1000)); err == nil {
				t.Fatal("expected an inference error")
			}
		})
	}
}

func TestTranslateFullContextOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("audio_ctx") != "0" {
			t.Error("full-context flag did not restore the full encoder")
		}
		io.WriteString(w, `{"text":"Hello."}`)
	}))
	defer server.Close()
	client := inferenceServer{url: server.URL, client: server.Client(), fullContext: true}
	if _, err := client.translate(context.Background(), audioFrame(1000)); err != nil {
		t.Fatal(err)
	}
}

func TestTranslateCancellation(t *testing.T) {
	client := inferenceServer{url: "http://127.0.0.1:1", client: &http.Client{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.translate(ctx, audioFrame(1000)); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
