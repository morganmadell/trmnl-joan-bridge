# trmnl-joan-bridge (Home Assistant fork)

A standalone Go server that drives a **Joan 6** e-ink display directly —
with **no Visionect cloud (VSS) dependency** — from either a **Home
Assistant** dashboard (this fork's default) or a
[TRMNL](https://github.com/usetrmnl/byos_hanami) (BYOS) server (the
upstream project's original mode, still available via `-source=trmnl`).

Forked from [mrfyda/trmnl-joan-bridge](https://github.com/mrfyda/trmnl-joan-bridge)
as **Path C** of the wider [joan_self_hosted](../README.md) project — see
that project's `Instructions.md` and `Protocol_Bypass_Research.md` for the
full background on why this exists and what's proven vs. still unverified.

The Joan 6 is a 1024×758 4-bit grayscale e-ink panel with a capacitive
touchscreen. Out of the box it only talks to Visionect's hosted software. This
shim reimplements the device-side wire protocol so the panel can be pointed at a
server you control.

```
┌─────────┐  PV3 / TCP:11112  ┌───────────────────┐  headless Chromium  ┌───────────────────┐
│ Joan 6  │ ◀───────────────▶ │ trmnl-joan-bridge │ ───────────────────▶ │ Home Assistant    │
│ e-ink   │   image frames    │  (this fork, HA   │   dashboard screenshot│ Lovelace dashboard│
│ + touch │   + touch events  │   mode, default)  │                       └───────────────────┘
└─────────┘                   └─────────┬─────────┘
                                         │ REST API (tap → service call)
                                         ▼
                               (same Home Assistant instance)
```

## Home Assistant mode (default)

1. **Renders** the current page's Lovelace dashboard with a headless
   Chromium, authenticated via a long-lived access token injected into the
   frontend's own `localStorage` scheme (see `ha_render.go` — this is a
   well-known community pattern, not an officially supported HA API, and
   hasn't been verified against a live HA frontend as part of this fork; if
   the screenshot comes back as a login page instead of your dashboard,
   start there).
2. **Encodes and serves** the screenshot exactly like the TRMNL path below —
   same PV3 framing, same partial-update logic.
3. **Routes taps** through `zones.json` (copy `zones.example.json` and edit
   it): a tap either switches to a different dashboard page, or calls a Home
   Assistant service (e.g. `media_player.toggle` on a specific `entity_id`)
   via HA's REST API using the same access token. Either way, it triggers an
   immediate re-render so the change shows up right away.

```bash
docker run -d --name trmnl-joan-bridge \
  -p 11112:11112 \
  -e HA_URL="http://10.218.10.81:8123" \
  -e HA_TOKEN="your-ha-long-lived-access-token" \
  -v $(pwd)/zones.json:/app/zones.json \
  ghcr.io/<your-fork>/trmnl-joan-bridge:latest
```

Create the long-lived access token in Home Assistant under your profile
(bottom-left avatar) → Security → Long-Lived Access Tokens.

| Variable | Required | Default | Description |
|---|---|---|---|
| `HA_URL` | yes | — | Home Assistant base URL, e.g. `http://10.218.10.81:8123` |
| `HA_TOKEN` | yes | — | Long-lived access token |
| `ZONES_FILE` | no | `zones.json` | Page/tap-zone config — see `zones.example.json` |
| `CHROMIUM_BIN` | no | `chromium` (`headless-shell` in the Dockerfile) | Browser executable name/path |
| `RENDER_WAIT_MS` | no | `4000` | Time to let the dashboard load before screenshotting |
| `DEBUG_SAVE_SCREENSHOTS` | no | — | If set, a directory to save the latest render to, for troubleshooting |

## TRMNL mode (`-source=trmnl` / `SOURCE=trmnl`, upstream behavior)

The original upstream mode, unchanged and still fully supported.

1. **Polls TRMNL** on the standard TRMNL device protocol: `GET /api/display`
   with `ID` (the device MAC) and `Access-Token` headers. TRMNL replies with
   an `image_url` and a `refresh_rate`.
2. **Fetches and encodes** the image into a Visionect **PV3** frame: resize to
   1024×758, convert to 4-bit grayscale, pack 2 px/byte, split into 80
   LZ4-compressed blocks, and wrap with the device descriptor and headers.
3. **Serves the panel** over raw TCP on port 11112. Joan opens a connection and
   sends a status "hello" roughly every 3 minutes; the shim replies with a
   session ACK and, when the image is new, the frame. When only part of the
   screen changed it sends a **partial update** — just the changed rectangle, so
   the panel does a fast, flicker-free local refresh instead of a full repaint; a
   whole-screen change (or the first push after a (re)connect) sends a full frame.
   See [`docs/partial-updates.md`](docs/partial-updates.md).
4. **Reports device health.** The status hello also carries battery voltage and
   WiFi RSSI; the shim parses them and forwards them to TRMNL as the standard
   `Battery-Voltage` and `RSSI` headers, so battery and signal appear in the
   TRMNL device dashboard. See [`docs/status-hello.md`](docs/status-hello.md).

The PV3 wire format, block layout, and session handshake were reverse-engineered
from captured device traffic; see `docs/` for the protocol notes.

```bash
docker run -d --name trmnl-joan-bridge \
  -p 11112:11112 \
  -e SOURCE=trmnl \
  -e TRMNL_SERVER="http://your-trmnl-host:2300" \
  -e DEVICE_ID="AA:BB:CC:DD:EE:FF" \
  -e ACCESS_TOKEN="your-trmnl-device-token" \
  ghcr.io/<your-fork>/trmnl-joan-bridge:latest
```

| Variable           | Required | Default   | Description                                                        |
| ------------------ | -------- | --------- | ------------------------------------------------------------------ |
| `TRMNL_SERVER`     | yes      | —         | TRMNL base URL, e.g. `http://192.168.1.10:2300`                 |
| `DEVICE_ID`        | yes      | —         | Joan MAC address, **uppercase**, e.g. `AA:BB:CC:DD:EE:FF`          |
| `ACCESS_TOKEN`     | yes      | —         | TRMNL device access token                                       |

> The `DEVICE_ID` must be uppercase — TRMNL rejects lowercase MACs with
> `Invalid device ID`.

## Pointing the panel at this bridge (either mode)

Once the container is running, point the physical Joan-6 at it with the
**Visionect Configurator** app (USB) or the device's own serial console
(`server_tcp_set <bridge-ip> 11112`) — see the parent project's
Instructions.md for the full walkthrough. No Visionect account or VSS
instance is involved in either mode.

## Running on Windows via WSL2 (initial testing)

Initial testing runs the bridge on the Windows machine itself, inside WSL2
(via the Host Compute Service), rather than a separate Docker host — the
bridge and its `chromedp/headless-shell` dependency are Linux-only, so WSL2
(a real Linux kernel/environment) rather than native Windows containers is
the right target. Build and run exactly as documented above, from inside
your WSL2 distro's shell (it has its own Docker, or install one — Docker
Desktop's WSL2 backend also works if you already have it).

**The one thing this setup changes: inbound network reachability.** WSL2's
default NAT networking puts the container behind a virtual subnet that other
devices on your physical LAN — including the Joan-6 on `Turing-Corp` —
generally cannot reach directly at `<windows-host-lan-ip>:11112`. This only
affects that one inbound direction (the device dialing into the bridge);
`HA_URL` calls going the other way, out to Home Assistant (which runs bare
metal on its own Raspberry Pi 4, a normal directly-addressable LAN device at
`10.218.10.81`), are outbound from WSL2 and work with no special
configuration regardless of which option below you pick. Two ways to fix the
inbound side, in order of preference:

1. **WSL2 mirrored networking mode** (WSL >= 2.0, Windows 11 22H2+): add to
   `%UserProfile%\.wslconfig`:
   ```ini
   [wsl2]
   networkingMode=mirrored
   ```
   then `wsl --shutdown` and restart your distro. This makes WSL2 share the
   Windows host's network interface directly — a port the bridge listens on
   inside WSL2 becomes reachable at the Windows machine's own LAN IP with no
   further steps. Verify with `ip addr` inside WSL2: you should see the same
   IP your Windows host uses on the LAN, not a `172.x` NAT address.
2. **Port proxy + firewall rule** (older WSL2, or if mirrored mode isn't
   available): from an elevated Windows PowerShell,
   ```powershell
   $wslIp = (wsl hostname -I).Trim().Split()[0]
   netsh interface portproxy add v4tov4 listenport=11112 listenaddress=0.0.0.0 connectport=11112 connectaddress=$wslIp
   New-NetFirewallRule -DisplayName "Joan bridge (11112)" -Direction Inbound -LocalPort 11112 -Protocol TCP -Action Allow
   ```
   Re-run the `portproxy add` command (with the new WSL IP) any time the WSL2
   VM's internal address changes, e.g. after a reboot — it isn't guaranteed
   stable across restarts in NAT mode.

Either way, confirm reachability from another device on `Turing-Corp` (or
just from the Windows host's own LAN-facing IP) before assuming the Joan-6's
failure to connect is a bridge or protocol problem rather than a networking
one.

## Shared configuration (both modes)

| Variable | Required | Default | Description |
|---|---|---|---|
| `SOURCE` | no | `ha` | `ha` or `trmnl` |
| `REFRESH_INTERVAL` | no | `60s` | Fallback re-render/re-fetch interval |
| `LISTEN_ADDR` | no | `:11112` | TCP address the panel connects to |

## Troubleshooting and touch calibration

**Render looks wrong or is a login screen.** Set `DEBUG_SAVE_SCREENSHOTS` to
a directory (bind-mount it) and inspect `latest-render.png` after a render.
A login page means the `hassTokens` auth injection in `ha_render.go` needs
fixing for your HA frontend version; a rendered-but-wrong-looking dashboard
is a Lovelace layout/CSS issue, not a bridge bug.

**Calibrating touch zones.** Every tap logs both the flipped display-space
coordinates and the raw wire coordinates, e.g. `tap (dispX,dispY) [raw x,y]
hit no zone on page "name"` (or the zone/action it matched) — watch
`docker logs -f` while tapping different areas of the real screen to see
where taps actually land relative to what's on screen, then adjust
`zones.json`'s rectangles to match. Start with just the top nav bar until
paging works reliably before calibrating the rest. If taps consistently land
offset from where you'd expect, that points at the 180°-flip constant in
`ha_client.go`'s `onTouch` rather than the zone rectangles themselves — the
logged raw vs. flipped coordinates tell you which one is off. If a tap seems
to do nothing at all, cross-checking the device's own serial/diagnostic
console (see `docs/`) alongside the bridge logs tells you whether the
device ever sent it in the first place.

## Building from source

```bash
# Local binary (requires Go 1.22+) — compiles and runs `go vet`/`go test`,
# but does NOT get you a browser for HA mode; use Docker for that (below).
go build -o bin/trmnl-joan-bridge .
go vet ./...
go test ./...

# Or via the Makefile, using a container toolchain
make build         # native dev binary  → bin/trmnl-joan-bridge-local
make build-arm     # static linux/arm64 binary → bin/trmnl-joan-bridge

# Docker image (includes headless-shell — this is what you actually deploy)
docker build -t trmnl-joan-bridge .
```

> **This fork's code has not yet been compiled or run** — it was written
> without a local Go toolchain available. `go build`/`go vet`/`go test` above
> is the first thing to run before trusting any of it; see the parent
> project's Claude_Work.md for the full caveat.

## Hardware notes

- **Panel:** Joan 6 — 1024×758, 4-bit grayscale e-ink, capacitive touch.
- **Transport:** Visionect PV3 over TCP port 11112.
- **Rotation:** the encoder applies a fixed 180° rotation to match this panel's
  scan orientation — `ha_client.go`'s `onTouch` un-rotates tap coordinates
  before hit-testing zones.

## Repository layout

```
main.go          TCP server, content-source selection, frame store, heartbeat loop
ha_client.go     Home Assistant content source: page state, tap routing, HA service calls
ha_render.go     Headless-Chromium screenshot + local HA auth-redirect page
ha_zones.go      zones.json (page/tap-zone config) loading and hit-testing
zones.example.json  Starting-point zone config matching the parent project's 3-page dashboard
pv3/             PV3 wire protocol: framing + decode, full & partial frame encode, session ACK
docs/            reverse-engineered protocol notes (upstream)
Dockerfile       multi-stage build → chromedp/headless-shell runtime image
.github/         CI: gofmt/vet/test + multi-arch build & push to ghcr.io (upstream config; update
                 the image name if you push this fork to your own registry)
```

## Acknowledgements

Upstream [mrfyda/trmnl-joan-bridge](https://github.com/mrfyda/trmnl-joan-bridge)
was built by reverse-engineering the Visionect device protocol purely for
interoperability, to keep a perfectly good display out of a landfill. This
fork adapts it to a different content source; all of the hard
protocol-level work is upstream's.
