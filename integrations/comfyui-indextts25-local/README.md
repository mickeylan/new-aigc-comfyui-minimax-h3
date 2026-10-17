# ComfyUI IndexTTS 2.5 Local

A deliberately small, offline-only ComfyUI integration for the official IndexTTS-2.5 runtime.

## Nodes

- `IndexTTS25LocalModelLoader`: validates local source and model directories and creates a lazy model handle.
- `IndexTTS25LocalGenerate`: accepts ComfyUI `AUDIO`, text, language, duration factor and seed; returns standard ComfyUI `AUDIO`.
- `IndexTTS25LocalSRTParse`: parses pasted UTF-8 SRT or a local `.srt` path into exact cue text, roles, timing, metadata and diagnostics without synthesis.
- `IndexTTS25LocalSRTSingleVoiceGenerate`: generates one explicitly selected role and places its cues at authoritative SRT timestamps. Multi-role SRTs require one node/reference voice per role.
- `IndexTTS25H3AudioWindow`: extracts a Scene/Shot interval from a longer reviewed track, optionally retaining warmup/cooldown context while returning the exact final source segment.
- `IndexTTS25H3ReviewedAudioPlan`: MIT helper around official H3 nodes. It derives the `17k+5` frame count from reviewed audio, blocks over-limit or silent audio instead of truncating Dialogue, adds the `<Audio 1>` contract idempotently, and emits quality/audit data. It does not replace official H3 conditioning or sampling.
- `IndexTTS25H3OutputTrim`: trims decoded IMAGE frames to the exact reviewed Dialogue interval after an aligned H3 render.
- `IndexTTS25H3AudioPolicy`: explicitly selects `source_only` (default), `generated_only`, or a conservative `mix`; it never silently replaces reviewed speech.
- `IndexTTS25LocalTimelineMix`: places the next generated line or role stem at an explicit second offset; chain it to assemble a reviewed dialogue timeline.
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
- `examples/srt_single_voice_ui.json`: parse and synthesize one selected role from pasted SRT or a local UTF-8 `.srt` file, preserving timestamps and writing an auditable per-cue report. The ComfyUI frontend panel provides a local file picker, example/clear actions, cue count, role list, time range, and multi-role guidance; browser-selected files are read locally and are not uploaded to a cloud service.

```text
LoadAudio -> IndexTTS25LocalGenerate -> SaveAudio
              ^
IndexTTS25LocalModelLoader
```

After loading the UI workflow, select or upload a clean 10–15 second role reference voice in `LoadAudio`, edit the dialogue text, then queue the prompt.

The reference audio is mixed to mono, limited to the first 15 seconds, resampled to 22.05 kHz, and cached as a temporary PCM WAV. Inference is serialized per loaded model instance.

## ASR and timeline boundaries

ASR quality review and multi-line dialogue timeline assembly are intentionally separate from authoritative text editing. ASR reports omissions/substitutions but never rewrites SRT text. The SRT generator places already parsed cues at explicit offsets and never becomes a second screenplay source.

Structured cue metadata supports `role`/`character`/`角色`, `voice`/`音色`, `speed`/`语速`, `emotion`/`感情`/`情绪`, `delivery`/`表达`, `language`/`语言`, and `seed`/`种子` in either a JSON metadata line or a bracketed key/value line. Cue text is synthesized verbatim. Generated speech longer than its subtitle slot is reported as `overflow` and is never truncated; enable `fail_on_overflow` to stop the workflow instead.

For multiple characters, use one `IndexTTS25LocalSRTSingleVoiceGenerate` node per exact character name and reference voice, then mix the returned role stems. The node refuses an unselected multi-role SRT rather than guessing a speaker or voice.

`IndexTTS25LocalASRReview` requires an existing local OpenAI Whisper `.pt` checkpoint path. It does not download a model. `IndexTTS25LocalTimelineMix` mixes two clips at a time; chain nodes for additional lines, or continue using the application's existing FFmpeg dialogue timeline.

## Licensing and responsible use

IndexTTS source/model use is governed by the Bilibili Model Use License Agreement. Keep the supplied license and disclaimer, obtain permission for every cloned voice, and do not use the model for impersonation, fraud, privacy infringement or other unlawful purposes.
