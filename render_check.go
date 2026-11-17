package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"trmnl-joan-bridge/pv3"
)

// inkThreshold is the luminance (0-255) below which a pixel counts as ink. It is the DarkThreshold of tools/measure_zones.ps1, which measured the zones, so the check sees the same text.
const inkThreshold = 150

// @brief Render every page in the zone config, check the layout, and report the result.
//
// @details Run this after each Home Assistant upgrade. An upgrade can rename the elements that
// kioskModeJS hides or move the cards, and then taps land on the wrong item. This function only
// renders pages. It never calls a Home Assistant service. Configuration comes from the same
// environment variables as the bridge (see newHAClient).
//
// @param dir Directory for the rendered pages, one PAGE.png for each page (PAGE is the page name). Created when missing.
// @return 0 when every page renders and passes every check, 1 otherwise. Use it as the exit code.
func runRenderCheck(dir string) int {
	h := newHAClient(0)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		return 1
	}
	code := 0
	for i := range h.zones.Pages {
		p := &h.zones.Pages[i]
		img, err := renderDashboard(h.chromiumBin, h.haURL, h.haToken, p.Path, h.waitMs)
		if err != nil {
			fmt.Printf("FAIL page %q: %v\n", p.Name, err)
			code = 1
			continue
		}
		out := filepath.Join(dir, p.Name+".png")
		if err := writePNG(out, img); err != nil {
			fmt.Printf("FAIL page %q: %v\n", p.Name, err)
			code = 1
			continue
		}
		failures := checkRender(img, p)
		if len(failures) == 0 {
			fmt.Printf("PASS page %q: %d zones (%s)\n", p.Name, len(p.Zones), out)
			continue
		}
		code = 1
		fmt.Printf("FAIL page %q (%s):\n", p.Name, out)
		for _, f := range failures {
			fmt.Printf("  - %s\n", f)
		}
	}
	return code
}

// @brief Write an image to a PNG file.
//
// @param path Output file path.
// @param img Image to encode.
// @return The create, encode, or close error, or nil.
func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// @brief Check a rendered page against the zones of its page config.
//
// @details A zone that has no ink lost its label. Ink on the top or bottom row of a zone, or on a
// vertical edge between two zones, means a label moved across a zone border, so a tap on that label
// can hit the wrong zone. The outer left and right edges are not checked, because a full-width row
// can have ink at the panel edge.
//
// @param img Rendered page.
// @param p Page config with the zones to check.
// @return One message for each failed check. Empty when the render passes.
func checkRender(img image.Image, p *page) []string {
	b := img.Bounds()
	if b.Dx() != pv3.PanelW || b.Dy() != pv3.PanelH {
		return []string{fmt.Sprintf("image is %dx%d, want %dx%d", b.Dx(), b.Dy(), pv3.PanelW, pv3.PanelH)}
	}
	var failures []string
	if !leftStripBlank(img, leftStripWidth) {
		failures = append(failures, fmt.Sprintf("left %d px are not blank: the Home Assistant sidebar may be visible (see kioskModeJS)", leftStripWidth))
	}
	for i, z := range p.Zones {
		name := fmt.Sprintf("zone %d (%d,%d)-(%d,%d)", i, z.X1, z.Y1, z.X2, z.Y2)
		if z.X1 < 0 || z.Y1 < 0 || z.X2 > pv3.PanelW || z.Y2 > pv3.PanelH || z.X1 >= z.X2 || z.Y1 >= z.Y2 {
			failures = append(failures, name+": not inside the image")
			continue
		}
		if n, _ := inkIn(img, image.Rect(z.X1, z.Y1, z.X2, z.Y2)); n == 0 {
			failures = append(failures, name+": no ink in the zone")
		}
		edges := []struct {
			label string
			r     image.Rectangle
			check bool
		}{
			{fmt.Sprintf("top row y=%d", z.Y1), image.Rect(z.X1, z.Y1, z.X2, z.Y1+1), true},
			{fmt.Sprintf("bottom row y=%d", z.Y2-1), image.Rect(z.X1, z.Y2-1, z.X2, z.Y2), true},
			{fmt.Sprintf("left column x=%d", z.X1), image.Rect(z.X1, z.Y1, z.X1+1, z.Y2), z.X1 > 0},
			{fmt.Sprintf("right column x=%d", z.X2-1), image.Rect(z.X2-1, z.Y1, z.X2, z.Y2), z.X2 < pv3.PanelW},
		}
		for _, e := range edges {
			if !e.check {
				continue
			}
			if n, at := inkIn(img, e.r); n > 0 {
				failures = append(failures, fmt.Sprintf("%s: %d ink pixels on the %s, first at (%d,%d) luminance %d", name, n, e.label, at.X, at.Y, luminance(img.At(b.Min.X+at.X, b.Min.Y+at.Y))))
			}
		}
	}
	return failures
}

// @brief Count the ink pixels in a rectangle.
//
// @param img Image to scan.
// @param r Rectangle relative to the top-left corner of img. Max is exclusive.
// @return The ink pixel count and the first ink pixel found, row by row, relative to img.
func inkIn(img image.Image, r image.Rectangle) (int, image.Point) {
	o := img.Bounds().Min
	n := 0
	var first image.Point
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if luminance(img.At(o.X+x, o.Y+y)) < inkThreshold {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, first
}

// @brief Compute the luminance of a color.
//
// @details The weights are the ones in tools/measure_zones.ps1 (0.30, 0.59, 0.11), so the check and
// the zone measurement agree on which pixels are ink.
//
// @param c Color to measure.
// @return Luminance from 0 (black) to 255 (white).
func luminance(c color.Color) int {
	r, g, b, _ := c.RGBA()
	return int(30*(r>>8)+59*(g>>8)+11*(b>>8)) / 100
}
