# ComfyUI IndexTTS 2.5 Local

A deliberately small, offline-only ComfyUI integration for the official IndexTTS-2.5 runtime.

## Nodes

- `IndexTTS25LocalModelLoader`: validates local source and model directories and creates a lazy model handle.
- `IndexTTS25LocalGenerate`: accepts ComfyUI `AUDIO`, text, language, duration factor and seed; returns standard ComfyUI `AUDIO`.
- `IndexTTS25LocalTimelineMix`: places the next generated line at an explicit second offset; chain it to assemble a reviewed dialogue timeline.
- `IndexTTS25LocalASRReview`: uses an existing local Whisper `.pt` checkpoint to compare generated speech with authoritative text. It reports differences and never rewrites the text.

The extension never downloads models, checks for updates, calls cloud APIs, rewrites dialogue, or sends telemetry.

## Install

Copy this directory to:

```text
ComfyUI/custom_nodes/ComfyUI-IndexTTS25-Local
```

Install `requirements.txt` into the same Python environment used by ComfyUI.

Set these environment variables before starting ComfyUI, or fill the loader inputs directly:

```text
INDEXTTS25_SOURCE_DIR=E:\mickeylan\ai\index-tts
INDEXTTS25_MODEL_DIR=E:\mickeylan\ai\index-tts\checkpoints
```

For a portable deployment, place official source and models in stable shared locations and update these variables. Model weights are not included in this repository.

## Basic workflow

Two workflow formats are included. The `indextts5_compat/` directory contains the coordinated IndexTTS generation, GPT2, cache, and beam compatibility stack for a host running Transformers 5.x; it does not replace or downgrade ComfyUI's installed Transformers.

- `examples/basic_dialogue_ui.json`: upload/select a custom reference voice with `LoadAudio`; load this directly in the ComfyUI browser UI.
- `examples/preset_dialogue_ui.json`: choose a local preset voice or select one deterministically by seed; load this directly in the ComfyUI browser UI.
- `examples/basic_dialogue_api.json`: submit this object to ComfyUI's `/prompt` API; it is not a browser UI workflow.

```text
LoadAudio -> IndexTTS25LocalGenerate -> SaveAudio
              ^
IndexTTS25LocalModelLoader
```

After loading the UI workflow, select or upload a clean 10–15 second role reference voice in `LoadAudio`, edit the dialogue text, then queue the prompt.

The reference audio is mixed to mono, limited to the first 15 seconds, resampled to 22.05 kHz, and cached as a temporary PCM WAV. Inference is serialized per loaded model instance.

## ASR and timeline boundaries

ASR quality review and multi-line dialogue timeline assembly are intentionally separate from synthesis. ASR reports omissions/substitutions but never rewrites authoritative Dialogue text. The timeline mixer only places already-generated lines at explicit offsets and never becomes a second screenplay source.

`IndexTTS25LocalASRReview` requires an existing local OpenAI Whisper `.pt` checkpoint path. It does not download a model. `IndexTTS25LocalTimelineMix` mixes two clips at a time; chain nodes for additional lines, or continue using the application's existing FFmpeg dialogue timeline.

## Licensing and responsible use

IndexTTS source/model use is governed by the Bilibili Model Use License Agreement. Keep the supplied license and disclaimer, obtain permission for every cloned voice, and do not use the model for impersonation, fraud, privacy infringement or other unlawful purposes.
