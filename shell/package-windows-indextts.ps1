param(
    [string]$RustRuntimeDir = "E:\mickeylan\ai\index-tts-rust\dist\index-tts-rust-win64-cuda",
    [string]$OutputDir = "dist\comfyui-console-win64-indextts",
    [string]$GoImportLibraryDir = "E:\mickeylan\ai\index-tts-rust\target\release",
    [switch]$Zip
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$backend = Join-Path $repo "backend"
$output = [IO.Path]::GetFullPath((Join-Path $repo $OutputDir))
$runtimeManifest = Join-Path $RustRuntimeDir "runtime-manifest.json"
$importLibrary = Join-Path $GoImportLibraryDir "libindextts.dll.a"

foreach ($required in @($runtimeManifest, $importLibrary)) {
    if (-not (Test-Path -LiteralPath $required)) { throw "Required IndexTTS runtime file not found: $required" }
}

$runtime = Get-Content -LiteralPath $runtimeManifest -Raw | ConvertFrom-Json
if ($runtime.abi_version -ne "1.5" -or $runtime.backend -ne "cuda") {
    throw "Expected IndexTTS CUDA ABI 1.5 runtime, found ABI $($runtime.abi_version) backend $($runtime.backend)"
}

# Deployment refresh must never remove production SQLite/configuration. Preserve them
# outside the output tree while replacing runtime binaries and libraries.
$preserve = Join-Path ([IO.Path]::GetTempPath()) ("comfyui-console-preserve-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $preserve -Force | Out-Null
try {
    if (Test-Path -LiteralPath $output) {
        foreach ($name in @("data", "config.yaml")) {
            $source = Join-Path $output $name
            if (Test-Path -LiteralPath $source) { Move-Item -LiteralPath $source -Destination $preserve -Force }
        }
        Remove-Item -LiteralPath $output -Recurse -Force
    }
    New-Item -ItemType Directory -Path $output -Force | Out-Null
    Copy-Item -Path (Join-Path $RustRuntimeDir "*") -Destination $output -Recurse -Force
    foreach ($name in @("data", "config.yaml")) {
        $saved = Join-Path $preserve $name
        if (Test-Path -LiteralPath $saved) { Move-Item -LiteralPath $saved -Destination $output -Force }
    }
} finally {
    New-Item -ItemType Directory -Path $output -Force | Out-Null
    foreach ($name in @("data", "config.yaml")) {
        $saved = Join-Path $preserve $name
        $destination = Join-Path $output $name
        if ((Test-Path -LiteralPath $saved) -and -not (Test-Path -LiteralPath $destination)) {
            Move-Item -LiteralPath $saved -Destination $output -Force
        }
    }
    Remove-Item -LiteralPath $preserve -Recurse -Force -ErrorAction SilentlyContinue
}

$oldCGO = $env:CGO_ENABLED
$oldFlags = $env:CGO_LDFLAGS
try {
    $env:CGO_ENABLED = "1"
    $env:CGO_LDFLAGS = "-L$($GoImportLibraryDir.Replace('\','/')) -lindextts"
    Push-Location $backend
    try {
        # Go's Windows external linker can emit malformed DWARF section RVAs when this
        # large embedded frontend is linked through MinGW. Release binaries must strip
        # DWARF so SizeOfImage and section layout remain valid for CreateProcess.
        go build -tags indextts -ldflags "-s -w" -o (Join-Path $output "comfyui-console.exe") .
        if ($LASTEXITCODE -ne 0) { throw "Go IndexTTS build failed with exit code $LASTEXITCODE" }
    } finally { Pop-Location }
} finally {
    $env:CGO_ENABLED = $oldCGO
    $env:CGO_LDFLAGS = $oldFlags
}

Copy-Item -LiteralPath (Join-Path $backend "config.yaml.example") -Destination (Join-Path $output "config.yaml.example") -Force
if (-not (Test-Path -LiteralPath (Join-Path $output "config.yaml"))) {
    Copy-Item -LiteralPath (Join-Path $backend "config.yaml.example") -Destination (Join-Path $output "config.yaml") -Force
}
$app = Get-Item -LiteralPath (Join-Path $output "comfyui-console.exe")
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class IndexTTSBinaryProbe {
    [DllImport("kernel32", SetLastError=true, CharSet=CharSet.Unicode)]
    public static extern bool GetBinaryTypeW(string path, out uint binaryType);
}
'@
$binaryType = 0
if (-not [IndexTTSBinaryProbe]::GetBinaryTypeW($app.FullName, [ref]$binaryType) -or $binaryType -ne 6) {
    $errorCode = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
    throw "Packaged application is not a loadable 64-bit Windows executable (type=$binaryType error=$errorCode)"
}
$summary = [ordered]@{
    package = "comfyui-console-win64-indextts"
    index_tts_abi = $runtime.abi_version
    index_tts_backend = $runtime.backend
    app_sha256 = (Get-FileHash -LiteralPath $app.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    app_bytes = $app.Length
    created_at = (Get-Date).ToString("o")
}
$summary | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output "application-manifest.json") -Encoding UTF8
Write-Host "Packaged application with IndexTTS CUDA runtime to $output"

if ($Zip) {
    $zipPath = "$output.zip"
    if (Test-Path -LiteralPath $zipPath) { Remove-Item -LiteralPath $zipPath -Force }
    Compress-Archive -Path "$output\*" -DestinationPath $zipPath -CompressionLevel Optimal
    Write-Host "Wrote $zipPath"
}
