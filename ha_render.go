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

// renderDashboard screenshots a Home Assistant Lovelace dashboard with a real
// headless Chromium session (via the DevTools Protocol, through chromedp) and
// returns the decoded image, sized to the Joan 6 panel's native resolution.
//
// Auth: HA's frontend reads a "hassTokens" localStorage entry (the same
// community-documented mechanism the official companion apps' web view uses)
// to hydrate an authenticated session without a login form. localStorage is
// scoped per-origin, so this only works if it's written on a page already
// loaded from haURL itself — an earlier version of this file wrote it on a
// local loopback redirect page instead, which meant it never reached HA's
// origin at all (confirmed by an actual login-page screenshot when first
// tested). This version navigates to haURL first, injects the token there via
// chromedp.Evaluate (same origin, so it sticks), then navigates to the
// dashboard path.
//
// chromiumBin is the executable name/path (varies by base image — see
// Dockerfile). waitMs bounds both the settle time after navigating to the
// dashboard (Home Assistant's frontend is a full SPA that needs a real
// websocket connection to populate) and the overall context timeout — tune it
// empirically against your real dashboard.
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
		return nil, fmt.Errorf("encode hassTokens: %w", err)
	}
	// tokens is JSON bytes describing the object; localStorage.setItem needs a
	// *string* value (HA's frontend JSON.parses it back), so it must be
	// embedded as a JS string literal, not spliced in as a bare object literal
	// (which JS would silently coerce to the useless string "[object Object]").
	// Marshaling it a second time produces a properly quoted/escaped JS string.
	tokensAsJSString, err := json.Marshal(string(tokens))
	if err != nil {
		return nil, fmt.Errorf("encode hassTokens string literal: %w", err)
	}
	setTokenJS := fmt.Sprintf(`localStorage.setItem("hassTokens", %s)`, tokensAsJSString)

	var buf []byte
	err = chromedp.Run(ctx,
		chromedp.EmulateViewport(int64(pv3.PanelW), int64(pv3.PanelH)),
		chromedp.Navigate(haURL+"/"),
		chromedp.Evaluate(setTokenJS, nil),
		chromedp.Navigate(haURL+dashboardPath),
		chromedp.Sleep(time.Duration(waitMs)*time.Millisecond),
		chromedp.Evaluate(kioskModeJS(pv3.PanelW, pv3.PanelH), nil),
		chromedp.Sleep(300*time.Millisecond), // let layout settle after the style overrides
		chromedp.CaptureScreenshot(&buf),
	)
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", dashboardPath, err)
	}

	if saveDir := env("DEBUG_SAVE_SCREENSHOTS", ""); saveDir != "" {
		saveDebugScreenshot(saveDir, buf)
	}

	// image/png and image/jpeg decoders are registered by pv3's blank imports.
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	return img, nil
}

// kioskModeJS strips Home Assistant's normal app chrome (the left sidebar,
// the per-view tab strip, and the top toolbar's search/edit/assist icons) and
// expands the dashboard content to fill the full panel — a small e-ink
// display has no use for navigation chrome meant for a desktop browser, and
// leaving it in place wasted about a quarter of the screen's width on the
// sidebar alone. Home Assistant's layout assigns the content pane a fixed
// width/offset (not a flexible one), so hiding the sidebar alone doesn't
// reclaim its space — the content pane and the sections view's own internal
// wrapper both need to be forced to the full panel width explicitly. Found
// by inspecting the actual shadow-DOM tree of a live render; the exact
// element/class names (ha-sidebar, hui-sections-view, etc.) are current
// frontend internals, not a stable public API, and could shift in a future
// HA release — if a render comes back with the sidebar back or squeezed
// content, this is the first place to check.
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

// saveDebugScreenshot writes the just-captured render to a fixed, overwritten
// path so you can look at it — e.g. to tell a stuck login screen apart from a
// real rendering problem. Set DEBUG_SAVE_SCREENSHOTS to a writable directory
// to enable this; leave unset in normal operation.
func saveDebugScreenshot(dir string, png []byte) {
	dest := dir + "/latest-render.png"
	if err := os.WriteFile(dest, png, 0o644); err != nil {
		log.Printf("debug screenshot: write %s: %v", dest, err)
	}
}
