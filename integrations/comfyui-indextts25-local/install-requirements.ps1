param(
    [Parameter(Mandatory = $true)]
    [string]$Python
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$requirements = Join-Path $root "requirements.txt"
$audioTools = Join-Path $root "requirements-audiotools.txt"

if (-not (Test-Path $Python)) {
    throw "ComfyUI Python does not exist: $Python"
}

Write-Host "Installing IndexTTS dependencies into: $Python"
& $Python -m pip install -r $requirements
if ($LASTEXITCODE -ne 0) { throw "requirements.txt installation failed" }

# This package has obsolete protobuf<3.20 metadata. Never let pip resolve it.
& $Python -m pip install --no-deps -r $audioTools
if ($LASTEXITCODE -ne 0) { throw "AudioTools installation failed" }

$verification = Join-Path $root "verify_requirements.py"
& $Python $verification
if ($LASTEXITCODE -ne 0) { throw "IndexTTS dependency verification failed" }

Write-Host "IndexTTS dependencies installed successfully. Restart ComfyUI before generating audio."
