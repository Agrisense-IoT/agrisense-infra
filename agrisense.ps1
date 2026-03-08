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
    [switch]$Help
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
$script:ConfigScript = Join-Path $script:ScriptDir 'config.ps1'

# State
$script:SelectedIndex = 0
$script:Containers = @()
$script:LastRefresh = [datetime]::MinValue
$script:RefreshIntervalMs = 2000
$script:Running = $true
$script:CurrentView = 'dashboard'  # dashboard | logs | env | confirm
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

# Confirm dialog state
$script:ConfirmMessage = ''
$script:ConfirmAction = $null

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
    return @('--env-file', $script:EnvFile, '--project-directory', $script:ScriptDir)
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

    # Run in foreground, capturing output
    $script:CurrentView = 'action'
    [Console]::Clear()
    [Console]::SetCursorPosition(0, 0)
    [Console]::CursorVisible = $true
    $header = " AgriSense > docker compose $($args_[4..$($args_.Count-1)] -join ' ') "
    Write-Host $header -ForegroundColor Black -BackgroundColor DarkCyan
    Write-Host ''

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
    Write-Host '  Press any key to return to the dashboard...' -ForegroundColor DarkGray
    [Console]::ReadKey($true) | Out-Null

    $script:CurrentView = 'dashboard'
    $script:LastRefresh = [datetime]::MinValue
    $script:ScreenDirty = $true
}

# ---------------------------------------------------------------------------
# Dashboard drawing
# ---------------------------------------------------------------------------

