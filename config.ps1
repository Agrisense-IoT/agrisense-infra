#Requires -Version 5.1
<#
.SYNOPSIS
    Interactive .env setup wizard for Agrisense infrastructure.

.DESCRIPTION
    Parses .env.example, prompts for user-specific values, optionally auto-generates
    secrets and keys, and writes a complete .env file preserving structure and comments.

.NOTES
    If you get an execution policy error, run once as Administrator:
        Set-ExecutionPolicy RemoteSigned -Scope CurrentUser
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------------------
# Terminal / TUI primitives
# ---------------------------------------------------------------------------

$W = $Host.UI.RawUI.WindowSize.Width
if ($W -lt 60) { $W = 72 }

function Clear-AndDraw { [Console]::Clear() }

function Write-Blank { Write-Host '' }

function Write-Rule {
    param([string]$Color = 'DarkGray')
    Write-Host ('─' * $W) -ForegroundColor $Color
}

function Write-Centered {
    param([string]$Text, [string]$Color = 'White')
    $pad = [Math]::Max(0, [Math]::Floor(($W - $Text.Length) / 2))
    Write-Host ((' ' * $pad) + $Text) -ForegroundColor $Color
}


function Write-StepBanner {
    param([int]$Step, [int]$Total, [string]$Title)
    Write-Blank
    Write-Rule 'DarkCyan'
    $tag = "  STEP $Step / $Total"
    $right = "  $Title  "
    $gap = $W - $tag.Length - $right.Length
    $line = $tag + (' ' * [Math]::Max(1, $gap)) + $right
    Write-Host $line -ForegroundColor Cyan
    Write-Rule 'DarkCyan'
    Write-Blank
}

function Write-SectionLabel {
    param([string]$Icon, [string]$Label)
    Write-Blank
    Write-Host "  $Icon  $Label" -ForegroundColor Yellow
    Write-Host ('  ' + '·' * ($W - 4)) -ForegroundColor DarkGray
}

function Write-Ok { param([string]$Msg) Write-Host "  ✔  $Msg" -ForegroundColor Green }
function Write-Warn { param([string]$Msg) Write-Host "  ⚠  $Msg" -ForegroundColor Yellow }
function Write-Err { param([string]$Msg) Write-Host "  ✖  $Msg" -ForegroundColor Red }
function Write-Info { param([string]$Msg) Write-Host "  ℹ  $Msg" -ForegroundColor Cyan }
function Write-Arrow { param([string]$Msg) Write-Host "  →  $Msg" -ForegroundColor DarkGray }

# ---------------------------------------------------------------------------
# Input helpers
# ---------------------------------------------------------------------------

