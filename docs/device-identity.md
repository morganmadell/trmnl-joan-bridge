# Joan 6 device identity

We took this data from the panel's own logs on 2026-05-24. We got it with the
Joan Configurator app's "Get Device Information" feature.

## Hardware

```
UUID:              42002800-0d51-3731-3734-393400000000
HW name id:        0x5 ("V Tablet 2 v1.0, BOM 2, APP: Joan")
HW version:        1.0.2
HW firmware iface: 0x0
GTIN:              3830065460078
```

## Display (EPD)

```
Size:              6.0"
Resolution:        1024 x 758
Waveform:          6.0_C276_U2
Driver IC:         6.0_p47000cd0502
Encoding (panel):  0x4 = 4-bit grayscale
                   (from an internal `l:` log line. This confirms what the panel renders.)
```

## Firmware & bootloader

```
FW:        4.12.2775   (build 2019-11-29)
FW CRC32:  0x5E0B9C17
FW SHA-ish hash: 0x3D4CB69B
FW length: 363260 bytes
BL:        4.12.2775   (build 2019-11-29)
```

## Network behavior

```
Protocol:               PV3 (version 3)
Heartbeat interval:     3 minutes
Connectivity stack:     CC3100 WiFi module (TI)
TCP target:             Whatever IP:port is configured in Joan Configurator
                        → Advanced Connectivity → Server IP / Server Port
Local IP (current AP):  192.168.1.218  (DHCP, will vary)
```

## Touch

```
Type:                   0x2
Touch FW version:       18.243
EVT count:              15
```

## Battery

```
Level:                  100% (charging)
Voltage:                4196 mV
Current:                61 mA
```

## File system (on-device)

```
Total:                  130988 bytes
Free:                   130988 bytes  (empty as of S09)
```

This is only about 128 KB of file storage. Image files sent through the File
protocol must be small. They must fit next to the space reserved for firmware.

## Important error code

`PV2` here is a literal string taken from the panel's own firmware log
output (see the source note at the top of this file), not a reference to a
second wire protocol version — it has nothing to do with the wire-format
"was the header's first field 2 or 3" confusion that
[wire-framing.md](wire-framing.md) later corrected. Every wire protocol
reference in this project, including elsewhere in this same file, is PV3.

```
PV2 error: 0x00210000   = the panel's rejection of our 3-message sequence
                          (session 09; cause still being investigated)
PV2 error: 0x00220000   = variant seen once; likely a sibling protocol error
```

## The 88-byte "introduction" message — what it is

When the panel boots fresh, it sometimes sends an 88-byte message to the
bridge. It sends this before the regular 456-byte Status hello. Bytes 8 to
11 of the 88-byte message are `17 9c 0b 5e` in little-endian order, which
equals `0x5e0b9c17`. This value matches `PV2_FW_CRC` from the logs (again,
the firmware's own internal log label — see the note above; not the PV3
wire protocol). So this message is an announcement of the panel's identity
and capabilities. We previously treated it as a mystery.

**Open question:** it is not confirmed whether this message uses the
standard 20-byte device→server header ([wire-framing.md](wire-framing.md)),
or is a bespoke 88-byte structure of its own. `bridge/pv3/decode.go` has no
case for it — an 88-byte message doesn't match any known message's body
size, so the bridge currently classifies it as `Unknown`. "Bytes 8 to 11"
above is a byte offset into the whole 88-byte message, counted from its
start; if this message does use the standard header, that offset falls
inside the header's `flags` field, which wire-framing.md's header table
otherwise documents as "always 0" — an exception this project has not yet
resolved. Treat this message's internal layout as unconfirmed.
