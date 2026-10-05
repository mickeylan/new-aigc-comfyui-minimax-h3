# IndexTTS Transformers 5 compatibility stack

This directory contains coordinated generation/GPT compatibility files adapted from:

- IndexTTS: https://github.com/index-tts/index-tts
- comfyui-indextts25-t8: https://github.com/T8mars/comfyui-indextts25-t8
- Hugging Face Transformers 4.52.1 beam utilities

The Hugging Face-derived Python sources retain their upstream copyright and Apache License 2.0 headers. The files are loaded only inside the IndexTTS custom-node path so the host ComfyUI installation can retain Transformers 5.x.
