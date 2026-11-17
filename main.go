// trmnl-joan-bridge: standalone PV3 server for Joan 6 e-ink display.
//
// Fork note: adapted to drive the panel from a Home Assistant dashboard
// (screenshotted with a headless Chromium — see ha_client.go/ha_render.go)
// instead of TRMNL, while keeping TRMNL support available via -source=trmnl.
// See Instructions.md ("Path C") and Protocol_Bypass_Research.md in the
// parent joan_self_hosted project for the full story.
//
// Renders the current content source's image, encodes it into a Visionect
// PV3 frame, and serves it to Joan over TCP:11112. The heartbeat loop keeps
// the connection alive between Joan's 3-minute hello cycles, and a screen tap
// is routed to the active content source (page switch / HA service call for
// Home Assistant; playlist advance for TRMNL).
//
// The PV3 wire protocol — framing, message decode, frame encode, the session
// ACK — lives in package pv3. This file is the application: the content
// source selection, frame cache, and per-connection heartbeat loop.
//
// Configuration (env vars; flags override):
//
//	SOURCE            "ha" (default) or "trmnl"
//	REFRESH_INTERVAL  Fallback re-render/re-fetch interval (default: 60s)
//	LISTEN_ADDR       TCP address to bind (default: :11112)
//
//	Home Assistant mode (SOURCE=ha):
//	HA_URL              Home Assistant base URL (e.g. http://10.218.10.81:8123)
//	HA_TOKEN            Long-lived access token (Profile → Security)
//	ZONES_FILE          Path to the page/tap-zone config (default: zones.json)
//	CHROMIUM_BIN        Chromium executable name/path (default: chromium)
//	RENDER_WAIT_MS      Time to let the dashboard load before screenshotting (default: 4000)
//	DEBUG_SAVE_SCREENSHOTS  If set, a directory to save the latest render to for inspection
//
//	TRMNL mode (SOURCE=trmnl):
//	TRMNL_SERVER      TRMNL base URL (e.g. http://192.168.1.210:2300)
//	DEVICE_ID         Joan MAC address uppercase (e.g. 42:00:28:00:0D:51)
//	ACCESS_TOKEN      TRMNL device access token
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"trmnl-joan-bridge/pv3"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// @brief Source of the current image and handler of screen taps.
//
// @details Methods: refresh(fc) renders or fetches one image into fc. loop(fc) refreshes forever on
// a cadence. onTouch(x, y, fc) reacts to a tap. haClient (default) and trmnlClient (source=trmnl)
// implement it. main picks one from -source or SOURCE.
type contentSource interface {
	refresh(fc *frameStore) error
	loop(fc *frameStore)
	onTouch(x, y int, fc *frameStore)
}

