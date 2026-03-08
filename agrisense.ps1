#Requires -Version 5.1
<#
.SYNOPSIS
    AgriSense TUI — Terminal User Interface for managing the AgriSense Docker stack.

.DESCRIPTION
    A full-featured terminal dashboard for monitoring and controlling all Docker
    containers in the AgriSense project. Arrow keys navigate, letter keys trigger
    actions, everything runs in the same terminal window.

.NOTES
    If you get an execution policy error, run once as Administrator:
        Set-ExecutionPolicy RemoteSigned -Scope CurrentUser
#>

param(
    [switch]$Start,
    [switch]$Stop,
    [switch]$Restart,
    [switch]$Build,
    [switch]$Destroy,
    [switch]$RemoveVolumes,
    [switch]$RemoveBuilds,
    [switch]$Status,
    [switch]$Help,
    [switch]$StartProject,
    [switch]$Development
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
$script:COMPOSE_PROJECT = 'agrisense'
$script:ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$script:EnvFile = Join-Path $script:ScriptDir '.env'
$script:EnvExampleFile = Join-Path $script:ScriptDir '.env.example'
$script:ComposeFile = Join-Path $script:ScriptDir 'docker-compose.yml'
$script:DevComposeFile = Join-Path $script:ScriptDir 'docker-compose.dev.yml'
$script:ConfigScript = Join-Path $script:ScriptDir 'config.ps1'

# Development mode — set from the -Development flag; also honoured via DEV_MODE=true in .env
$script:Development = $Development.IsPresent
if (-not $script:Development -and (Test-Path (Join-Path $script:ScriptDir '.env'))) {
    $devLine = Select-String -Path (Join-Path $script:ScriptDir '.env') -Pattern '^DEV_MODE\s*=\s*true\s*$' -Quiet
    if ($devLine) { $script:Development = $true }
}

# State
$script:SelectedIndex = 0
$script:Containers = @()
$script:LastRefresh = [datetime]::MinValue
$script:RefreshIntervalMs = 2000
$script:Running = $true
$script:CurrentView = 'dashboard'  # dashboard | logs | env | popup-menu | action
$script:ScreenDirty = $true        # Dirty flag — only redraw when true
$script:LastWidth = 0
$script:LastHeight = 0

# Log viewer state
$script:LogProcess = $null
$script:LogBuffer = [System.Collections.Generic.List[string]]::new()
$script:LogMaxLines = 5000
$script:LogScrollOffset = 0
$script:LogAutoTail = $true
$script:LogContainerName = ''

# Action view state
$script:ActionProcess = $null
$script:ActionBuffer = [System.Collections.Generic.List[string]]::new()
$script:ActionMaxLines = 5000
$script:ActionScrollOffset = 0
$script:ActionAutoTail = $true
$script:ActionCommandLabel = ''
$script:ActionComplete = $false
$script:ActionExitCode = 0
$script:ActionQueue = [System.Collections.Concurrent.ConcurrentQueue[string]]::new()
$script:ActionDeferredCleanup = $null
$script:ActionSpinnerFrame = 0
$script:ActionSpinnerChars = @('|', '/', '-', '\')

# Env editor state
$script:EnvSections = @()
$script:EnvSelectedIndex = 0
$script:EnvScrollOffset = 0
$script:EnvEditing = $false
$script:EnvEditBuffer = ''
$script:EnvEditCursorPos = 0
$script:EnvDirty = $false
$script:EnvFieldsCache = @()         # Cached flat field list
$script:EnvDisplayCache = $null      # Cached display items list
$script:EnvFieldsCacheDirty = $true  # Invalidate on env data change

# Popup menu state
$script:PopupTitle = ''
$script:PopupOptions = @()
$script:PopupSelectedIndex = 0
$script:PopupPreviousView = 'dashboard'
$script:PopupRowLayout = @()   # array of row start indices into PopupOptions

# Saved console state
$script:OrigCursorVisible = $true
$script:OrigTitle = ''
$script:LastError = $null

# ANSI frame buffer — all draw operations append here, then flush once
$script:FrameBuffer = [System.Text.StringBuilder]::new(16384)

# ---------------------------------------------------------------------------
# ANSI escape code rendering (replaces slow Write-Host calls)
# ---------------------------------------------------------------------------

# ConsoleColor → ANSI foreground code mapping
$script:AnsiFg = @{
    'Black' = "`e[30m"; 'DarkBlue' = "`e[34m"
    'DarkGreen' = "`e[32m"; 'DarkCyan' = "`e[36m"
    'DarkRed' = "`e[31m"; 'DarkMagenta' = "`e[35m"
    'DarkYellow' = "`e[33m"; 'Gray' = "`e[37m"
    'DarkGray' = "`e[90m"; 'Blue' = "`e[94m"
    'Green' = "`e[92m"; 'Cyan' = "`e[96m"
    'Red' = "`e[91m"; 'Magenta' = "`e[95m"
    'Yellow' = "`e[93m"; 'White' = "`e[97m"
}

# ConsoleColor → ANSI background code mapping
$script:AnsiBg = @{
    'Black' = "`e[40m"; 'DarkBlue' = "`e[44m"
    'DarkGreen' = "`e[42m"; 'DarkCyan' = "`e[46m"
    'DarkRed' = "`e[41m"; 'DarkMagenta' = "`e[45m"
    'DarkYellow' = "`e[43m"; 'Gray' = "`e[47m"
    'DarkGray' = "`e[100m"; 'Blue' = "`e[104m"
    'Green' = "`e[102m"; 'Cyan' = "`e[106m"
    'Red' = "`e[101m"; 'Magenta' = "`e[105m"
    'Yellow' = "`e[103m"; 'White' = "`e[107m"
}

$script:DefaultBg = 'Black'

function Get-SafeBg {
    return $script:DefaultBg
}

# Per-frame cached dimensions
$script:FW = 80
$script:FH = 24

function Update-FrameDimensions {
    $newW = [Math]::Max(40, [Console]::WindowWidth)
    $newH = [Math]::Max(10, [Console]::WindowHeight)
    if ($newW -ne $script:FW -or $newH -ne $script:FH) {
        $script:FW = $newW
        $script:FH = $newH
        $script:ScreenDirty = $true
    }
}

# Append a positioned, colored line to the frame buffer
function Buf-At {
    param([int]$X, [int]$Y, [string]$Text, [string]$Fg = 'White', [string]$Bg = '')
    if ($Y -lt 0 -or $Y -ge $script:FH -or $X -lt 0) { return }
    $fgCode = $script:AnsiFg[$Fg]
    $bgCode = if ($Bg) { $script:AnsiBg[$Bg] } else { '' }
    [void]$script:FrameBuffer.Append("`e[$($Y+1);$($X+1)H$fgCode$bgCode$Text`e[0m")
}

# Append a full-width line (padded/truncated to screen width)
function Buf-FullLine {
    param([int]$Y, [string]$Text, [string]$Fg = 'White', [string]$Bg = '')
    if ($Y -lt 0 -or $Y -ge $script:FH) { return }
    $w = $script:FW
    $padded = if ($Text.Length -ge $w) { $Text.Substring(0, $w) } else { $Text.PadRight($w) }
    $fgCode = $script:AnsiFg[$Fg]
    $bgCode = if ($Bg) { $script:AnsiBg[$Bg] } else { '' }
    [void]$script:FrameBuffer.Append("`e[$($Y+1);1H$fgCode$bgCode$padded`e[0m")
}

# Append colored text at X,Y without clearing the line
function Buf-At-Raw {
    param([int]$X, [int]$Y, [string]$Text, [string]$Fg = 'White', [string]$Bg = '')
    if ($Y -lt 0 -or $Y -ge $script:FH) { return }
    $fgCode = $script:AnsiFg[$Fg]
    $bgCode = if ($Bg) { $script:AnsiBg[$Bg] } else { '' }
    [void]$script:FrameBuffer.Append("`e[$($Y+1);$($X+1)H$fgCode$bgCode$Text`e[0m")
}

# Clear a line (fill with spaces)
function Buf-ClearLine {
    param([int]$Y)
    if ($Y -lt 0 -or $Y -ge $script:FH) { return }
    [void]$script:FrameBuffer.Append("`e[$($Y+1);1H`e[2K")
}

# Flush the frame buffer to the console in one write
function Flush-Frame {
    if ($script:FrameBuffer.Length -gt 0) {
        [Console]::Out.Write($script:FrameBuffer.ToString())
        [void]$script:FrameBuffer.Clear()
    }
}

# Legacy Write-Host helpers — only used for non-buffered contexts (action view, startup)
function Get-ScreenWidth { return $script:FW }
function Get-ScreenHeight { return $script:FH }

function Write-At {
    param([int]$X, [int]$Y, [string]$Text, [ConsoleColor]$Fg = 'White', [ConsoleColor]$Bg = 'Black')
    $maxY = [Console]::BufferHeight - 1
    if ($Y -lt 0 -or $Y -gt $maxY -or $X -lt 0) { return }
    [Console]::SetCursorPosition($X, $Y)
    Write-Host $Text -ForegroundColor $Fg -BackgroundColor $Bg -NoNewline
}

function Write-CenteredAt {
    param([int]$Y, [string]$Text, [ConsoleColor]$Fg = 'White', [ConsoleColor]$Bg = 'Black')
    $w = $script:FW
    $pad = [Math]::Max(0, [Math]::Floor(($w - $Text.Length) / 2))
    [Console]::SetCursorPosition(0, $Y)
    Write-Host (' ' * $w) -NoNewline
    [Console]::SetCursorPosition($pad, $Y)
    Write-Host $Text -ForegroundColor $Fg -BackgroundColor $Bg -NoNewline
}

function Write-FullLine {
    param([int]$Y, [string]$Text, [ConsoleColor]$Fg = 'White', [ConsoleColor]$Bg = 'Black')
    $maxY = [Console]::BufferHeight - 1
    if ($Y -lt 0 -or $Y -gt $maxY) { return }
    $w = $script:FW
    $padded = if ($Text.Length -ge $w) { $Text.Substring(0, $w) } else { $Text.PadRight($w) }
    [Console]::SetCursorPosition(0, $Y)
    Write-Host $padded -ForegroundColor $Fg -BackgroundColor $Bg -NoNewline
}

# ---------------------------------------------------------------------------
# Docker helpers
# ---------------------------------------------------------------------------

function Get-ComposeArgs {
    $args_ = @('--env-file', $script:EnvFile, '--project-directory', $script:ScriptDir)
    if ($script:Development) {
        $args_ += @('-f', $script:ComposeFile, '-f', $script:DevComposeFile)
    }
    return $args_
}

function Get-ProjectContainers {
    try {
        $args_ = Get-ComposeArgs
        $args_ += @('ps', '-a', '--format', 'json')
        $raw = & docker compose @args_ 2>$null
        if (-not $raw) { return @() }
        # docker compose ps --format json may output one JSON object per line
        $results = [System.Collections.Generic.List[object]]::new()
        foreach ($line in $raw) {
            if ($null -eq $line) { continue }
            $trimmed = "$line".Trim()
            if ($trimmed -eq '' -or $trimmed -eq '[]') { continue }
            try {
                $parsed = $trimmed | ConvertFrom-Json
                if ($parsed -is [System.Array]) {
                    foreach ($p in $parsed) { $results.Add($p) }
                }
                else {
                    $results.Add($parsed)
                }
            }
            catch { continue }
        }
        return @($results)
    }
    catch {
        return @()
    }
}

function Test-AllContainersRunning {
    $containers = @(Get-ProjectContainers)
    if ($containers.Count -eq 0) { return $false }
    foreach ($c in $containers) {
        if ($c.State -ne 'running') { return $false }
    }
    return $true
}

function Invoke-DockerAction {
    param(
        [string]$Action,
        [switch]$WithVolumes,
        [switch]$WithBuilds,
        [string]$ServiceName
    )
    $args_ = Get-ComposeArgs
    switch ($Action) {
        'start' {
            if ($ServiceName) { $args_ += @('up', '-d', '--no-deps', $ServiceName) }
            else { $args_ += @('up', '-d') }
        }
        'stop' {
            $args_ += 'stop'
            if ($ServiceName) { $args_ += $ServiceName }
        }
        'restart' {
            $args_ += 'restart'
            if ($ServiceName) { $args_ += $ServiceName }
        }
        'build' {
            if ($ServiceName) { $args_ += @('up', '-d', '--build', '--no-deps', $ServiceName) }
            else { $args_ += @('up', '-d', '--build') }
        }
        'destroy' {
            $args_ += @('down', '--remove-orphans')
            if ($WithVolumes) { $args_ += '-v' }
            if ($WithBuilds) { $args_ += '--rmi', 'all' }
        }
    }

    # Build command label for display (skip the --env-file / --project-directory preamble)
    $displayArgs = $args_[4..($args_.Count - 1)]
    $script:ActionCommandLabel = "docker compose $($displayArgs -join ' ')"

    # Deferred cleanup for destroy (bind-mount paths that -v doesn't remove)
    $script:ActionDeferredCleanup = $null
    if ($Action -eq 'destroy' -and $WithVolumes) {
        $dbPath = Join-Path $script:ScriptDir 'supabase\volumes\db\data'
        $storagePath = Join-Path $script:ScriptDir 'supabase\volumes\storage'
        $script:ActionDeferredCleanup = {
            if (Test-Path $dbPath) { Get-ChildItem -Path $dbPath      | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue }
            if (Test-Path $storagePath) { Get-ChildItem -Path $storagePath | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue }
        }
    }

    # Reset action view state
    $script:ActionBuffer.Clear()
    $script:ActionScrollOffset = 0
    $script:ActionAutoTail = $true
    $script:ActionComplete = $false
    $script:ActionExitCode = 0
    $script:ActionSpinnerFrame = 0
    while ($script:ActionQueue.TryDequeue([ref]$null)) {}

    # Launch docker compose as a background process with piped output
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = 'docker'
    $psi.Arguments = "compose $($args_ -join ' ')"
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true

    $script:ActionProcess = [System.Diagnostics.Process]::new()
    $script:ActionProcess.StartInfo = $psi
    $script:ActionProcess.EnableRaisingEvents = $true

    $q = $script:ActionQueue
    Register-ObjectEvent -InputObject $script:ActionProcess -EventName 'OutputDataReceived' -Action {
        if ($null -ne $EventArgs.Data) { $Event.MessageData.Enqueue($EventArgs.Data) }
    } -MessageData $q -SourceIdentifier 'ActionStdout' | Out-Null

    Register-ObjectEvent -InputObject $script:ActionProcess -EventName 'ErrorDataReceived' -Action {
        if ($null -ne $EventArgs.Data) { $Event.MessageData.Enqueue($EventArgs.Data) }
    } -MessageData $q -SourceIdentifier 'ActionStderr' | Out-Null

    $script:ActionProcess.Start()          | Out-Null
    $script:ActionProcess.BeginOutputReadLine()
    $script:ActionProcess.BeginErrorReadLine()

    $script:CurrentView = 'action'
    $script:ScreenDirty = $true
    [Console]::Clear()
}

function Stop-ActionView {
    try { Unregister-Event -SourceIdentifier 'ActionStdout' -ErrorAction SilentlyContinue } catch {}
    try { Unregister-Event -SourceIdentifier 'ActionStderr' -ErrorAction SilentlyContinue } catch {}
    try { Get-Job -Name 'ActionStdout' -ErrorAction SilentlyContinue | Remove-Job -Force } catch {}
    try { Get-Job -Name 'ActionStderr' -ErrorAction SilentlyContinue | Remove-Job -Force } catch {}

    if ($script:ActionProcess -and -not $script:ActionProcess.HasExited) {
        try { $script:ActionProcess.Kill() } catch {}
    }
    if ($script:ActionProcess) {
        try { $script:ActionProcess.Dispose() } catch {}
    }
    $script:ActionProcess = $null
    $script:CurrentView = 'dashboard'
    $script:LastRefresh = [datetime]::MinValue
    $script:ScreenDirty = $true
    [Console]::Clear()
}

function Update-ActionBuffer {
    if (-not $script:ActionProcess) { return }

    $lineText = $null
    $count = 0
    while ($script:ActionQueue.TryDequeue([ref]$lineText) -and $count -lt 200) {
        $script:ActionBuffer.Add($lineText)
        if ($script:ActionBuffer.Count -gt $script:ActionMaxLines) {
            $script:ActionBuffer.RemoveAt(0)
        }
        $count++
    }

    if ($script:ActionProcess.HasExited -and -not $script:ActionComplete) {
        # Drain remaining output
        $lineText = $null
        while ($script:ActionQueue.TryDequeue([ref]$lineText)) {
            $script:ActionBuffer.Add($lineText)
        }

        $script:ActionExitCode = $script:ActionProcess.ExitCode
        if ($script:ActionExitCode -ne 0) {
            $script:ActionBuffer.Add('')
            $script:ActionBuffer.Add("  [!] Command exited with code $($script:ActionExitCode)")
        }
        else {
            $script:ActionBuffer.Add('')
            $script:ActionBuffer.Add('  [OK] Done.')
        }

        # Run deferred cleanup (destroy -WithVolumes)
        if ($null -ne $script:ActionDeferredCleanup) {
            try { & $script:ActionDeferredCleanup } catch {}
            $script:ActionDeferredCleanup = $null
        }

        $script:ActionComplete = $true
    }
}

function Draw-ActionView {
    $w = $script:FW
    $h = $script:FH

    # Header
    if ($script:ActionComplete) {
        $statusText = if ($script:ActionExitCode -eq 0) { ' [Done] ' } else { " [Exit $($script:ActionExitCode)] " }
        $statusFg = if ($script:ActionExitCode -eq 0) { 'Green' } else { 'Red' }
    }
    else {
        $spinChar = $script:ActionSpinnerChars[$script:ActionSpinnerFrame % 4]
        $script:ActionSpinnerFrame++
        $statusText = " $spinChar Running "
        $statusFg = 'Yellow'
    }

    $header = " AgriSense > $($script:ActionCommandLabel)"
    $gap = $w - $header.Length - $statusText.Length
    if ($gap -lt 1) { $gap = 1 }
    $headerLine = $header + (' ' * $gap) + $statusText
    Buf-FullLine 0 $headerLine 'White' 'DarkCyan'

    # Output area (row 1 to h-3)
    $areaStart = 1
    $areaEnd = $h - 2
    $areaH = [Math]::Max(0, $areaEnd - $areaStart)

    if ($script:ActionAutoTail) {
        $script:ActionScrollOffset = [Math]::Max(0, $script:ActionBuffer.Count - $areaH)
    }

    if ($script:ActionBuffer.Count -eq 0) {
        Buf-FullLine ([int]($h / 2)) '  Waiting for output...' 'DarkGray'
    }

    for ($i = 0; $i -lt $areaH; $i++) {
        $lineIdx = $script:ActionScrollOffset + $i
        $row = $areaStart + $i
        if ($lineIdx -lt $script:ActionBuffer.Count) {
            $lineText = $script:ActionBuffer[$lineIdx]
            if ($lineText.Length -gt $w) { $lineText = $lineText.Substring(0, $w) }
            $fg = if ($lineText -match '^\s*\[!\]') { 'Red' } elseif ($lineText -match '^\s*\[OK\]') { 'Green' } else { 'Gray' }
            Buf-FullLine $row $lineText $fg
        }
        else {
            Buf-ClearLine $row
        }
    }

    # Bottom bar
    $footerBg = 'DarkGray'
    Buf-FullLine ($h - 1) '' 'White' $footerBg
    $curX = 2

    if ($script:ActionComplete) {
        $nav = @(
            @{ K = 'Esc'; L = 'Return to Dashboard'; C = 'Yellow' }
            @{ K = "$([char]0x2191)$([char]0x2193)"; L = 'Scroll'; C = 'Gray' }
        )
        foreach ($n in $nav) {
            Buf-At-Raw $curX ($h - 1) '[' 'Gray' $footerBg
            Buf-At-Raw ($curX + 1) ($h - 1) $n.K $n.C $footerBg
            Buf-At-Raw ($curX + 1 + $n.K.Length) ($h - 1) "] $($n.L)  " 'White' $footerBg
            $curX += $n.L.Length + $n.K.Length + 5
        }
    }
    else {
        Buf-At-Raw $curX ($h - 1) '  Running... please wait' 'Yellow' $footerBg
        $curX = $w - 20
        Buf-At-Raw $curX ($h - 1) '[' 'Gray' $footerBg
        Buf-At-Raw ($curX + 1) ($h - 1) "$([char]0x2191)$([char]0x2193)" 'Gray' $footerBg
        Buf-At-Raw ($curX + 3) ($h - 1) '] Scroll' 'White' $footerBg
    }
}

function Handle-ActionInput {
    param([System.ConsoleKeyInfo]$Key)
    $areaH = $script:FH - 3

    switch ($Key.Key) {
        'Escape' {
            if ($script:ActionComplete) { Stop-ActionView }
        }
        'UpArrow' {
            $script:ActionAutoTail = $false
            $script:ActionScrollOffset = [Math]::Max(0, $script:ActionScrollOffset - 1)
        }
        'DownArrow' {
            $script:ActionAutoTail = $false
            $maxOff = [Math]::Max(0, $script:ActionBuffer.Count - $areaH)
            $script:ActionScrollOffset = [Math]::Min($maxOff, $script:ActionScrollOffset + 1)
            if ($script:ActionScrollOffset -ge $maxOff) { $script:ActionAutoTail = $true }
        }
        'PageUp' {
            $script:ActionAutoTail = $false
            $script:ActionScrollOffset = [Math]::Max(0, $script:ActionScrollOffset - $areaH)
        }
        'PageDown' {
            $script:ActionAutoTail = $false
            $maxOff = [Math]::Max(0, $script:ActionBuffer.Count - $areaH)
            $script:ActionScrollOffset = [Math]::Min($maxOff, $script:ActionScrollOffset + $areaH)
            if ($script:ActionScrollOffset -ge $maxOff) { $script:ActionAutoTail = $true }
        }
        'Home' { $script:ActionAutoTail = $false; $script:ActionScrollOffset = 0 }
        'End' { $script:ActionAutoTail = $true }
    }
}

# ---------------------------------------------------------------------------
# Dashboard drawing
# ---------------------------------------------------------------------------

function Draw-Dashboard {
    $w = $script:FW
    $h = $script:FH

    # Header (row 0-2)
    $devTag = if ($script:Development) { ' [DEV]' } else { '' }
    $title = " AgriSense Dashboard$devTag"
    $countRunning = 0
    foreach ($c in $script:Containers) { if ($c.State -eq 'running') { $countRunning++ } }
    $countTotal = $script:Containers.Count
    $statusText = "$countRunning/$countTotal running "
    $gap = $w - $title.Length - $statusText.Length
    if ($gap -lt 1) { $gap = 1 }
    $headerLine = $title + (' ' * $gap) + $statusText
    Buf-FullLine 0 $headerLine 'White' 'DarkCyan'
    Buf-FullLine 1 (' ' + ([char]0x2500).ToString() * [Math]::Max(1, $w - 2) + ' ') 'DarkCyan'

    # Column headers (row 2)
    $nameCol = '  Container'
    $statusCol = 'Status'
    $uptimeCol = 'Info'
    $nameW = [Math]::Min([Math]::Max(20, [int]($w * 0.35)), $w - 10)
    $statusW = [Math]::Min([Math]::Max(10, [int]($w * 0.20)), $w - $nameW - 5)
    $uptimeW = [Math]::Max(1, $w - $nameW - $statusW)
    $headerRow = $nameCol.PadRight($nameW) + $statusCol.PadRight($statusW) + $uptimeCol.PadRight($uptimeW)
    Buf-FullLine 2 $headerRow 'DarkGray'

    # Container list (row 3 to h-4)
    $listStart = 3
    $listEnd = $h - 4
    $listH = [Math]::Max(0, $listEnd - $listStart)
    $defBg = $script:DefaultBg

    for ($i = 0; $i -lt $listH; $i++) {
        $row = $listStart + $i
        if ($i -lt $script:Containers.Count) {
            $c = $script:Containers[$i]
            $name = if ($c.Name) { $c.Name }   else { $c.Service }
            $state = if ($c.State) { $c.State }  else { 'unknown' }
            $status = if ($c.Status) { $c.Status } else { '' }

            $nameStr = ("  $name").PadRight($nameW).Substring(0, $nameW)
            $stateStr = $state.PadRight($statusW).Substring(0, $statusW)
            $statusStr = $status.PadRight($uptimeW).Substring(0, $uptimeW)
            $line = $nameStr + $stateStr + $statusStr

            $isSelected = ($i -eq $script:SelectedIndex)
            $fg = switch ($state) {
                'running' { if ($isSelected) { 'White' } else { 'Green' } }
                'exited' { if ($isSelected) { 'White' } else { 'Red' } }
                'restarting' { if ($isSelected) { 'White' } else { 'Yellow' } }
                default { if ($isSelected) { 'White' } else { 'Gray' } }
            }
            $bg = if ($isSelected) { 'DarkBlue' } else { $defBg }
            Buf-FullLine $row $line $fg $bg
        }
        else {
            Buf-ClearLine $row
        }
    }

    # Separator
    Buf-FullLine ($h - 3) (' ' + (([char]0x2500).ToString() * ($w - 2)) + ' ') 'DarkGray'

    # Action bar (row h-2, h-1)
    $footerBg = 'DarkGray'
    Buf-FullLine ($h - 2) '' 'White' $footerBg
    Buf-FullLine ($h - 1) '' 'White' $footerBg

    # Row 1: Service Lifecycle (Fixed Grid)
    $pipeX = 52
    $colWidth = 16
    $actions = @(
        @{ K = 'S'; L = 'Start'; C = 'Green' }
        @{ K = 'R'; L = 'Restart'; C = 'Yellow' }
        @{ K = 'B'; L = 'Re-Build'; C = 'Cyan' }
    )
    for ($i = 0; $i -lt $actions.Count; $i++) {
        $a = $actions[$i]
        $x = 2 + ($i * $colWidth)
        Buf-At-Raw $x ($h - 2) '[' 'Gray' $footerBg
        Buf-At-Raw ($x + 1) ($h - 2) $a.K $a.C $footerBg
        Buf-At-Raw ($x + 1 + $a.K.Length) ($h - 2) "] $($a.L)" 'White' $footerBg
    }
    Buf-At-Raw $pipeX ($h - 2) '|' 'DarkGray' $footerBg
    $actionsRight = @(
        @{ K = 'X'; L = 'Stop'; C = 'Red' }
        @{ K = 'D'; L = 'Destroy'; C = 'DarkRed' }
    )
    for ($i = 0; $i -lt $actionsRight.Count; $i++) {
        $a = $actionsRight[$i]
        $x = $pipeX + 4 + ($i * $colWidth)
        Buf-At-Raw $x ($h - 2) '[' 'Gray' $footerBg
        Buf-At-Raw ($x + 1) ($h - 2) $a.K $a.C $footerBg
        Buf-At-Raw ($x + 1 + $a.K.Length) ($h - 2) "] $($a.L)" 'White' $footerBg
    }

    # Row 2: Navigation & Global (Fixed Grid)
    $nav = @(
        @{ K = 'Enter'; L = 'Menu'; C = 'White' }
        @{ K = "$([char]0x2191)$([char]0x2193)"; L = 'Nav'; C = 'Gray' }
    )
    for ($i = 0; $i -lt $nav.Count; $i++) {
        $n = $nav[$i]
        $x = 2 + ($i * $colWidth)
        Buf-At-Raw $x ($h - 1) '[' 'Gray' $footerBg
        Buf-At-Raw ($x + 1) ($h - 1) $n.K $n.C $footerBg
        Buf-At-Raw ($x + 1 + $n.K.Length) ($h - 1) "] $($n.L)" 'White' $footerBg
    }
    Buf-At-Raw $pipeX ($h - 1) '|' 'DarkGray' $footerBg
    $navRight = @(
        @{ K = 'E'; L = 'Edit .env'; C = 'Cyan' }
        @{ K = 'Q'; L = 'Quit'; C = 'Red' }
    )
    for ($i = 0; $i -lt $navRight.Count; $i++) {
        $n = $navRight[$i]
        $x = $pipeX + 4 + ($i * $colWidth)
        Buf-At-Raw $x ($h - 1) '[' 'Gray' $footerBg
        Buf-At-Raw ($x + 1) ($h - 1) $n.K $n.C $footerBg
        Buf-At-Raw ($x + 1 + $n.K.Length) ($h - 1) "] $($n.L)" 'White' $footerBg
    }
}

# ---------------------------------------------------------------------------
# Log viewer
# ---------------------------------------------------------------------------

# Thread-safe queue for incoming log lines
$script:LogQueue = [System.Collections.Concurrent.ConcurrentQueue[string]]::new()

function Start-LogViewer {
    param([string]$ContainerName)
    $script:LogContainerName = $ContainerName
    $script:LogBuffer.Clear()
    $script:LogScrollOffset = 0
    $script:LogAutoTail = $true
    $script:CurrentView = 'logs'

    # Clear the queue
    while ($script:LogQueue.TryDequeue([ref]$null)) {}

    # Start docker logs in a background process
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = 'docker'
    $psi.Arguments = "logs --tail 200 -f $ContainerName"
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true

    $script:LogProcess = [System.Diagnostics.Process]::new()
    $script:LogProcess.StartInfo = $psi
    $script:LogProcess.EnableRaisingEvents = $true

    # Register async event handlers that enqueue lines
    $q = $script:LogQueue
    Register-ObjectEvent -InputObject $script:LogProcess -EventName 'OutputDataReceived' -Action {
        if ($null -ne $EventArgs.Data) {
            $Event.MessageData.Enqueue($EventArgs.Data)
        }
    } -MessageData $q -SourceIdentifier 'LogStdout' | Out-Null

    Register-ObjectEvent -InputObject $script:LogProcess -EventName 'ErrorDataReceived' -Action {
        if ($null -ne $EventArgs.Data) {
            $Event.MessageData.Enqueue($EventArgs.Data)
        }
    } -MessageData $q -SourceIdentifier 'LogStderr' | Out-Null

    $script:LogProcess.Start() | Out-Null
    $script:LogProcess.BeginOutputReadLine()
    $script:LogProcess.BeginErrorReadLine()

    [Console]::Clear()
    $script:ScreenDirty = $true
}

function Stop-LogViewer {
    # Unregister events
    try { Unregister-Event -SourceIdentifier 'LogStdout' -ErrorAction SilentlyContinue } catch {}
    try { Unregister-Event -SourceIdentifier 'LogStderr' -ErrorAction SilentlyContinue } catch {}
    try { Get-Job -Name 'LogStdout' -ErrorAction SilentlyContinue | Remove-Job -Force } catch {}
    try { Get-Job -Name 'LogStderr' -ErrorAction SilentlyContinue | Remove-Job -Force } catch {}

    if ($script:LogProcess -and -not $script:LogProcess.HasExited) {
        try { $script:LogProcess.Kill() } catch {}
    }
    if ($script:LogProcess) {
        try { $script:LogProcess.Dispose() } catch {}
    }
    $script:LogProcess = $null
    $script:CurrentView = 'dashboard'
    $script:LastRefresh = [datetime]::MinValue
    $script:ScreenDirty = $true
    [Console]::Clear()
}

function Update-LogBuffer {
    if (-not $script:LogProcess) { return }

    # Drain the queue into the buffer (non-blocking)
    $lineText = $null
    $count = 0
    while ($script:LogQueue.TryDequeue([ref]$lineText) -and $count -lt 200) {
        $script:LogBuffer.Add($lineText)
        if ($script:LogBuffer.Count -gt $script:LogMaxLines) {
            $script:LogBuffer.RemoveAt(0)
        }
        $count++
    }
}

function Draw-LogViewer {
    $w = $script:FW
    $h = $script:FH

    # Header
    $header = " Logs: $($script:LogContainerName)"
    $tailIndicator = if ($script:LogAutoTail) { '[AUTO-TAIL ON] ' } else { "[Line $($script:LogScrollOffset + 1)/$($script:LogBuffer.Count)] " }
    $gap = $w - $header.Length - $tailIndicator.Length
    if ($gap -lt 1) { $gap = 1 }
    Buf-FullLine 0 ($header + (' ' * $gap) + $tailIndicator) 'White' 'DarkMagenta'

    # Log content area (row 1 to h-3)
    $logStart = 1
    $logEnd = $h - 2
    $logH = [Math]::Max(0, $logEnd - $logStart)

    if ($script:LogAutoTail) {
        $script:LogScrollOffset = [Math]::Max(0, $script:LogBuffer.Count - $logH)
    }

    # Show waiting message if buffer is empty
    if ($script:LogBuffer.Count -eq 0) {
        Buf-FullLine ([int]($h / 2)) '  Waiting for logs...' 'DarkGray'
    }

    for ($i = 0; $i -lt $logH; $i++) {
        $lineIdx = $script:LogScrollOffset + $i
        $row = $logStart + $i
        if ($lineIdx -lt $script:LogBuffer.Count) {
            $lineText = $script:LogBuffer[$lineIdx]
            if ($lineText.Length -gt $w) { $lineText = $lineText.Substring(0, $w) }
            Buf-FullLine $row $lineText 'Gray'
        }
        else {
            Buf-ClearLine $row
        }
    }

    # Bottom bar
    $footerBg = 'DarkGray'
    Buf-FullLine ($h - 2) ((' ' + (([char]0x2500).ToString() * ($w - 2)) + ' ')) 'DarkGray'
    Buf-FullLine ($h - 1) '' 'White' $footerBg

    $curX = 2
    $nav = @(
        @{ K = 'Esc'; L = 'Dashboard'; C = 'Yellow' }
        @{ K = "$([char]0x2191)$([char]0x2193)"; L = 'Scroll'; C = 'Gray' }
        @{ K = 'PgUp'; L = 'Page'; C = 'Gray' }
        @{ K = 'Home'; L = 'Top'; C = 'Gray' }
        @{ K = 'End'; L = 'Bottom'; C = 'Cyan' }
    )
    foreach ($n in $nav) {
        Buf-At-Raw $curX ($h - 1) '[' 'Gray' $footerBg
        Buf-At-Raw ($curX + 1) ($h - 1) $n.K $n.C $footerBg
        Buf-At-Raw ($curX + 1 + $n.K.Length) ($h - 1) "] $($n.L)  " 'White' $footerBg
        $curX += $n.L.Length + $n.K.Length + 5
    }
}

function Handle-LogInput {
    param([System.ConsoleKeyInfo]$Key)
    $logH = $script:FH - 3

    switch ($Key.Key) {
        'Escape' {
            Stop-LogViewer
            return
        }
        'UpArrow' {
            $script:LogAutoTail = $false
            $script:LogScrollOffset = [Math]::Max(0, $script:LogScrollOffset - 1)
        }
        'DownArrow' {
            $script:LogAutoTail = $false
            $maxOff = [Math]::Max(0, $script:LogBuffer.Count - $logH)
            $script:LogScrollOffset = [Math]::Min($maxOff, $script:LogScrollOffset + 1)
            if ($script:LogScrollOffset -ge $maxOff) { $script:LogAutoTail = $true }
        }
        'PageUp' {
            $script:LogAutoTail = $false
            $script:LogScrollOffset = [Math]::Max(0, $script:LogScrollOffset - $logH)
        }
        'PageDown' {
            $script:LogAutoTail = $false
            $maxOff = [Math]::Max(0, $script:LogBuffer.Count - $logH)
            $script:LogScrollOffset = [Math]::Min($maxOff, $script:LogScrollOffset + $logH)
            if ($script:LogScrollOffset -ge $maxOff) { $script:LogAutoTail = $true }
        }
        'Home' {
            $script:LogAutoTail = $false
            $script:LogScrollOffset = 0
        }
        'End' {
            $script:LogAutoTail = $true
        }
    }
}

# ---------------------------------------------------------------------------
# Secret generation helpers
# ---------------------------------------------------------------------------

$script:GeneratableFields = @(
    'JWT_SECRET', 'ANON_KEY', 'SERVICE_ROLE_KEY',
    'SECRET_KEY_BASE', 'VAULT_ENC_KEY', 'PG_META_CRYPTO_KEY',
    'LOGFLARE_PUBLIC_ACCESS_TOKEN', 'LOGFLARE_PRIVATE_ACCESS_TOKEN',
    'S3_PROTOCOL_ACCESS_KEY_ID', 'S3_PROTOCOL_ACCESS_KEY_SECRET',
    'DASHBOARD_PASSWORD', 'POSTGRES_PASSWORD', 'SMTP_PASS', 'MINIO_ROOT_PASSWORD'
)

function New-RandomBytes {
    param([int]$Count)
    $bytes = [byte[]]::new($Count)
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($bytes)
    return $bytes
}

function New-SupabaseJwt {
    param([string]$Role, [string]$Secret)
    # Header
    $header = '{"alg":"HS256","typ":"JWT"}'
    # Payload — issued now, expires in 10 years
    $iat = [int][DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    $exp = $iat + (10 * 365 * 24 * 3600)
    $payload = "{`"role`":`"$Role`",`"iss`":`"supabase`",`"iat`":$iat,`"exp`":$exp}"

    # Base64url encode
    $toB64Url = {
        param([byte[]]$data)
        [Convert]::ToBase64String($data).Replace('+', '-').Replace('/', '_').TrimEnd('=')
    }
    $hdrB64 = & $toB64Url ([System.Text.Encoding]::UTF8.GetBytes($header))
    $payB64 = & $toB64Url ([System.Text.Encoding]::UTF8.GetBytes($payload))
    $sigInput = "$hdrB64.$payB64"

    # HMAC-SHA256 signature
    $hmac = [System.Security.Cryptography.HMACSHA256]::new(
        [System.Text.Encoding]::UTF8.GetBytes($Secret)
    )
    $sigBytes = $hmac.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($sigInput))
    $sigB64 = & $toB64Url $sigBytes

    return "$sigInput.$sigB64"
}

function New-GeneratedValue {
    param([string]$FieldKey, [hashtable]$AllFieldMap)

    switch ($FieldKey) {
        'JWT_SECRET' {
            return [Convert]::ToBase64String((New-RandomBytes 64))
        }
        'ANON_KEY' {
            $jwtSecret = $AllFieldMap['JWT_SECRET']
            if (-not $jwtSecret -or $jwtSecret -match '^<') {
                $jwtSecret = [Convert]::ToBase64String((New-RandomBytes 64))
                $AllFieldMap['JWT_SECRET'] = $jwtSecret
            }
            return New-SupabaseJwt -Role 'anon' -Secret $jwtSecret
        }
        'SERVICE_ROLE_KEY' {
            $jwtSecret = $AllFieldMap['JWT_SECRET']
            if (-not $jwtSecret -or $jwtSecret -match '^<') {
                $jwtSecret = [Convert]::ToBase64String((New-RandomBytes 64))
                $AllFieldMap['JWT_SECRET'] = $jwtSecret
            }
            return New-SupabaseJwt -Role 'service_role' -Secret $jwtSecret
        }
        'SECRET_KEY_BASE' {
            return [Convert]::ToBase64String((New-RandomBytes 64))
        }
        'VAULT_ENC_KEY' {
            return [BitConverter]::ToString((New-RandomBytes 32)).Replace('-', '').ToLower()
        }
        'PG_META_CRYPTO_KEY' {
            return [Convert]::ToBase64String((New-RandomBytes 32)).Replace('+', '-').Replace('/', '_').TrimEnd('=')
        }
        { $_ -in @('LOGFLARE_PUBLIC_ACCESS_TOKEN', 'LOGFLARE_PRIVATE_ACCESS_TOKEN') } {
            return [Guid]::NewGuid().ToString()
        }
        'S3_PROTOCOL_ACCESS_KEY_ID' {
            return [BitConverter]::ToString((New-RandomBytes 20)).Replace('-', '').ToLower()
        }
        'S3_PROTOCOL_ACCESS_KEY_SECRET' {
            return [BitConverter]::ToString((New-RandomBytes 40)).Replace('-', '').ToLower()
        }
        { $_ -in @('DASHBOARD_PASSWORD', 'POSTGRES_PASSWORD', 'SMTP_PASS', 'MINIO_ROOT_PASSWORD') } {
            return [Convert]::ToBase64String((New-RandomBytes 24)).Replace('+', '-').Replace('/', '_').TrimEnd('=')
        }
        default { return $null }
    }
}

# ---------------------------------------------------------------------------
# Env editor
# ---------------------------------------------------------------------------

function Initialize-EnvEditor {
    $script:EnvSections = @()
    $script:EnvSelectedIndex = 0
    $script:EnvScrollOffset = 0
    $script:EnvEditing = $false
    $script:EnvDirty = $false

    # Load current .env values
    $envValues = @{}
    if (Test-Path $script:EnvFile) {
        Get-Content $script:EnvFile | ForEach-Object {
            if ($_ -match '^([A-Z_][A-Z0-9_]*)=(.*)$') {
                $envValues[$Matches[1]] = $Matches[2]
            }
        }
    }

    # Parse .env.example for structure
    $currentSection = 'General'
    $currentDescription = ''
    $items = [System.Collections.Generic.List[PSCustomObject]]::new()

    $exampleLines = Get-Content $script:EnvExampleFile
    foreach ($line in $exampleLines) {
        # Section header: ############...
        if ($line -match '^#{3,}\s*$') { continue }
        if ($line -match '^#{3,}\s+(.+?)(?:\s*#{0,})$') {
            # Close previous section if it has items
            if ($items.Count -gt 0) {
                $script:EnvSections += [PSCustomObject]@{
                    Name  = $currentSection
                    Items = $items.ToArray()
                }
                $items = [System.Collections.Generic.List[PSCustomObject]]::new()
            }
            $sectionName = $Matches[1].Trim()
            # Remove trailing decoration
            $sectionName = $sectionName -replace '\s*─+\s*$', ''
            $sectionName = $sectionName -replace '\s*#{1,}\s*$', ''
            $currentSection = $sectionName
            $currentDescription = ''
            continue
        }

        # Sub-header comment like "## General" or "## Email auth"
        if ($line -match '^##\s+(.+)$') {
            # Treat as inline group label; add as a separator item
            $items.Add([PSCustomObject]@{
                    Type        = 'separator'
                    Label       = $Matches[1].Trim()
                    Key         = ''
                    Value       = ''
                    Description = ''
                })
            $currentDescription = ''
            continue
        }

        # Description comment: # some text (but not section headers)
        if ($line -match '^\s*#\s+(.+)$' -and $line -notmatch '^#{2,}') {
            $currentDescription = $Matches[1].Trim()
            continue
        }

        # Variable line: KEY=value
        if ($line -match '^([A-Z_][A-Z0-9_]*)=(.*)$') {
            $key = $Matches[1]
            $defaultVal = $Matches[2]
            $currentVal = if ($envValues.ContainsKey($key)) { $envValues[$key] } else { $defaultVal }

            # Support "Label: Description" format in currentDescription
            $label = $key
            $description = $currentDescription
            if ($currentDescription -match '^(.+?):\s*(.*)$') {
                $label = $Matches[1].Trim()
                $description = $Matches[2].Trim()
            }

            $items.Add([PSCustomObject]@{
                    Type        = 'field'
                    Label       = $label
                    Key         = $key
                    Value       = $currentVal
                    Description = $description
                })
            $currentDescription = ''
        }
    }

    # Add last section
    if ($items.Count -gt 0) {
        $script:EnvSections += [PSCustomObject]@{
            Name  = $currentSection
            Items = $items.ToArray()
        }
    }

    $script:EnvFieldsCacheDirty = $true
    $script:EnvDisplayCache = $null
    $script:CurrentView = 'env'
    $script:ScreenDirty = $true
    [Console]::Clear()
}

function Get-AllEnvFields {
    if (-not $script:EnvFieldsCacheDirty -and $script:EnvFieldsCache.Count -gt 0) {
        return $script:EnvFieldsCache
    }
    $list = [System.Collections.Generic.List[PSCustomObject]]::new()
    foreach ($section in $script:EnvSections) {
        foreach ($item in $section.Items) {
            if ($item.Type -eq 'field') {
                $list.Add($item)
            }
        }
    }
    $script:EnvFieldsCache = $list.ToArray()
    $script:EnvFieldsCacheDirty = $false
    return $script:EnvFieldsCache
}

function Build-EnvDisplayItems {
    # Pre-build a field index counter (O(n) instead of O(n²))
    $allFields = Get-AllEnvFields
    $fieldIndexMap = @{}
    for ($fi = 0; $fi -lt $allFields.Count; $fi++) {
        $fieldIndexMap[[System.Runtime.CompilerServices.RuntimeHelpers]::GetHashCode($allFields[$fi])] = $fi
    }

    $displayList = [System.Collections.Generic.List[PSCustomObject]]::new()
    $isFirst = $true
    foreach ($section in $script:EnvSections) {
        if (-not $isFirst) {
            $displayList.Add([PSCustomObject]@{ Type = 'blank'; Text = ''; Item = $null; FieldIndex = -1 })
        }
        $isFirst = $false
        $displayList.Add([PSCustomObject]@{ Type = 'section'; Text = $section.Name; Item = $null; FieldIndex = -1 })
        $itemIdx = 0
        foreach ($item in $section.Items) {
            if ($item.Type -eq 'separator') {
                if ($itemIdx -gt 0) {
                    $displayList.Add([PSCustomObject]@{ Type = 'blank'; Text = ''; Item = $null; FieldIndex = -1 })
                }
                $displayList.Add([PSCustomObject]@{ Type = 'separator'; Text = $item.Label; Item = $null; FieldIndex = -1 })
            }
            else {
                $hash = [System.Runtime.CompilerServices.RuntimeHelpers]::GetHashCode($item)
                $fIdx = if ($fieldIndexMap.ContainsKey($hash)) { $fieldIndexMap[$hash] } else { -1 }
                $displayList.Add([PSCustomObject]@{ Type = 'field'; Text = ''; Item = $item; FieldIndex = $fIdx })
            }
            $itemIdx++
        }
    }
    return $displayList.ToArray()
}

function Draw-EnvEditor {
    $w = $script:FW
    $h = $script:FH
    $defBg = $script:DefaultBg

    # Header
    $dirty = if ($script:EnvDirty) { ' [MODIFIED]' } else { '' }
    $header = " Environment Editor$dirty"
    Buf-FullLine 0 $header 'White' 'DarkGreen'

    # Build flat display list (cached)
    if ($null -eq $script:EnvDisplayCache) {
        $script:EnvDisplayCache = Build-EnvDisplayItems
    }
    $displayItems = $script:EnvDisplayCache

    # Content area (row 1 to h-4)
    $contentStart = 1
    $contentEnd = $h - 4
    $contentH = $contentEnd - $contentStart

    # Find the display row of the selected field to keep it visible
    $selectedDisplayIdx = -1
    for ($di = 0; $di -lt $displayItems.Count; $di++) {
        if ($displayItems[$di].FieldIndex -eq $script:EnvSelectedIndex) {
            $selectedDisplayIdx = $di
            break
        }
    }

    # Adjust scroll
    if ($selectedDisplayIdx -ne -1) {
        if ($selectedDisplayIdx -lt $script:EnvScrollOffset) {
            $script:EnvScrollOffset = $selectedDisplayIdx
        }
        if ($selectedDisplayIdx -ge $script:EnvScrollOffset + $contentH) {
            $script:EnvScrollOffset = $selectedDisplayIdx - $contentH + 1
        }
    }

    $keyW = [Math]::Min(35, [int]($w * 0.30))
    $valW = $w - $keyW - 4

    for ($i = 0; $i -lt $contentH; $i++) {
        $row = $contentStart + $i
        $dIdx = $script:EnvScrollOffset + $i

        if ($dIdx -lt $displayItems.Count) {
            $dItem = $displayItems[$dIdx]

            switch ($dItem.Type) {
                'blank' {
                    Buf-ClearLine $row
                }
                'section' {
                    $sectionText = " $([char]0x2500)$([char]0x2500)  $($dItem.Text) "
                    $remaining = $w - $sectionText.Length
                    if ($remaining -gt 0) { $sectionText += ([char]0x2500).ToString() * $remaining }
                    Buf-FullLine $row $sectionText 'Yellow'
                }
                'separator' {
                    Buf-FullLine $row "    $([char]0x00BB) $($dItem.Text)" 'Cyan'
                }
                'field' {
                    $field = $dItem.Item
                    $isSelected = ($dItem.FieldIndex -eq $script:EnvSelectedIndex)
                    $isGen = ($field.Key -in $script:GeneratableFields)
                    $genIcon = if ($isGen) { "$([char]0x26A1)" } else { ' ' }
                    $marker = if ($isSelected) { " $([char]0x25B8)$genIcon" } else { "  $genIcon" }

                    # ⚡ occupies 2 visual cells but 1 string char length. Adjust target width.
                    $targetKeyW = if ($isGen) { $keyW - 1 } else { $keyW }

                    $keyStr = "$marker$($field.Label)".PadRight($targetKeyW).Substring(0, $targetKeyW)

                    if ($script:EnvEditing -and $isSelected) {
                        # Show edit buffer with cursor
                        $valStr = "= $($script:EnvEditBuffer)$([char]0x2588)"
                    }
                    else {
                        $displayVal = $field.Value
                        # Mask sensitive fields
                        if ($field.Key -match 'PASSWORD|SECRET|KEY|TOKEN' -and $displayVal -ne '' -and $displayVal -notmatch '^<') {
                            if ($displayVal.Length -gt 8) {
                                $displayVal = $displayVal.Substring(0, 4) + ('*' * [Math]::Min(20, $displayVal.Length - 4))
                            }
                        }
                        $valStr = "= $displayVal"
                    }
                    if ($valStr.Length -gt $valW) { $valStr = $valStr.Substring(0, $valW) }
                    $valStr = $valStr.PadRight($valW)

                    $fg = if ($isSelected) { 'White' } else { 'Gray' }
                    $bg = if ($isSelected) { 'DarkBlue' } else { $defBg }
                    $valFg = if ($isSelected) { 'Cyan' } else { 'DarkGray' }

                    # Write key + value in one buffered operation
                    $fgCode = $script:AnsiFg[$fg]
                    $bgCode = $script:AnsiBg[$bg]
                    $valFgCode = $script:AnsiFg[$valFg]
                    [void]$script:FrameBuffer.Append("`e[$($row+1);1H$fgCode$bgCode$keyStr$valFgCode$valStr`e[0m")
                }
            }
        }
        else {
            Buf-ClearLine $row
        }
    }

    # Selection info line (Key + Description)
    $descRow = $h - 4
    $allFields = Get-AllEnvFields
    if ($script:EnvSelectedIndex -lt $allFields.Count) {
        $field = $allFields[$script:EnvSelectedIndex]
        $desc = "  $([char]0x00BB) $($field.Key) : $($field.Description)"
        if ($desc.Length -gt $w) { $desc = $desc.Substring(0, $w - 3) + '...' }
        Buf-FullLine $descRow $desc 'DarkCyan'
    }
    else {
        Buf-ClearLine $descRow
    }

    # Separator
    Buf-FullLine ($h - 3) (' ' + (([char]0x2500).ToString() * ($w - 2)) + ' ') 'DarkGray'

    # Bottom bar
    $footerBg = 'DarkGray'
    if ($script:EnvEditing) {
        Buf-FullLine ($h - 2) '' 'White' $footerBg
        Buf-FullLine ($h - 1) '' 'White' $footerBg
        
        $curX = 2
        Buf-At-Raw $curX ($h - 2) '[' 'Gray' $footerBg
        Buf-At-Raw ($curX + 1) ($h - 2) 'Enter' 'Green' $footerBg
        Buf-At-Raw ($curX + 6) ($h - 2) '] Confirm Edit' 'White' $footerBg

        $curX = 2
        Buf-At-Raw $curX ($h - 1) '[' 'Gray' $footerBg
        Buf-At-Raw ($curX + 1) ($h - 1) 'Esc' 'Red' $footerBg
        Buf-At-Raw ($curX + 4) ($h - 1) '] Cancel Edit' 'White' $footerBg
    }
    else {
        Buf-FullLine ($h - 2) '' 'White' $footerBg
        Buf-FullLine ($h - 1) '' 'White' $footerBg

        # Row 1: Global Save Actions (Fixed Grid)
        $pipeX = 66
        $colWidth = 20
        $actions = @(
            @{ K = 'Ctrl+S'; L = 'Save'; C = 'Green' }
            @{ K = 'Ctrl+R'; L = 'Restart'; C = 'Cyan' }
            @{ K = 'Ctrl+B'; L = 'Re-Build'; C = 'Blue' }
        )
        for ($i = 0; $i -lt $actions.Count; $i++) {
            $a = $actions[$i]
            $x = 2 + ($i * $colWidth)
            Buf-At-Raw $x ($h - 2) '[' 'Gray' $footerBg
            Buf-At-Raw ($x + 1) ($h - 2) $a.K $a.C $footerBg
            Buf-At-Raw ($x + 1 + $a.K.Length) ($h - 2) "] $($a.L)" 'White' $footerBg
        }
        Buf-At-Raw $pipeX ($h - 2) '|' 'DarkGray' $footerBg
        $x = $pipeX + 4
        Buf-At-Raw $x ($h - 2) '[' 'Gray' $footerBg
        Buf-At-Raw ($x + 1) ($h - 2) 'Esc' 'Yellow' $footerBg
        Buf-At-Raw ($x + 4) ($h - 2) "] Return" 'White' $footerBg

        # Row 2: Field Actions & Nav (Fixed Grid)
        $nav = @(
            @{ K = 'Enter'; L = 'Edit'; C = 'White' }
            @{ K = 'G'; L = 'Generate'; C = 'Magenta' }
        )
        for ($i = 0; $i -lt $nav.Count; $i++) {
            $n = $nav[$i]
            $x = 2 + ($i * $colWidth)
            Buf-At-Raw $x ($h - 1) '[' 'Gray' $footerBg
            Buf-At-Raw ($x + 1) ($h - 1) $n.K $n.C $footerBg
            Buf-At-Raw ($x + 1 + $n.K.Length) ($h - 1) "] $($n.L)" 'White' $footerBg
        }
        Buf-At-Raw $pipeX ($h - 1) '|' 'DarkGray' $footerBg
        $x = $pipeX + 4
        Buf-At-Raw $x ($h - 1) '[' 'Gray' $footerBg
        Buf-At-Raw ($x + 1) ($h - 1) "$([char]0x2191)$([char]0x2193)" 'Gray' $footerBg
        Buf-At-Raw ($x + 3) ($h - 1) "] Nav" 'White' $footerBg
    }
}

function Save-EnvFile {
    # Read .env.example as template for structure
    $outputLines = @()
    $allFields = Get-AllEnvFields
    $fieldMap = @{}
    foreach ($f in $allFields) { $fieldMap[$f.Key] = $f.Value }

    if (Test-Path $script:EnvExampleFile) {
        $templateLines = Get-Content $script:EnvExampleFile
        foreach ($line in $templateLines) {
            if ($line -match '^([A-Z_][A-Z0-9_]*)=') {
                $key = $Matches[1]
                if ($fieldMap.ContainsKey($key)) {
                    $val = $fieldMap[$key]
                    if ($val -match '\s' -and $val -notmatch '^"') { $val = '"' + $val + '"' }
                    $outputLines += "$key=$val"
                }
                else {
                    $outputLines += $line
                }
            }
            else {
                $outputLines += $line
            }
        }
    }

    $outputLines | Set-Content -Path $script:EnvFile -Encoding UTF8
    $script:EnvDirty = $false
}

function Handle-EnvInput {
    param([System.ConsoleKeyInfo]$Key)

    $allFields = Get-AllEnvFields
    $fieldCount = $allFields.Count
    if ($fieldCount -eq 0) { return }

    if ($script:EnvEditing) {
        # Editing mode
        switch ($Key.Key) {
            'Enter' {
                # Confirm edit
                $allFields[$script:EnvSelectedIndex].Value = $script:EnvEditBuffer
                $script:EnvEditing = $false
                $script:EnvDirty = $true
                return
            }
            'Escape' {
                # Cancel edit
                $script:EnvEditing = $false
                return
            }
            'Backspace' {
                if ($script:EnvEditBuffer.Length -gt 0) {
                    $script:EnvEditBuffer = $script:EnvEditBuffer.Substring(0, $script:EnvEditBuffer.Length - 1)
                }
                return
            }
            default {
                if ($Key.KeyChar -ne [char]0) {
                    $script:EnvEditBuffer += $Key.KeyChar
                }
                return
            }
        }
    }

    # Non-editing mode
    # Check for Ctrl+S (Save)
    if ($Key.Key -eq 'S' -and ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
        Save-EnvFile
        # Flash a save confirmation
        Write-CenteredAt ([int]($script:FH / 2)) '  ✔ Saved .env  ' 'Green'
        Start-Sleep -Milliseconds 800
        return
    }

    # Check for Ctrl+R (Save & Restart)
    if ($Key.Key -eq 'R' -and ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
        Save-EnvFile
        $script:CurrentView = 'dashboard'
        Invoke-DockerAction 'restart'
        return
    }

    # Check for Ctrl+B (Save & Rebuild)
    if ($Key.Key -eq 'B' -and ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
        Save-EnvFile
        $script:CurrentView = 'dashboard'
        Invoke-DockerAction 'build'
        return
    }

    switch ($Key.Key) {
        'Escape' {
            Show-PopupMenu -Title 'Environment Editor' -Options @(
                [PSCustomObject]@{
                    Label       = 'Save & Exit'
                    Description = 'Save changes to .env and return to the dashboard'
                    Action      = {
                        Save-EnvFile
                        $script:CurrentView = 'dashboard'
                        $script:LastRefresh = [datetime]::MinValue
                        $script:ScreenDirty = $true
                    }
                },
                [PSCustomObject]@{
                    Label       = 'Discard & Exit'
                    Description = 'Discard unsaved changes and return to the dashboard'
                    Action      = {
                        $script:CurrentView = 'dashboard'
                        $script:LastRefresh = [datetime]::MinValue
                        $script:ScreenDirty = $true
                    }
                },
                [PSCustomObject]@{
                    Label       = 'Cancel'
                    Description = 'Return to the environment editor'
                    Action      = {
                        $script:CurrentView = 'env'
                        $script:ScreenDirty = $true
                    }
                }
            ) -PreviousView 'env'
            return
        }
        'UpArrow' {
            if ($script:EnvSelectedIndex -gt 0) { $script:EnvSelectedIndex-- }
            else { if ($fieldCount -gt 0) { $script:EnvSelectedIndex = $fieldCount - 1 } }
        }
        'DownArrow' {
            if ($script:EnvSelectedIndex -lt $fieldCount - 1) { $script:EnvSelectedIndex++ }
            else { if ($fieldCount -gt 0) { $script:EnvSelectedIndex = 0 } }
        }
        'Enter' {
            # Start editing the selected field
            $script:EnvEditing = $true
            $script:EnvEditBuffer = $allFields[$script:EnvSelectedIndex].Value
        }
        'G' {
            # Generate a value for the selected field
            $selectedField = $allFields[$script:EnvSelectedIndex]
            if ($selectedField.Key -in $script:GeneratableFields) {
                # Build a map of all current field values
                $fieldMap = @{}
                foreach ($f in $allFields) { $fieldMap[$f.Key] = $f.Value }

                $generated = New-GeneratedValue -FieldKey $selectedField.Key -AllFieldMap $fieldMap
                if ($null -ne $generated) {
                    $selectedField.Value = $generated
                    $script:EnvDirty = $true

                    # If JWT_SECRET was auto-generated for ANON/SERVICE keys, update it too
                    if ($selectedField.Key -in @('ANON_KEY', 'SERVICE_ROLE_KEY')) {
                        $jwtField = $allFields | Where-Object { $_.Key -eq 'JWT_SECRET' } | Select-Object -First 1
                        if ($jwtField -and $fieldMap['JWT_SECRET'] -ne $jwtField.Value) {
                            $jwtField.Value = $fieldMap['JWT_SECRET']
                        }
                    }

                    # Flash confirmation
                    Write-CenteredAt ([int]($script:FH / 2)) "  ⚡ Generated $($selectedField.Key)  " 'Green'
                    Start-Sleep -Milliseconds 600
                }
            }
        }
    }
}

# ---------------------------------------------------------------------------
# Popup menu (horizontal frame-buffer rendering)
# ---------------------------------------------------------------------------

function Show-PopupMenu {
    param(
        [string]$Title,
        [array]$Options,
        [string]$PreviousView = 'dashboard'
    )
    $script:PopupTitle = $Title
    $script:PopupOptions = $Options
    $script:PopupSelectedIndex = 0
    $script:PopupPreviousView = $PreviousView
    $script:CurrentView = 'popup-menu'
    $script:ScreenDirty = $true
}

function Get-PopupOptionColor {
    param([string]$Label, [bool]$IsSelected)
    if ($IsSelected) { return @{ Fg = 'White'; Bg = 'DarkCyan' } }
    $lower = $Label.ToLower()
    if ($lower -match 'start|logs|save') { return @{ Fg = 'Green'; Bg = '' } }
    if ($lower -match 'stop|destroy|discard|remove|delete') { return @{ Fg = 'Red'; Bg = '' } }
    if ($lower -match 'build|restart|generate|rebuild') { return @{ Fg = 'Yellow'; Bg = '' } }
    if ($lower -match 'cancel|back') { return @{ Fg = 'DarkGray'; Bg = '' } }
    return @{ Fg = 'Cyan'; Bg = '' }
}

function Build-PopupRowLayout {
    # Returns an array of arrays; each inner array is the option indices for that row
    $w = $script:FW
    $boxW = [Math]::Max(50, [Math]::Min(90, $w - 4))
    $innerW = $boxW - 4   # 2 border chars + 2 padding

    $rows = [System.Collections.Generic.List[object]]::new()
    $currentRow = [System.Collections.Generic.List[int]]::new()
    $currentWidth = 0

    for ($i = 0; $i -lt $script:PopupOptions.Count; $i++) {
        $label = $script:PopupOptions[$i].Label
        $btnW = $label.Length + 4   # "[ " + label + " ]"
        $gapW = if ($currentRow.Count -gt 0) { 2 } else { 0 }

        if ($currentRow.Count -gt 0 -and ($currentWidth + $gapW + $btnW) -gt $innerW) {
            $rows.Add($currentRow.ToArray())
            $currentRow = [System.Collections.Generic.List[int]]::new()
            $currentWidth = 0
            $gapW = 0
        }
        $currentRow.Add($i)
        $currentWidth += $gapW + $btnW
    }
    if ($currentRow.Count -gt 0) { $rows.Add($currentRow.ToArray()) }
    return $rows.ToArray()
}

function Draw-PopupMenu {
    $w = $script:FW
    $h = $script:FH

    $rows = Build-PopupRowLayout
    $script:PopupRowLayout = $rows

    $boxW = [Math]::Max(50, [Math]::Min(90, $w - 4))
    $boxW = [Math]::Max($boxW, $script:PopupTitle.Length + 6)

    # Box height: 1 title + 1 separator + 1 blank + rows.Count option rows + 1 blank + 1 description + 1 blank + 2 border = rows.Count + 7
    $numOptionRows = $rows.Count
    $boxH = $numOptionRows + 7
    $boxX = [int](($w - $boxW) / 2)
    $boxY = [int](($h - $boxH) / 2)

    $hline = ([char]0x2500).ToString() * ($boxW - 2)
    $vbar = [char]0x2502

    # Top border
    $topBorder = "$([char]0x250C)$hline$([char]0x2510)"
    Buf-At $boxX $boxY $topBorder 'Cyan'

    # Title row
    $titlePad = [Math]::Max(0, [int](($boxW - 2 - $script:PopupTitle.Length) / 2))
    $titleInner = (' ' * $titlePad) + $script:PopupTitle + (' ' * ($boxW - 2 - $titlePad - $script:PopupTitle.Length))
    Buf-At $boxX ($boxY + 1) "$vbar$titleInner$vbar" 'White' 'DarkBlue'

    # Separator
    $sep = "$([char]0x251C)$hline$([char]0x2524)"
    Buf-At $boxX ($boxY + 2) $sep 'Cyan'

    # Blank row before options
    $blank = "$vbar$(' ' * ($boxW - 2))$vbar"
    Buf-At $boxX ($boxY + 3) $blank 'Cyan'

    # Option rows
    for ($ri = 0; $ri -lt $rows.Count; $ri++) {
        $rowIndices = $rows[$ri]
        # Build the inner content for this row
        $rowContent = ''
        foreach ($optIdx in $rowIndices) {
            $label = $script:PopupOptions[$optIdx].Label
            $btn = "[ $label ]"
            if ($rowContent.Length -gt 0) { $rowContent += '  ' }
            $rowContent += $btn
        }
        # Center the row content
        $pad = [Math]::Max(0, [int](($boxW - 2 - $rowContent.Length) / 2))
        $line = (' ' * $pad) + $rowContent + (' ' * ($boxW - 2 - $pad - $rowContent.Length))

        $rowY = $boxY + 3 + 1 + $ri   # +1 for the blank row above

        # Draw the full line as background first
        Buf-At $boxX $rowY "$vbar$line$vbar" 'DarkGray' 'Black'

        # Now overdraw individual buttons with correct colors
        # Recalculate button X positions within the line
        $curX = $boxX + 1 + $pad
        foreach ($optIdx in $rowIndices) {
            $label = $script:PopupOptions[$optIdx].Label
            $btn = "[ $label ]"
            $isSelected = ($optIdx -eq $script:PopupSelectedIndex)
            $col = Get-PopupOptionColor -Label $label -IsSelected $isSelected
            $fgCode = $script:AnsiFg[$col.Fg]
            $bgCode = if ($col.Bg) { $script:AnsiBg[$col.Bg] } else { $script:AnsiBg['Black'] }
            [void]$script:FrameBuffer.Append("`e[$($rowY + 1);$($curX + 1)H$fgCode$bgCode$btn`e[0m")
            $curX += $btn.Length + 2   # +2 for the '  ' gap
        }
    }

    # Blank row after options
    $afterOptionsY = $boxY + 3 + 1 + $rows.Count
    Buf-At $boxX $afterOptionsY $blank 'Cyan'

    # Description row
    $descY = $afterOptionsY + 1
    $selectedOpt = $script:PopupOptions[$script:PopupSelectedIndex]
    $desc = if ($selectedOpt.PSObject.Properties['Description'] -and $selectedOpt.Description) { $selectedOpt.Description } else { '' }
    $descInner = if ($desc.Length -gt $boxW - 4) { $desc.Substring(0, $boxW - 4) } else { $desc }
    $descPad = [Math]::Max(0, [int](($boxW - 2 - $descInner.Length) / 2))
    $descLine = (' ' * $descPad) + $descInner + (' ' * ($boxW - 2 - $descPad - $descInner.Length))
    Buf-At $boxX $descY "$vbar$descLine$vbar" 'Cyan'

    # Bottom blank row
    Buf-At $boxX ($descY + 1) $blank 'Cyan'

    # Bottom border
    $bottomBorder = "$([char]0x2514)$hline$([char]0x2518)"
    Buf-At $boxX ($descY + 2) $bottomBorder 'Cyan'
}

function Handle-PopupMenuInput {
    param([System.ConsoleKeyInfo]$Key)

    $optCount = $script:PopupOptions.Count
    if ($optCount -eq 0) { return }

    switch ($Key.Key) {
        'Escape' {
            $script:CurrentView = $script:PopupPreviousView
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
        }
        { $_ -in @('LeftArrow', 'UpArrow') } {
            if ($script:PopupSelectedIndex -gt 0) { $script:PopupSelectedIndex-- }
            else { $script:PopupSelectedIndex = $optCount - 1 }
        }
        { $_ -in @('RightArrow', 'DownArrow') } {
            if ($script:PopupSelectedIndex -lt $optCount - 1) { $script:PopupSelectedIndex++ }
            else { $script:PopupSelectedIndex = 0 }
        }
        'Enter' {
            $action = $script:PopupOptions[$script:PopupSelectedIndex].Action
            if ($action) { & $action }
        }
    }
}

# ---------------------------------------------------------------------------
# Wrappers for legacy dialogs
# ---------------------------------------------------------------------------

function Show-Confirm {
    param([string]$Message, [scriptblock]$OnConfirm)
    $script:ConfirmAction = $OnConfirm
    Show-PopupMenu -Title $Message -Options @(
        [PSCustomObject]@{
            Label       = 'Yes'
            Description = 'Confirm and proceed'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
                if ($script:ConfirmAction) { & $script:ConfirmAction }
            }
        },
        [PSCustomObject]@{
            Label       = 'No'
            Description = 'Cancel and return to the dashboard'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
            }
        }
    ) -PreviousView $script:CurrentView
}

