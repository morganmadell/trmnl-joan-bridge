package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// @brief What happens when a tap lands in a zone.
//
// @details Set Page to switch to another page, or set Domain, Service, and Entity to call a Home
// Assistant service. Set exactly one of the two in each zones.json entry.
type zoneAction struct {
	Page    string `json:"page,omitempty"`    // name of the page to switch to
	Domain  string `json:"domain,omitempty"`  // HA service domain, e.g. "media_player"
	Service string `json:"service,omitempty"` // HA service name, e.g. "toggle"
	Entity  string `json:"entity,omitempty"`  // entity_id the service call targets
}

// @brief A tappable rectangle in display coordinates.
//
// @details Display coordinates are what a person sees on the rendered dashboard, already flipped
// from the raw 180-degree-rotated panel coordinates. X2 and Y2 are exclusive.
type zone struct {
	X1     int        `json:"x1"`
	Y1     int        `json:"y1"`
	X2     int        `json:"x2"`
	Y2     int        `json:"y2"`
	Action zoneAction `json:"action"`
}

// @brief One dashboard view and its tappable zones.
//
// @details Path is appended to HA_URL, for example "/lovelace-joan/sensors".
type page struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // e.g. "/lovelace-joan/sensors"
	Zones []zone `json:"zones"`
}

// @brief The structure of zones.json: an ordered list of pages.
//
// @details Page 0 is shown first at startup.
type zoneConfig struct {
	Pages []page `json:"pages"`
}

// @brief Read and validate a zones.json file.
//
// @param path Path of the zone config file.
// @return The parsed config, or an error when the file is missing, is not valid JSON, has no pages, or has a page without a name or path.
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

// @brief Find a page by name.
//
// @param name Page name to look for.
// @return The page and true, or nil and false when no page has that name.
func (zc *zoneConfig) pageByName(name string) (*page, bool) {
	for i := range zc.Pages {
		if zc.Pages[i].Name == name {
			return &zc.Pages[i], true
		}
	}
	return nil, false
}

// @brief Find the zone that contains a point.
//
// @param x Display X coordinate.
// @param y Display Y coordinate.
// @return The action of the first zone that contains (x, y), or nil when no zone does.
func (p *page) hit(x, y int) *zoneAction {
	for i := range p.Zones {
		z := p.Zones[i]
		if x >= z.X1 && x < z.X2 && y >= z.Y1 && y < z.Y2 {
			return &z.Action
		}
	}
	return nil
}
