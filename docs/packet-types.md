# Packet types (the "type" field in the outer header)

We got this by disassembling each gateway handler's `Type()` method in the
public `visionect/visionect-server-v3:8.5.5-arm` Docker image. Each method
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

The handler's `Type()` value is the type ID it receives in incoming messages.
By convention, a message from the bridge to the panel uses the same type ID.
For example:

- The panel announces itself with a `type=3` message. We receive this as a
  Status message.
- We send an image to the panel with a `type=10` message. This message goes
  to the panel's file handler.

## Source files

All handlers are under `/code/go/src/vss/cmd/gateway/handlers/`. DWARF debug
info in the binary shows this path. The path is not present on the image's
file system.

## What we know works

- The panel reliably sends `type=3` Status messages. We have about 100
  captures of these messages.
- A reply with `type=3, version=0, len=0` has only a header. The panel
  accepts it as a no-op.
- The panel rejects any reply with `len=0` for types 4, 8, or 10. It closes
  the connection fast, in about 4 to 5 seconds.

## What we don't know yet

- We do not know the body schemas for types 6, 7, 8, 10, 11, and 12. Type
  10's stateful sequence is documented in `proto-package.md`.
