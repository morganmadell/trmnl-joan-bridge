# Context — domain glossary

The shared language of trmnl-joan-bridge. This page gives definitions only,
not implementation detail (wire offsets and byte layouts live in
[`docs/`](docs/)). When the code names a type or module after one of these
terms, it means *this*.

## PV3

The Visionect device wire protocol that the Joan 6 panel speaks over TCP.
The bridge reimplements the device-facing side of PV3, so the panel needs no
Visionect cloud. Everything the panel sends or receives on the socket is a
PV3 **Message** or a PV3 **Frame**.

## Message

One thing the panel sends *to* the bridge on its connection. It is exactly
one of these:

- **Hello** — the status heartbeat the panel sends roughly every 3 minutes.
  It carries device telemetry: battery, signal, and charge.
- **Touch** — a single touch event. It carries panel coordinates. In
  **SOURCE=trmnl mode only**, the bridge treats every touch as "advance the
  playlist". In SOURCE=ha mode (this fork's default), a touch is instead
  un-rotated and hit-tested against **Zones** — see the Page/Zone/Action
  entries below.
- **Image ACK** — the panel's acknowledgement that it rendered a pushed
  **Frame**.
- **Unknown** — a well-formed but unrecognized message. The bridge carries
  it, not as an error.

A read failure, or a desynced stream, is *not* a Message. It is an error.

## Frame

A rendered image, encoded for the panel and wrapped for PV3 delivery. A
**full frame** carries the whole 1024×758 screen. A **partial update**
carries only a changed rectangle. The bridge pushes a Frame on first
contact, and whenever the image changes: a partial update when it can, a
full frame otherwise.

## Partial update

A Frame that repaints only the rectangle that changed since the device's
last rendered image, instead of the whole screen. The panel composites it
with a fast, flicker-free e-ink refresh. The bridge sends a partial update
when it has a confirmed baseline of what the device shows, and the change
is small enough. Otherwise it sends a full frame. See
[`docs/partial-updates.md`](docs/partial-updates.md).

## Dirty rectangle

The bounding box of the pixels that changed between the device's current
image and the next one. This is the region a **Partial update** paints. To
find it, the bridge compares the two packed framebuffers. If the whole
screen changed, or nothing changed, there is no usable dirty rectangle. In
that case the bridge falls back to a full Frame.

## Session ACK

The fixed reply the bridge sends to a **Hello** from the panel, to
(re)establish the session before any **Frame**. It carries the device
identity and display parameters.

## Heartbeat

The panel's ~3-minute **Hello** cadence. Between heartbeats, the connection
stays idle. The bridge keeps the connection open and answers each Hello
with a Session ACK, plus a Frame when the image is new.

## Playlist

**SOURCE=trmnl mode only.** The ordered set of screens TRMNL rotates
through. Each TRMNL poll advances the playlist. A **Touch** triggers an
extra poll, so it shows the next screen.

## Page

**SOURCE=ha mode.** A named dashboard view the bridge can display — for
example `sensors`. Each Page has a URL path (appended to `HA_URL`) and its
own set of tappable **Zones**. The bridge tracks one current Page at a
time and re-renders it on a fixed cadence, or immediately after a **Touch**
changes it or triggers an **Action**. See `ha_zones.go`.

## Zone

**SOURCE=ha mode.** A rectangle, in display coordinates, defined in
`zones.json` for one Page. A **Touch** that lands inside a Zone triggers
that Zone's **Action**; a Touch outside every Zone on the current Page does
nothing. See `ha_zones.go`'s `hit`.

## Action

**SOURCE=ha mode.** What a **Zone** dispatches to when tapped: either
switching the current **Page** to a different one, or calling a Home
Assistant service (a domain/service/entity triple, e.g.
`media_player.toggle` on a specific `entity_id`) through HA's REST API. See
`ha_client.go`'s `onTouch` and `callService`.

## Telemetry

The **Hello** message carries device-health readings: battery voltage,
signal strength, and charge percent. The bridge forwards these readings to
TRMNL, so they appear on its device dashboard.