function Show-DestroyConfirm {
    Show-PopupMenu -Title 'Stop & Destroy Containers' -Options @(
        [PSCustomObject]@{
            Label       = 'Containers Only'
            Description = 'Remove containers and networks, keep volumes and images'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
                Invoke-DockerAction -Action 'destroy'
            }
        },
        [PSCustomObject]@{
            Label       = '+Volumes'
            Description = 'Also remove named volumes and Supabase bind-mount data'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
                Invoke-DockerAction -Action 'destroy' -WithVolumes
            }
        },
        [PSCustomObject]@{
            Label       = '+Builds'
            Description = 'Also remove built Docker images (forces full rebuild next time)'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
                Invoke-DockerAction -Action 'destroy' -WithBuilds
            }
        },
        [PSCustomObject]@{
            Label       = 'All'
            Description = 'Remove containers, volumes, Supabase data, and built images'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
                Invoke-DockerAction -Action 'destroy' -WithVolumes -WithBuilds
            }
        },
        [PSCustomObject]@{
            Label       = 'Cancel'
            Description = 'Return to the dashboard without destroying anything'
            Action      = {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
            }
        }
    ) -PreviousView 'dashboard'
}

function Show-ContainerMenu {
    $c = $script:Containers[$script:SelectedIndex]
    $script:MenuContextName = if ($c.Name) { $c.Name } else { $c.Service }
    $script:MenuContextSvc = if ($c.Service) { $c.Service } else { $script:MenuContextName }
    $state = if ($c.State) { $c.State } else { 'unknown' }

    $options = @()
    $options += [PSCustomObject]@{
        Label       = 'Logs'
        Description = "Stream live logs from $($script:MenuContextName)"
        Action      = { Start-LogViewer $script:MenuContextName }
    }

    if ($state -eq 'running') {
        $options += [PSCustomObject]@{
            Label       = 'Stop'
            Description = "Stop the $($script:MenuContextSvc) service"
            Action      = {
                $script:CurrentView = 'dashboard'
                Invoke-DockerAction -Action 'stop' -ServiceName $script:MenuContextSvc
            }
        }
        $options += [PSCustomObject]@{
            Label       = 'Restart'
            Description = "Restart the $($script:MenuContextSvc) service"
            Action      = {
                $script:CurrentView = 'dashboard'
                Invoke-DockerAction -Action 'restart' -ServiceName $script:MenuContextSvc
            }
        }
    }
    else {
        $options += [PSCustomObject]@{
            Label       = 'Start'
            Description = "Start the $($script:MenuContextSvc) service"
            Action      = {
                $script:CurrentView = 'dashboard'
                Invoke-DockerAction -Action 'start' -ServiceName $script:MenuContextSvc
            }
        }
    }

    $options += [PSCustomObject]@{
        Label       = 'Build'
        Description = "Rebuild and restart the $($script:MenuContextSvc) service"
        Action      = {
            $script:CurrentView = 'dashboard'
            Invoke-DockerAction -Action 'build' -ServiceName $script:MenuContextSvc
        }
    }
    $options += [PSCustomObject]@{
        Label       = 'Cancel'
        Description = 'Return to the dashboard'
        Action      = {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
        }
    }

    Show-PopupMenu -Title "$($script:MenuContextName) ($state)" -Options $options -PreviousView 'dashboard'
}