function Read-Field {
    param(
        [string]$Label,
        [string]$Hint = '',
        [string]$Default = '',
        [bool]  $Required = $false,
        [bool]  $Secret = $false,
        [scriptblock]$Validator = $null
    )
    while ($true) {
        $suffix = if ($Default -ne '') { " ($Default)" } else { '' }
        $hint = if ($Hint -ne '') { " — $Hint" }    else { '' }
        Write-Host "  " -NoNewline
        Write-Host $Label -NoNewline -ForegroundColor White
        Write-Host $hint   -NoNewline -ForegroundColor DarkGray
        Write-Host $suffix -NoNewline -ForegroundColor DarkGray
        Write-Host ': ' -NoNewline -ForegroundColor DarkGray

        if ($Secret) {
            $raw = Read-Host -AsSecureString
            $value = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto(
                [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($raw))
        }
        else {
            $value = Read-Host
        }

        if ($value -eq '' -and $Default -ne '') { $value = $Default }

        if ($Required -and $value -eq '') {
            Write-Err 'This field is required.'; continue
        }
        if ($null -ne $Validator -and $value -ne '') {
            $errMsg = & $Validator $value
            if ($errMsg) { Write-Err $errMsg; continue }
        }
        return $value
    }
}

# Arrow-key menu. Returns the index of the selected item.
function Read-Menu {
    param(
        [string]  $Label,
        [string[]]$Items,
        [int]     $Default = 0
    )

    $cursor = $Default
    [Console]::CursorVisible = $false

    # Reserve lines: label + blank + N items + blank
    $headerLines = 2
    $totalLines = $headerLines + $Items.Count + 1

    function Show-Menu {
        param([int]$Selected)
        # Move cursor to top of menu block
        [Console]::SetCursorPosition(0, $script:menuTop)
        Write-Host ""
        Write-Host "  $Label" -ForegroundColor White
        for ($i = 0; $i -lt $Items.Count; $i++) {
            if ($i -eq $Selected) {
                Write-Host "    " -NoNewline
                Write-Host "❯ $($Items[$i])" -ForegroundColor Cyan
            }
            else {
                Write-Host "      $($Items[$i])" -ForegroundColor Gray
            }
        }
        Write-Host "  " -NoNewline
        Write-Host "(↑↓ move  Enter select)" -ForegroundColor DarkGray
    }

    # Record the row where the menu will start
    $script:menuTop = [Console]::CursorTop

    # Print blank lines to reserve space so scrolling doesn't shift content
    for ($i = 0; $i -lt $totalLines; $i++) { Write-Host "" }

    Show-Menu $cursor

    while ($true) {
        $key = [Console]::ReadKey($true)
        switch ($key.Key) {
            'UpArrow' { if ($cursor -gt 0) { $cursor-- }; Show-Menu $cursor }
            'DownArrow' { if ($cursor -lt $Items.Count - 1) { $cursor++ }; Show-Menu $cursor }
            'Enter' {
                # Move past the menu block before returning
                [Console]::SetCursorPosition(0, $script:menuTop + $totalLines)
                [Console]::CursorVisible = $true
                return $cursor
            }
        }
    }
}

# Arrow-key yes/no (two-item menu). Returns $true/$false.
function Read-YesNo {
    param([string]$Label, [bool]$Default = $true)
    $defaultIdx = if ($Default) { 0 } else { 1 }
    $idx = Read-Menu -Label $Label -Items @('Yes', 'No') -Default $defaultIdx
    return ($idx -eq 0)
}

# Arrow-key choice. Options = @('Description one', 'Description two', ...).
# Returns the zero-based index.
function Read-Choice {
    param([string]$Label, [string[]]$Options, [int]$Default = 0)
    return (Read-Menu -Label $Label -Items $Options -Default $Default)
}

# ---------------------------------------------------------------------------
# Cryptographic helpers
# ---------------------------------------------------------------------------

function New-RandomBytes {
    param([int]$Count)
    $bytes = [byte[]]::new($Count)
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    return $bytes
}

function New-RandomHex { param([int]$ByteCount) return (New-RandomBytes $ByteCount | ForEach-Object { $_.ToString('x2') }) -join '' }
function New-RandomBase64 { param([int]$ByteCount) return [Convert]::ToBase64String((New-RandomBytes $ByteCount)) }
function New-RandomAlphaNum {
    param([int]$Length)
    $chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
    return (New-RandomBytes $Length | ForEach-Object { $chars[$_ % $chars.Length] }) -join ''
}

function New-RandomPassword {
    param([int]$Length = 32)
    $upper = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
    $lower = 'abcdefghijklmnopqrstuvwxyz'
    $digits = '0123456789'
    $special = '-_'
    $all = $upper + $lower + $digits + $special

    $pass = (New-RandomBytes $Length | ForEach-Object { $all[$_ % $all.Length] }) -join ''
    $b4 = New-RandomBytes 4
    $pass = $upper[$b4[0] % $upper.Length] `
        + $lower[$b4[1] % $lower.Length] `
        + $digits[$b4[2] % $digits.Length] `
        + $special[$b4[3] % $special.Length] `
        + $pass.Substring(4)

    $arr = $pass.ToCharArray()
    for ($i = $arr.Length - 1; $i -gt 0; $i--) {
        $j = (New-RandomBytes 1)[0] % ($i + 1)
        $tmp = $arr[$i]; $arr[$i] = $arr[$j]; $arr[$j] = $tmp
    }
    return -join $arr
}

function New-SupabaseJwt {
    param([string]$Secret, [string]$Role, [int]$ExpiryYears = 5)
    $header = @{ alg = 'HS256'; typ = 'JWT' }
    $now = [int][double]::Parse((Get-Date -UFormat '%s'))
    $exp = $now + ($ExpiryYears * 365 * 24 * 3600)
    $payload = @{ role = $Role; iss = 'supabase'; iat = $now; exp = $exp }

    function ConvertTo-Base64Url {
        param([string]$s)
        $b64 = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($s))
        return $b64.TrimEnd('=').Replace('+', '-').Replace('/', '_')
    }

    $hdrB64 = ConvertTo-Base64Url ($header  | ConvertTo-Json -Compress)
    $payB64 = ConvertTo-Base64Url ($payload | ConvertTo-Json -Compress)
    $signing = "$hdrB64.$payB64"
    $hmac = [System.Security.Cryptography.HMACSHA256]::new([System.Text.Encoding]::UTF8.GetBytes($Secret))
    $sig = [Convert]::ToBase64String($hmac.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($signing))).TrimEnd('=').Replace('+', '-').Replace('/', '_')
    return "$signing.$sig"
}

