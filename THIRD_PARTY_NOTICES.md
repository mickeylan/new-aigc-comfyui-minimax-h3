# Third-Party Notices

## Alibaba LumenX

Portions of the director-workflow prompt design in this repository were independently adapted from concepts in Alibaba LumenX:

- Source: https://github.com/alibaba/lumenx
- Local reference reviewed: `E:\mickeylan\ai\lumenx`
- License: MIT License
- Copyright: Copyright (c) 2026 Alibaba

The adapted concepts include visual-beat shot decomposition, structured director shot packets, faithful minimal-delta prompt polishing, generation candidate review, and staged audio-production ideas. This repository does not copy or depend on the LumenX React/Next.js, Python/FastAPI, Tauri, canvas, or editor implementation.

MIT License notice:

> Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, subject to inclusion of the copyright and permission notice in substantial copies.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## T8 Directional Combat Skills

Portions of the MiniMax H3 combat-direction prompt design were adapted from the local T8 directional skills `high_density_combat`, `continuous_combat`, and `ning_wenwu`:

- Local source reviewed: `E:\mickeylan\ai\comfyui-minimax-h3-prompt-enhancer-T8\directional_skills`
- License: MIT License
- Copyright: Copyright (c) 2026 Terry Jia

The adapted concepts include per-participant body/asset/ability tracking, readable attack-response-contact-displacement chains, continuity of momentum and possession, duration-bounded choreography, non-invented outcomes, and one motivated camera task per shot. The implementation is independently integrated into this repository's existing Go Skill system and Scene/Shot authority model.

## IndexTTS 2.5

The optional local ComfyUI integration under `integrations/comfyui-indextts25-local` calls the official IndexTTS 2.5 inference API and does not redistribute model weights.

- Source: https://github.com/index-tts/index-tts
- Local source reviewed: `E:\mickeylan\ai\index-tts`
- License: bilibili Model Use License Agreement
- Developer: Bilibili Index Team

The integration includes a copy of the model license and disclaimer. Users must obtain authorization for cloned voices and comply with the model license and applicable law. The node implementation is a clean, minimal integration; the separate `comfyui-indextts25-t8` repository was inspected only as a behavioral reference and its extended workflow implementation was not copied.
