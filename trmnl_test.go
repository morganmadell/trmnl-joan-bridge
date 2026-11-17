package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trmnl-joan-bridge/pv3"
)

func TestPollIntervalClampsToOneSecond(t *testing.T) {
	tc := &trmnlClient{fallback: 0}
	if got := tc.pollInterval(); got != time.Second {
		t.Errorf("pollInterval = %v, want 1s", got)
	}
	tc.fallback = 90 * time.Second
	if got := tc.pollInterval(); got != 90*time.Second {
		t.Errorf("pollInterval = %v, want 90s", got)
	}
}

func TestTRMNLRefreshStoresFrameAndRate(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, pv3.PanelW, pv3.PanelH))); err != nil {
		t.Fatal(err)
	}
	var gotID, gotToken string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/api/display", func(w http.ResponseWriter, r *http.Request) {
		gotID, gotToken = r.Header.Get("ID"), r.Header.Get("Access-Token")
		json.NewEncoder(w).Encode(map[string]any{"image_url": "http://umbrel.local/img.png", "refresh_rate": 120})
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) })

	tc := &trmnlClient{server: srv.URL, deviceID: "AA:BB", token: "tok", fallback: 60 * time.Second}
	fc := &frameStore{}
	if err := tc.refresh(fc); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if gotID != "AA:BB" || gotToken != "tok" {
		t.Errorf("headers ID=%q Access-Token=%q", gotID, gotToken)
	}
	if _, full, _, isNew := fc.load(); len(full) == 0 || !isNew {
		t.Errorf("frame not stored (len=%d, isNew=%v)", len(full), isNew)
	}
	if got := tc.pollInterval(); got != 120*time.Second {
		t.Errorf("pollInterval = %v, want 120s from refresh_rate", got)
	}
}

// Fails under -race if pollInterval reads fallback without tc.mu while refresh writes it.
func TestTRMNLRefreshConcurrentPollInterval(t *testing.T) {
	const refreshes = 10
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	var served atomic.Int32
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/api/display", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"image_url": "http://umbrel.local/img.png", "refresh_rate": served.Add(1)})
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) })

	tc := &trmnlClient{server: srv.URL, fallback: 60 * time.Second}
	fc := &frameStore{}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(done)
		for range refreshes {
			if err := tc.refresh(fc); err != nil {
				t.Errorf("refresh: %v", err)
				return
			}
		}
	}()
	// Read until the writer finishes: a reader that exits before the first write leaves the race
	// detector no live stack to report against.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				tc.pollInterval()
			}
		}
	}()
	wg.Wait()

	if got := tc.pollInterval(); got < time.Second || got > refreshes*time.Second || got%time.Second != 0 {
		t.Errorf("pollInterval = %v, want a served refresh_rate in 1s..%ds", got, refreshes)
	}
}

func TestTRMNLRefreshErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()
	tc := &trmnlClient{server: srv.URL}
	if err := tc.refresh(&frameStore{}); err == nil {
		t.Fatal("want an error for status 404, got nil")
	}
}
