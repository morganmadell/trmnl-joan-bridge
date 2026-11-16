package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"log"
	"os"
	"time"

	"github.com/chromedp/chromedp"
	"trmnl-joan-bridge/pv3"
)

// @brief Screenshot a Home Assistant dashboard page with headless Chromium.
//
// @details Auth: the Home Assistant frontend reads a "hassTokens" localStorage entry to start a
// session without a login form. localStorage is per origin, so the token is set on a page loaded
// from haURL, and then the dashboard path is loaded. The token value must be a JS string literal (a
// JSON-encoded JSON string). A raw object is coerced to "[object Object]". Both mistakes were real
// bugs on first test (see Claude_Work.md). Set DEBUG_SAVE_SCREENSHOTS to a directory to keep the
// last render.
//
// @param chromiumBin Chromium executable name or path (differs by base image, see Dockerfile).
// @param haURL Home Assistant base URL without a trailing slash.
// @param haToken Long-lived access token.
// @param dashboardPath Dashboard path appended to haURL.
// @param waitMs Settle time in milliseconds after navigation. The overall timeout is this plus 15 seconds.
// @return The screenshot as an image of the panel size, or an error.
func renderDashboard(chromiumBin, haURL, haToken, dashboardPath string, waitMs int) (image.Image, error) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromiumBin),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true), // required to run Chromium as root in a minimal container
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.WindowSize(pv3.PanelW, pv3.PanelH),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()
	ctx, cancelTimeout := context.WithTimeout(ctx, time.Duration(waitMs)*time.Millisecond+15*time.Second)
	defer cancelTimeout()

	setTokenJS, err := hassTokensJS(haURL, haToken)
	if err != nil {
		return nil, err
	}

	var buf []byte
	var sections int
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(int64(pv3.PanelW), int64(pv3.PanelH)),
		chromedp.Navigate(haURL+"/"),
		chromedp.Evaluate(setTokenJS, nil),
		chromedp.Navigate(haURL+dashboardPath),
		chromedp.Sleep(time.Duration(waitMs)*time.Millisecond),
		chromedp.Evaluate(countSectionsJS, &sections),
		chromedp.Evaluate(kioskModeJS(pv3.PanelW, pv3.PanelH), nil),
		chromedp.Sleep(300*time.Millisecond), // let layout settle after the style overrides
		chromedp.CaptureScreenshot(&buf),
	)
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", dashboardPath, err)
	}
	if sections == 0 {
		return nil, fmt.Errorf("render %s: no dashboard section on the page (login screen, wrong path, or a changed Home Assistant frontend)", dashboardPath)
	}

	if saveDir := env("DEBUG_SAVE_SCREENSHOTS", ""); saveDir != "" {
		saveDebugScreenshot(saveDir, buf)
	}

	// image/png and image/jpeg decoders are registered by pv3's blank imports.
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	if !leftStripBlank(img, leftStripWidth) {
		log.Printf("render %s: the left edge is not blank; kiosk mode may have stopped hiding the Home Assistant sidebar", dashboardPath)
	}
	return img, nil
}

// leftStripWidth is the width in pixels of the left edge that must stay blank in a kiosk-mode render.
const leftStripWidth = 8

// countSectionsJS counts the dashboard sections on the page. Zero means the dashboard did not render.
const countSectionsJS = `
(function() {
  function q(root, tag, out) {
    Array.from(root.children || []).forEach(function(el) {
      if (el.tagName === tag) out.push(el);
      q(el, tag, out);
      if (el.shadowRoot) q(el.shadowRoot, tag, out);
    });
  }
  var found = [];
  q(document.body, 'HUI-GRID-SECTION', found);
  return found.length;
})()
`

// @brief Check that the left edge of a render is blank.
//
// @details The dashboard layout keeps a margin on the left. Anything dark in the first pixels means
// the Home Assistant sidebar is visible again, for example after a frontend update that renamed the
// element that kioskModeJS hides.
//
// @param img Rendered screenshot.
// @param width Width in pixels of the left strip to check.
// @return True when every pixel in the strip is light (luminance 240 or more).
func leftStripBlank(img image.Image, width int) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < min(b.Min.X+width, b.Max.X); x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if (299*r+587*g+114*bl)/1000>>8 < 240 {
				return false
			}
		}
	}
	return true
}

// @brief Build the JavaScript that signs the Home Assistant frontend in with a long-lived token.
//
// @details The frontend reads the "hassTokens" localStorage entry (same mechanism as the companion
// apps' web view). The value must be a JS string literal that holds JSON. A bare object literal is
// coerced to "[object Object]" and the frontend shows the login page.
//
// @param haURL Home Assistant base URL.
// @param haToken Long-lived access token.
// @return JavaScript source for chromedp.Evaluate, or an error when JSON encoding fails.
func hassTokensJS(haURL, haToken string) (string, error) {
	// HA frontend's expected shape for a pre-authenticated session. expires is
	// set far in the future (there's no refresh_token behind a long-lived
	// access token, so we never want the frontend to try to silently refresh).
	tokens, err := json.Marshal(map[string]any{
		"access_token":  haToken,
		"token_type":    "Bearer",
		"expires_in":    315360000, // 10 years, seconds — shape-only; see "expires" below
		"hassUrl":       haURL,
		"clientId":      haURL + "/",
		"expires":       time.Now().AddDate(50, 0, 0).UnixMilli(),
		"refresh_token": "",
	})
	if err != nil {
		return "", fmt.Errorf("encode hassTokens: %w", err)
	}
	// tokens is JSON bytes describing the object; localStorage.setItem needs a
	// *string* value (HA's frontend JSON.parses it back), so it must be
	// embedded as a JS string literal, not spliced in as a bare object literal
	// (which JS would silently coerce to the useless string "[object Object]").
	// Marshaling it a second time produces a properly quoted/escaped JS string.
	tokensAsJSString, err := json.Marshal(string(tokens))
	if err != nil {
		return "", fmt.Errorf("encode hassTokens string literal: %w", err)
	}
	return fmt.Sprintf(`localStorage.setItem("hassTokens", %s)`, tokensAsJSString), nil
}

