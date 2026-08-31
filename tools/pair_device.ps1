<#
.SYNOPSIS
    Point a Joan-6 e-ink panel's serial console at a self-hosted bridge server
    (instead of Visionect's cloud), and reboot it to apply the change.

.DESCRIPTION
    This formalizes a process that was previously only ever done by hand:
    connect to the Joan-6's serial console - an FTDI UART exposed over the
    Micro-USB port, 115200 8N1 - read back its current server_tcp_get
    setting, then send server_tcp_set / flash_save / reboot to point it at a
    self-hosted trmnl-joan-bridge server instead of Visionect's cloud (the
    observed factory default is we3.gw.getjoan.com 11113).

    IMPORTANT (observed hardware quirk): the device's serial console can be
    asleep when you first connect, and the very first thing sent after
    opening the port can come back garbled - the device's own internal log
    shows a "Wakeup" event triggered by the first received byte. This script
    handles that automatically: it sends one bare line terminator to wake the
    console, waits briefly, then discards whatever garbled response comes
    back without trying to parse it, before sending any real command.

    Verified end to end against a real Joan-6 on 2026-08-31: the device was
    deliberately reset to an unreachable placeholder address first, then
    this script correctly read that state back, repointed it, saved, and
    rebooted it, with no changes needed to the terminator or timing values
    below. It's still written defensively rather than cleverly, since it
    was new and unverified when first written:
      - It does NOT assume this console's line-ending/echo/prompt behavior.
        Responses are read with an idle-gap poll (accumulate text until a
        short quiet period, or an overall timeout, is hit) instead of a
        ReadLine() keyed to a terminator that might not match reality and
        could hang forever.
      - The command line terminator sent by this script is a single easily
        changed constant ($CommandTerminator below), in case `\r` turns out
        to be wrong for this console during real-device testing.
      - The current server setting is sanity-checked before anything is
        changed, and the destructive step (server_tcp_set / flash_save /
        reboot) requires explicit confirmation.
      - The serial port is always closed on the way out, even on error -
        an unclosed port locks the COM port for every subsequent attempt,
        including a re-run of this exact script.

    This script deliberately does NOT send `encryption_mode_set`. That
    command is documented for this device, but was confirmed NOT part of the
    actual working pairing sequence for this project. See
    Protocol_Bypass_Research.md (repository root) if a future device or
    firmware ever needs it.

.PARAMETER ComPort
    The Windows COM port name the device's serial console is on, e.g. COM3.
    Required. Check Windows Device Manager > Ports (COM & LPT) if unsure -
    the FTDI UART typically shows up there once the Joan is connected via
    Micro-USB.

.PARAMETER ServerIP
    The self-hosted bridge server's LAN IP address to point the device at.
    Required.

.PARAMETER ServerPort
    The bridge's TCP listen port. Optional, default: 11112 - this matches
    bridge/.env.example's LISTEN_ADDR default of ":11112". Port 11112 is
    the unencrypted PV3 port; 11113 is the TLS-wrapped variant and is NOT
    what this bridge listens on.

.PARAMETER Force
    Skip the "About to point this device at..." Y/n confirmation prompt and
    proceed immediately. Off by default - the pairing step disconnects the
    device from whatever server it currently talks to, so confirmation is
    required unless this switch is passed.

.EXAMPLE
    .\pair_device.ps1 -ComPort COM3 -ServerIP 10.218.10.61 -ServerPort 11112

.EXAMPLE
    .\pair_device.ps1 -ComPort COM3 -ServerIP 10.218.10.61 -Force
#>

param(
    [Parameter(Mandatory = $true)]
    [string]$ComPort,

    [Parameter(Mandatory = $true)]
    [string]$ServerIP,

    [int]$ServerPort = 11112,

    [switch]$Force
)

# --- Serial port configuration --------------------------------------------
# 115200 8N1, matching the observed FTDI UART on the Joan-6's Micro-USB port.
$BaudRate = 115200
$DataBits = 8
$Parity = [System.IO.Ports.Parity]::None
$StopBits = [System.IO.Ports.StopBits]::One

# PowerShell's SerialPort defaults can block indefinitely if left unset -
# always set these explicitly.
$SerialReadTimeoutMs = 3000
$SerialWriteTimeoutMs = 3000

# The console's real line-ending convention has never actually been captured
# for this device. `\r` is the most common convention for this class of
# embedded serial console, but if real-device testing shows otherwise, this
# is the one place to change it.
$CommandTerminator = "`r"

# Wake-step timing (see IMPORTANT note above / in .DESCRIPTION).
$WakeWaitMs = 500