# ---------------------------------------------------------------------------
# Validators
# ---------------------------------------------------------------------------

$validatePort = { param([string]$v)
    $n = 0
    if (-not [int]::TryParse($v, [ref]$n) -or $n -lt 1 -or $n -gt 65535) {
        return 'Port must be a number between 1 and 65535.'
    }
}

$validateUrl = { param([string]$v)
    if ($v -notmatch '^https?://') { return 'Must start with http:// or https://' }
}

$validateNonEmpty = { param([string]$v)
    if ($v.Trim() -eq '') { return 'Value cannot be blank.' }
}

# ---------------------------------------------------------------------------
# Parse .env.example
# ---------------------------------------------------------------------------

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$examplePath = Join-Path $scriptDir '.env.example'
$envPath = Join-Path $scriptDir '.env'

if (-not (Test-Path $examplePath)) {
    Write-Err ".env.example not found at: $examplePath"
    exit 1
}

$exampleLines = Get-Content $examplePath
$varOrder = [System.Collections.Generic.List[string]]::new()
$placeholders = @{}

foreach ($line in $exampleLines) {
    if ($line -match '^([A-Z_][A-Z0-9_]*)=(.*)$') {
        $name = $Matches[1]; $val = $Matches[2]
        if (-not $placeholders.ContainsKey($name)) {
            $varOrder.Add($name) | Out-Null
            $placeholders[$name] = $val
        }
    }
}

$resolved = @{}
foreach ($k in $varOrder) { $resolved[$k] = $placeholders[$k] }

# ===========================================================================
# SPLASH
# ===========================================================================

Clear-AndDraw
Write-Blank
Write-Rule 'DarkCyan'
Write-Centered 'Agrisense — Environment Setup Wizard' 'Cyan'
Write-Rule 'DarkCyan'
Write-Blank
Write-Arrow "Source  $examplePath"
Write-Arrow "Output  $envPath"
Write-Blank
Write-Rule 'DarkGray'

# ---------------------------------------------------------------------------
# Existing .env check
# ---------------------------------------------------------------------------

if (Test-Path $envPath) {
    Write-Blank
    Write-Warn '.env already exists at this location.'
    # 0 = Overwrite, 1 = Backup and replace, 2 = Abort
    $choice = Read-Choice 'What would you like to do?' @(
        'Overwrite it'
        'Back it up, then create a fresh one'
        'Abort — keep the existing file'
    ) 0

    if ($choice -eq 2) {
        Write-Blank
        Write-Info 'Aborted. Your existing .env was not modified.'
        Write-Blank
        exit 0
    }
    if ($choice -eq 1) {
        $stamp = Get-Date -Format 'yyyyMMdd_HHmmss'
        $backup = "$envPath.backup_$stamp"
        Copy-Item $envPath $backup
        Write-Ok "Backed up to: $(Split-Path -Leaf $backup)"
    }
}

# ===========================================================================
# STEP 1a — Dashboard credentials
# ===========================================================================

Clear-AndDraw
Write-StepBanner 1 5 'Supabase Dashboard'
Write-SectionLabel '🔐' 'Supabase Dashboard'
Write-Arrow 'Access the Supabase Studio web UI with these credentials.'
Write-Blank

$resolved['DASHBOARD_USERNAME'] = Read-Field 'Username' `
    -Default 'admin' -Required $true -Validator $validateNonEmpty

$dashPass = Read-Field 'Password' -Hint 'leave blank to auto-generate' -Secret $true `
    -Validator { param($v) if ($v -ne '' -and $v.Length -lt 8) { return 'Minimum 8 characters.' } }
if ($dashPass -eq '') {
    $dashPass = New-RandomPassword 20
    Write-Ok "Generated password: $dashPass"
}
$resolved['DASHBOARD_PASSWORD'] = $dashPass

# ===========================================================================
# STEP 1b — Public URLs
# ===========================================================================

Clear-AndDraw
Write-StepBanner 2 5 'Public URLs'
Write-SectionLabel '🌐' 'Public URLs'
Write-Arrow 'Keep defaults for local dev. Use your domain for production.'
Write-Blank

$resolved['SITE_URL'] = Read-Field 'Frontend URL  (SITE_URL)' `
    -Default 'http://localhost:3000' -Required $true -Validator $validateUrl

$resolved['SUPABASE_PUBLIC_URL'] = Read-Field 'Supabase URL  (SUPABASE_PUBLIC_URL)' `
    -Default 'http://localhost:8000' -Required $true -Validator $validateUrl

