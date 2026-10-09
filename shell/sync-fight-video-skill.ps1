param(
    [string]$SourceDir = "E:\mickeylan\ai\fight-video-create-skill",
    [switch]$SkipSourceTests
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$source = [IO.Path]::GetFullPath($SourceDir)
$sourcePlain = Join-Path $source "data\plain"
$sourceSkill = Join-Path $source "SKILL.md"
$destination = Join-Path $repo "backend\internal\service\combat_reference_data"
$destinationPlain = Join-Path $destination "plain"

if (-not (Test-Path -LiteralPath $sourcePlain -PathType Container)) { throw "Missing fight skill data: $sourcePlain" }
if (-not (Test-Path -LiteralPath $sourceSkill -PathType Leaf)) { throw "Missing fight skill contract: $sourceSkill" }

if (-not $SkipSourceTests) {
    Push-Location $source
    try {
        & python -X utf8 -m unittest discover -s tests -p "test_*.py" -v
        if ($LASTEXITCODE -ne 0) { throw "fight-video-create-skill tests failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

$staging = Join-Path ([IO.Path]::GetTempPath()) ("fight-video-skill-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $staging -Force | Out-Null
try {
    Copy-Item -LiteralPath $sourcePlain -Destination (Join-Path $staging "plain") -Recurse -Force
    Copy-Item -LiteralPath $sourceSkill -Destination (Join-Path $staging "SKILL.md") -Force

    # The embedded copy is canonical UTF-8/LF with no trailing spaces, so updates
    # remain reviewable across Windows and Linux checkouts.
    Get-ChildItem -LiteralPath $staging -Recurse -File | ForEach-Object {
        $text = [IO.File]::ReadAllText($_.FullName)
        $lines = $text.Replace("`r`n", "`n").Replace("`r", "`n").Split("`n") | ForEach-Object { $_.TrimEnd() }
        $normalized = ($lines -join "`n").TrimEnd() + "`n"
        [IO.File]::WriteAllText($_.FullName, $normalized, [Text.UTF8Encoding]::new($false))
    }

    $scopes = @("scenes", "design", "moves", "skills", "scripts")
    $counts = [ordered]@{}
    foreach ($scope in $scopes) {
        $indexPath = Join-Path $staging "plain\$scope\_index.json"
        if (-not (Test-Path -LiteralPath $indexPath)) { throw "Missing index: $indexPath" }
        $index = Get-Content -LiteralPath $indexPath -Raw -Encoding UTF8 | ConvertFrom-Json
        $counts[$scope] = @($index.available).Count
        foreach ($item in @($index.available)) {
            $id = [string]$item.id
            $meta = Join-Path $staging "plain\$scope\$id.meta.json"
            $md = Join-Path $staging "plain\$scope\$id.md"
            $txt = Join-Path $staging "plain\$scope\$id.txt"
            if (-not (Test-Path -LiteralPath $meta)) { throw "Missing metadata: $scope/$id" }
            if (-not (Test-Path -LiteralPath $md) -and -not (Test-Path -LiteralPath $txt)) { throw "Missing body: $scope/$id" }
        }
    }

    $commit = (& git -C $source rev-parse HEAD 2>$null)
    if ($LASTEXITCODE -ne 0) { $commit = "unknown" }
    $dirty = @(& git -C $source status --short --untracked-files=no 2>$null).Count -gt 0
    $files = Get-ChildItem -LiteralPath $staging -Recurse -File | Sort-Object FullName
    $aggregate = [Text.StringBuilder]::new()
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($staging.Length).TrimStart([char[]]@('\', '/')).Replace("\", "/")
        [void]$aggregate.Append($relative).Append(":").Append((Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()).Append("`n")
    }
    $bytes = [Text.Encoding]::UTF8.GetBytes($aggregate.ToString())
    $sha = [Security.Cryptography.SHA256]::Create()
    try { $hash = ([BitConverter]::ToString($sha.ComputeHash($bytes))).Replace("-", "").ToLowerInvariant() } finally { $sha.Dispose() }
    $manifest = [ordered]@{
        format = 1
        source = "fight-video-create-skill"
        source_commit = ([string]$commit).Trim()
        source_dirty = $dirty
        content_sha256 = $hash
        imported_files = $files.Count
        scopes = $counts
    }
    $manifestJSON = ($manifest | ConvertTo-Json -Depth 5).Replace("`r`n", "`n").TrimEnd() + "`n"
    [IO.File]::WriteAllText((Join-Path $staging "source-manifest.json"), $manifestJSON, [Text.UTF8Encoding]::new($false))

    if (Test-Path -LiteralPath $destination) { Remove-Item -LiteralPath $destination -Recurse -Force }
    New-Item -ItemType Directory -Path $destination -Force | Out-Null
    Copy-Item -Path (Join-Path $staging "*") -Destination $destination -Recurse -Force
    Write-Host "Synchronized fight skill commit $($manifest.source_commit), files=$($manifest.imported_files), content_sha256=$hash"
} finally {
    Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue
}
