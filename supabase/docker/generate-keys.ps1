# generate-keys.ps1 - Agrisense secret generator for Supabase

$ErrorActionPreference = "Stop"

function Write-Agrisense {
    param([ConsoleColor]$Color, [string]$Prefix, [string]$Message)
    Write-Host "$Prefix " -NoNewline -ForegroundColor $Color
    Write-Host $Message
}

function Write-Info    { Write-Agrisense -Color Cyan   -Prefix "[agrisense]" -Message $args }
function Write-Success { Write-Agrisense -Color Green  -Prefix "[agrisense]" -Message $args }
function Write-Warn    { Write-Agrisense -Color Yellow -Prefix "[agrisense]" -Message $args }
function Write-ErrorMsg   { Write-Agrisense -Color Red    -Prefix "[agrisense]" -Message $args }

# Helper to generate random hex strings
function New-RandomHex {
    param([int]$Length)
    $bytes = New-Object Byte[] ($Length / 2)
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    return [System.BitConverter]::ToString($bytes).Replace("-", "").ToLower()
}

# Helper to generate random alphanumeric strings
function New-RandomString {
    param([int]$Length)
    $chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    $random = New-Object System.Random
    $result = ""
    for ($i = 0; $i -lt $Length; $i++) {
        $result += $chars[$random.Next(0, $chars.Length)]
    }
    return $result
}

# Helper to Base64Url encode
function ConvertTo-Base64Url {
    param([byte[]]$Bytes)
    $base64 = [Convert]::ToBase64String($Bytes)
    return $base64.Split('=')[0].Replace('+', '-').Replace('/', '_')
}

# Helper to generate JWT
function New-SupabaseJwt {
    param(
        [string]$Secret,
        [PSCustomObject]$Payload
    )
    $header = @{ alg = "HS256"; typ = "JWT" }
    $headerJson = $header | ConvertTo-Json -Compress
    $payloadJson = $Payload | ConvertTo-Json -Compress

    $headerBytes = [System.Text.Encoding]::UTF8.GetBytes($headerJson)
    $payloadBytes = [System.Text.Encoding]::UTF8.GetBytes($payloadJson)

    $headerBase64 = ConvertTo-Base64Url $headerBytes
    $payloadBase64 = ConvertTo-Base64Url $payloadBytes

    $unsignedToken = "$headerBase64.$payloadBase64"
    $secretBytes = [System.Text.Encoding]::UTF8.GetBytes($Secret)
    $hmac = New-Object System.Security.Cryptography.HMACSHA256 -ArgumentList (,$secretBytes)
    $unsignedBytes = [System.Text.Encoding]::UTF8.GetBytes($unsignedToken)
    $signatureBytes = $hmac.ComputeHash($unsignedBytes)
    $signatureBase64 = ConvertTo-Base64Url $signatureBytes

    return "$unsignedToken.$signatureBase64"
}

Write-Host "`n  Agrisense Key Generator (PowerShell)`n"

# Paths
$ScriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path (Join-Path $ScriptPath "../../")
$EnvFile = Join-Path $ProjectRoot ".env"

if (-not (Test-Path $EnvFile)) {
    Write-ErrorMsg "Environment file not found: $EnvFile"
    Write-ErrorMsg "Please create a .env file first (e.g., by copying .env.example)."
    exit 1
}

Write-Info "Generating fresh secrets..."

# 1. JWT Secret
$JwtSecret = New-RandomString 32

# 2. JWT Tokens (Anon and Service Role)
$iat = [DateTimeOffset]::Now.ToUnixTimeSeconds()
$exp = [DateTimeOffset]::Now.AddYears(10).ToUnixTimeSeconds()

$anonPayload = [PSCustomObject]@{
    role = "anon"
    iss = "supabase"
    iat = $iat
    exp = $exp
}

$serviceRolePayload = [PSCustomObject]@{
    role = "service_role"
    iss = "supabase"
    iat = $iat
    exp = $exp
}

$anonKey = New-SupabaseJwt -Secret $JwtSecret -Payload $anonPayload
$serviceRoleKey = New-SupabaseJwt -Secret $JwtSecret -Payload $serviceRolePayload

# 3. Other Secrets
$secretKeyBase = New-RandomHex 64
$vaultEncKey = New-RandomHex 32
$pgMetaCryptoKey = New-RandomString 32
$logflarePublic = New-RandomString 24
$logflarePrivate = New-RandomString 24
$s3AccessKeyId = New-RandomHex 24
$s3AccessKeySecret = New-RandomHex 24
$postgresPassword = New-RandomString 32
$dashboardPassword = New-RandomString 16

Write-Info "Updating $EnvFile..."

$content = Get-Content $EnvFile -Raw

# Replacements
$replacements = @{
    "JWT_SECRET=<generate-a-random-secret>" = "JWT_SECRET=$JwtSecret"
    "ANON_KEY=<generate-with-supabase-keygen>" = "ANON_KEY=$anonKey"
    "SERVICE_ROLE_KEY=<generate-with-supabase-keygen>" = "SERVICE_ROLE_KEY=$serviceRoleKey"
    "SECRET_KEY_BASE=<generate-a-random-base64-string>" = "SECRET_KEY_BASE=$secretKeyBase"
    "VAULT_ENC_KEY=<generate-a-random-hex-string>" = "VAULT_ENC_KEY=$vaultEncKey"
    "PG_META_CRYPTO_KEY=<generate-a-random-string>" = "PG_META_CRYPTO_KEY=$pgMetaCryptoKey"
    "LOGFLARE_PUBLIC_ACCESS_TOKEN=<generate-a-random-token>" = "LOGFLARE_PUBLIC_ACCESS_TOKEN=$logflarePublic"
    "LOGFLARE_PRIVATE_ACCESS_TOKEN=<generate-a-random-token>" = "LOGFLARE_PRIVATE_ACCESS_TOKEN=$logflarePrivate"
    "S3_PROTOCOL_ACCESS_KEY_ID=<generate-a-random-hex-string>" = "S3_PROTOCOL_ACCESS_KEY_ID=$s3AccessKeyId"
    "S3_PROTOCOL_ACCESS_KEY_SECRET=<generate-a-random-hex-string>" = "S3_PROTOCOL_ACCESS_KEY_SECRET=$s3AccessKeySecret"
    "POSTGRES_PASSWORD=<generate-a-strong-password>" = "POSTGRES_PASSWORD=$postgresPassword"
    "DASHBOARD_PASSWORD=<your-dashboard-password>" = "DASHBOARD_PASSWORD=$dashboardPassword"
    "MINIO_ROOT_PASSWORD=<generate-a-strong-password>" = "MINIO_ROOT_PASSWORD=$postgresPassword"
}

foreach ($key in $replacements.Keys) {
    if ($content -match [regex]::Escape($key)) {
        $content = $content -replace [regex]::Escape($key), $replacements[$key]
        Write-Success "Updated: $($key.Split('=')[0])"
    } else {
        Write-Warn "Not found in .env: $($key.Split('=')[0]) (already updated or placeholder changed?)"
    }
}

$content | Set-Content $EnvFile -NoNewline

Write-Host "`n"
Write-Success "All keys generated and .env updated successfully!"
Write-Host "Remember to set your DASHBOARD_USERNAME in .env manually.`n"
