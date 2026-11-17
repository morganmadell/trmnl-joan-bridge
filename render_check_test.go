package main

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"trmnl-joan-bridge/pv3"
)

// @brief Make a white image with black rectangles.
//
// @param w Image width.
// @param h Image height.
// @param ink Rectangles to fill with black.
// @return The image.
func inkImage(w, h int, ink ...image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for _, r := range ink {
		draw.Draw(img, r, image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	return img
}

// checkPage has two side-by-side zones like the nav row, so the edge at x=356 is interior.
var checkPage = &page{Name: "test", Path: "/test", Zones: []zone{
	{X1: 0, Y1: 40, X2: 356, Y2: 118},
	{X1: 356, Y1: 40, X2: 668, Y2: 118},
}}

// checkLabels is label-sized ink inside each zone of checkPage, clear of every edge.
var checkLabels = []image.Rectangle{image.Rect(100, 60, 200, 90), image.Rect(450, 60, 550, 90)}

// @brief Run checkRender and require failures that contain want, or no failures when want is empty.
//
// @param t Test handle.
// @param img Image to check.
// @param p Page to check against.
// @param want Text that one failure must contain. Empty means the render must pass.
func expectCheck(t *testing.T, img image.Image, p *page, want string) {
	t.Helper()
	got := checkRender(img, p)
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("want pass, got %q", got)
		}
		return
	}
	for _, f := range got {
		if strings.Contains(f, want) {
			return
		}
	}
	t.Fatalf("want a failure with %q, got %q", want, got)
}

func TestCheckRenderInkInsideZonesPasses(t *testing.T) {
	expectCheck(t, inkImage(pv3.PanelW, pv3.PanelH, checkLabels...), checkPage, "")
}

func TestCheckRenderEmptyZoneFails(t *testing.T) {
	expectCheck(t, inkImage(pv3.PanelW, pv3.PanelH, checkLabels[0]), checkPage, "zone 1 (356,40)-(668,118): no ink")
}

func TestCheckRenderInkOnTopRowFails(t *testing.T) {
	img := inkImage(pv3.PanelW, pv3.PanelH, append(checkLabels, image.Rect(120, 30, 140, 50))...)
	expectCheck(t, img, checkPage, "top row y=40")
}

func TestCheckRenderInkOnBottomRowFails(t *testing.T) {
	img := inkImage(pv3.PanelW, pv3.PanelH, append(checkLabels, image.Rect(120, 110, 140, 125))...)
	expectCheck(t, img, checkPage, "bottom row y=117")
}

func TestCheckRenderInkOnInteriorEdgeFails(t *testing.T) {
	img := inkImage(pv3.PanelW, pv3.PanelH, append(checkLabels, image.Rect(350, 70, 360, 80))...)
	expectCheck(t, img, checkPage, "right column x=355")
	expectCheck(t, img, checkPage, "left column x=356")
}

// A full-width row can have ink at the panel edge. Only an edge shared with another zone is a border.
func TestCheckRenderOuterEdgeInkPasses(t *testing.T) {
	p := &page{Zones: []zone{{X1: 0, Y1: 150, X2: pv3.PanelW, Y2: 238}}}
	img := inkImage(pv3.PanelW, pv3.PanelH, image.Rect(pv3.PanelW-10, 170, pv3.PanelW, 200))
	expectCheck(t, img, p, "")
}

func TestCheckRenderWrongSizeFails(t *testing.T) {
	expectCheck(t, inkImage(800, 600, checkLabels...), checkPage, "image is 800x600")
}

func TestCheckRenderZoneOutOfBoundsFails(t *testing.T) {
	p := &page{Zones: []zone{{X1: 900, Y1: 700, X2: 1100, Y2: 760}}}
	expectCheck(t, inkImage(pv3.PanelW, pv3.PanelH), p, "not inside the image")
}

func TestCheckRenderDarkLeftStripFails(t *testing.T) {
	img := inkImage(pv3.PanelW, pv3.PanelH, append(checkLabels, image.Rect(0, 300, 4, 400))...)
	expectCheck(t, img, checkPage, "left 8 px are not blank")
}

func TestLuminanceThreshold(t *testing.T) {
	if l := luminance(color.Gray{Y: 149}); l != 149 {
		t.Errorf("gray 149: luminance %d, want 149", l)
	}
	if n, _ := inkIn(inkImage(4, 4), image.Rect(0, 0, 4, 4)); n != 0 {
		t.Errorf("white image: %d ink pixels, want 0", n)
	}
	img := image.NewGray(image.Rect(0, 0, 2, 1))
	img.SetGray(0, 0, color.Gray{Y: 149})
	img.SetGray(1, 0, color.Gray{Y: 150})
	if n, at := inkIn(img, img.Bounds()); n != 1 || at != (image.Point{}) {
		t.Errorf("gray 149 and 150: %d ink pixels first at %v, want 1 at (0,0)", n, at)
	}
}