# Idle-gap response polling: accumulate ReadExisting() output until either
# IdleGapMs passes with no new data (after having received something), or
# OverallTimeoutMs is hit with nothing received at all.
$ResponseOverallTimeoutMs = 3000
$ResponseIdleGapMs = 400
$ResponsePollIntervalMs = 100

# ----------------------------------------------------------------------
# Reads a response from the serial port defensively: no assumption is made
# about a line terminator. Instead this polls ReadExisting() in a loop,
# accumulating text, and returns once either a short quiet period follows
# some received data, or the overall timeout elapses with nothing at all.
# ----------------------------------------------------------------------
function Read-SerialResponse {
    param(
        [Parameter(Mandatory = $true)]
        [System.IO.Ports.SerialPort]$Port,

        [int]$OverallTimeoutMs = $ResponseOverallTimeoutMs,
        [int]$IdleGapMs = $ResponseIdleGapMs,
        [int]$PollIntervalMs = $ResponsePollIntervalMs
    )

    $overallStopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    $sinceLastDataStopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    $received = New-Object System.Text.StringBuilder
    $gotAnyData = $false

    while ($true) {
        Start-Sleep -Milliseconds $PollIntervalMs

        $chunk = ""
        try {
            $chunk = $Port.ReadExisting()
        }
        catch {
            # A transient read hiccup here isn't fatal to the poll - just
            # treat this cycle as "no data" and keep going until a timeout.
            $chunk = ""
        }

        if ($chunk.Length -gt 0) {
            [void]$received.Append($chunk)
            $gotAnyData = $true
            $sinceLastDataStopwatch.Restart()
        }

        if ($gotAnyData -and $sinceLastDataStopwatch.ElapsedMilliseconds -ge $IdleGapMs) {
            break
        }
        if ($overallStopwatch.ElapsedMilliseconds -ge $OverallTimeoutMs) {
            break
        }
    }

    return $received.ToString()
}

# ----------------------------------------------------------------------
# Sends one command (with the configured terminator appended), reads the
# response with the idle-gap poll above, prints both, and returns the raw
# response text.
# ----------------------------------------------------------------------
function Send-Command {
    param(
        [Parameter(Mandatory = $true)]
        [System.IO.Ports.SerialPort]$Port,

        [Parameter(Mandatory = $true)]
        [string]$Command
    )

    Write-Host "Sending: $Command"
    $Port.DiscardInBuffer()
    $Port.Write("$Command$CommandTerminator")
    $response = Read-SerialResponse -Port $Port
    Write-Host "Response: $($response.Trim())"
    return $response
}

# =============================================================================
# Main
# =============================================================================

Write-Host "Opening $ComPort at $BaudRate 8N1..."

$port = New-Object System.IO.Ports.SerialPort
$port.PortName = $ComPort
$port.BaudRate = $BaudRate
$port.DataBits = $DataBits
$port.Parity = $Parity
$port.StopBits = $StopBits
$port.ReadTimeout = $SerialReadTimeoutMs
$port.WriteTimeout = $SerialWriteTimeoutMs

