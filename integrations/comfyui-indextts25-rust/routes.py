from __future__ import annotations

from aiohttp import web
from server import PromptServer

from .runtime import RUNTIME_CACHE, probe_runtime


@PromptServer.instance.routes.get("/indextts25-rust/health")
async def indextts25_rust_health(_request):
    runtime = probe_runtime("")
    snapshot = RUNTIME_CACHE.health_snapshot()
    healthy = bool(runtime.get("available")) and all(model.get("device_healthy", False) for model in snapshot["models"])
    return web.json_response({"available": bool(runtime.get("available")), "healthy": healthy, "runtime": runtime, **snapshot})
