package main

import (
	"os"
	"path/filepath"
	"testing"
)

const testZonesJSON = `{
  "pages": [
    {
      "name": "sensors",
      "path": "/lovelace-joan/0",
      "zones": [
        {"x1": 0, "y1": 0, "x2": 342, "y2": 80, "action": {"page": "sensors"}},
        {"x1": 342, "y1": 0, "x2": 683, "y2": 80, "action": {"page": "controls"}},
        {"x1": 683, "y1": 0, "x2": 1024, "y2": 80, "action": {"page": "graphs"}}
      ]
    },
    {
      "name": "controls",
      "path": "/lovelace-joan/1",
      "zones": [
        {"x1": 0, "y1": 0, "x2": 342, "y2": 80, "action": {"page": "sensors"}},
        {"x1": 0, "y1": 80, "x2": 512, "y2": 419, "action": {"domain": "media_player", "service": "toggle", "entity": "media_player.boardroom_tv"}}
      ]
    },
    {
      "name": "graphs",
      "path": "/lovelace-joan/2",
      "zones": []
    }
  ]
}`

func writeTestZones(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zones.json")
	if err := os.WriteFile(path, []byte(testZonesJSON), 0o644); err != nil {
		t.Fatalf("write test zones file: %v", err)
	}
	return path
}

func TestLoadZoneConfig(t *testing.T) {
	zc, err := loadZoneConfig(writeTestZones(t))
	if err != nil {
		t.Fatalf("loadZoneConfig: %v", err)
	}
	if len(zc.Pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(zc.Pages))
	}
	if zc.Pages[0].Name != "sensors" || zc.Pages[0].Path != "/lovelace-joan/0" {
		t.Errorf("page 0 = %+v, want name=sensors path=/lovelace-joan/0", zc.Pages[0])
	}
}

func TestLoadZoneConfigMissingFile(t *testing.T) {
	if _, err := loadZoneConfig(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("want error for missing file, got nil")
	}
}

func TestLoadZoneConfigNoPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zones.json")
	os.WriteFile(path, []byte(`{"pages": []}`), 0o644)
	if _, err := loadZoneConfig(path); err == nil {
		t.Fatal("want error for zero pages, got nil")
	}
}

func TestPageByName(t *testing.T) {
	zc, err := loadZoneConfig(writeTestZones(t))
	if err != nil {
		t.Fatalf("loadZoneConfig: %v", err)
	}
	if _, ok := zc.pageByName("controls"); !ok {
		t.Error("pageByName(controls) not found")
	}
	if _, ok := zc.pageByName("nonexistent"); ok {
		t.Error("pageByName(nonexistent) unexpectedly found")
	}
}

func TestZoneHit(t *testing.T) {
	zc, err := loadZoneConfig(writeTestZones(t))
	if err != nil {
		t.Fatalf("loadZoneConfig: %v", err)
	}
	sensors, _ := zc.pageByName("sensors")

	tests := []struct {
		x, y     int
		wantPage string
		wantNil  bool
	}{
		{100, 40, "sensors", false},  // left nav third
		{500, 40, "controls", false}, // middle nav third
		{900, 40, "graphs", false},   // right nav third
		{500, 500, "", true},        // below the nav bar — no zone on this page
		{341, 0, "sensors", false},  // still inside the first zone (x2=342 is exclusive)
		{342, 0, "controls", false}, // first pixel of the second zone
	}
	for _, tc := range tests {
		got := sensors.hit(tc.x, tc.y)
		if tc.wantNil {
			if got != nil {
				t.Errorf("hit(%d,%d) = %+v, want nil", tc.x, tc.y, got)
			}
			continue
		}
		if got == nil {
			t.Fatalf("hit(%d,%d) = nil, want page=%q", tc.x, tc.y, tc.wantPage)
		}
		if got.Page != tc.wantPage {
			t.Errorf("hit(%d,%d).Page = %q, want %q", tc.x, tc.y, got.Page, tc.wantPage)
		}
	}
}

func TestZoneHitServiceAction(t *testing.T) {
	zc, err := loadZoneConfig(writeTestZones(t))
	if err != nil {
		t.Fatalf("loadZoneConfig: %v", err)
	}
	controls, _ := zc.pageByName("controls")

	got := controls.hit(100, 200)
	if got == nil {
		t.Fatal("hit(100,200) on controls page = nil, want the media_player zone")
	}
	if got.Domain != "media_player" || got.Service != "toggle" || got.Entity != "media_player.boardroom_tv" {
		t.Errorf("action = %+v, want domain=media_player service=toggle entity=media_player.boardroom_tv", got)
	}
}
