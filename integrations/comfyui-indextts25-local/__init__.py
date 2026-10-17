"""Minimal local-only IndexTTS 2.5 nodes for ComfyUI."""

WEB_DIRECTORY = "./web"

if __package__:
    from .nodes import comfy_entrypoint

    __all__ = ["comfy_entrypoint", "WEB_DIRECTORY"]
else:  # Allows standalone runtime tests without importing ComfyUI.
    __all__ = []