# The ENTIRE port lifecycle from here on is wrapped in try/finally so Close()
# always runs, even on error or an uncaught exception. An unclosed port locks
# the COM port for every subsequent attempt, including a re-run of this exact
# script.
try {
    try {
        $port.Open()
    }
    catch [System.UnauthorizedAccessException] {
        Write-Host ""
        Write-Host "ERROR: Access to $ComPort was denied." -ForegroundColor Red
        Write-Host "This usually means another program already has the port open:" -ForegroundColor Red
        Write-Host " - a terminal app (PuTTY, Tera Term, the Arduino IDE serial monitor, etc.)" -ForegroundColor Red
        Write-Host " - a previous run of this exact script that didn't exit cleanly" -ForegroundColor Red
        Write-Host "Close whatever else is using $ComPort and try again." -ForegroundColor Red
        return
    }
    catch {
        Write-Host ""
        Write-Host "ERROR: Failed to open ${ComPort}: $($_.Exception.Message)" -ForegroundColor Red
        Write-Host ""
        Write-Host "Ports Windows currently sees:" -ForegroundColor Yellow
        $available = [System.IO.Ports.SerialPort]::GetPortNames()
        if ($available.Count -eq 0) {
            Write-Host "  (none)"
        }
        else {
            $available | Sort-Object | ForEach-Object { Write-Host "  $_" }
        }
        Write-Host ""
        Write-Host "Check that -ComPort matches one of the ports listed above." -ForegroundColor Yellow
        return
    }

    Write-Host "Port open."

    # IMPORTANT: if the port opened successfully above but the device never
    # responds to anything below (every Read-SerialResponse call comes back
    # empty), some FTDI-based USB-serial adapters need DTR and/or RTS
    # asserted before the UART actually starts talking. This has never been
    # confirmed necessary for this specific device/adapter combination, so it
    # is left commented out on purpose - uncomment if needed:
    # $port.DtrEnable = $true
    # $port.RtsEnable = $true

    try {
        # --- Wake step ------------------------------------------------
        # See IMPORTANT note in .DESCRIPTION: the console can be asleep, and
        # the first thing sent after opening the port can come back garbled.
        # Send a single bare line terminator (not a real command word - a
        # real command-like string risks the console actually acting on it)
        # to wake it, then discard whatever garbled response comes back.
        Write-Host ""
        Write-Host "Waking console..."
        $port.Write($CommandTerminator)
        Start-Sleep -Milliseconds $WakeWaitMs
        $port.DiscardInBuffer()

        # --- Step 1: read and sanity-check the current server setting --
        Write-Host ""
        Write-Host "Querying current server setting (server_tcp_get)..."
        $port.DiscardInBuffer()
        $port.Write("server_tcp_get$CommandTerminator")
        $currentRaw = Read-SerialResponse -Port $port
        $currentTrimmed = $currentRaw.Trim()

        Write-Host "Current server setting: $currentTrimmed"

        # Light sanity check: a plausible "host port" (or "host:port") value
        # should be non-empty and contain a space or colon separating the two
        # fields. If it doesn't look like that, don't silently march on.
        $looksPlausible = ($currentTrimmed.Length -gt 0) -and
                           ($currentTrimmed.Contains(' ') -or $currentTrimmed.Contains(':'))

        if (-not $looksPlausible) {
            Write-Host ""
            Write-Host "WARNING: that response doesn't look like a plausible 'host port' value." -ForegroundColor Yellow
            Write-Host "Raw response (may include echoed prompt/control characters):" -ForegroundColor Yellow
            Write-Host "  [$currentRaw]"
            $sanityAnswer = Read-Host "Continue anyway? (y/N)"
            if ($sanityAnswer -notmatch '^[Yy]') {
                Write-Host "Aborting at user's request. No changes made."
                return
            }
        }

        # --- Step 2: confirm the destructive change -------------------
        Write-Host ""
        Write-Host "About to point this device at ${ServerIP}:${ServerPort} and reboot it." -ForegroundColor Cyan
        Write-Host "This will disconnect it from whatever server it currently talks to." -ForegroundColor Cyan

        if (-not $Force) {
            $confirmAnswer = Read-Host "Proceed? (y/N)"
            if ($confirmAnswer -notmatch '^[Yy]') {
                Write-Host "Aborting - no changes made."
                return
            }
        }

        # --- Step 3: apply the change -----------------------------------
        Write-Host ""
        [void](Send-Command -Port $port -Command "server_tcp_set $ServerIP $ServerPort")
        [void](Send-Command -Port $port -Command "flash_save")
        [void](Send-Command -Port $port -Command "reboot")

        # NOTE: encryption_mode_set is intentionally NOT sent here. It's a
        # documented command for this device, but was confirmed NOT part of
        # the actual working pairing sequence for this project.
        Write-Host ""
        Write-Host "NOTE: this device also supports an 'encryption_mode_set 0/1' command" -ForegroundColor DarkGray
        Write-Host "(documented for this device family), but it was confirmed NOT part of" -ForegroundColor DarkGray
        Write-Host "the actual working pairing sequence for this project, so it was NOT sent" -ForegroundColor DarkGray
        Write-Host "above. See Protocol_Bypass_Research.md (repository root) if a future" -ForegroundColor DarkGray
        Write-Host "device/firmware combination ever needs it." -ForegroundColor DarkGray

        Write-Host ""
        Write-Host "Done. The device should now be rebooting and connecting to the bridge." -ForegroundColor Green
        Write-Host "On the bridge host, run:" -ForegroundColor Green
        Write-Host "    docker logs -f trmnl-joan-bridge" -ForegroundColor Green
        Write-Host "and watch for this exact sequence (device address will vary):" -ForegroundColor Green
        Write-Host "    [<device-ip>:<port>] connected"
        Write-Host "    [<device-ip>:<port>] pushed full frame (<N> bytes)"
        Write-Host "    [<device-ip>:<port>] image ACK received"
        Write-Host "That last line is the real proof the device is displaying the bridge's content." -ForegroundColor Green
    }
    catch {
        Write-Host ""
        Write-Host "ERROR: something went wrong talking to the device: $($_.Exception.Message)" -ForegroundColor Red
        Write-Host "The device may not have responded as expected, or a read/write timed out." -ForegroundColor Red
        return
    }
}
finally {
    if ($port.IsOpen) {
        $port.Close()
    }
    $port.Dispose()
}
