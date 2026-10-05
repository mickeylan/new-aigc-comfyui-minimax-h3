"""Minimal local-only IndexTTS 2.5 nodes for ComfyUI."""

if __package__:
    from .nodes import comfy_entrypoint

    __all__ = ["comfy_entrypoint"]
else:  # Allows standalone runtime tests without importing ComfyUI.
    __all__ = []
