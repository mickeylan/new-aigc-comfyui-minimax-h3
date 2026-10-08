# ComfyUI-IndexTTS25-Rust

Production-oriented ComfyUI nodes for the optional native `index-tts-rust` C ABI.
They are intentionally separate from `ComfyUI-IndexTTS25-Local`, which remains the Python experimentation/ASR plugin.

## Phase 1 nodes

- `IndexTTS25RustRuntimeStatus`: probes the DLL and reports capabilities without loading a model.
- `IndexTTS25RustModelLoader`: lazily loads one CPU/CUDA model and returns its immutable runtime/model fingerprint.
- `IndexTTS25RustPrepareVoice`: prepares and caches a reference voice by stable voice key plus audio hash.
- `IndexTTS25RustGenerate`: produces standard ComfyUI `AUDIO` and an auditable JSON report, including native emotion-text controls when supported.

The module itself never loads the DLL. A missing DLL, CUDA runtime, model, or voice reference fails only the executing node and does not prevent ComfyUI startup.

## Installation

Copy this directory to `ComfyUI/custom_nodes/ComfyUI-IndexTTS25-Rust`.
Place the pinned release `indextts.dll` and its CUDA/ONNX dependencies beside the plugin, on `PATH`, or set:

```powershell
$env:INDEXTTS_DLL = 'C:\path\to\indextts.dll'
$env:INDEXTTS25_RUST_MODEL_DIR = 'E:\models\indextts25-rust'
```

Do not point this plugin at an arbitrary ABI build. The loader reports ABI, runtime version, model manifest SHA-256, backend and device in `runtime_json`.

## Production boundaries

- Dialogue text and speaker identity remain authoritative in the Go backend.
- The plugin does not rewrite text, guess speakers, select fallback voices, call cloud services, or download models.
- `emotion_text` is passed only when the loaded ABI reports native emotion-text support.
- `duration_factor` is a model duration condition, not the application's FFmpeg playback-speed control.
- Outputs are candidates until the Go backend validates token/hash and the user approves them.

## Next phases

Phase 2 connects these nodes to the existing Go `TaskService` for multi-ComfyUI scheduling and audio result recovery. Phase 3 adds structured character batches/SRT preview reports. Phase 4 adds optional ASR and quality diagnostics.