$resolved['API_EXTERNAL_URL'] = Read-Field 'Auth callback URL  (API_EXTERNAL_URL)' `
    -Default $resolved['SUPABASE_PUBLIC_URL'] -Required $true -Validator $validateUrl

# ===========================================================================
# STEP 1c — Database
# ===========================================================================

Clear-AndDraw
Write-StepBanner 3 5 'Database'
Write-SectionLabel '🗄️' 'Database'
Write-Blank

$pgPass = Read-Field 'PostgreSQL password' -Hint 'leave blank to auto-generate' -Secret $true `
    -Validator { param($v) if ($v -ne '' -and $v.Length -lt 12) { return 'Minimum 12 characters.' } }
if ($pgPass -eq '') {
    $pgPass = New-RandomPassword 32
    Write-Ok 'Generated PostgreSQL password  (saved to .env)'
}
$resolved['POSTGRES_PASSWORD'] = $pgPass

# ===========================================================================
# STEP 2 — Ports & Options
# ===========================================================================

# ===========================================================================
# STEP 4 — Application Ports
# ===========================================================================

Clear-AndDraw
Write-StepBanner 4 6 'Application Ports'
Write-SectionLabel '🔌' 'Application Ports'
Write-Blank

if (Read-YesNo 'Customise ports? (no = keep defaults)' $false) {
    Write-Blank
    $resolved['PORT'] = Read-Field 'NestJS API port      (PORT)'           -Default '3143' -Required $true -Validator $validatePort
    $resolved['FRONTEND_PORT'] = Read-Field 'Frontend port        (FRONTEND_PORT)'  -Default '3000' -Required $true -Validator $validatePort
    $resolved['KONG_HTTP_PORT'] = Read-Field 'Kong HTTP port       (KONG_HTTP_PORT)' -Default '8000' -Required $true -Validator $validatePort
    $resolved['KONG_HTTPS_PORT'] = Read-Field 'Kong HTTPS port      (KONG_HTTPS_PORT)'-Default '8443' -Required $true -Validator $validatePort
}
else {
    Write-Info "Using defaults — API :$($placeholders['PORT'])  Frontend :$($placeholders['FRONTEND_PORT'])  Kong :$($placeholders['KONG_HTTP_PORT'])/:$($placeholders['KONG_HTTPS_PORT'])"
}

# Check each port for conflicts and re-prompt if in use
Write-Blank
$portVars = [ordered]@{
    'PORT'                          = 'NestJS API port'
    'FRONTEND_PORT'                 = 'Frontend port'
    'KONG_HTTP_PORT'                = 'Kong HTTP port'
    'KONG_HTTPS_PORT'               = 'Kong HTTPS port'
    'POSTGRES_PORT'                 = 'PostgreSQL port'
    'POOLER_PROXY_PORT_TRANSACTION' = 'Supavisor transaction port'
    'MQTT_PORT'                     = 'MQTT port'
}

$script:activePorts = [System.Net.NetworkInformation.IPGlobalProperties]::GetIPGlobalProperties().GetActiveTcpListeners() |
ForEach-Object { $_.Port }

