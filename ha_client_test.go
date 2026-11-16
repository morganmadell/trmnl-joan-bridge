package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trmnl-joan-bridge/pv3"
)

// A chromium path that cannot start, so the re-render after a tap fails fast and
// the tests only check the state change and the outgoing requests.
const noChromium = "/nonexistent/chromium"

func newTestHAClient(t *testing.T, haURL string) *haClient {
	t.Helper()
	zc, err := loadZoneConfig(writeTestZones(t))
	if err != nil {
		t.Fatalf("loadZoneConfig: %v", err)
	}
	return &haClient{haURL: haURL, haToken: "tok", chromiumBin: noChromium, waitMs: 0, zones: zc, curPage: zc.Pages[0].Name}
}

func TestCallServiceRequest(t *testing.T) {
	var gotPath, gotAuth, gotType string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotType = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
	}))
	defer srv.Close()

	h := newTestHAClient(t, srv.URL)
	if err := h.callService("media_player", "toggle", "media_player.tv"); err != nil {
		t.Fatalf("callService: %v", err)
	}
	if gotPath != "/api/services/media_player/toggle" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotType != "application/json" {
		t.Errorf("Content-Type = %q", gotType)
	}
	if gotBody["entity_id"] != "media_player.tv" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestCallServiceErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := newTestHAClient(t, srv.URL).callService("light", "toggle", "light.x")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want an error that names status 401", err)
	}
}

func TestCurrentPageFallsBackToFirst(t *testing.T) {
	h := newTestHAClient(t, "http://unused")
	h.curPage = "does-not-exist"
	if got := h.currentPage(); got != &h.zones.Pages[0] {
		t.Errorf("currentPage = %v, want page 0", got.Name)
	}
}

// Raw panel taps are rotated 180 degrees from the display. A tap at raw
// (PanelW-x, PanelH-y) must hit the zone at display (x, y).
func TestOnTouchFlipsRawCoordinates(t *testing.T) {
	h := newTestHAClient(t, "http://unused")
	dispX, dispY := 500, 40 // "controls" tab zone (342..683, 0..80)
	h.onTouch(pv3.PanelW-dispX, pv3.PanelH-dispY, &frameStore{})
	if h.curPage != "controls" {
		t.Errorf("curPage = %q, want controls", h.curPage)
	}
}

// The same numbers passed unflipped land at display (524, 718), outside every
// zone, so a missing flip would show up as no page change.
func TestOnTouchOutsideZoneKeepsPage(t *testing.T) {
	h := newTestHAClient(t, "http://unused")
	h.onTouch(500, 40, &frameStore{})
	if h.curPage != "sensors" {
		t.Errorf("curPage = %q, want sensors (tap hit no zone)", h.curPage)
	}
}

func TestOnTouchServiceCall(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotPath = r.URL.Path }))
	defer srv.Close()

	h := newTestHAClient(t, srv.URL)
	h.curPage = "controls"
	h.onTouch(pv3.PanelW-100, pv3.PanelH-200, &frameStore{}) // display (100, 200): boardroom TV zone
	if gotPath != "/api/services/media_player/toggle" {
		t.Errorf("service path = %q", gotPath)
	}
	if h.curPage != "controls" {
		t.Errorf("curPage = %q, want controls (service call keeps page)", h.curPage)
	}
}

func TestBackoff(t *testing.T) {
	base := 60 * time.Second
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, base},
		{1, 2 * base},
		{2, 4 * base},
		{3, 8 * base},
		{4, maxRenderBackoff},
		{50, maxRenderBackoff},
	}
	for _, tc := range tests {
		if got := backoff(base, tc.failures); got != tc.want {
			t.Errorf("backoff(%v, %d) = %v, want %v", base, tc.failures, got, tc.want)
		}
	}
	if got := backoff(time.Hour, 3); got != time.Hour {
		t.Errorf("a base above the cap must stay at base, got %v", got)
	}
}