# ---------------------------------------------------------------------------
# Dashboard input handler
# ---------------------------------------------------------------------------

function Handle-DashboardInput {
    param([System.ConsoleKeyInfo]$Key)

    switch ($Key.Key) {
        'UpArrow' {
            if ($script:SelectedIndex -gt 0) { $script:SelectedIndex-- }
            else { if ($script:Containers.Count -gt 0) { $script:SelectedIndex = $script:Containers.Count - 1 } }
        }
        'DownArrow' {
            if ($script:SelectedIndex -lt $script:Containers.Count - 1) { $script:SelectedIndex++ }
            else { if ($script:Containers.Count -gt 0) { $script:SelectedIndex = 0 } }
        }
        'Enter' {
            if ($script:Containers.Count -gt 0 -and $script:SelectedIndex -lt $script:Containers.Count) {
                Show-ContainerMenu
            }
        }
        'S' {
            if (-not ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
                Show-PopupMenu -Title 'Start Containers' -Options @(
                    [PSCustomObject]@{ Label = 'Start All'; Description = 'Run docker compose up -d for all services'; Action = { Invoke-DockerAction 'start' } },
                    [PSCustomObject]@{ Label = 'Cancel'; Description = 'Return to the dashboard without changes'; Action = { $script:CurrentView = 'dashboard'; $script:ScreenDirty = $true } }
                ) -PreviousView 'dashboard'
            }
        }
        'X' {
            Show-PopupMenu -Title 'Stop Containers' -Options @(
                [PSCustomObject]@{ Label = 'Stop All'; Description = 'Run docker compose stop for all services'; Action = { Invoke-DockerAction 'stop' } },
                [PSCustomObject]@{ Label = 'Cancel'; Description = 'Return to the dashboard without changes'; Action = { $script:CurrentView = 'dashboard'; $script:ScreenDirty = $true } }
            ) -PreviousView 'dashboard'
        }
        'R' {
            if (-not ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
                Show-PopupMenu -Title 'Restart Containers' -Options @(
                    [PSCustomObject]@{ Label = 'Restart All'; Description = 'Run docker compose restart for all services'; Action = { Invoke-DockerAction 'restart' } },
                    [PSCustomObject]@{ Label = 'Cancel'; Description = 'Return to the dashboard without changes'; Action = { $script:CurrentView = 'dashboard'; $script:ScreenDirty = $true } }
                ) -PreviousView 'dashboard'
            }
        }
        'B' {
            if (-not ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
                Show-PopupMenu -Title 'Re-Build Containers' -Options @(
                    [PSCustomObject]@{ Label = 'Build & Start'; Description = 'Run docker compose up -d --build for all services'; Action = { Invoke-DockerAction 'build' } },
                    [PSCustomObject]@{ Label = 'Cancel'; Description = 'Return to the dashboard without changes'; Action = { $script:CurrentView = 'dashboard'; $script:ScreenDirty = $true } }
                ) -PreviousView 'dashboard'
            }
        }
        'D' {
            Show-DestroyConfirm
        }
        'E' {
            Initialize-EnvEditor
        }
        'Q' {
            $script:Running = $false
        }
    }
}

