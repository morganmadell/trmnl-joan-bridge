# Wire framing — Joan 6 ↔ server

## Transport

- **TCP, plaintext.** The firmware we have, version 4.12.2775, does not use
  TLS. The first byte of every message from the panel is `0x03`. It is
  never `0x16`, the TLS ClientHello byte.
- The panel dials whatever IP:port is configured in the desktop **Visionect
  Configurator**, under *Advanced connectivity → Server IP / Server port*.
  The Configurator's default port is `11113`. We set it to `11112`, to
  match the bridge's listener.
- When the bridge does not respond, the panel's connection cycle is: open
  TCP, send a 456-byte hello message, wait about 16 seconds, then EOF. The
  panel retries every 5 to 15 seconds, while it has battery power.

## Outer 20-byte fixed header — ASYMMETRIC by direction

**Note:** the panel and the bridge use different 20-byte header formats. We
found this in session 06, by disassembling `vpacket/pkgutil.prependHeader`
and checking it against the captures.

### Device → Server (incoming)

```
offset  size  field             notes
------  ----  ----------------  ----------------------------------------
+0x00    4    type   (uint32 LE)  packet kind — see reference/packet-types.md
+0x04    4    version (uint32 LE) sub-type / protocol revision
+0x08    4    flags  (uint32 LE)  reserved (always 0 in observed packets)
+0x0c    4    length (uint32 LE)  number of payload bytes that follow
+0x10    4    dev_id (uint32 LE)  device-stable lo-32 of a session/device ID
                                  (constant across packets in same session)
```

### Server → Device (outgoing) — per `prependHeader` disassembly

```
offset  size  field             notes
------  ----  ----------------  ----------------------------------------
+0x00    4    uint32 LE = 3     Version (PV3) — see below
+0x04    4    uint32 LE = 0     Security (constant, unencrypted)
+0x08    4    uint32 LE = 1     Compression (constant — 1 = LZ4)
+0x0c    4    body length
+0x10    4    CRC32-IEEE of body
```

The bridge's outgoing header uses the constants `[3, 0, 1, len, CRC32(body)]`.
There is no `dev_id` stamp in messages from the bridge to the panel, and no
separate "type" field either. The panel must dispatch on the body content.

**Correction:** the black-box disassembly in session 06 first read the value
at offset `0x00` as `2`. Later, direct struct analysis from the VSS
binaries' DWARF debug info (see [pv3-frame-format.md](pv3-frame-format.md))
named this field `Version` and gave its real value as `3` — matching "PV3"
(Protocol Version 3), the protocol's own name. That matches the bridge's
actual code (`pv3/encode.go`), which has always sent `3`. So the bridge is
correct; this document's earlier value of `2` was wrong, from a less
reliable research method, and this file's outer-header table above and the
`[3, 0, 1, len, CRC32(body)]` reference above are now updated to match.

Both formats give a total message length of `20 + length`.

### Why we got this wrong for sessions 2-5

We assumed the same format worked for both directions. We built every reply
as `[type, ver, flags=0, len, dev_id_echo]`. The panel fast-rejected every
empty-body reply, and it fast-rejected every File-formed reply too. The
prependHeader analysis in session 06 suggests we sent the wrong outer
framing on every attempt.

In `joan-hello-v2.bin`, the `dev_id` value `0x56a14c5d` happens to equal
`CRC32-IEEE(header[0..16])`. At first, this looked like evidence that the
field was a CRC. We checked this against `joan-first-connect-v1.bin`: the
same `dev_id`, but different header bytes, and the CRC does not match. So
the value is truly a static ID for the panel. The CRC match in hello-v2 was
a coincidence, with odds of about 1 in 4 billion.

## CRC trailer

Some message bodies end with a 4-byte CRC32 trailer.

- Algorithm: standard CRC32-IEEE (`hash/crc32.ChecksumIEEE` in Go terms). We
  confirmed this by disassembling `vss/pkg/utils/vpacket/pkgutil.prependHeader`.
  It calls `hash/crc32.ChecksumIEEE` at `packet.go:39`.
- The panel's 456-byte hello ends with `8a 43 3e 91`, which fits this
  format.
- In testing, leaving the CRC off our header-only replies did not change
  the panel's behavior. So the CRC can be optional, or it can be required
  only for certain message types. Check this for each type.

## Key constants observed in captures

| Constant | Meaning |
|---|---|
| `0x56a14c5d` | This panel's `dev_id_lo` (a 4-byte session ID, stable across reboots in our captures). |
| `0x5e0b9c17` | Appears in both the 456-byte hello body and the 88-byte mystery message. Probably a Visionect firmware build hash or magic number. |
| `0xffffffff` | "End-of-list" sentinel inside structured bodies. |

## Sender-side reference (server → device)

VSS queues outgoing bytes through a per-panel channel:
`xsync.Map[uuid [16]byte, chan []byte]`. A separate goroutine drains each
channel to the panel's TCP socket. So "send a message" at the application
level means "put bytes onto this channel".

## Reply behavior table (observed)

All the replies we tested had a 20-byte header, with `flags=0`, `len=0`, and
`dev_id` echoed back.

| Reply `(type, version)` | Disconnect after | Reading |
|---|---|---|
| (no reply) | 16s | The panel's idle "waiting for command" timeout |
| (3, 0) | **~12s** | Parsed as a valid status no-op, polite wait, normal close |
| (3, 1) | ~4.5s | Parsed, decode error, fast bail |
| (3, 2) | ~12s | Same as (3, 0) — version field tolerated |
| (4, 0) | ~4s | Unknown type, fast bail |
| (8, 0) | ~4s | Empty Param body, decode error, fast bail |
| (10, 0) | ~4s | Empty File body, decode error, fast bail |
| (10, 0) + CRC32 trailer | ~4s | Same as above — CRC presence didn't help an empty body |

**Conclusion:** header-only replies cannot produce useful behavior from the
panel, on any type except 3-as-no-op. To trigger anything else, you need
real body content that matches the type — see `reference/proto-package.md`
for the body type names.
