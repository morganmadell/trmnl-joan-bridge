package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
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