# ---------------------------------------------------------------------------
# Startup logic
# ---------------------------------------------------------------------------

function Invoke-Startup {
    # Force UTF-8 for special characters and emojis
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    [Console]::Clear()
    [Console]::CursorVisible = $false

    $w = Get-ScreenWidth
    Write-Host ''
    Write-Host (' AgriSense'.PadRight($w)) -ForegroundColor White -BackgroundColor DarkCyan
    Write-Host ''
    Write-Host '  Checking project status...' -ForegroundColor Cyan
    Write-Host ''

    # Check if .env exists
    if (-not (Test-Path $script:EnvFile)) {
        Write-Host '  .env not found. Running setup wizard...' -ForegroundColor Cyan
        Start-Sleep -Milliseconds 1500
        [Console]::CursorVisible = $true
        [Console]::Clear()

        # Invoke config.ps1 inline
        & $script:ConfigScript
        [Console]::CursorVisible = $false
    }

    # Check if containers are running
    Write-Host '  Querying Docker containers...' -ForegroundColor Cyan

    $allRunning = Test-AllContainersRunning

    if (-not $allRunning -and $StartProject) {
        Write-Host ''
        Write-Host '  Not all containers are running.' -ForegroundColor Cyan
        Write-Host '  Starting the stack: docker compose up -d --build' -ForegroundColor Cyan
        Write-Host ''

        $args_ = Get-ComposeArgs
        $args_ += @('up', '-d', '--build')
        [Console]::CursorVisible = $true
        try {
            $procArgs = @('compose') + $args_
            $proc = Start-Process -FilePath "docker" -ArgumentList $procArgs -NoNewWindow -Wait -PassThru
            if ($proc.ExitCode -ne 0) {
                Write-Host "  Command exited with code $($proc.ExitCode)" -ForegroundColor Red
            }
        }
        catch {
            Write-Host "  Error: $_" -ForegroundColor Red
        }
        finally {
            [Console]::CursorVisible = $false
        }

        Write-Host ''
        Write-Host '  Stack started. Loading dashboard...' -ForegroundColor Green
        Start-Sleep -Milliseconds 1000
    }
    elseif (-not $allRunning) {
        Write-Host '  Not all containers are running. Start them from the dashboard.' -ForegroundColor Yellow
        Start-Sleep -Milliseconds 800
    }
    else {
        Write-Host '  All containers are running. Loading dashboard...' -ForegroundColor Green
        Start-Sleep -Milliseconds 800
    }

    [Console]::Clear()
}


# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------

function Enter-MainLoop {
    [Console]::CursorVisible = $false
    # Initialize frame dimensions
    Update-FrameDimensions

    while ($script:Running) {
        # Check for window resize
        Update-FrameDimensions

        # Refresh container data periodically (dashboard view only)
        $now = [datetime]::Now
        if ($script:CurrentView -eq 'dashboard' -and ($now - $script:LastRefresh).TotalMilliseconds -ge $script:RefreshIntervalMs) {
            $script:Containers = @(Get-ProjectContainers)
            if ($script:SelectedIndex -ge $script:Containers.Count -and $script:Containers.Count -gt 0) {
                $script:SelectedIndex = $script:Containers.Count - 1
            }
            $script:LastRefresh = $now
            $script:ScreenDirty = $true
        }

        # Log view: always check for new lines (they come from async events)
        if ($script:CurrentView -eq 'logs') {
            $prevCount = $script:LogBuffer.Count
            Update-LogBuffer
            if ($script:LogBuffer.Count -ne $prevCount) {
                $script:ScreenDirty = $true
            }
        }

        # Action view: drain output and check process state
        if ($script:CurrentView -eq 'action') {
            $prevCount = $script:ActionBuffer.Count
            $wasComplete = $script:ActionComplete
            Update-ActionBuffer
            if (-not $script:ActionComplete) {
                $script:ScreenDirty = $true   # keep spinner animating
            }
            elseif ($script:ActionBuffer.Count -ne $prevCount -or $script:ActionComplete -ne $wasComplete) {
                $script:ScreenDirty = $true
            }
        }

        # Draw current view ONLY if dirty
        if ($script:ScreenDirty) {
            switch ($script:CurrentView) {
                'dashboard' { Draw-Dashboard }
                'logs' { Draw-LogViewer }
                'env' { Draw-EnvEditor }
                'popup-menu' { Draw-PopupMenu }
                'action' { Draw-ActionView }
            }
            Flush-Frame
            $script:ScreenDirty = $false
        }

        # Process input (non-blocking)
        $hadInput = $false
        while ([Console]::KeyAvailable) {
            $hadInput = $true
            $key = [Console]::ReadKey($true)
            switch ($script:CurrentView) {
                'dashboard' { Handle-DashboardInput $key }
                'logs' { Handle-LogInput $key }
                'env' { Handle-EnvInput $key }
                'popup-menu' { Handle-PopupMenuInput $key }
                'action' { Handle-ActionInput $key }
            }
        }

        # After any key press, mark dirty for immediate redraw
        if ($hadInput) {
            $script:ScreenDirty = $true
            Start-Sleep -Milliseconds 16  # ~60fps cap
        }
        else {
            # Idle — longer sleep to save CPU
            Start-Sleep -Milliseconds 50
        }
    }
}

