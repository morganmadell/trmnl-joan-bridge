package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"

	"trmnl-joan-bridge/pv3"
)

// authRedirectAddr is the loopback-only address the bridge's tiny auth page
// listens on. Chromium is pointed at this instead of Home Assistant directly,
// so the long-lived access token never appears in a URL, shell history, or
// process argument list — only in this one server-rendered response, served
// on localhost only and never exposed on the container's network interface.
const authRedirectAddr = "127.0.0.1:38911"

// startAuthServer runs the local page that logs a headless Chromium session
// into Home Assistant and redirects it to the requested dashboard path. Call
// it in a goroutine; it never returns.
//
// CAVEAT: this uses the community-documented pattern of writing HA's frontend
// "hassTokens" localStorage entry directly (the same mechanism the official
// Android/iOS companion apps' web view and various Raspberry-Pi kiosk-mode
// guides use), rather than a supported public API for headless dashboard
// auth. It has NOT been verified against a real HA frontend build as part of
// this work — treat it as the first thing to check if a render comes back
// showing a login screen instead of a dashboard (see Instructions.md's Path C
// build/test steps for how to inspect a saved screenshot).
func startAuthServer(haURL, haToken string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/authredirect", authRedirectHandler(haURL, haToken))

	log.Printf("local HA auth-redirect page listening on %s (loopback only)", authRedirectAddr)
	if err := http.ListenAndServe(authRedirectAddr, mux); err != nil {
		log.Fatalf("auth server: %v", err)
	}
}

func authRedirectHandler(haURL, haToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}

		// HA frontend's expected shape for a pre-authenticated session. expires
		// is set far in the future (there's no refresh_token behind a
		// long-lived access token, so we never want the frontend to try to
		// silently refresh).
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
			http.Error(w, "token encode failed", http.StatusInternalServerError)
			return
		}
		target, err := json.Marshal(haURL + path)
		if err != nil {
			http.Error(w, "target encode failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"></head><body>
<script>
localStorage.setItem("hassTokens", JSON.stringify(%s));
location.replace(%s);
</script>
</body></html>`, tokens, target)
	}
}

// renderDashboard screenshots the given Home Assistant dashboard path with a
// headless Chromium (via the local auth-redirect page above) and returns the
// decoded image, sized to the Joan 6 panel's native resolution.
//
// chromiumBin is the executable name/path (varies by base image — see
// Dockerfile). waitMs bounds both Chromium's --virtual-time-budget (how long
// it lets the page's JS/network activity run before capturing) and the
// overall subprocess timeout; Home Assistant's frontend is a full SPA that
// needs a real websocket connection to populate, so this needs to be several
// seconds, not milliseconds — tune it empirically against your real dashboard.
func renderDashboard(chromiumBin, dashboardPath string, waitMs int) (image.Image, error) {
	tmpFile, err := os.CreateTemp("", "joan-render-*.png")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	target := fmt.Sprintf("http://%s/authredirect?path=%s", authRedirectAddr, url.QueryEscape(dashboardPath))

	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox", // required to run Chromium as root in a minimal container
		"--hide-scrollbars",
		"--disable-dev-shm-usage",
		fmt.Sprintf("--window-size=%d,%d", pv3.PanelW, pv3.PanelH),
		fmt.Sprintf("--virtual-time-budget=%d", waitMs),
		"--screenshot=" + tmpPath,
		target,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(waitMs)*time.Millisecond+15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, chromiumBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s screenshot failed: %w (output: %s)", chromiumBin, err, out)
	}

	if saveDir := env("DEBUG_SAVE_SCREENSHOTS", ""); saveDir != "" {
		saveDebugScreenshot(saveDir, tmpPath)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("open screenshot: %w", err)
	}
	defer f.Close()

	// image/png and image/jpeg decoders are registered by pv3's blank imports.
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	return img, nil
}

// saveDebugScreenshot copies the just-captured render to a fixed, overwritten
// path so you can pull it off the container and look at it — e.g. to tell a
// stuck login screen apart from a real rendering problem. Set
// DEBUG_SAVE_SCREENSHOTS to a writable directory to enable this; leave unset
// in normal operation.
func saveDebugScreenshot(dir, tmpPath string) {
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		log.Printf("debug screenshot: read temp file: %v", err)
		return
	}
	dest := dir + "/latest-render.png"
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		log.Printf("debug screenshot: write %s: %v", dest, err)
	}
}