// @brief Build the JavaScript that removes Home Assistant's app chrome and fills the panel.
//
// @details kioskModeJS strips Home Assistant's normal app chrome (the left sidebar, the per-view tab
// strip, and the top toolbar's search/edit/assist icons) and expands the dashboard content to fill
// the full panel — a small e-ink display has no use for navigation chrome meant for a desktop
// browser, and leaving it in place wasted about a quarter of the screen's width on the sidebar
// alone. Home Assistant's layout assigns the content pane a fixed width/offset (not a flexible one),
// so hiding the sidebar alone doesn't reclaim its space — the content pane and the sections view's
// own internal wrapper both need to be forced to the full panel width explicitly. Found by
// inspecting the actual shadow-DOM tree of a live render; the exact element/class names (ha-sidebar,
// hui-sections-view, etc.) are current frontend internals, not a stable public API, and could shift
// in a future HA release — if a render comes back with the sidebar back or squeezed content, this is
// the first place to check.
//
// @param panelW Panel width in pixels.
// @param panelH Panel height in pixels.
// @return JavaScript source for chromedp.Evaluate.
func kioskModeJS(panelW, panelH int) string {
	return fmt.Sprintf(`
(function() {
  function deepQuery(root, tag, out) {
    Array.from(root.children || []).forEach(function(el) {
      if (el.tagName === tag) out.push(el);
      deepQuery(el, tag, out);
      if (el.shadowRoot) deepQuery(el.shadowRoot, tag, out);
    });
  }

  var sidebars = [];
  deepQuery(document.body, 'HA-SIDEBAR', sidebars);
  sidebars.forEach(function(el) { el.style.display = 'none'; });

  var tabGroups = [];
  deepQuery(document.body, 'HA-TAB-GROUP', tabGroups);
  tabGroups.forEach(function(el) { el.style.display = 'none'; });

  var plainDivs = [];
  deepQuery(document.body, 'DIV', plainDivs);
  plainDivs.forEach(function(el) {
    // 'header': the top toolbar (tabs + search/edit/assist icons). Leftover
    // 'sidebar-shell': an empty 256px-wide placeholder in ha-drawer's
    // own layout div that isn't the visible ha-sidebar itself, but still
    // reserves its width, leaving a stray vertical divider line behind after
    // the sidebar is hidden.
    if (el.className === 'header' || el.className === 'sidebar-shell') {
      el.style.display = 'none';
    }
  });

  var content = [];
  deepQuery(document.body, 'PARTIAL-PANEL-RESOLVER', content);
  content.forEach(function(el) {
    el.style.position = 'fixed';
    el.style.left = '0px';
    el.style.top = '0px';
    el.style.width = '%dpx';
    el.style.height = '%dpx';
  });

  var sectionsViews = [];
  deepQuery(document.body, 'HUI-SECTIONS-VIEW', sectionsViews);
  sectionsViews.forEach(function(el) {
    var w = el.shadowRoot ? el.shadowRoot.querySelector('.wrapper') : null;
    if (w) { w.style.maxWidth = 'none'; w.style.width = '100%%'; w.style.margin = '0'; }
    var c = el.shadowRoot ? el.shadowRoot.querySelector('.container') : null;
    if (c) { c.style.maxWidth = 'none'; c.style.width = '100%%'; c.style.margin = '0'; c.style.padding = '0 16px'; }
  });

  // Each section's own grid container (hui-grid-section) renders at exactly
  // the panel's full width but offset ~48px from the left (some other
  // default padding this override doesn't reach), so its right edge extends
  // past the physical screen edge by that same 48px — cards inside just
  // inherit that overflow. Clamp each section's width to end exactly at the
  // panel edge, symmetric with its own left inset, instead of chasing every
  // card type individually.
  var gridSections = [];
  deepQuery(document.body, 'HUI-GRID-SECTION', gridSections);
  gridSections.forEach(function(el) {
    var r = el.getBoundingClientRect();
    if (r.right > %[1]d) {
      el.style.boxSizing = 'border-box';
      el.style.width = (%[1]d - 2 * r.left) + 'px';
    }
  });
})()
`, panelW, panelH)
}

// @brief Write the last render to DIR/latest-render.png, overwriting the previous one.
//
// @details Use it to tell a login screen from a real layout problem. Write errors are logged, not
// returned.
//
// @param dir Existing, writable directory (DEBUG_SAVE_SCREENSHOTS).
// @param png PNG bytes of the render.
func saveDebugScreenshot(dir string, png []byte) {
	dest := dir + "/latest-render.png"
	if err := os.WriteFile(dest, png, 0o644); err != nil {
		log.Printf("debug screenshot: write %s: %v", dest, err)
	}
}