# ---------------------------------------------------------------------------
# CLI dispatch (non-interactive flags)
# ---------------------------------------------------------------------------

function Show-CliHelp {
    Write-Host ''
    Write-Host '  AgriSense CLI' -ForegroundColor Cyan
    Write-Host '  Usage: .\agrisense.ps1 [flags]' -ForegroundColor Gray
    Write-Host ''
    Write-Host '  No flags          Launch the interactive TUI dashboard'
    Write-Host '  -StartProject     Automatically start containers when launching TUI'
    Write-Host '  -Development      Enable hot-reload dev mode (mounts local source dirs)'
    Write-Host '  -Start            Start all containers (docker compose up -d)'
    Write-Host '  -Stop             Stop all containers'
    Write-Host '  -Restart          Restart all containers'
    Write-Host '  -Build            Rebuild and start all containers'
    Write-Host '  -Destroy          Stop and destroy containers'
    Write-Host '  -RemoveVolumes    Also remove named volumes when destroying'
    Write-Host '  -RemoveBuilds     Also remove image builds when destroying'
    Write-Host '  -Status           Show container status and exit'
    Write-Host '  -Help             Show this help message'
    Write-Host ''
    Write-Host '  -Development can be combined with any action flag:'
    Write-Host '    .\agrisense.ps1 -Development -Start'
    Write-Host '    .\agrisense.ps1 -Development -Restart'
    Write-Host '    .\agrisense.ps1 -Development -Build'
    Write-Host '    .\agrisense.ps1 -Development              (TUI, dev mode)'
    Write-Host ''
}