// @brief Program entry: choose the content source, wait for a first frame, and serve Joan connections.
//
// @details Flags default to the matching environment variables. Retries the first refresh every 5 seconds after an error, and every 10 seconds while no frame exists. Then starts the refresh loop, the zombie reaper, and the TCP
// listener (default :11112).
func main() {
	source := flag.String("source", env("SOURCE", "ha"), `content source: "ha" (Home Assistant, default) or "trmnl"`)

	// TRMNL-mode flags (only required when -source=trmnl).
	trmnlServer := flag.String("trmnl-server", env("TRMNL_SERVER", ""), "TRMNL base URL (e.g. http://192.168.1.210:2300)")
	deviceID := flag.String("device-id", env("DEVICE_ID", ""), "Joan MAC address uppercase (e.g. 42:00:28:00:0D:51)")
	accessToken := flag.String("access-token", env("ACCESS_TOKEN", ""), "TRMNL device access token")

	refresh := flag.Duration("refresh", envDuration("REFRESH_INTERVAL", 60*time.Second), "Fallback refresh interval (HA: between unprompted re-renders; TRMNL: when it omits refresh_rate)")
	addr := flag.String("addr", env("LISTEN_ADDR", ":11112"), "TCP listen address")
	renderCheck := flag.String("render-check", "", "render every page in zones.json to this directory, check the layout, and exit")
	flag.Parse()

	// Exit before the listener starts, so a check can run beside the live bridge.
	if *renderCheck != "" {
		os.Exit(runRenderCheck(*renderCheck))
	}

	fc := &frameStore{}
	st := &deviceStatus{}

	var cs contentSource
	switch *source {
	case "ha":
		cs = newHAClient(*refresh)
		log.Print("content source: Home Assistant")
	case "trmnl":
		if *trmnlServer == "" || *deviceID == "" || *accessToken == "" {
			log.Fatal("source=trmnl requires: -trmnl-server, -device-id, -access-token")
		}
		cs = &trmnlClient{server: *trmnlServer, deviceID: *deviceID, token: *accessToken, fallback: *refresh, status: st}
		log.Printf("content source: TRMNL at %s (device %s)", *trmnlServer, *deviceID)
	default:
		log.Fatalf("unknown -source %q (want %q or %q)", *source, "ha", "trmnl")
	}

	for {
		if err := cs.refresh(fc); err != nil {
			log.Printf("initial fetch failed: %v — retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		_, full, _, _ := fc.load()
		if len(full) == 0 {
			log.Printf("no image ready yet — retrying in 10s")
			time.Sleep(10 * time.Second)
			continue
		}
		log.Printf("initial frame ready (%d bytes)", len(full))
		break
	}
	go cs.loop(fc)
	go reapZombies()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	log.Printf("listening on %s", *addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(conn, fc, st, cs)
	}
}

// @brief Serve one Joan connection until it closes.
//
// @param conn Accepted socket. Closed on return.
// @param fc Shared frame store.
// @param st Shared device telemetry.
// @param cs Active content source that receives taps.
func handleConn(conn net.Conn, fc *frameStore, st *deviceStatus, cs contentSource) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	log.Printf("[%s] connected", remote)

	s := &netSession{conn: conn, r: bufio.NewReader(conn)}
	serve(s, fc, st, func(x, y int) { cs.onTouch(x, y, fc) }, remote)
}

// @brief Per-connection transport that serve needs.
//
// @details read(timeout) returns the next PV3 message, waiting at most timeout. write(p) sends raw
// bytes. netSession implements it for a real socket. Tests use a scripted fake, so serve can be
// tested without a network.
type session interface {
	read(timeout time.Duration) (pv3.Message, error)
	write(p []byte) error
}

// @brief session implementation over a net.Conn.
type netSession struct {
	conn net.Conn
	r    *bufio.Reader
}

// @brief Set the read deadline, then decode one framed PV3 message.
//
// @param timeout Maximum wait for a message.
// @return The message, or the read or decode error.
func (s *netSession) read(timeout time.Duration) (pv3.Message, error) {
	s.conn.SetReadDeadline(time.Now().Add(timeout))
	return pv3.ReadMessage(s.r)
}

// @brief Write raw bytes to the socket.
//
// @param p Bytes to send.
// @return The write error, or nil.
func (s *netSession) write(p []byte) error {
	_, err := s.conn.Write(p)
	return err
}

// @brief Run the heartbeat exchange over one session until a read fails.
//
// @details serve runs the heartbeat exchange policy over a session. Joan sends a Status hello every
// heartbeat (~3 min) and a touch packet when tapped. For each message: a touch calls onTouch(x, y)
// so the active content source can react (switch dashboard page, fire an HA service call, advance a
// TRMNL playlist — whatever it does with the tap); a hello records telemetry. Then we ACK, but only when a frame exists. When the
// stored image is new we push it — as a partial update (only the changed rectangle, flicker-free)
// when we have a confirmed baseline of what the device currently shows and the change is small
// enough, otherwise as a full frame — and wait up to 15 seconds for the next message. Only an image ACK updates lastDisplayed. Any other message in that window (for example a tap) is logged and dropped. lastDisplayed is the packed
// framebuffer the device has actually rendered; it is updated only on an image ACK and is local to
// the connection (nil at the start). So the first push of any connection is full, a reconnect
// re-syncs with a full, and a partial is only ever diffed against a frame we know the device shows.
// Returns on a read or write error (a disconnect, or no image ACK within 15 seconds after a push), or when no frame is ready.
//
// @param s Transport for the connection.
// @param fc Frame store with the current image.
// @param st Receives telemetry from hello messages.
// @param onTouch Called with raw coordinates for each tap.
// @param remote Remote address, for log lines only.
func serve(s session, fc *frameStore, st *deviceStatus, onTouch func(x, y int), remote string) {
	var lastDisplayed []byte
	for {
		msg, err := s.read(5 * time.Minute)
		if err != nil {
			log.Printf("[%s] disconnected: %v", remote, err)
			return
		}
		switch m := msg.(type) {
		case pv3.Touch:
			log.Printf("[%s] touch (%d,%d)", remote, m.X, m.Y)
			onTouch(m.X, m.Y)
		case pv3.Hello:
			if m.Telemetry != nil {
				st.update(*m.Telemetry)
			}
		}

		packed, full, version, isNew := fc.load()
		if len(full) == 0 {
			log.Printf("[%s] no frame ready", remote)
			return
		}

		if err := s.write(pv3.SessionACK); err != nil {
			log.Printf("[%s] write ack: %v", remote, err)
			return
		}
		if !isNew {
			log.Printf("[%s] heartbeat ACK", remote)
			continue
		}

		// A re-poll of identical content (TRMNL bumps the frame every interval):
		// the device already shows it, so don't repaint.
		if lastDisplayed != nil && bytes.Equal(packed, lastDisplayed) {
			fc.markSent(version)
			log.Printf("[%s] image unchanged — heartbeat ACK", remote)
			continue
		}

		// Partial when we have a confirmed baseline and the change is small enough;
		// EncodePartial returns ok=false (→ full) for a whole-screen or tiny change.
		frame, mode := full, "full"
		if lastDisplayed != nil {
			if p, ok := pv3.EncodePartial(lastDisplayed, packed); ok {
				frame, mode = p, "partial"
			}
		}

		if err := s.write(frame); err != nil {
			log.Printf("[%s] write frame: %v", remote, err)
			return
		}
		fc.markSent(version)
		log.Printf("[%s] pushed %s frame (%d bytes)", remote, mode, len(pv3.SessionACK)+len(frame))

		// Joan replies with an image ACK ~1.6s after rendering the frame. The ACK
		// confirms the device now shows this framebuffer — the baseline for the
		// next partial. A missed ACK returns (disconnect), so the next connection
		// re-syncs with a full frame.
		reply, err := s.read(15 * time.Second)
		if err != nil {
			log.Printf("[%s] no image ACK after push: %v", remote, err)
			return
		}
		if _, ok := reply.(pv3.ImageAck); ok {
			lastDisplayed = packed
			log.Printf("[%s] image ACK received", remote)
		} else {
			log.Printf("[%s] post-push: expected image ACK, got %T", remote, reply)
		}
	}
}

// @brief Latest battery and signal readings from Joan's hello message.
//
// @details Forwarded to TRMNL on the next /api/display poll. Safe for concurrent use.
type deviceStatus struct {
	mu         sync.Mutex
	valid      bool
	voltageMv  int
	rssiMag    int // magnitude; actual RSSI is negative (dBm)
	batteryPct int
}

// @brief Store new telemetry when it is plausible; ignore it otherwise.
//
// @details Ranges (confirmed against the device's own readout): 2000 to 5000 mV, RSSI magnitude at
// most 120, charge at most 100 percent. TRMNL answers 404 to a bad header, and then Joan gets no
// image.
//
// @param t Readings parsed from a hello message.
func (d *deviceStatus) update(t pv3.Telemetry) {
	if t.VoltageMv < 2000 || t.VoltageMv > 5000 || t.RSSIMag > 120 || t.BatteryPct > 100 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.valid, d.voltageMv, d.rssiMag, d.batteryPct = true, t.VoltageMv, t.RSSIMag, t.BatteryPct
}

// @brief Add the TRMNL telemetry headers in TRMNL's units.
//
// @details Battery-Voltage in volts, RSSI in dBm (negative), Percent-Charged from 0 to 100. Does
// nothing until a plausible hello has been stored.
//
// @param h Request headers to add to.
func (d *deviceStatus) setHeaders(h http.Header) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.valid {
		return
	}
	h.Set("Battery-Voltage", strconv.FormatFloat(float64(d.voltageMv)/1000, 'f', 3, 64))
	h.Set("RSSI", strconv.Itoa(-d.rssiMag))
	h.Set("Percent-Charged", strconv.FormatFloat(float64(d.batteryPct), 'f', 1, 64))
}

