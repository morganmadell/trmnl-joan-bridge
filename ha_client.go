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

// @brief Content source that renders a Home Assistant Lovelace dashboard for the panel.
//
// @details Screenshots the current page with headless Chromium (see ha_render.go) and encodes it
// with pv3.Pack and pv3.EncodeFramePacked, as trmnlClient does for TRMNL images. Owns the
// current-page state and routes taps through the zone config (see ha_zones.go). A tap that switches
// page or calls a service triggers an immediate re-render, so the change shows before the next
// heartbeat.
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

// @brief Build an haClient from environment variables.
//
// @details Reads HA_URL, HA_TOKEN, ZONES_FILE (default zones.json), CHROMIUM_BIN (default chromium),
// and RENDER_WAIT_MS (default 4000). Calls log.Fatal when HA_URL or HA_TOKEN is empty, or when the
// zone file cannot be loaded.
//
// @param fallback Interval between unprompted re-renders (REFRESH_INTERVAL).
// @return A client that shows the first page of the zone config.
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

// @brief Return the page that is currently shown.
//
// @return The page named by curPage. Never nil: falls back to page 0 when curPage names no page.
func (h *haClient) currentPage() *page {
	h.mu.Lock()
	name := h.curPage
	h.mu.Unlock()
	if p, ok := h.zones.pageByName(name); ok {
		return p
	}
	return &h.zones.Pages[0] // config changed under us or curPage stale; fall back to page 0
}

// @brief Render the current page and store the frame (satisfies contentSource).
//
// @param fc Frame store that receives the packed framebuffer and the full frame.
// @return The render error, or nil.
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

// @brief Re-render the current page forever, once per fallback interval (satisfies contentSource).
//
// @details Home Assistant gives no refresh rate, so the interval is REFRESH_INTERVAL. A longer
// interval gives less e-ink ghosting and less battery use. Render errors are logged and the loop
// continues, and the wait doubles after each failure in a row (see backoff). Never returns.
//
// @param fc Frame store to update.
func (h *haClient) loop(fc *frameStore) {
	failures := 0
	for {
		time.Sleep(backoff(h.fallback, failures))
		if err := h.refresh(fc); err != nil {
			failures++
			log.Printf("HA render failed (%d in a row): %v", failures, err)
			continue
		}
		failures = 0
	}
}

// maxRenderBackoff caps the wait between renders after repeated failures.
const maxRenderBackoff = 10 * time.Minute

// @brief Compute the wait before the next render after a number of failures in a row.
//
// @details A stuck Home Assistant makes every render fail. Without a back-off, each failure starts
// one more Chromium process, which is how the 2026-09-24 zombie build-up started.
//
// @param base Normal interval between renders.
// @param failures Number of failed renders in a row. Zero gives base.
// @return base doubled once per failure, capped at maxRenderBackoff (or at base when base is larger).
func backoff(base time.Duration, failures int) time.Duration {
	limit := max(maxRenderBackoff, base)
	d := base
	for i := 0; i < failures && d < limit; i++ {
		d *= 2
	}
	return min(d, limit)
}

// @brief Handle a tap (satisfies contentSource).
//
// @details Joan reports taps in raw panel coordinates, rotated 180 degrees from the display (see
// Protocol_Bypass_Research.md section 6). This function flips them with the nominal panel size,
// finds the zone on the current page, and either switches page or calls the zone's Home Assistant
// service. After an action it re-renders. A tap in no zone, or in a zone with no action, changes
// nothing.
//
// @param x Raw panel X coordinate from Joan.
// @param y Raw panel Y coordinate from Joan.
// @param fc Frame store to update after the tap.
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

// @brief Call a Home Assistant service through the REST API.
//
// @details POSTs {"entity_id": ...} to /api/services/{domain}/{service} with the long-lived token as
// a Bearer token. See https://developers.home-assistant.io/docs/api/rest/.
//
// @param domain Service domain, for example "media_player".
// @param service Service name, for example "toggle".
// @param entity The entity_id the service acts on.
// @return An error when the request fails or Home Assistant returns status 300 or higher.
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