function Invoke-CliAction {
    param(
        [string]$Action,
        [switch]$WithVolumes,
        [switch]$WithBuilds
    )
    $args_ = Get-ComposeArgs
    switch ($Action) {
        'start' { $args_ += @('up', '-d') }
        'stop' { $args_ += 'stop' }
        'restart' { $args_ += 'restart' }
        'build' { $args_ += @('up', '-d', '--build') }
        'destroy' {
            $args_ += @('down', '--remove-orphans')
            if ($WithVolumes) {
                $args_ += '-v'
                # Supabase uses bind mounts for persistence that aren't removed by `down -v`
                $dbPath = Join-Path $script:ScriptDir 'supabase\volumes\db\data'
                $storagePath = Join-Path $script:ScriptDir 'supabase\volumes\storage'
                if (Test-Path $dbPath) { Get-ChildItem -Path $dbPath | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue }
                if (Test-Path $storagePath) { Get-ChildItem -Path $storagePath | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue }
            }
            if ($WithBuilds) { $args_ += '--rmi', 'all' }
        }
    }
    Write-Host "  docker compose $($args_[4..$($args_.Count-1)] -join ' ')" -ForegroundColor Cyan
    Write-Host ''
    $procArgs = @('compose') + $args_
    $proc = Start-Process -FilePath "docker" -ArgumentList $procArgs -NoNewWindow -Wait -PassThru
    $global:LASTEXITCODE = $proc.ExitCode
}

