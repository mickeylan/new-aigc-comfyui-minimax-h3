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

& $Python -c @'
import importlib.metadata as metadata
from google.protobuf.internal import builder
from google.protobuf import runtime_version
from whisper.tokenizer import Tokenizer
from munch import Munch
from flatten_dict import flatten
import audiotools

expected = {
    "numpy": "1.26.4",
    "protobuf": "5.29.6",
    "transformers": "5.10.1",
    "tokenizers": "0.22.2",
    "huggingface-hub": "1.33.0",
    "descript-audiotools": "0.7.2",
}
actual = {name: metadata.version(name) for name in expected}
bad = {name: (expected[name], value) for name, value in actual.items() if value != expected[name]}
if bad:
    raise SystemExit(f"IndexTTS dependency version mismatch: {bad}")
print("IndexTTS dependency imports and versions are valid:", actual)
'@
if ($LASTEXITCODE -ne 0) { throw "IndexTTS dependency verification failed" }

Write-Host "IndexTTS dependencies installed successfully. Restart ComfyUI before generating audio."
