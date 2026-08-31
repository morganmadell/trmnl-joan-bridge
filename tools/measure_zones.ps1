<#
.SYNOPSIS
    Find the real pixel y-boundaries of rendered dashboard text blocks, to
    build accurate bridge/zones.json touch-zone rectangles.

.DESCRIPTION
    This formalizes the technique actually used to re-measure zones.json's
    real touch-zone rectangles against a live device render: scan every row
    of a screenshot for dark ("text") pixels, and report the contiguous
    y-ranges where text is present as candidate zone boundaries.

    IMPORTANT: -ImagePath should point at a REAL screenshot pulled from a
    running bridge's DEBUG_SAVE_SCREENSHOTS directory (latest-render.png) —
    specifically, the literal frame that was actually pushed to and ACKed by
    the physical Joan panel. Do NOT point this at a fresh, unrelated render;
    layout can shift between renders (card order, entity state, HA frontend
    version, etc.), and the whole point is that the measured coordinates
    match what's really on the panel's screen right now.

    This script only finds VERTICAL (row) boundaries. Horizontal (column)
    boundaries for multi-column layouts (e.g. splitting a nav row into 3
    page-switch zones side by side) must still be reasoned about separately
    — e.g. dividing the known page width evenly across columns, or by
    extending this same row-scanning technique per vertical column slice if
    the columns aren't evenly spaced.

.PARAMETER ImagePath
    Path to the PNG screenshot to scan. Required.

.PARAMETER DarkThreshold
    Luminance threshold (0-255) below which a pixel counts as "text".
    Default: 150.

.PARAMETER RowGapThreshold
    Number of consecutive non-text rows that still count as part of the same
    text block (i.e. gaps smaller than this are bridged rather than starting
    a new block). Default: 5.

.PARAMETER XStep
    Pixel-column sampling stride, for speed — every XStep'th column is
    checked instead of every column. Default: 2.

.EXAMPLE
    .\measure_zones.ps1 -ImagePath C:\path\to\latest-render.png

.EXAMPLE
    .\measure_zones.ps1 -ImagePath .\latest-render.png -DarkThreshold 130 -RowGapThreshold 3
#>

param(
    [Parameter(Mandatory = $true)]
    [string]$ImagePath,

    [int]$DarkThreshold = 150,

    [int]$RowGapThreshold = 5,

    [int]$XStep = 2
)

Add-Type -AssemblyName System.Drawing

if (-not (Test-Path $ImagePath)) {
    Write-Error "Image not found: $ImagePath"
    exit 1
}

$resolvedPath = (Resolve-Path $ImagePath).Path
$bitmap = [System.Drawing.Bitmap]::new($resolvedPath)

try {
    $width = $bitmap.Width
    $height = $bitmap.Height

    # For each row, find the minimum luminance across a strided sample of
    # columns. A row is "dark" (contains text) if that minimum falls below
    # DarkThreshold.
    $isDarkRow = New-Object bool[] $height

    for ($y = 0; $y -lt $height; $y++) {
        $minLuminance = 255.0
        for ($x = 0; $x -lt $width; $x += $XStep) {
            $pixel = $bitmap.GetPixel($x, $y)
            $luminance = 0.3 * $pixel.R + 0.59 * $pixel.G + 0.11 * $pixel.B
            if ($luminance -lt $minLuminance) {
                $minLuminance = $luminance
            }
        }
        $isDarkRow[$y] = ($minLuminance -lt $DarkThreshold)
    }

    # Group consecutive dark rows into blocks, bridging gaps smaller than
    # RowGapThreshold.
    $blocks = New-Object System.Collections.Generic.List[object]
    $blockStart = -1
    $gapCount = 0

    for ($y = 0; $y -lt $height; $y++) {
        if ($isDarkRow[$y]) {
            if ($blockStart -eq -1) {
                $blockStart = $y
            }
            $gapCount = 0
        }
        elseif ($blockStart -ne -1) {
            $gapCount++
            if ($gapCount -ge $RowGapThreshold) {
                $blockEnd = $y - $gapCount
                $blocks.Add([pscustomobject]@{ Start = $blockStart; End = $blockEnd })
                $blockStart = -1
                $gapCount = 0
            }
        }
    }
    # Close a block that runs to the bottom of the image.
    if ($blockStart -ne -1) {
        $blockEnd = $height - 1 - $gapCount
        $blocks.Add([pscustomobject]@{ Start = $blockStart; End = $blockEnd })
    }

    foreach ($block in $blocks) {
        Write-Output ("{0}-{1}" -f $block.Start, $block.End)
    }
}
finally {
    $bitmap.Dispose()
}