function Draw-Dashboard {
    $w = $script:FW
    $h = $script:FH

    # Header (row 0-2)
    $title = " AgriSense Dashboard"
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
    $bar1 = '  [S] Start  [X] Stop  [R] Restart  [B] Re-Build  [D] Stop & Destroy'
    $bar2 = '  [E] Edit .env  [Q] Quit  [Enter] View Logs  [' + $([char]0x2191) + $([char]0x2193) + '] Navigate'
    Buf-FullLine ($h - 2) $bar1 'White' 'DarkGray'
    Buf-FullLine ($h - 1) $bar2 'White' 'DarkGray'
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
    $bar = '  [Esc] Back to Dashboard  [' + $([char]0x2191) + $([char]0x2193) + '] Scroll  [PgUp/PgDn] Page  [Home] Top  [End] Bottom (auto-tail)'
    Buf-FullLine ($h - 2) (' ' + (([char]0x2500).ToString() * ($w - 2)) + ' ') 'DarkGray'
    Buf-FullLine ($h - 1) $bar 'White' 'DarkGray'
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
    if ($script:EnvEditing) {
        $bar1 = '  [Enter] Confirm Edit  [Esc] Cancel Edit'
        $bar2 = ''
    }
    else {
        $bar1 = '  [Esc] Discard & Return  [Ctrl+S] Save  [Ctrl+R] Save & Restart'
        $bar2 = '  [Ctrl+B] Save & Re-Build  [Enter] Edit  [G] Generate  [' + $([char]0x2191) + $([char]0x2193) + '] Navigate'
    }
    Buf-FullLine ($h - 2) $bar1 'White' 'DarkGray'
    Buf-FullLine ($h - 1) $bar2 'White' 'DarkGray'
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
        [Console]::Clear()
        Invoke-DockerAction 'restart'
        return
    }

    # Check for Ctrl+B (Save & Rebuild)
    if ($Key.Key -eq 'B' -and ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
        Save-EnvFile
        $script:CurrentView = 'dashboard'
        [Console]::Clear()
        Invoke-DockerAction 'build'
        return
    }

    switch ($Key.Key) {
        'Escape' {
            if ($script:EnvDirty) {
                Show-Confirm 'Discard unsaved changes?' {
                    $script:CurrentView = 'dashboard'
                    $script:LastRefresh = [datetime]::MinValue
                    $script:ScreenDirty = $true
                    [Console]::Clear()
                }
            }
            else {
                $script:CurrentView = 'dashboard'
                $script:LastRefresh = [datetime]::MinValue
                $script:ScreenDirty = $true
                [Console]::Clear()
            }
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
# Confirmation dialog
# ---------------------------------------------------------------------------

function Show-Confirm {
    param([string]$Message, [scriptblock]$OnConfirm)
    $script:ConfirmMessage = $Message
    $script:ConfirmAction = $OnConfirm
    $script:CurrentView = 'confirm'
    Draw-Confirm
}

function Draw-Confirm {
    $w = Get-ScreenWidth
    $h = Get-ScreenHeight
    $boxW = [Math]::Min(60, $w - 8)
    $boxH = 5
    $boxX = [int](($w - $boxW) / 2)
    $boxY = [int](($h - $boxH) / 2)

    # Draw box
    $topBorder = $([char]0x250C) + (([char]0x2500).ToString() * ($boxW - 2)) + $([char]0x2510)
    $bottomBorder = $([char]0x2514) + (([char]0x2500).ToString() * ($boxW - 2)) + $([char]0x2518)
    $emptyLine = $([char]0x2502) + (' ' * ($boxW - 2)) + $([char]0x2502)

    Write-At $boxX $boxY $topBorder 'Cyan'
    Write-At $boxX ($boxY + 1) $emptyLine 'Cyan'

    # Center message in box
    $msgPad = [Math]::Max(0, [int](($boxW - 2 - $script:ConfirmMessage.Length) / 2))
    $msgLine = $([char]0x2502) + (' ' * $msgPad) + $script:ConfirmMessage + (' ' * ($boxW - 2 - $msgPad - $script:ConfirmMessage.Length)) + $([char]0x2502)
    Write-At $boxX ($boxY + 2) $msgLine 'White'

    $promptText = '[Y] Yes  [N] No'
    $promptPad = [Math]::Max(0, [int](($boxW - 2 - $promptText.Length) / 2))
    $promptLine = $([char]0x2502) + (' ' * $promptPad) + $promptText + (' ' * ($boxW - 2 - $promptPad - $promptText.Length)) + $([char]0x2502)
    Write-At $boxX ($boxY + 3) $promptLine 'White'

    Write-At $boxX ($boxY + 4) $bottomBorder 'Cyan'
}

function Handle-ConfirmInput {
    param([System.ConsoleKeyInfo]$Key)
    switch ($Key.Key) {
        'Y' {
            $action = $script:ConfirmAction
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
            if ($action) { & $action }
        }
        { $_ -eq 'N' -or $_ -eq 'Escape' } {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
        }
    }
}

# ---------------------------------------------------------------------------
# Destroy Confirmation dialog
# ---------------------------------------------------------------------------

function Show-DestroyConfirm {
    $script:CurrentView = 'confirm-destroy'
    Draw-DestroyConfirm
}

function Draw-DestroyConfirm {
    $w = Get-ScreenWidth
    $h = Get-ScreenHeight
    $boxW = [Math]::Min(75, $w - 2)
    $boxH = 7
    $boxX = [int](($w - $boxW) / 2)
    $boxY = [int](($h - $boxH) / 2)

    # Draw box
    $topBorder = $([char]0x250C) + (([char]0x2500).ToString() * ($boxW - 2)) + $([char]0x2510)
    $bottomBorder = $([char]0x2514) + (([char]0x2500).ToString() * ($boxW - 2)) + $([char]0x2518)
    $emptyLine = $([char]0x2502) + (' ' * ($boxW - 2)) + $([char]0x2502)

    Write-At $boxX $boxY $topBorder 'Red'
    for ($i = 1; $i -lt ($boxH - 1); $i++) {
        Write-At $boxX ($boxY + $i) $emptyLine 'Red'
    }

    $msg1 = "Stop & destroy containers?"
    $msgPad1 = [Math]::Max(0, [int](($boxW - 2 - $msg1.Length) / 2))
    $msgLine1 = $([char]0x2502) + (' ' * $msgPad1) + $msg1 + (' ' * ($boxW - 2 - $msgPad1 - $msg1.Length)) + $([char]0x2502)
    Write-At $boxX ($boxY + 2) $msgLine1 'White'

    $promptText1 = '[Y] Containers Only  [V] +Volumes  [B] +Builds  [A] All  [N] Cancel'
    $promptPad1 = [Math]::Max(0, [int](($boxW - 2 - $promptText1.Length) / 2))
    $promptLine1 = $([char]0x2502) + (' ' * $promptPad1) + $promptText1 + (' ' * ($boxW - 2 - $promptPad1 - $promptText1.Length)) + $([char]0x2502)
    Write-At $boxX ($boxY + 4) $promptLine1 'Yellow'

    Write-At $boxX ($boxY + ($boxH - 1)) $bottomBorder 'Red'
}

function Handle-DestroyConfirmInput {
    param([System.ConsoleKeyInfo]$Key)
    switch ($Key.Key) {
        'Y' {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
            Invoke-DockerAction -Action 'destroy'
        }
        'V' {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
            Invoke-DockerAction -Action 'destroy' -WithVolumes
        }
        'B' {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
            Invoke-DockerAction -Action 'destroy' -WithBuilds
        }
        'A' {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
            Invoke-DockerAction -Action 'destroy' -WithVolumes -WithBuilds
        }
        { $_ -in @('N', 'Escape') } {
            $script:CurrentView = 'dashboard'
            $script:LastRefresh = [datetime]::MinValue
            $script:ScreenDirty = $true
            [Console]::Clear()
        }
    }
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
                $name = $script:Containers[$script:SelectedIndex].Name
                if (-not $name) { $name = $script:Containers[$script:SelectedIndex].Service }
                if ($name) { Start-LogViewer $name }
            }
        }
        'S' {
            if (-not ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
                Invoke-DockerAction 'start'
            }
        }
        'X' {
            Invoke-DockerAction 'stop'
        }
        'R' {
            if (-not ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
                Invoke-DockerAction 'restart'
            }
        }
        'B' {
            if (-not ($Key.Modifiers -band [ConsoleModifiers]::Control)) {
                Invoke-DockerAction 'build'
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

    if (-not $allRunning) {
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

        # Draw current view ONLY if dirty
        if ($script:ScreenDirty) {
            switch ($script:CurrentView) {
                'dashboard' { Draw-Dashboard }
                'logs' { Draw-LogViewer }
                'env' { Draw-EnvEditor }
                'confirm' { <# Already drawn on show #> }
                'confirm-destroy' { <# Already drawn on show #> }
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
                'confirm' { Handle-ConfirmInput $key }
                'confirm-destroy' { Handle-DestroyConfirmInput $key }
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
    Write-Host '  No flags         Launch the interactive TUI dashboard'
    Write-Host '  -Start           Start all containers (docker compose up -d)'
    Write-Host '  -Stop            Stop all containers'
    Write-Host '  -Restart         Restart all containers'
    Write-Host '  -Build           Rebuild and start all containers'
    Write-Host '  -Destroy         Stop and destroy containers'
    Write-Host '  -RemoveVolumes   Also remove named volumes when destroying'
    Write-Host '  -RemoveBuilds    Also remove image builds when destroying'
    Write-Host '  -Status          Show container status and exit'
    Write-Host '  -Help            Show this help message'
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

# Check if any CLI flag was passed
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
