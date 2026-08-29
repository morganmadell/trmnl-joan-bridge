# Status hello — device telemetry (PV3 type 3)

The panel opens a TCP connection. It sends a Status hello (PV3 type 3) on
every heartbeat, about every 3 minutes. Besides announcing the panel, the
hello body carries live telemetry as a key-value list: battery, signal,
temperature, and file system.

## Body layout

After the 20-byte outer header, the body is a flat list of
`[key:u32][value:u32]` pairs in little-endian order. This list starts at
body offset `0x34`. A key of `0xffffffff` ends the list, followed by a
4-byte CRC32 trailer. The 456-byte and 532-byte hello variants share this
layout. The 532-byte variant just appends extra keys.

Parse by key, not by fixed offset. Keys are sparse: some keys, such as 14,
1c, and 21, are absent. Keys always appear in ascending order.

## Field map

We identified this field map by diffing 19 hello messages captured across
6.8 hours. The constant fields stayed fixed, and the dynamic sensor fields
drifted. We cross-checked the fields against the panel's own "Get Device
Information" readout (see [device-identity.md](device-identity.md)).

| key (hex / dec) | field | notes |
| --- | --- | --- |
| 0x03 / 3 | firmware CRC | `0x5E0B9C17` — constant |
| 0x0a / 10 | **battery %** | 0..100. VSS dashboard "Battery 20%" = key 10 = 20 |
| 0x0c / 12 | **temperature (°C)** | VSS dashboard "Temperature 25°C" = key 12 = 25 |
| 0x0d / 13 | **RSSI** | value = `\|dBm\|`; report negative. VSS "Signal -54" = key 13 = 54 |
| 0x12, 0x15 / 18, 21 | firmware build | 2775 — constant (FW 4.12.2775) |
| **0x22 / 34** | **battery voltage (mV)** | 3898 → 3702 over the capture; *rose* while charging |
| 0x23 / 35 | charge current (mA) | 954 charging → 0 once unplugged |
| 0x26, 0x27 / 38, 39 | panel width, height | 1024, 758 — constant |
| 0x32–0x35 / 50–53 | GTIN | "3830065460078" as 4-char ASCII chunks |
| 0x3c, 0x3d / 60, 61 | filesystem free, total | 130988 bytes |

The correlation between voltage and current pins down keys 34 and 35.
Voltage climbs while current is about 950 mA, during charging. Voltage
falls the instant current hits 0, when the panel is unplugged.

We confirmed keys 10, 12, and 13 against the live VSS admin dashboard for
this panel: Battery 20%, Temperature 25°C, and Signal -54. These values
matched keys 10, 12, and 13 exactly. Time-series guesswork alone had keys 10
and 13 swapped. Key 10's apparent "fast swing" was the battery gauge
correcting itself when the charger was unplugged. This happened at the same
instant that key 35's current went from 954 to 0.

## Forwarding to TRMNL (BYOS)

TRMNL reads telemetry from the panel from request headers on
`GET /api/display`. The contract comes from byos_hanami's
`app/schemas/firmware/header.rb` and `app/aspects/firmware/headers/model.rb`:

| HTTP header | type | persisted device field |
| --- | --- | --- |
| `ID` (**required**) | MAC | mac_address |
| `Access-Token` | string | api_key |
| `Battery-Voltage` | float (**volts**) | battery_voltage |
| `RSSI` | int (**dBm**) | wifi |
| `Percent-Charged` | float | battery_charge |
| `FW-Version` | version | firmware_version |
| `Width` / `Height` | int | width / height |
| `Model` | string | model_name |
| `Refresh-Rate` | int | refresh_rate |

**Validation is strict and fail-closed:** if a header fails its type check,
the whole `/api/display` action returns 404. Then the panel would get no
image. So the bridge only sends well-formed values. It sends battery and
RSSI values only after it has parsed and range-checked a real hello: 2000 to
5000 mV for voltage, and RSSI of 120 or less.

**Sent by the bridge:** `ID`, `Access-Token`, `Battery-Voltage` (key 34 ÷
1000), `RSSI` (−key 13), `Percent-Charged` (key 10), `Width` (1024), `Height`
(758), and `Refresh-Rate` (poll interval).

**Deliberately not sent:**
- `FW-Version` — this risks failing the `Types::Version` check, which
  returns 404. It also feeds TRMNL's firmware-update comparison. This check
  is cosmetic here, because the bridge never forwards TRMNL's
  `firmware_url` to the panel. The panel only ever receives PV3 image
  frames.

## Touch events (type 6)

A touch on the panel sends a 76-byte message from the panel to the bridge.
The panel sends one message per touch; it does not send separate down and
up events. The message carries the panel's serial number, an event-type
marker of `6` at body+20 (u16 LE), and coordinates: X at body+48 and Y at
body+52 (u16 LE):

| touch (user view) | X (body+48) | Y (body+52) |
| --- | --- | --- |
| top-left | ~970 | ~770–820 |
| top-right | ~50 | ~730 |
| bottom-left | ~930 | ~10 |
| bottom-right | ~67 | ~70 |
| centre | ~505 | ~420 |

Coordinates are in the panel's native orientation. They are flipped 180°
from the displayed image: a high X value means the user's left side, and a
high Y value means the user's top side. `userX ≈ Xmax − rawX` and
`userY ≈ Ymax − rawY`, with X from about 0 to 1024 and Y from about 0 to
820. The digitizer runs a little taller than the 758-pixel display. For
touch zones, the raw values are enough. For exact pixels, use a linear fit
from touches at the corners.

**Touch → next playlist item:** TRMNL's `/api/display` rotator advances the
playlist on every poll, unless the playlist is set to `manual`. So the
bridge treats any touch as an "advance" signal. On a 76-byte message, the
bridge re-polls TRMNL, which rotates to the next screen and re-encodes it.
The bridge then pushes the new frame on the same connection. This takes
about 2 seconds from touch to render. The coordinates are not used for
this; any touch advances the playlist.
