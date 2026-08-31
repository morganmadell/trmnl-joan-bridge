# trmnl-joan-bridge (Home Assistant fork)

A standalone Go server that drives a **Joan 6** panel directly. It needs
**no Visionect cloud (VSS)**. The bridge renders content from either a
**Home Assistant** dashboard (this fork's default) or a
[TRMNL](https://github.com/usetrmnl/byos_hanami) (BYOS) server (the
upstream project's original mode, still available via `-source=trmnl`).

This fork comes from [mrfyda/trmnl-joan-bridge](https://github.com/mrfyda/trmnl-joan-bridge).
It is **Path C** of the wider [joan_self_hosted](../README.md) project. See
that project's `Instructions.md` and `Protocol_Bypass_Research.md` for the
full background: why this project exists, and what is proven versus still
unverified.

The Joan 6 panel is a 1024×758, 4-bit grayscale e-ink display with a
capacitive touchscreen. By default, it only talks to Visionect's hosted
software. The bridge reimplements the device-side wire protocol, so you can
point the panel at a server you control.

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

## Setting up a new Joan-6 + Home Assistant instance

Follow these files **in this order** to go from a factory Joan-6 and a
fresh Home Assistant instance to a working, paired display. Each one links
to the next — this section is just the map.

1. **This file (`README.md`)** — deploy the bridge server itself. Copy
   `.env.example` to `.env`, fill in `HA_URL`/`HA_TOKEN`, then run
   `./deploy.sh`. See "Deploying with deploy.sh" below.
2. **[`dashboard/README.md`](dashboard/README.md)** — import a Lovelace
   dashboard into your Home Assistant instance for the bridge to render.
   `dashboard/lovelace-joan.yaml` is a real, working example to start from
   and customize with your own entities.
3. **`zones.json`** (copy `zones.example.json` to start) — define which
   parts of your dashboard the panel's touch input maps to. Only needed
   once your dashboard layout (from step 2) is in its final shape, since
   the coordinates depend on it. See "Troubleshooting and touch
   calibration" below, and `tools/measure_zones.ps1`.
4. **[`PAIRING.md`](PAIRING.md)** — do this last, once the bridge (step 1)
   is actually running and reachable. Physically connect the Joan-6 over
   USB and point it at your bridge using `tools/pair_device.ps1`. This is
   the step that makes the physical panel actually show your dashboard.

## Configuration files — what to edit for your own setup

| File | Controls | Start from |
|---|---|---|
| `.env` | Bridge server settings: Home Assistant URL/token, listen port, zones file path, render timing. Every variable is documented inline. | `.env.example` |
| `zones.json` | Which screen regions respond to touch, and what each one does (switch page / call a Home Assistant service). Tied to your dashboard's exact layout — re-measure after any layout change. | `zones.example.json`, `tools/measure_zones.ps1` |
| Your Home Assistant dashboard | What the panel actually displays — sensors, controls, graphs. Lives inside Home Assistant itself, not in this repo, once imported. | `dashboard/lovelace-joan.yaml` (see `dashboard/README.md`) |

`.env` is gitignored — you create it yourself from `.env.example` and it's
never meant to be committed with your real token in it. `zones.json` is
different: it's a tracked file, currently checked in with *this* project's
own real touch-zone layout and entity IDs. Starting a new setup means
editing it (or replacing its contents with your own, using
`zones.example.json`'s structure as a guide) to match your own dashboard,
not creating it fresh.

## Home Assistant mode (default)

1. **Renders** the current page's dashboard with a headless Chromium
   browser. The bridge drives Chromium through the DevTools Protocol
   (`chromedp`), not a bare CLI screenshot command. It needs this because
   the authentication step below must run JavaScript against Home
   Assistant's own page. Authentication injects a long-lived access token
   into the frontend's own `localStorage` scheme (see `ha_render.go`). This
   is a well-known community pattern, not an officially supported HA API.

   This method is verified against a live HA frontend: the check confirmed
   a real rendered dashboard, not just an absence of errors. See this
   fork's own history in the parent project's `Claude_Work.md` for two real
   bugs this method caught along the way. First, the token was originally
   set on the *wrong origin*: a local loopback redirect page, since
   removed, instead of HA's own page (`localStorage` is per-origin).
   Second, the token JSON was spliced into `localStorage.setItem(...)` as a
   bare object literal instead of a string, and JavaScript silently coerced
   this to `"[object Object]"`. A future HA frontend release could still
   change how it reads `hassTokens`. So if a screenshot ever comes back as
   a login page again, start here.
2. **Hides Home Assistant's own app chrome**: the sidebar, its native
   per-view tab strip, and the top toolbar. It then widens the dashboard
   content to fill the full panel. A small, dedicated e-ink display has no
   use for desktop-browser navigation chrome, and leaving that chrome in
   place used up about a quarter of the screen's width. The bridge does
   this with targeted DOM/CSS overrides in `ha_render.go`'s `kioskModeJS`,
   run just before the screenshot. The exact element and class names it
   targets are current HA frontend internals, not a stable public API. If
   a screenshot ever shows the sidebar again after an HA update, check
   this first.
3. **Encodes and serves** the screenshot the same way as the TRMNL path
   below: same PV3 framing, same partial-update logic.
4. **Routes touches** through `zones.json` (copy `zones.example.json` and
   edit it to set up your own zones). Each touch either switches to a
   different dashboard page, or calls a Home Assistant service (for
   example, `media_player.toggle` on a specific `entity_id`) through HA's
   REST API, using the same access token. Either way, the bridge then
   renders the dashboard again immediately, so the change shows up right
   away.

### Deploying with deploy.sh

The easiest way to run the bridge is `bridge/deploy.sh`:

1. Copy `.env.example` to `.env`.
2. Fill in `HA_URL` and `HA_TOKEN` (and any other values you want to
   override — see the table below and the comments in `.env.example`).
3. From the `bridge/` directory, run `./deploy.sh`.

`deploy.sh` builds the image locally from this directory's `Dockerfile` and
runs it with `--restart unless-stopped`, mounting `zones.json` and a
`debug-screenshots` directory into the container. It also removes any
existing container of the same name first, so re-running it after a code
change is a safe way to redeploy.

`deploy.sh` also accepts a `--pull` flag, which skips the local build and
instead pulls a published `ghcr.io` image and runs that. This code has been
on `main` since 2026-08-31, and `.github/workflows/ci.yml` publishes to
`ghcr.io` on every push to `main`, so a build likely exists. But GHCR
packages can default to **private** visibility separately from the source
repo's own visibility, so `--pull` can fail with an authentication error
until that's confirmed. If it does, check (or fix) the package's visibility
from your GitHub profile → Packages tab → find `trmnl-joan-bridge` →
Package settings → Change visibility (or Settings → Packages on the repo
itself). Use `./deploy.sh` (without `--pull`) to build locally in the
meantime — it always works regardless of package visibility.

Create the long-lived access token `.env`'s `HA_TOKEN` needs in Home
Assistant. Go to your profile (bottom-left avatar), then Security, then
Long-Lived Access Tokens.

The raw `docker run` invocation below is a simplified illustration of what
`deploy.sh` does, not a literal equivalent — `deploy.sh` also mounts
`debug-screenshots` and uses `--env-file .env` (which picks up every
variable you've set, not just the two shown here). It's still useful if you
want to run the container manually, tweak the flags yourself, or just see
roughly what's happening without reading the script:

```bash
docker run -d --name trmnl-joan-bridge \
  --restart unless-stopped \
  -p 11112:11112 \
  -e HA_URL="http://10.218.10.81:8123" \
  -e HA_TOKEN="your-ha-long-lived-access-token" \
  -v $(pwd)/zones.json:/app/zones.json \
  ghcr.io/<your-fork>/trmnl-joan-bridge:latest
```

> That `ghcr.io/<your-fork>/...` tag assumes a published image already
> exists. This code has been on `main` since 2026-08-31 and CI builds and
> pushes `main` to `ghcr.io`, so an image likely exists — but see the
> `--pull` note above about GHCR package visibility before relying on it.
> `./deploy.sh` (without `--pull`) builds and runs locally either way.

`--restart unless-stopped` matters here more than on a typical container.
As of 2026-08-31, the bridge has an unresolved, intermittent crash
(`ExitCode 2`, a Go unhandled panic, no trace captured in `docker logs`
yet) that shows up within seconds of a real device connection or image
ACK. This flag makes Docker bring the container back up within a couple
of seconds of any such crash, instead of leaving the panel unable to
reach it until someone notices. It does not fix the underlying bug — see
TODO.md's "Known unverified or risky areas" for the current state of that
investigation.

| Variable | Required | Default | Description |
|---|---|---|---|
| `HA_URL` | yes | — | Home Assistant base URL, e.g. `http://10.218.10.81:8123` |
| `HA_TOKEN` | yes | — | Long-lived access token |
| `ZONES_FILE` | no | `zones.json` | Page and zone config — see `zones.example.json` |
| `CHROMIUM_BIN` | no | `chromium` (`headless-shell` in the Dockerfile) | Browser executable name/path |
| `RENDER_WAIT_MS` | no | `4000` | Time to let the dashboard load before the bridge renders it |
| `DEBUG_SAVE_SCREENSHOTS` | no | — | If set, a directory where the bridge saves the latest screenshot, for diagnosis |

## Importing the dashboard

The Home Assistant dashboard this bridge renders isn't included by
default — Home Assistant dashboards live in your own instance, so you
need to create or import one there before HA mode has anything meaningful
to show. This repository includes a real, working, exported example of
exactly the 3-page Sensors/Controls/Graphs layout this project actually
uses in production, at
[`bridge/dashboard/lovelace-joan.yaml`](dashboard/lovelace-joan.yaml). Full
import instructions and a customization checklist (which entities you must
replace with your own before it will show real data) are in
[`bridge/dashboard/README.md`](dashboard/README.md) — see that file rather
than duplicating the steps here.

## TRMNL mode (`-source=trmnl` / `SOURCE=trmnl`, upstream behavior)

This is the original upstream mode. It is unchanged, and the bridge still
fully supports it.

1. **Polls TRMNL** using the standard TRMNL device protocol: `GET
   /api/display` with `ID` (the device MAC) and `Access-Token` headers.
   TRMNL replies with an `image_url` and a `refresh_rate`.
2. **Fetches and encodes** the image as a Visionect **PV3** frame. The
   bridge resizes it to 1024×758, converts it to 4-bit grayscale, packs 2
   pixels per byte, splits it into 80 LZ4-compressed blocks, and wraps it
   with the device descriptor and headers.
3. **Serves the panel** over raw TCP on port 11112. The panel opens a
   connection and sends a status Hello roughly every 3 minutes. The bridge
   replies with a session ACK, and, when the image is new, the frame. When
   only part of the screen changed, the bridge sends a **partial update**:
   just the changed rectangle, so the panel does a fast, flicker-free
   local refresh instead of a full repaint. A whole-screen change, or the
   first push after a (re)connect, sends a full frame. See
   [`docs/partial-updates.md`](docs/partial-updates.md).
4. **Reports device health.** The status Hello also carries battery
   voltage and WiFi RSSI. The bridge parses these values and forwards them
   to TRMNL as the standard `Battery-Voltage` and `RSSI` headers. So
   battery and signal appear on the TRMNL device dashboard. See
   [`docs/status-hello.md`](docs/status-hello.md).

The upstream project supplied the original Go implementation of the PV3
wire format, block layout, and session handshake. Most of the protocol
notes in `docs/` are this project's own subsequent research — DWARF-based
binary disassembly, MITM capture analysis, and VSS server binary
inspection — going beyond what upstream's own documentation covers. See
`docs/` for the protocol notes, and "Acknowledgements" below for the full
attribution.

```bash
docker run -d --name trmnl-joan-bridge \
  --restart unless-stopped \
  -p 11112:11112 \
  -e SOURCE=trmnl \
  -e TRMNL_SERVER="http://your-trmnl-host:2300" \
  -e DEVICE_ID="AA:BB:CC:DD:EE:FF" \
  -e ACCESS_TOKEN="your-trmnl-device-token" \
  ghcr.io/<your-fork>/trmnl-joan-bridge:latest
```

> As above, that `ghcr.io/<your-fork>/...` tag assumes a published image —
> this code has been on `main` since 2026-08-31 and CI pushes `main` to
> `ghcr.io`, but see the `--pull` note in the Home Assistant mode section
> above about GHCR package visibility before relying on it. `./deploy.sh`
> (see the Home Assistant mode section above) builds and runs locally for
> either mode, since it just builds this directory's `Dockerfile` and reads
> `SOURCE` from `.env`.

| Variable           | Required | Default   | Description                                                        |
| ------------------ | -------- | --------- | ------------------------------------------------------------------ |
| `TRMNL_SERVER`     | yes      | —         | TRMNL base URL, e.g. `http://192.168.1.10:2300`                 |
| `DEVICE_ID`        | yes      | —         | Panel MAC address, **uppercase**, e.g. `AA:BB:CC:DD:EE:FF`          |
| `ACCESS_TOKEN`     | yes      | —         | TRMNL device access token                                       |

> `DEVICE_ID` must be uppercase. TRMNL rejects lowercase MACs with
> `Invalid device ID`.

## Pointing the panel at this bridge (either mode)

Once the container is running, point the physical Joan-6 panel at it. The
full walkthrough — the physical USB connection, finding the device, the
serial console commands, a script that automates it, and how to verify
success — lives in [PAIRING.md](PAIRING.md). Neither mode needs a
Visionect account or a VSS instance.

## Running on Windows via WSL2 (initial testing)

Initial testing runs the bridge on the Windows machine itself, inside WSL2
(through the Host Compute Service), instead of on a separate Docker host.
The bridge and its `chromedp/headless-shell` dependency are Linux-only. So
WSL2, a real Linux kernel and environment, is the right target, not native
Windows containers. Build and run the bridge exactly as documented above,
from inside your WSL2 distro's shell. That shell has its own Docker, or
you can install one; Docker Desktop's WSL2 backend also works if you
already have it.

**This setup changes one thing: inbound network reachability.** WSL2's
default NAT networking puts the container behind a virtual subnet. Other
devices on your physical LAN, including the Joan-6 panel on
`Turing-Corp`, generally cannot reach the container directly at
`<windows-host-lan-ip>:11112`. This only affects that one inbound
direction: the device dialing into the bridge. `HA_URL` calls go the other
way, out to Home Assistant. Home Assistant runs bare metal on its own
Raspberry Pi 4, a normal, directly addressable LAN device at
`10.218.10.81`. These outbound calls work from WSL2 with no special
configuration, regardless of which option below you pick. There are two
ways to fix the inbound side, in order of preference:

1. **WSL2 mirrored networking mode** (WSL >= 2.0, Windows 11 22H2+). Add
   this to `%UserProfile%\.wslconfig`:
   ```ini
   [wsl2]
   networkingMode=mirrored

   [experimental]
   hostAddressLoopback=true
   ```
   Then run `wsl --shutdown` and restart your distro. This makes WSL2
   share the Windows host's network interface directly. Verify this with
   `ip addr` inside WSL2. Check that the IP matches the one your Windows
   host uses on the LAN, not a `172.x` NAT address.

   **Two more steps are needed, confirmed on this project on 2026-08-28
   after `networkingMode=mirrored` alone was not enough:**

   - **`hostAddressLoopback=true`, shown above, is required.** Without it,
     a port bound inside WSL2 (with or without Docker) stays unreachable
     from the Windows host at the shared LAN IP, even though the IP
     address itself is correctly shared and the port is reachable from
     inside WSL2 on that same IP. With it, the port becomes reachable from
     the Windows host, which sits on the same LAN segment the Joan-6 panel
     does. This setting needs a `wsl --shutdown` and restart to take
     effect, the same as `networkingMode`.
   - **A firewall rule is still required.** Mirrored mode does not bypass
     Windows Firewall's inbound inspection for the shared interface. Run
     this from an elevated Windows PowerShell, once:
     ```powershell
     New-NetFirewallRule -DisplayName "Joan bridge (11112)" -Direction Inbound -LocalPort 11112 -Protocol TCP -Action Allow
     ```
     Unlike the port-proxy command below, this rule does not depend on the
     WSL2 IP, so it does not need to run again after a reboot.

   **A note on testing this yourself:** connecting to your own LAN IP from
   the same Windows machine (self-connect) works fine for a normal,
   natively-bound Windows program, so it is a valid way to test this
   setup — do not assume a failure there is just a self-connect quirk.
   A genuinely separate device on the LAN is still the strongest test,
   since it is what the panel itself will do.
2. **Port proxy and firewall rule** (older WSL2, or use this if mirrored
   mode is not available). Run this from an elevated Windows PowerShell:
   ```powershell
   $wslIp = (wsl hostname -I).Trim().Split()[0]
   netsh interface portproxy add v4tov4 listenport=11112 listenaddress=0.0.0.0 connectport=11112 connectaddress=$wslIp
   New-NetFirewallRule -DisplayName "Joan bridge (11112)" -Direction Inbound -LocalPort 11112 -Protocol TCP -Action Allow
   ```
   Run the `portproxy add` command again, with the new WSL IP, any time
   the WSL2 VM's internal address changes — for example, after a reboot.
   In NAT mode, this address is not guaranteed to stay stable across
   restarts.

Either way, confirm reachability from another device on `Turing-Corp` (or
from the Windows host's own LAN-facing IP). Do this before you assume the
Joan-6 panel's failure to connect is a bridge or protocol problem. It can
simply be a networking problem instead.

## Shared configuration (both modes)

| Variable | Required | Default | Description |
|---|---|---|---|
| `SOURCE` | no | `ha` | `ha` or `trmnl` |
| `REFRESH_INTERVAL` | no | `60s` | Fallback interval: how often to render or fetch again |
| `LISTEN_ADDR` | no | `:11112` | TCP address the panel connects to |

## Troubleshooting and touch calibration

**The screenshot looks wrong, or is a login screen.** Set
`DEBUG_SAVE_SCREENSHOTS` to a directory (bind-mount it), and inspect
`latest-render.png` after the bridge renders the dashboard. This auth path
is verified working, as of this fork. So a login page now most likely
means something changed in your HA frontend version, not a fresh bug. See
`ha_render.go`'s `renderDashboard` comment for what to check. If the
dashboard renders but looks wrong, that is a Lovelace layout or CSS issue,
not a bridge bug.

**Touch zone calibration.** Every touch logs both the flipped
display-space coordinates and the raw wire coordinates, for example: `tap
(dispX,dispY) [raw x,y] hit no zone on page "name"` (or the zone or action
it matched). Watch `docker logs -f trmnl-joan-bridge` while you tap
different areas of the real screen, to see where each touch actually lands
relative to what is on screen. Then adjust the rectangles in `zones.json`
to match. Start with just the top nav bar, and confirm paging works
reliably, before you
calibrate the rest. If touches consistently land offset from where you
expect, that points to the 180°-flip constant in `ha_client.go`'s
`onTouch`, not the zone rectangles themselves. The logged raw and flipped
coordinates tell you which one is wrong. If a touch seems to do nothing at
all, check the device's own serial or diagnostic console (see `docs/`)
alongside the bridge logs. Together, they tell you whether the device
sent the touch at all.

**Re-measuring zones after a layout change.**
[`bridge/tools/measure_zones.ps1`](tools/measure_zones.ps1) formalizes the
technique above for finding the touch-zone y-boundaries: it pixel-scans a
real screenshot for text-block boundaries using a luminance threshold, and
prints the resulting y-ranges as candidate zone boundaries. Point its
`-ImagePath` parameter at a real `DEBUG_SAVE_SCREENSHOTS` capture — the
actual frame that was pushed to and ACKed by the physical panel — not a
fresh, unrelated render, since layout can shift between renders (card
order, entity state, HA frontend version). Use `bridge/zones.example.json`
as the structural template for the resulting `zones.json`.

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

> `go build`, `go vet`, and `go test` all pass clean. The HA render-and-auth
> path, and the touch/zone dispatch, are both verified end-to-end against a
> real Home Assistant instance (a native Windows binary plus a local
> Chrome; that part needs no Docker or WSL2). The Docker image itself has
> been built and run for a long time now (see "Running on Windows via
> WSL2" above), and both the image-push protocol and real touch-coordinate
> reporting are verified against the physical Joan-6 panel — see
> [`PAIRING.md`](PAIRING.md) for the pairing sessions themselves, and the
> parent project's `TODO.md` and `Claude_Work.md` for the full history.

## Hardware notes

- **Panel:** Joan 6 — 1024×758, 4-bit grayscale e-ink, capacitive touch.
- **Transport:** Visionect PV3 over TCP port 11112.
- **Rotation:** the encoder applies a fixed 180° rotation to match this
  panel's scan orientation. `ha_client.go`'s `onTouch` un-rotates the
  touch coordinates before it hit-tests them against the zones.

## Repository layout

```
main.go          TCP server, content-source selection, frame store, heartbeat loop
ha_client.go     Home Assistant content source: page state, tap routing, HA service calls
ha_render.go     Headless-Chromium screenshot (via chromedp/CDP): HA auth
                 injection + kiosk-mode chrome hiding
ha_zones.go      zones.json (page/tap-zone config) loading and hit-testing
zones.example.json  Starting-point zone config matching the parent project's 3-page dashboard
pv3/             PV3 wire protocol: framing + decode, full & partial frame encode, session ACK
docs/            reverse-engineered protocol notes — mostly this project's own original
                 research (DWARF disassembly, MITM capture analysis, VSS binary inspection),
                 not carried over from upstream — see "Acknowledgements" below
Dockerfile       multi-stage build → chromedp/headless-shell runtime image
.github/         CI: gofmt/vet/test + multi-arch build & push to ghcr.io (image name is set
                 automatically from ${{ github.repository }}, so a fork publishes to its own
                 registry path with no changes needed)
```

## Acknowledgements

The upstream project,
[mrfyda/trmnl-joan-bridge](https://github.com/mrfyda/trmnl-joan-bridge),
reverse-engineered the Visionect device protocol purely for
interoperability, to keep a perfectly good display out of a landfill, and
this fork's original PV3 implementation builds on that work. This fork
adapts it to a different content source. Most of the protocol reference
material in `docs/` is this project's own subsequent research, though — the
DWARF-based binary disassembly, MITM capture analysis, and VSS server
binary inspection recorded there go beyond what upstream's own
documentation covers, and it's presented as this project's original work,
not carried over from upstream.
