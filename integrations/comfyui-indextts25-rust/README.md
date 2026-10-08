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

## Structured SRT and character batches

- `IndexTTS25RustParseSRT` accepts BOM, CRLF/LF, comma/dot milliseconds, multiline text, JSON metadata and bracket key/value metadata. It supports `dialogue`, `offscreen_dialogue`, `narration` and `monologue` and emits a read-only diagnostic snapshot.
- `IndexTTS25RustCharacterBatch` consumes reviewed segment JSON for exactly one character/prepared voice, preserves overlaps on a timeline, reports per-cue overflow, and never truncates or rewrites text.
- SRT `speed` is reported as the application/FFmpeg playback condition. It is deliberately not passed as IndexTTS `duration_factor`.

Phase 2 connects single-dialogue generation to the existing Go `TaskService`, filters instances by `/object_info`, persists task/CAS bindings and recovers `audio`/`audios` results into `DialogueAudioCandidate`.

`IndexTTS25RustASRReview` is optional and local-only. Missing Whisper or checkpoint files produce an explicit `ASR unavailable` error without affecting synthesis. ASR reports edit distance/error rate and never rewrites authoritative Dialogue. Native generation and character-batch reports also expose semantic tokens, stage timings, peak/RMS/silence ratio, slot overflow and stable seed provenance.