// @brief Content source that polls a TRMNL /api/display endpoint.
//
// @details Gets the image URL and refresh rate, downloads the image, and encodes it for Joan.
type trmnlClient struct {
	mu       sync.Mutex // serializes refresh (poll loop + touch handler both call it)
	server   string
	deviceID string
	token    string
	fallback time.Duration
	status   *deviceStatus
}

// @brief The JSON fields of a TRMNL /api/display reply that this program uses.
type displayResponse struct {
	ImageURL    string `json:"image_url"`
	RefreshRate int    `json:"refresh_rate"` // seconds; 0 keeps the current interval
}

// @brief Poll TRMNL once and store the new frame (satisfies contentSource).
//
// @param fc Frame store to update.
// @return An error when the request, the status, the JSON, or the image fails. Nil also when TRMNL sends no new image.
func (tc *trmnlClient) refresh(fc *frameStore) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	req, err := http.NewRequest("GET", tc.server+"/api/display", nil)
	if err != nil {
		return err
	}
	req.Header.Set("ID", tc.deviceID)
	req.Header.Set("Access-Token", tc.token)
	// Panel resolution is a fixed device fact TRMNL persists and uses for
	// render sizing. Constant integers, so always valid.
	req.Header.Set("Width", strconv.Itoa(pv3.PanelW))
	req.Header.Set("Height", strconv.Itoa(pv3.PanelH))
	// Report our poll cadence (the last positive refresh_rate TRMNL gave us, or the configured interval before that).
	req.Header.Set("Refresh-Rate", strconv.Itoa(int(tc.fallback.Seconds())))
	// Forward the latest device telemetry (deviceStatus owns the units and the
	// TRMNL header contract); a no-op until a hello has been parsed.
	if tc.status != nil {
		tc.status.setHeaders(req.Header)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TRMNL returned %s", resp.Status)
	}

	var dr displayResponse
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return err
	}
	if dr.ImageURL == "" {
		return nil // no new image
	}

	// Rewrite image_url host to match configured server (TRMNL may return its
	// local hostname which won't resolve inside a container).
	imageURL := tc.rewriteHost(dr.ImageURL)
	log.Printf("TRMNL image_url=%s refresh_rate=%ds", imageURL, dr.RefreshRate)
	packed, full, err := tc.fetchFrame(imageURL)
	if err != nil {
		return err
	}
	fc.set(packed, full)
	if dr.RefreshRate > 0 {
		tc.fallback = time.Duration(dr.RefreshRate) * time.Second
	}
	return nil
}

