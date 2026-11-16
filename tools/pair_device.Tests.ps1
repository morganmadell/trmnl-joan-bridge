# Pester tests for the serial helper functions in pair_device.ps1.
# Run: Invoke-Pester tools/pair_device.Tests.ps1   (Pester 3.4 or later)
#
# pair_device.ps1 opens a COM port as soon as it runs, so it cannot be dot-sourced.
# These tests read the two functions out of the script with the PowerShell parser,
# drop the [SerialPort] type constraint, and run them against a fake port object.

$scriptPath = Join-Path $PSScriptRoot 'pair_device.ps1'
$ast = [System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$null, [ref]$null)
$functions = $ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true)

foreach ($name in 'Read-SerialResponse', 'Send-Command') {
    $definition = $functions | Where-Object { $_.Name -eq $name }
    if (-not $definition) { throw "Function $name not found in pair_device.ps1" }
    $text = $definition.Extent.Text -replace '\[System\.IO\.Ports\.SerialPort\]', ''
    . ([scriptblock]::Create($text))
}

$ResponseOverallTimeoutMs = 400
$ResponseIdleGapMs = 40
$ResponsePollIntervalMs = 10
$CommandTerminator = "`r"

function New-FakePort {
    param([object[]]$Chunks)
    $state = @{ Queue = New-Object System.Collections.Queue; Written = @(); Discards = 0 }
    foreach ($c in $Chunks) { $state.Queue.Enqueue($c) }
    $port = New-Object psobject
    $port | Add-Member -NotePropertyName State -NotePropertyValue $state
    $port | Add-Member -MemberType ScriptMethod -Name ReadExisting -Value {
        if ($this.State.Queue.Count -eq 0) { return '' }
        $next = $this.State.Queue.Dequeue()
        if ($next -is [System.Exception]) { throw $next }
        return $next
    }
    $port | Add-Member -MemberType ScriptMethod -Name DiscardInBuffer -Value { $this.State.Discards++ }
    $port | Add-Member -MemberType ScriptMethod -Name Write -Value { param($text) $this.State.Written += $text }
    return $port
}

Describe 'Read-SerialResponse' {
    It 'joins chunks and returns after a quiet gap' {
        $port = New-FakePort -Chunks @('ab', 'cd')
        Read-SerialResponse -Port $port | Should Be 'abcd'
    }

    It 'returns an empty string when nothing arrives before the timeout' {
        $port = New-FakePort -Chunks @()
        Read-SerialResponse -Port $port -OverallTimeoutMs 120 | Should Be ''
    }

    It 'treats a failed read as no data and keeps polling' {
        $port = New-FakePort -Chunks @((New-Object System.IO.IOException 'hiccup'), 'ok')
        Read-SerialResponse -Port $port | Should Be 'ok'
    }
}

Describe 'Send-Command' {
    It 'discards pending input, writes the command with the terminator, and returns the response' {
        $port = New-FakePort -Chunks @('pong')
        $response = Send-Command -Port $port -Command 'ping'
        $response | Should Be 'pong'
        $port.State.Discards | Should Be 1
        $port.State.Written.Count | Should Be 1
        $port.State.Written[0] | Should Be "ping`r"
    }
}
