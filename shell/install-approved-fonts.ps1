param(
    [string]$DataDir = "dist\comfyui-console-win64-indextts\data"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$root = [IO.Path]::GetFullPath((Join-Path $repo $DataDir))
$fonts = Join-Path $root "fonts"
$licenses = Join-Path $fonts "licenses"
New-Item -ItemType Directory -Path $licenses -Force | Out-Null

# Pin both artifacts to a reviewed Google Fonts commit. Never use an OS font as
# the distributable source and never accept changed bytes silently.
$commit = "2894aab31764f10f29c421bdfd2340d3b382d384"
$fontURL = "https://raw.githubusercontent.com/google/fonts/$commit/ofl/notosanssc/NotoSansSC%5Bwght%5D.ttf"
$licenseURL = "https://raw.githubusercontent.com/google/fonts/$commit/ofl/notosanssc/OFL.txt"
$fontFile = Join-Path $fonts "NotoSansSC-wght.ttf"
$licenseFile = Join-Path $licenses "OFL-1.1.txt"
$expectedSHA256 = "a3041811a78c361b1de50f953c805e0244951c21c5bd412f7232ef0d899af0da"

Invoke-WebRequest -UseBasicParsing $fontURL -OutFile $fontFile
Invoke-WebRequest -UseBasicParsing $licenseURL -OutFile $licenseFile
$actualSHA256 = (Get-FileHash -LiteralPath $fontFile -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSHA256 -ne $expectedSHA256) {
    Remove-Item -LiteralPath $fontFile -Force -ErrorAction SilentlyContinue
    throw "Noto Sans SC SHA-256 mismatch: expected $expectedSHA256, got $actualSHA256"
}
$licenseText = Get-Content -LiteralPath $licenseFile -Raw
if ($licenseText -notmatch "SIL OPEN FONT LICENSE Version 1.1") {
    throw "Downloaded Noto Sans SC license is not SIL OFL 1.1"
}

$manifest = @(
    [ordered]@{
        code = "noto-sans-sc"
        display_name = "Noto Sans SC"
        family = "Noto Sans SC"
        file = "NotoSansSC-wght.ttf"
        sha256 = $actualSHA256
        version = "google-fonts-$commit"
        license_id = "OFL-1.1"
        license_file = "licenses/OFL-1.1.txt"
        source_url = "https://github.com/google/fonts/tree/$commit/ofl/notosanssc"
        commercial_use = $true
        redistribution = $true
        embedding = $true
        supports_chinese = $true
        supports_vertical = $true
        enabled = $true
    }
)
$json = ConvertTo-Json -InputObject $manifest -Depth 4
[IO.File]::WriteAllText((Join-Path $fonts "manifest.json"), $json, [Text.UTF8Encoding]::new($false))
Write-Host "Installed approved Noto Sans SC: $actualSHA256"
Write-Host "Manifest: $(Join-Path $fonts 'manifest.json')"