// @brief Replace the scheme and host of a URL with those of the configured TRMNL server.
//
// @details TRMNL can return its local hostname (for example umbrel.local). Containers without mDNS
// cannot resolve it.
//
// @param rawURL Image URL from TRMNL.
// @return The rewritten URL, or rawURL unchanged when either URL does not parse.
func (tc *trmnlClient) rewriteHost(rawURL string) string {
	base, err := url.Parse(tc.server)
	if err != nil {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Scheme = base.Scheme
	u.Host = base.Host
	return u.String()
}

// @brief Download an image and encode it for Joan.
//
// @param url Image URL.
// @return The packed framebuffer and the full-frame encoding, or an error.
func (tc *trmnlClient) fetchFrame(url string) (packed, full []byte, err error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("image fetch returned %s", resp.Status)
	}
	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	packed = pv3.Pack(img)
	return packed, pv3.EncodeFramePacked(packed), nil
}

// @brief Re-poll TRMNL after a tap (satisfies contentSource).
//
// @details TRMNL advances its own playlist on the server, so only the fact of the tap matters.
//
// @param x Raw X coordinate. Not used.
// @param y Raw Y coordinate. Not used.
// @param fc Frame store to update.
func (tc *trmnlClient) onTouch(x, y int, fc *frameStore) {
	if err := tc.refresh(fc); err != nil {
		log.Printf("touch refresh failed: %v", err)
	}
}

