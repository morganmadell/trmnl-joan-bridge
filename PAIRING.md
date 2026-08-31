# Pairing a Joan-6 to this bridge

## 1. What this covers

This document covers pairing a physical Joan-6 e-ink panel to this
project's self-hosted bridge server over the device's own serial console —
no Visionect account, no VSS (Visionect Software Suite) instance, ever.

It assumes the bridge server is already deployed and running (see the main
`bridge/README.md` and `bridge/deploy.sh`) and that you already know two
things about it: its LAN IP address, and the port it's listening on
(default `11112`).

## 2. Proven vs. unverified

| | Status |
|---|---|
| The manual command sequence (`server_tcp_get` → `server_tcp_set` → `flash_save` → `reboot`, 115200 8N1, direct USB) | **Proven on real hardware**, 2026-08-28 and again 2026-08-31 — this is literally how this project's own device was paired, twice. |
| `pair_device.ps1` itself | **Proven on real hardware, 2026-08-31.** Run end to end against a real Joan-6 that had been deliberately reset to an unreachable placeholder address first: the script correctly read the reset state back, applied the new address, saved, and rebooted, with no adjustments needed to the terminator character or timing values. The device reconnected to the bridge and pushed a real frame that got ACKed. |
| Pairing on Mac/Linux | **Unverified** — never tried. A generic serial tool at the same settings (115200 8N1) should work, but this is a guess, not a tested fact. |
| This procedure on a Joan-6 with different firmware | **Unverified** — this project only tested against the one firmware version its unit shipped with. Command names and behavior could differ on other firmware. |

## 3. Physical connection

Connect the Joan-6 to your PC with a **Micro-USB cable, plugged in
directly** — no USB hub, no dock, no passthrough adapter in between.

This project hit a real failure mode from ignoring that: connecting through
a USB hub made Windows show only a generic **"USB-C alt-mode Billboard"**
device descriptor, not the panel's actual serial console. If you see that
instead of a serial port showing up, the fix is to remove the hub and plug
the cable straight from the Joan-6 into a USB port on the PC.

## 4. Finding the device

**Windows:**

1. Open **Device Manager**.
2. Expand **Ports (COM & LPT)**.
3. Look for an FTDI-based **USB Serial Port** entry.
4. Note its COM number — e.g. `COM3`. This is just an example from one
   specific setup; expect a different number on your PC.

**Mac/Linux:** this project only ever paired a device from Windows, so
nothing below is tested. On Mac/Linux you'd expect the device to show up as
a `/dev/tty*`-style device path instead of a COM port (e.g. something under
`/dev/tty.usbserial-*` on macOS or `/dev/ttyUSB*` on Linux), but treat that
as unverified guidance, not a confirmed procedure.

## 5. Running pair_device.ps1

Example invocation:

```powershell
.\pair_device.ps1 -ComPort COM3 -ServerIP 10.218.10.61 -ServerPort 11112
```

- `-ComPort` (required) — the COM port you found in section 4, e.g. `COM3`.
- `-ServerIP` (required) — your bridge server's LAN IP address.
- `-ServerPort` (optional) — defaults to `11112`, the bridge's listen port.

What the script does, in order, and why:

1. **Opens the serial console** at 115200 baud, 8 data bits, no parity, 1
   stop bit (115200 8N1).

2. **Works around a sleep/wake quirk.** The Joan-6's serial console can be
   asleep when you first connect. If the very first command is sent while
   it's asleep, the response that comes back is garbled — not because
   anything is broken, but because the device is still waking up mid-reply.
   The script handles this automatically: it sends a blank line first to
   wake the console, discards whatever garbled response comes back from
   that, and only then proceeds with real commands. You don't need to know
   about this or retry anything by hand — it's mentioned here so that if
   you ever do this manually (section 6) and see a garbled first response,
   you'll recognize it as this quirk and not a broken connection.

3. **Reads the current server setting** with `server_tcp_get` and prints
   it. This tells you what server (Visionect's cloud, by default, or
   whatever the device was last pointed at) the device currently thinks
   it's talking to.

