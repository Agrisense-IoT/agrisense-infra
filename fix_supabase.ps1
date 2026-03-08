$baseUrl = 'https://raw.githubusercontent.com/supabase/supabase/master/docker/volumes/'
$baseDir = 'c:\Users\Administrator\Development\Agrisense\agrisense-infra\supabase\volumes\'

$filesToRestore = @(
    'db/_supabase.sql',
    'db/jwt.sql',
    'db/logs.sql',
    'db/pooler.sql',
    'db/realtime.sql',
    'db/roles.sql',
    'db/webhooks.sql',
    'api/kong.yml',
    'pooler/pooler.exs'
)

# Shutdown containers so docker releases file locks
docker compose -f docker-compose.yml -f docker-compose.dev.yml down

foreach ($file in $filesToRestore) {
    $fullPath = Join-Path $baseDir $file.Replace('/', '\')
    if (Test-Path $fullPath -PathType Container) {
        Remove-Item -Path $fullPath -Force -Recurse
    }
    $parentDir = Split-Path $fullPath -Parent
    if (-not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir | Out-Null
    }
    $url = $baseUrl + $file
    Invoke-WebRequest -Uri $url -OutFile $fullPath
}

# Wipe database initialization to trigger recreate
Remove-Item -Recurse -Force (Join-Path $baseDir 'db\data\*') -ErrorAction SilentlyContinue

# Bring it back up
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
