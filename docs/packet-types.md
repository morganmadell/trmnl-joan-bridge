# Packet types (the "type" field in the outer header)

We got this by disassembling each gateway handler's `Type()` method in the
public `visionect/visionect-server-v3:7.6.5` Docker image. Each method
returns the type constant in one instruction.

| Type | Handler                  | Source            | What it carries                                    |
|------|--------------------------|-------------------|----------------------------------------------------|
| **3** | `PV3StatusHandler`      | `status.go:83`    | Device → server status/announce (our hello)         |
| 6    | `pv3TouchHandler`        | `touch.go:54`     | Touchscreen events                                  |
| 7    | `pv3GPSHandler`          | `gps.go:67`       | GPS coordinates                                     |
| **8** | `PV3ParamHandler`       | `param.go:51`     | Get/set device parameters (uint16-keyed key-value)  |
| **10** | `PV3FileHandler`       | `filesystem.go:60`| File / image transfer — **stateful 4-step sequence**|
| 11   | `pv3ButtonHandler`       | `button.go:43`    | Physical button press                               |
| 12   | `pv3CBORHandler`         | `cbor.go:58`      | Modern CBOR-encoded protocol alternative            |

"PV3" = Protocol Version 3 (Visionect's internal name).

## Direction

This type-ID table is VSS's own convention for dispatching **incoming**
device messages: the handler's `Type()` value is the type ID a message from
the panel is tagged with, and VSS routes it to the matching handler. For
example, the panel announces itself with a `type=3` message, which VSS
routes to `PV3StatusHandler`.

This table does **not** describe anything the bridge itself writes on
outgoing (bridge → panel) frames. The bridge's outer header has no type
field at all — see [wire-framing.md](wire-framing.md)'s Server → Device
header table and `pv3/encode.go`'s header construction, which only ever
writes Version, Security, Compression, Length, and Checksum. When the
bridge pushes an image, the panel identifies it as image data by its body
content (the `ImageHeader`/`RectangleHeader` structure described in
[pv3-frame-format.md](pv3-frame-format.md)), not by a type=10 field on the
outer header.

## Source files

All handlers are under `/code/go/src/vss/cmd/gateway/handlers/`. DWARF debug
info in the binary shows this path. The path is not present on the image's
file system.

## What we know works

- The panel reliably sends `type=3` Status messages. We have about 100
  captures of these messages.
- These are outgoing (bridge → panel) replies, so they use the outgoing
  header's own field names, `Version`/`Security` — not the incoming `type`
  field this file's own table above describes. See
  [wire-framing.md](wire-framing.md)'s "Reply behavior table" for the full
  results. A reply with `Version=3, Security=0, len=0` has only a header.
  The panel accepts it as a no-op.
- The panel rejects any reply with `len=0` for `Version` 4, 8, or 10. It
  closes the connection fast, in about 4 to 5 seconds. (These `Version`
  values are not the same thing as this file's incoming type IDs — `4` is
  not even a real type ID above; it is one of the `Version` values tested
  in wire-framing.md's experiment.)

## What we don't know yet

- We do not know the body schemas for types 6, 7, 8, 10, 11, and 12. Type
  10's stateful sequence is documented in
  [Protocol_Bypass_Research.md](../../Protocol_Bypass_Research.md), section 3.
