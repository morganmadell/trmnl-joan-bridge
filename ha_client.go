package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"trmnl-joan-bridge/pv3"
)

// haClient renders a Home Assistant Lovelace dashboard with a headless
// Chromium (see ha_render.go) and feeds the screenshot through the same
// pv3.Pack/EncodeFramePacked pipeline trmnlClient uses for TRMNL images. It
// owns the "current page" state and routes taps through the zone config (see
// ha_zones.go): a tap can switch pages or fire a Home Assistant service call,
// and either way triggers an immediate re-render so the change is visible
// well before Joan's next ~3-minute heartbeat.
type haClient struct {
	haURL       string
	haToken     string
	chromiumBin string
	waitMs      int
	fallback    time.Duration
	zones       *zoneConfig

	mu      sync.Mutex
	curPage string
}

// newHAClient reads HA_URL, HA_TOKEN, ZONES_FILE, CHROMIUM_BIN, and
// RENDER_WAIT_MS from the environment (see README), starts the local
// auth-redirect server, and returns a client ready for refresh()/loop().
// Fatal on missing required config or an unreadable/invalid zone file —
// there's no reasonable way to run in HA mode without them.
func newHAClient(fallback time.Duration) *haClient {
	haURL := strings.TrimRight(env("HA_URL", ""), "/")
	haToken := env("HA_TOKEN", "")
	if haURL == "" || haToken == "" {
		log.Fatal("source=ha requires: HA_URL, HA_TOKEN (a Home Assistant long-lived access token — Profile → Security → Long-Lived Access Tokens)")
	}

	zonesPath := env("ZONES_FILE", "zones.json")
	zc, err := loadZoneConfig(zonesPath)
	if err != nil {
		log.Fatalf("load zone config: %v", err)
	}

	return &haClient{
		haURL:       haURL,
		haToken:     haToken,
		chromiumBin: env("CHROMIUM_BIN", "chromium"),
		waitMs:      envInt("RENDER_WAIT_MS", 4000),
		fallback:    fallback,
		zones:       zc,
		curPage:     zc.Pages[0].Name,
	}
}

func (h *haClient) currentPage() *page {
	h.mu.Lock()
	name := h.curPage
	h.mu.Unlock()
	if p, ok := h.zones.pageByName(name); ok {
		return p
	}
	return &h.zones.Pages[0] // config changed under us or curPage stale; fall back to page 0
}

// refresh satisfies contentSource: render the current page and store it.
func (h *haClient) refresh(fc *frameStore) error {
	p := h.currentPage()
	img, err := renderDashboard(h.chromiumBin, h.haURL, h.haToken, p.Path, h.waitMs)
	if err != nil {
		return err
	}
	packed := pv3.Pack(img)
	fc.set(packed, pv3.EncodeFramePacked(packed))
	log.Printf("rendered page %q (%s)", p.Name, p.Path)
	return nil
}

// loop satisfies contentSource: re-render on a fixed cadence. Unlike TRMNL
// (which tells us a refresh_rate), Home Assistant has no equivalent, so this
// is just REFRESH_INTERVAL — tune it to balance freshness against e-ink
// ghosting and battery life (see Instructions.md Step 5).
func (h *haClient) loop(fc *frameStore) {
	for {
		time.Sleep(h.fallback)
		if err := h.refresh(fc); err != nil {
			log.Printf("HA render failed: %v", err)
		}
	}
}

// onTouch satisfies contentSource. Joan reports taps in raw panel
// coordinates, rotated 180° from what's displayed (see
// Protocol_Bypass_Research.md §6); this flips them into display space before
// hit-testing the current page's zones. The flip uses the panel's nominal
// dimensions — real taps near the edges may need a small calibration offset
// once you can see actual behavior on your device; adjust here if so.
func (h *haClient) onTouch(x, y int, fc *frameStore) {
	dispX, dispY := pv3.PanelW-x, pv3.PanelH-y

	p := h.currentPage()
	action := p.hit(dispX, dispY)
	if action == nil {
		log.Printf("tap (%d,%d) [raw %d,%d] hit no zone on page %q", dispX, dispY, x, y, p.Name)
		return
	}

	switch {
	case action.Page != "":
		h.mu.Lock()
		h.curPage = action.Page
		h.mu.Unlock()
		log.Printf("tap (%d,%d) → switching to page %q", dispX, dispY, action.Page)
	case action.Domain != "" && action.Service != "":
		if err := h.callService(action.Domain, action.Service, action.Entity); err != nil {
			log.Printf("tap (%d,%d) → HA service call %s.%s on %s failed: %v", dispX, dispY, action.Domain, action.Service, action.Entity, err)
		} else {
			log.Printf("tap (%d,%d) → called %s.%s on %s", dispX, dispY, action.Domain, action.Service, action.Entity)
		}
		time.Sleep(500 * time.Millisecond) // give HA a moment to apply the change before we re-render
	default:
		log.Printf("tap (%d,%d) hit zone with no action configured", dispX, dispY)
		return
	}

	if err := h.refresh(fc); err != nil {
		log.Printf("post-tap render failed: %v", err)
	}
}

// callService invokes a Home Assistant service via its REST API — the same
// long-lived token used for dashboard auth also authorizes this call. See
// https://developers.home-assistant.io/docs/api/rest/ for the endpoint shape.
func (h *haClient) callService(domain, service, entity string) error {
	payload, err := json.Marshal(map[string]string{"entity_id": entity})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", h.haURL+"/api/services/"+domain+"/"+service, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.haToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HA returned %s", resp.Status)
	}
	return nil
}
