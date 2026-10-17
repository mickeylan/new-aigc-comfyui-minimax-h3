"""Optional production IndexTTS 2.5 Rust/CUDA nodes for ComfyUI."""

if __package__:
    from . import routes as _routes
    from .nodes import comfy_entrypoint

    __all__ = ["comfy_entrypoint"]
else:
    __all__ = []
