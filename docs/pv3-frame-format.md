# PV3 frame format (Joan 6 / Visionect)

Visionect does not publish the PV3 wire protocol used by the panel. This
document describes the frame format that the bridge implements. We
reverse-engineered it from the VSS binaries, which ship unstripped, with
DWARF debug info, from the Go modules `bill.vnct.xyz/vss/proto@v1.2.23` and
`bill.vnct.xyz/vss/lz4`. See [vss-binary-recon.md](vss-binary-recon.md) for
how we obtained and inspected the binaries.

## Frame layout

```
ProtocolHeader (20)  +  ImageHeader (20)  +  pre-header  +  80 × dataBlock
```

Everything after the `ProtocolHeader` makes up its `Length` bytes. The
`ProtocolHeader`'s `Checksum` field is `CRC32(body)`.

## Structs (exact layouts from DWARF)

**ProtocolHeader** — 20 B, the outer/transport header
| off | field | value |
|----|-------|-------|
| 0 | Version | **3** (PV3) |
| 4 | Security | 0 (unencrypted) |
| 8 | Compression | **1** (LZ4) |
| 12 | Length | body length |
| 16 | Checksum | CRC32(body) |

**ImageHeader** — 20 B
| off | field | value |
|----|-------|-------|
| 0 | Checksum | 0 |
| 4 | NrPrimitives | 80 (dataBlocks) |
| 8 | Options | pre-header length − 4 |
| 12 | PayloadLength | 4800 (block/chunk size) |
| 16 | Reserved | 1 |

**dataBlock** — 24-B header + data: `BlockID`(u32, 1-based), `BlockLast`(u32,
= NrPrimitives), `compSize`(u32), `rawSize`(u32), 8 B pad, then `compSize`
bytes of LZ4 data. If LZ4 does not actually shrink a given chunk, the block
stores that chunk's data raw/uncompressed instead (`compSize == rawSize` in
that case). Walking through 80 of these blocks consumes exactly `Length`
bytes.

**RectangleHeader** — 24 B, the **last 24 bytes of the pre-header header** in
every frame (not just partials): `ImageType`(u16, 1=Gray), `ScreenID`(u16),
`X`(u16), `Y`(u16), `Width`(u16), `Height`(u16), `RectangleUpdateOptions`(u16,
0x0102 for 4-bit), `Options`(u16), `Encoding`(u16, 4), `Reserved`(u16),
`PayloadLength`(u32, = `Width*Height/2`). It is the region that the payload
paints. This is the whole screen (`0,0,1024,758`) for a full frame, and a
sub-rectangle for a partial update. See [partial-updates.md](partial-updates.md).

**DataHeader** — 36 B: `Priority`, `UUID` (16 B), `Type`, `ID`, `Length`.
If the four unlabeled scalar fields (`Priority`, `Type`, `ID`, `Length`)
are u32 each, matching this doc's convention for unlabeled scalars
elsewhere, the fields sum to only 32 B, 4 B short of the stated 36 B — one
of them is likely wider than u32 (`ID` at 8 B would reconcile it exactly),
but the DWARF dump this table came from didn't record it. `DataHeader`
isn't implemented anywhere in `bridge/pv3/*.go`, so this can't be checked
against the bridge's own code; flagging it here as unconfirmed rather than
guessing.

## Compression

`vss/lz4.Lz4Compress` is a CGO wrapper around the stock LZ4 C library
(`LZ4_compress` / `LZ4_decompress_safe`). It uses plain, stateless LZ4, with
no dictionary.

## Encode pipeline (in VSS)

```
CompressPacket(packet) → Marshall(packet) → ToBlocks: chunk into PayloadLength
(4800)-byte pieces, LZ4 each → dataBlocks.
```

## Block / pixel coverage

The panel is 1024×758 pixels, at 4-bit grayscale. This equals 388096 bytes,
at 2 pixels per byte, or 512 bytes per row. The 80 `dataBlock`s cover
`packed[0:383376]`, which is 79×4800 + 4176 bytes. `383376 / 512 = 748.78125`,
so the blocks do not end on a row boundary: they fully cover the top 748
rows, plus the first 400 bytes of row 749. The pre-header carries the
remaining 4720 bytes: `packed[383376:388096]` — the last 112 bytes of row
749, plus the last 9 full rows after it (`112 + 9×512 = 4720`). The panel
places these bytes at the end of the framebuffer, after the fixed 180°
rotation.

## The pre-header

```
pre-header = 60-byte preamble + RectangleHeader (24 B) + tail pixels (raw, 4720 B)
```

The 60-byte preamble carries the panel's UUID
(`42 00 28 00 0d 51 37 31 37 34 39 34`) and the `PacketImage` type (5). It
also carries two nonce fields that the panel does not validate, and two
payload-size fields that the panel does validate: byte 32 is `payload + 44`,
and byte 52 is `payload + 24`, where `payload` is the region's byte count.
The `RectangleHeader` above is the region descriptor. The tail is the last
4720 bytes of the region's pixels, the part the blocks do not cover.
`ImageHeader.Options` equals the pre-header region length minus 4.

## How the bridge builds a frame

`pv3/encode.go` regenerates the pre-header for every frame. `buildPreHeader`
combines the fixed 84-byte header (60-byte `preamble` plus a 24-byte
`rectHeader(r)`) with the image's real tail pixels,
`packed[383376:388096]`, sent raw, and sets
`Options = len − 4`. The bridge LZ4-compresses the 80 blocks, but sends the
tail raw. The panel accepts a raw tail; VSS itself sends raw data for
content it cannot compress well. This is why the bottom of the panel — the
last 9 rows plus a sliver of the row above them — shows live content, not a
frozen image.
