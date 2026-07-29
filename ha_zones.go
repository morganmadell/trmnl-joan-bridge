package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// zoneAction is what happens when a tap lands inside a zone: either switch to
// a different page (Page), or call a Home Assistant service against one
// entity (Domain/Service/Entity). Exactly one of the two should be set in a
// given zones.json entry.
type zoneAction struct {
	Page    string `json:"page,omitempty"`    // name of the page to switch to
	Domain  string `json:"domain,omitempty"`  // HA service domain, e.g. "media_player"
	Service string `json:"service,omitempty"` // HA service name, e.g. "toggle"
	Entity  string `json:"entity,omitempty"`  // entity_id the service call targets
}

// zone is a tappable rectangle in *display* coordinates — what a person
// looking at the rendered dashboard sees, i.e. already flipped from Joan's
// raw (180°-rotated) panel coordinates. X2/Y2 are exclusive.
type zone struct {
	X1     int        `json:"x1"`
	Y1     int        `json:"y1"`
	X2     int        `json:"x2"`
	Y2     int        `json:"y2"`
	Action zoneAction `json:"action"`
}

// page is one dashboard view: a URL path appended to HA_URL, and the zones
// that are tappable while it's showing.
type page struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // e.g. "/lovelace-joan/0"
	Zones []zone `json:"zones"`
}

// zoneConfig is the shape of zones.json: an ordered list of pages. Page 0 is
// shown first at startup.
type zoneConfig struct {
	Pages []page `json:"pages"`
}

func loadZoneConfig(path string) (*zoneConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read zone config %s: %w", path, err)
	}
	var zc zoneConfig
	if err := json.Unmarshal(b, &zc); err != nil {
		return nil, fmt.Errorf("parse zone config %s: %w", path, err)
	}
	if len(zc.Pages) == 0 {
		return nil, fmt.Errorf("zone config %s: no pages defined", path)
	}
	for i, p := range zc.Pages {
		if p.Name == "" || p.Path == "" {
			return nil, fmt.Errorf("zone config %s: page %d missing name or path", path, i)
		}
	}
	return &zc, nil
}

func (zc *zoneConfig) pageByName(name string) (*page, bool) {
	for i := range zc.Pages {
		if zc.Pages[i].Name == name {
			return &zc.Pages[i], true
		}
	}
	return nil, false
}

// hit returns the action for the first zone containing (x, y), or nil if the
// tap didn't land in any defined zone for this page.
func (p *page) hit(x, y int) *zoneAction {
	for i := range p.Zones {
		z := p.Zones[i]
		if x >= z.X1 && x < z.X2 && y >= z.Y1 && y < z.Y2 {
			return &z.Action
		}
	}
	return nil
}
