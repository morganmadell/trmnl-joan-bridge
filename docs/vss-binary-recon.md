# What we learned from static analysis of the VSS image

The public `visionect/visionect-server-v3:8.5.5-arm` Docker image gives us a
lot of information about how Joan panels talk to VSS. Three mistakes in how
Visionect shipped this image help us:

1. **The binaries are not stripped, and they include DWARF debug info**
   (`networkmanager`, `gateway`, `engine`). The symbol tables show every Go
   package, function, and method name, with the full path. The `gateway`
   binary alone has about 73,000 symbols.
2. **The image ships `/opt/visionect/vss/bin/dlv`**, the Delve
   source-level Go debugger, alongside the production binaries. We can step
   through any function, set breakpoints, and inspect Go types while the
   program runs.
3. **Pre-rendered images at native panel resolutions** sit in
   `/opt/visionect/vss/images/blocked_device_*.png`. The 1024x758 variant
   (note: 758, not 768) is for the Joan 6 panel. This suggests 10 px of
   header and footer space that the protocol does not paint.

## Architecture

Wire path for a Joan-class panel:

```
[Joan device] --TCP:11112--> [gateway] --gRPC--> [networkmanager] --DB/Redis--> [admin/engine/ac-render]
                                  |
                                  +-- HTTP --> [admin UI on https://*:8081]
```

`gateway` is the wire frontend. `networkmanager` owns the device state. The
internal gRPC service is `grpc_networkmanager.Networkmanager/DeviceGateway`.

## Two device protocols coexist

- **Legacy "VSS" protocol** — `vss/pkg/ac.HandleVSS`, framed by
  `vss/pkg/utils/vpacket`. This is a custom binary format. It would need
  reverse engineering.
- **Newer "AC" protocol** — `vss/pkg/ac.HandleCBOR`, `createCBORPacket`. It
  uses CBOR (RFC 8949) to encode the body. This is a documented format, so
  we do not need to invent the framing. We only need to work out the
  schema.

## Command vocabulary

These are the command verbs the protocol understands. They are literal
strings found in `gateway` and `networkmanager`:

`SendImage`, `update_fw`, `update_bl`, `Sends device to sleep mode`, `Mobile power saving timeout`, `Mirroring`, `T2S speak`, `flashing`, `Firmware`, `checksum`, `wifi.json`, `status packet`, `heartbeat`, `proximity`, `Server IP`, `engineID`, `engineIP`.

Inferred boot flow of the panel:
1. The panel boots with `Server IP` and `engineIP` values already stored in
   NVRAM.
2. The panel fetches `config.json` from the server. This is probably an
   HTTP-style request; the string `getVersion(): Getting config.json`
   appears in the binary.
3. The panel opens a TCP connection on port 11112 to `gateway`.
4. The panel sends a status message that announces itself.
5. The server replies with either AC (CBOR) commands or legacy VSS framed
   commands.

## Reusable modules inside the image

- **`vss/pkg/driver`** — clean and small, with about 50 functions. It
  converts 8 bpp grayscale into byte streams encoded for a specific panel.
  Registered drivers: `eInkGeneric`, `eInkFlip`, `eInk32InchColorMask{,Flip}`,
  `plGeneric`. Bit-depth converters: `encode_eight_to_one`,
  `encode_eight_to_four`. If we write a custom server, this is the hardest
  part to recreate. Here, we can just read it.
- **`vss/pkg/utils/vpacket/pkgutil`** — `prependHeader`, `Image.Dump`,
  `ImageToRectanges`. This is the wire-level message format, including
  dirty-rectangle partial updates for e-ink.

## Possible shortcut

The license check that blocked us from running VSS live lives inside the
`networkmanager` binary. Delve is already shipped with the image. In
principle, we could replace this check with a NOP instruction, or step over
it during debugging. We have not done this. It risks violating the vendor's
terms of service. But it is a known option, if a researcher wants to
capture an authoritative transcript between VSS and the panel.

## How to reproduce the extraction

```sh
mkdir -p /tmp/vss-analysis
container run --rm --entrypoint /bin/sh \
  --mount type=bind,source=/tmp/vss-analysis,target=/host \
  visionect/visionect-server-v3:8.5.5-arm \
  -c 'cp /opt/visionect/vss/bin/{networkmanager,gateway,engine} /host/'

# Functions in the `device` package, etc.
strings /tmp/vss-analysis/gateway | grep -E '^vss/pkg/[a-z]+\.' | sort -u
```