4. **Asks for confirmation before proceeding.** The next step disconnects
   the device from whatever it's currently paired with, so the script
   pauses here to let you confirm you actually want to repoint it.

5. **Runs the actual repointing sequence**, in this exact order:
   - `server_tcp_set <ServerIP> <ServerPort>` — tells the device to talk to
     your bridge server instead.
   - `flash_save` — persists that setting to the device's flash storage so
     it survives a reboot.
   - `reboot` — restarts the device so it reconnects using the new setting.

Note what the script deliberately does **not** run: `encryption_mode_set`.
That command exists and is documented as available on this device, but it
is confirmed to not be part of the actual working pairing sequence this
project used — it's left out on purpose, not by oversight.

## 6. Manual/fallback path

If you'd rather type commands into a generic serial terminal (e.g. PuTTY,
or any terminal that supports raw serial connections) instead of running
the script, use the same settings and the same command sequence in the same
order:

1. Open a raw serial connection at **115200 8N1** to the device's COM port
   (or `/dev/tty*` path).
2. `server_tcp_get` — read and confirm the current setting first, before
   changing anything.
3. `server_tcp_set <bridge-ip> <bridge-port>` — point the device at your
   bridge server.
4. `flash_save` — persist the change.
5. `reboot` — restart the device to apply it.

If your very first command comes back garbled, see the sleep/wake
explanation in section 5, step 2 — send a blank line first, discard that
response, then retry the real command.

## 7. A hostname discrepancy, correctly sized

This project's technical research doc (`Protocol_Bypass_Research.md`, in
the parent directory) documents `we3.gw.getjoan.com 11113` as an observed,
general factory-default server address for Joan devices. But this specific
project's own device logged a different value —
`wu.gw.getjoan.com:11113` — when queried during actual pairing (see
`Claude_Work.md`, in the parent directory, for that log). Both are
plausible real Visionect cloud hosts, and this discrepancy was never
reconciled. It doesn't matter for pairing: `server_tcp_get` here is only a
sanity check that you're talking to a real, responsive console before you
overwrite the value with `server_tcp_set` — not something that needs to
match any particular expected string. Don't be concerned if your own device
shows yet another value entirely.

## 8. Verification

Once the device reboots and connects to your bridge, watch the bridge's
logs:

```
docker logs -f trmnl-joan-bridge
```

You should see these three lines, in order:

1. `[<device-ip>:<port>] connected`
2. `[<device-ip>:<port>] pushed full frame (<N> bytes)`
3. `[<device-ip>:<port>] image ACK received`

That last line is the real confirmation — it means the device actually
rendered and displayed the bridge's content, not just that it connected at
the network level.

**Expect this to take a couple of tries.** In both real pairings this
project has done, the device's first one or two connection attempts right
after a reboot failed on their own (one wire-decode error, one that
connected then dropped after a few seconds) before a third attempt
succeeded cleanly. This is normal first-connection-after-reboot noise, not
a sign anything is wrong — give it 30-60 seconds and a few attempts before
concluding pairing failed.

If the device never appears in the logs at all:

- Check `bridge/README.md`'s WSL2/firewall networking section, if the
  bridge is hosted there — a common cause is the bridge's listening port
  not actually being reachable from outside the host.
- Double-check the `-ServerIP` you gave the script is the bridge host's
  real, LAN-reachable address — not an internal or virtual one. For
  example, if the bridge runs inside WSL2 but something outside WSL2 (like
  the Joan-6, over your LAN) needs to reach it, a WSL2-internal IP will not
  work; you need the Windows host's LAN-reachable IP instead.

## 9. Alternative: Visionect Configurator

Visionect publishes its own official **Configurator** desktop app, which is
a named alternative GUI tool for talking to Joan devices over USB. This
project never used or verified it for pointing a device at this specific
self-hosted bridge — it was only used for the separate, VSS-based fallback
paths documented in `Licensing.md`. It isn't documented here as a
procedure for that reason; it's mentioned only as something that exists, in
case you'd rather use official tooling for the device-discovery/USB-
connection part instead of Device Manager and a serial console.