function Show-CliStatus {
    $containers = @(Get-ProjectContainers)
    if ($containers.Count -eq 0) {
        Write-Host '  No containers found.' -ForegroundColor DarkGray
        return
    }
    $running = @($containers | Where-Object { $_.State -eq 'running' }).Count
    Write-Host "  $running/$($containers.Count) containers running" -ForegroundColor $(if ($running -eq $containers.Count) { 'Green' } else { 'Yellow' })
    Write-Host ''
    Write-Host ('  {0,-30} {1,-12} {2}' -f 'CONTAINER', 'STATE', 'STATUS') -ForegroundColor DarkGray
    foreach ($c in $containers) {
        $name = if ($c.Name) { $c.Name } else { $c.Service }
        $state = if ($c.State) { $c.State } else { 'unknown' }
        $status = if ($c.Status) { $c.Status } else { '' }
        $fg = switch ($state) {
            'running' { 'Green' }
            'exited' { 'Red' }
            default { 'Gray' }
        }
        Write-Host ('  {0,-30} {1,-12} {2}' -f $name, $state, $status) -ForegroundColor $fg
    }
    Write-Host ''
}

# Check if any CLI action flag was passed (-Development alone opens the TUI in dev mode)
$cliMode = $Help -or $Start -or $Stop -or $Restart -or $Build -or $Destroy -or $Status

if ($cliMode) {
    if ($Help) { Show-CliHelp; exit 0 }
    if ($Status) { Show-CliStatus; exit 0 }
    if ($Start) { Invoke-CliAction 'start'; exit $LASTEXITCODE }
    if ($Stop) { Invoke-CliAction 'stop'; exit $LASTEXITCODE }
    if ($Restart) { Invoke-CliAction 'restart'; exit $LASTEXITCODE }
    if ($Build) { Invoke-CliAction 'build'; exit $LASTEXITCODE }
    if ($Destroy) { Invoke-CliAction -Action 'destroy' -WithVolumes:$RemoveVolumes -WithBuilds:$RemoveBuilds; exit $LASTEXITCODE }
    exit 0
}

# ---------------------------------------------------------------------------
# TUI entry point (no flags = interactive mode)
# ---------------------------------------------------------------------------

try {
    # Save console state
    try { $script:OrigCursorVisible = [Console]::CursorVisible } catch { $script:OrigCursorVisible = $true }
    try { $script:OrigTitle = [Console]::Title } catch { $script:OrigTitle = '' }
    [Console]::Title = 'AgriSense Dashboard'

    Invoke-Startup
    Enter-MainLoop
}
catch {
    $script:LastError = $_
}
finally {
    # Restore console state
    try { [Console]::CursorVisible = $script:OrigCursorVisible } catch {}
    try { [Console]::Title = $script:OrigTitle } catch {}
    [Console]::ResetColor()

    # Cleanup log process and events if running
    if ($script:LogProcess) {
        try { Stop-LogViewer } catch {}
    }

    # Cleanup action process and events if running
    if ($script:ActionProcess) {
        try { Stop-ActionView } catch {}
    }

    [Console]::Clear()

    if ($script:LastError) {
        Write-Host 'AgriSense TUI crashed with an error:' -ForegroundColor Red
        Write-Host ''
        Write-Host "  $($script:LastError)" -ForegroundColor Yellow
        Write-Host ''
        Write-Host "  at $($script:LastError.InvocationInfo.PositionMessage)" -ForegroundColor DarkGray
        Write-Host ''
    }
    else {
        Write-Host 'AgriSense TUI exited.' -ForegroundColor DarkGray
    }
}
