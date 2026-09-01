# Partial updates (e-ink dirty-rectangle)

**Status: implemented and live.** The bridge sends a partial update when a
new image differs from what the panel currently shows in only a small
region. A partial update sends only the changed rectangle. Otherwise the
bridge sends a full frame. `pv3.EncodePartial` builds the frame. The `serve`
loop decides between a partial and a full update. We validated this on the
real panel: we saw an image-ACK and a flicker-free local refresh. We used a
replayed real VSS partial update as a control.

## The format

A partial update uses the same frame format as a full image, with a smaller
rectangle embedded in it. In VSS, a full update and a partial update go
through one encoder: `render.DisplaysEncoder.generateImagePacketUnlocked`,
which calls `isFullScreenUnlocked`, `getRectsAndClear`, and
`forceFullScreen`. The only difference is the rectangle list. A full update
has one rectangle that covers the whole screen.

The pre-header's 84-byte header portion has two parts: a 60-byte preamble
and a 24-byte `RectangleHeader` (see
[pv3-frame-format.md](pv3-frame-format.md), which calls this the
"pre-header header" to distinguish it from the full pre-header — the
84-byte header plus the tail pixels that follow it). For a full frame, the
rectangle covers the whole screen:

```
ImageType=1(Gray) ScreenID=0 X=0 Y=0 Width=1024 Height=758
RUO=0x0102 Options=0 Encoding=4 Reserved=0 PayloadLength=388096   # = 1024*758/2
```

A partial update just shrinks this rectangle. Compared with `EncodeFrame`, a
partial frame differs in these ways:

1. The `RectangleHeader` carries the sub-region as `X, Y, Width, Height`,
   with `PayloadLength = Width*Height/2`.
2. The blocks carry only the region's pixels. There are far fewer than 80 of
   them.
3. `ImageHeader.NrPrimitives` = that block count.
4. The preamble's payload-size fields are rewritten for the region (below).

Everything else is identical: the preamble shape, the `ImageHeader`, the
`ProtocolHeader`, the LZ4 block format
`[BlockID][BlockLast][compSize][rawSize][pad8]`, and the CRC32.

### Region pixel layout

The region uses region-packed layout: `H` rows by `W/2` bytes, row-major,
with stride `W/2` (not the panel's stride of 512). The bridge reads it from
the same packed framebuffer that a full frame uses. It applies the
colStartOffset scan shift. Region byte `(r, cb)` is:

```
framebuffer[(Y+r)*512 + ((X/2 + cb − 112) mod 512)]    # 112 = colStartOffset/2 = 224/2
```

Rows do not wrap. Columns wrap at 512, from the −224 px shift. There is no
extra rotation between the region and the framebuffer. The 180° rotation and
the colStartOffset shift apply equally to full frames and partial frames.

### The tail/block split (the "4720 constant")

For every frame, `PayloadLength (= W*H/2) == 4720 + Σ block rawSize`. The
region payload has two parts: the pre-header tail (the last 4720 bytes),
then the block stream (the rest). The block stream is chunked at a rawSize
of 4800, the same as a full frame, where the tail is 4720 bytes and the
blocks cover the first 383376 bytes. A multi-rectangle capture tiles three
regions back-to-back in this payload with zero bytes left over. This
confirms the split byte for byte.

### Coordinate transform (panel is 180°)

In `common.Display.RectangleTranslateToReal`, with rotation 2 (180°) on a
full-panel display, a dirty rectangle `(rx, ry, rw, rh)` in image space maps
to:

```
X = 1024 − rx − rw    Y = 758 − ry − rh    Width = rw    Height = rh
```

This is a 180° point-reflection, clamped to the panel's bounds. The bridge
diffs images directly in packed panel space. So this transform is already
built into `EncodePartial`.

### Field values

`ImageType=1` (Gray), `Encoding=4` (4-bit), `RectangleUpdateOptions=0x0102`
(`0x0101` for 1-bit), `Options=0`, `ScreenID=0`, `Reserved=0`.
`ImageHeader.Options` = pre-header length − 4.

**Preamble payload-size fields (mandatory):** the panel validates these
fields. If they are wrong, the panel silently drops the frame. Byte 32 (u32)
is `payload + 44`. Byte 52 (u32) is `payload + 24`, where `payload = W*H/2`.
Byte 44 (u32) is the rectangle count, which is 1 for a full frame. We found
these values by diffing the full (1-rectangle) and multi-rectangle
(3-rectangle) captured preambles. This is the one requirement that is not
obvious. We found it by comparing a generated frame against a replayed real
partial update on the panel.

## How the bridge uses it

`pv3.EncodePartial(prev, next)` diffs two packed framebuffers. It finds the
dirty bounding box and builds a region frame. It returns `ok=false` for a
whole-screen change, or for a region too small to fill the 4720-byte tail;
in both cases the bridge sends a full frame instead. The `serve` loop tracks
the framebuffer that the panel has actually rendered. It updates this only
on an image-ACK, and resets it on each new connection. So the first push on
a connection is a full frame, and a reconnect re-syncs with a full frame. A
partial update is only ever diffed against a frame the panel is known to be
showing. The bridge skips identical re-polls entirely. So unchanged content
no longer triggers a full-screen refresh.

## How it was reverse-engineered

The VSS binaries ship unstripped, with DWARF debug info (see
[vss-binary-recon.md](vss-binary-recon.md)). The `RectangleHeader` layout,
the coordinate transform, and the field values came from disassembling the
symbols below. We then confirmed the pixel layout, the 4720 split, and the
preamble size-field requirement. We did this by capturing real VSS partial
updates with a MITM setup (one full update and four partial updates) and
decoding them. The captures and the decode scripts are in
`exploration/captures-partial-re/` (this folder is gitignored).

| Symbol (in `gateway`) | What it does |
|---|---|
| `common.Display.RectangleTranslateToReal` | session↔panel coordinate transform (rotation-aware; 180° = flip, no w/h swap) |
| `vss/proto.CreateRectangles` | builds the rectangle list; sets RUO from encoding (4-bit → 0x0102) |
| `render.DisplaysEncoder.generateImagePacketUnlocked` | the real encoder that sends data to the panel; chooses between full and partial |

## Value

The benefit is a flicker-free update that uses only a fraction of the
bytes. This matters most for a busy background with small, local changes,
such as a dashboard with a live clock or one changing widget. When a change
touches most of the screen, `EncodePartial` falls back to a full frame. The
bridge also skips unchanged content. So the bridge never sends a partial
update that would not help.