// @brief Return the wait before the next TRMNL poll.
//
// @details Reads the refresh rate under the mutex, because refresh writes it. Never less than 1 second.
//
// @return The current refresh rate, at least 1 second.
func (tc *trmnlClient) pollInterval() time.Duration {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return max(tc.fallback, time.Second)
}

// @brief Poll TRMNL forever (satisfies contentSource).
//
// @details Waits the current refresh rate between polls, at least 1 second. Errors are logged and
// the loop continues. Never returns.
//
// @param fc Frame store to update.
func (tc *trmnlClient) loop(fc *frameStore) {
	for {
		time.Sleep(tc.pollInterval())
		if err := tc.refresh(fc); err != nil {
			log.Printf("TRMNL refresh failed: %v", err)
			continue
		}
		_, full, _, _ := fc.load()
		log.Printf("frame updated (%d bytes)", len(full))
	}
}

// @brief Holder of the current image and its delivery state.
//
// @details Keeps the packed framebuffer (so serve can diff it for a partial update) and the
// pre-encoded full frame (the fallback). Tracks which version was sent. Storage only: no HTTP and no
// image decoding. Safe for concurrent use.
type frameStore struct {
	mu      sync.Mutex
	packed  []byte
	full    []byte
	version uint64
	sent    uint64
}

// @brief Store a new current frame and bump the version.
//
// @details The next heartbeat treats the frame as new until markSent records that version.
//
// @param packed Packed 4-bit framebuffer.
// @param full Full PV3 frame encoding of the same image.
func (fc *frameStore) set(packed, full []byte) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.packed, fc.full = packed, full
	fc.version++
}

// @brief Read the current frame without marking it sent.
//
// @details Call markSent(version) after a successful write.
//
// @return The packed framebuffer, the full frame, the version, and whether the version is unsent.
func (fc *frameStore) load() (packed, full []byte, version uint64, isNew bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.packed, fc.full, fc.version, fc.version > fc.sent
}

// @brief Record a version as delivered.
//
// @details Only moves the sent counter forward, never back.
//
// @param version Version that was written to the device.
func (fc *frameStore) markSent(version uint64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if version > fc.sent {
		fc.sent = version
	}
}

// @brief Read a string environment variable.
//
// @param key Variable name.
// @param def Value to use when the variable is unset or empty.
// @return The variable or def.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// @brief Read a duration environment variable, for example "60s".
//
// @param key Variable name.
// @param def Value to use when the variable is unset, empty, or not a valid duration.
// @return The parsed duration or def.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// @brief Read an integer environment variable.
//
// @param key Variable name.
// @param def Value to use when the variable is unset, empty, or not an integer.
// @return The parsed integer or def.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
