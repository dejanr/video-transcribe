package whisper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestVADModelDownloadIsVerifiedAndCached(t *testing.T) {
	body := "verified model"
	hash := sha256.Sum256([]byte(body))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.WriteString(w, body)
	}))
	defer server.Close()
	dir := t.TempDir()
	for range 2 {
		path, err := ensureVADModel(context.Background(), dir, server.URL, hex.EncodeToString(hash[:]))
		if err != nil {
			t.Fatal(err)
		}
		if content, _ := os.ReadFile(path); string(content) != body {
			t.Fatal("model contents changed")
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("cached model was downloaded again: %d requests", requests.Load())
	}
}

func TestVADModelRejectsIncompleteOrFailedDownloads(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, "incomplete model")
		}))
		dir := t.TempDir()
		if _, err := ensureVADModel(context.Background(), dir, server.URL, sileroModelSHA256); err == nil {
			t.Fatal("invalid model was accepted")
		}
		server.Close()
		if _, err := os.Stat(filepath.Join(dir, sileroModelName)); !os.IsNotExist(err) {
			t.Fatal("invalid download poisoned the cache")
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("temporary download was not removed")
		}
	}
}

func TestVADModelDownloadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ensureVADModel(ctx, t.TempDir(), "http://127.0.0.1:1", sileroModelSHA256); err == nil {
		t.Fatal("download ignored cancellation")
	}
}