foreach ($varName in $portVars.Keys) {
    $label = $portVars[$varName]
    $portNum = [int]$resolved[$varName]
    if ($portNum -in $script:activePorts) {
        Write-Warn "$label :$portNum is already in use."
        $resolved[$varName] = Read-Field "$label" `
            -Hint 'enter a free port' `
            -Required $true `
            -Validator {
            param($v)
            $n = 0
            if (-not [int]::TryParse($v, [ref]$n) -or $n -lt 1 -or $n -gt 65535) {
                return 'Port must be a number between 1 and 65535.'
            }
            if ($n -in $script:activePorts) { return "Port $n is also in use. Choose another." }
        }
    }
    else {
        Write-Ok "$label :$portNum is free."
    }
}

# ===========================================================================
# STEP 5 — Startup Options
# ===========================================================================

Clear-AndDraw
Write-StepBanner 5 6 'Startup Options'
Write-SectionLabel '⚙️' 'Startup Options'
Write-Blank

$resolved['SEED'] = if (Read-YesNo 'Seed the database on first container start?' $true) { 'true' } else { 'false' }

# MinIO — always auto-generated silently
$resolved['MINIO_ROOT_PASSWORD'] = New-RandomPassword 32

# ===========================================================================
# STEP 3 — Secret generation
# ===========================================================================

Clear-AndDraw
Write-StepBanner 6 6 'Secret Generation'
Write-Blank
Write-Info  'JWT signing key, Supabase anon/service tokens, S3 credentials, and'
Write-Info  'encryption keys will be generated using cryptographically secure randomness.'
Write-Blank

$autoGenerate = Read-YesNo 'Auto-generate all secrets and keys? (strongly recommended)' $true

$secretVars = @(
    'JWT_SECRET', 'ANON_KEY', 'SERVICE_ROLE_KEY',
    'SECRET_KEY_BASE', 'VAULT_ENC_KEY', 'PG_META_CRYPTO_KEY',
    'LOGFLARE_PUBLIC_ACCESS_TOKEN', 'LOGFLARE_PRIVATE_ACCESS_TOKEN',
    'S3_PROTOCOL_ACCESS_KEY_ID', 'S3_PROTOCOL_ACCESS_KEY_SECRET'
)

if ($autoGenerate) {
    Write-Blank

    $jwtSecret = $null

    # Generate synchronously but show spinner-style progress per item
    $steps = [ordered]@{
        'JWT signing secret'     = { $jwtSecret = New-RandomBase64 64; $resolved['JWT_SECRET'] = $jwtSecret }
        'Anon key (JWT)'         = { $resolved['ANON_KEY'] = New-SupabaseJwt -Secret $jwtSecret -Role 'anon' }
        'Service role key (JWT)' = { $resolved['SERVICE_ROLE_KEY'] = New-SupabaseJwt -Secret $jwtSecret -Role 'service_role' }
        'Secret key base'        = { $resolved['SECRET_KEY_BASE'] = New-RandomBase64 64 }
        'Vault encryption key'   = { $resolved['VAULT_ENC_KEY'] = New-RandomAlphaNum 32 }
        'PG meta crypto key'     = { $resolved['PG_META_CRYPTO_KEY'] = New-RandomBase64 32 }
        'Logflare public token'  = { $resolved['LOGFLARE_PUBLIC_ACCESS_TOKEN'] = New-RandomHex 32 }
        'Logflare private token' = { $resolved['LOGFLARE_PRIVATE_ACCESS_TOKEN'] = New-RandomHex 32 }
        'S3 access key ID'       = { $resolved['S3_PROTOCOL_ACCESS_KEY_ID'] = New-RandomAlphaNum 20 }
        'S3 access key secret'   = { $resolved['S3_PROTOCOL_ACCESS_KEY_SECRET'] = New-RandomAlphaNum 40 }
    }

    foreach ($label in $steps.Keys) {
        & $steps[$label]
        Write-Ok $label
    }

    Write-Blank
    Write-Ok 'All secrets generated.'
}
else {
    Write-Blank
    Write-Arrow 'Enter each secret manually. Press Enter to leave a placeholder.'
    Write-Blank
    foreach ($name in $secretVars) {
        $val = Read-Field $name -Secret $true
        $resolved[$name] = if ($val -ne '') { $val } else { $placeholders[$name] }
    }
}

# ===========================================================================
# Write .env + Summary
# ===========================================================================

Clear-AndDraw

Write-Blank
Write-Rule 'DarkCyan'
Write-Centered '  SETUP COMPLETE  ' 'Cyan'
Write-Rule 'DarkCyan'
Write-Blank

$outputLines = [System.Collections.Generic.List[string]]::new()
foreach ($line in $exampleLines) {
    if ($line -match '^([A-Z_][A-Z0-9_]*)=') {
        $name = $Matches[1]
        if ($resolved.ContainsKey($name)) {
            $val = $resolved[$name]
            if ($val -match '\s' -and $val -notmatch '^"') { $val = '"' + $val + '"' }
            $outputLines.Add("$name=$val") | Out-Null
        }
        else {
            $outputLines.Add($line) | Out-Null
        }
    }
    else {
        $outputLines.Add($line) | Out-Null
    }
}

$outputLines | Set-Content -Path $envPath -Encoding UTF8
Write-Ok ".env written  →  $envPath"

$unfilled = @($varOrder | Where-Object { $resolved[$_] -like '<*>' })
if ($unfilled.Count -gt 0) {
    Write-Blank
    Write-Warn 'The following variables still have placeholder values:'
    foreach ($name in $unfilled) {
        Write-Host "      $name" -NoNewline -ForegroundColor Yellow
        Write-Host " = $($resolved[$name])" -ForegroundColor DarkGray
    }
    Write-Blank
    Write-Arrow 'Edit .env manually before starting services.'
}
else {
    Write-Ok 'All variables configured.'
}

Write-Blank
Write-Rule 'DarkGray'
Write-Blank
