from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_phase_one_nodes_are_separate_from_python_plugin():
    source = (ROOT / "nodes.py").read_text(encoding="utf-8")
    for name in [
        "IndexTTS25RustRuntimeStatus",
        "IndexTTS25RustModelLoader",
        "IndexTTS25RustPrepareVoice",
        "IndexTTS25RustGenerate",
    ]:
        assert name in source
    assert "INDEXTTS25_RUST_MODEL" in source
    assert "INDEXTTS25_RUST_VOICE" in source
    assert "emotion_text" in source
    assert "generation_json" in source


def test_runtime_is_lazy_and_uses_rust_abi():
    source = (ROOT / "runtime.py").read_text(encoding="utf-8")
    assert "C.CDLL" in source
    assert "indextts_voice_prepare_pcm" in source
    assert "indextts_generate_result_request_v2" in source
    assert "supports_emotion_text" in source
    assert "RUNTIME_CACHE = RuntimeCache()" in source
    assert "NativeLibrary(path)" not in source.split("RUNTIME_CACHE = RuntimeCache()")[1]


def test_plugin_does_not_import_old_python_indextts_stack():
    combined = "\n".join((ROOT / name).read_text(encoding="utf-8") for name in ["__init__.py", "nodes.py", "runtime.py"])
    assert "infer_v2_5" not in combined
    assert "transformers" not in combined
    assert "modelscope" not in combined
