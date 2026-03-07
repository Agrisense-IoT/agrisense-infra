# start.ps1 - Agrisense full-stack launcher for PowerShell

param (
    [switch]$Build,
    [switch]$Down,
    [switch]$NoFrontend
)

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

$ScriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptPath

$EnvFile = Join-Path $ScriptPath ".env"

Write-Host " "
Write-Host "  Agrisense Full-Stack Launcher (PowerShell)"
Write-Host " "

$oldPreference = $ErrorActionPreference
$ErrorActionPreference = 'SilentlyContinue'
docker info > $null 2>&1
$dockerExitCode = $LASTEXITCODE
$ErrorActionPreference = $oldPreference

if ($dockerExitCode -eq 0) {
    Write-Success "Docker is running."
} else {
    Write-ErrorMsg "Docker is not running or not accessible. Please start Docker Desktop and try again."
    exit 1
}

if (-not (Test-Path $EnvFile)) {
    Write-Warn "Environment file not found: $EnvFile"
    Write-Warn "Using default configuration values."
} else {
    Write-Success "Environment file found: $EnvFile"
}

if (-not (Test-Path "..\agrisense-backend")) {
    Write-ErrorMsg "Backend repo not found at ..\agrisense-backend"
    Write-ErrorMsg "Clone it: git clone git@github.com:Agrisense-IoT/agrisense-backend.git ../agrisense-backend"
    exit 1
}

if (($NoFrontend.IsPresent -eq $false) -and (-not (Test-Path "..\agrisense-frontend"))) {
    Write-Warn "Frontend repo not found at ..\agrisense-frontend - starting without frontend."
    $NoFrontend = $true
}

$NetworkName = "agrisense_internal"
if (Test-Path $EnvFile) {
    $Lines = Get-Content $EnvFile
    foreach ($Line in $Lines) {
        if ($Line -match '^DOCKER_NETWORK_NAME=(.*)') {
            $NetworkName = $matches[1].Trim(" '`"")
            break
        }
    }
}

Write-Info "Docker network: $NetworkName"

$NetworkCheck = docker network ls --filter name="^$NetworkName$" --format "{{.Name}}"
if ($NetworkCheck -eq $NetworkName) {
    Write-Success "Network '$NetworkName' already exists."
} else {
    Write-Info "Creating Docker network '$NetworkName'..."
    docker network create $NetworkName
    Write-Success "Network '$NetworkName' created."
}

if ($Down) {
    Write-Info "Tearing down the stack..."
    docker compose down
    Write-Success "Stack stopped."
    exit 0
}

$ComposeArgs = @("up", "-d")
if ($NoFrontend) {
    $ComposeArgs += "--scale"
    $ComposeArgs += "frontend=0"
}

if ($Build) {
    Write-Info "Building images and starting stack..."
    $ComposeArgs += "--build"
} else {
    Write-Info "Starting stack (use -Build to force image rebuild)..."
}

docker compose @ComposeArgs

Write-Host " "
Write-Success "Stack is up!"
Write-Host " "

function Get-EnvVar {
    param([string]$Key, [string]$Default)
    $Lines = Get-Content $EnvFile
    foreach ($Line in $Lines) {
        if ($Line -match "^$Key=(.*)") {
            return $matches[1].Trim(" '`"")
        }
    }
    return $Default
}

$Port = Get-EnvVar "PORT" "3143"
$FrontendPort = Get-EnvVar "FRONTEND_PORT" "3000"
$SupabaseUrl = Get-EnvVar "SUPABASE_PUBLIC_URL" "http://localhost:8000"

Write-Host "  Frontend         http://localhost:$FrontendPort" -ForegroundColor Cyan
Write-Host "  Agrisense API    http://localhost:$Port" -ForegroundColor Cyan
Write-Host "  API Docs         http://localhost:$Port/api" -ForegroundColor Cyan
Write-Host "  Supabase Studio  $SupabaseUrl" -ForegroundColor Cyan
Write-Host "  MQTT Broker      localhost:1883" -ForegroundColor Cyan
Write-Host " "
Write-Host "  Tip: Run docker compose logs -f api to follow API logs." -ForegroundColor Cyan
Write-Host "  Tip: Run .\start.ps1 -Down to stop the stack." -ForegroundColor Cyan
Write-Host " "
