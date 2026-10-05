from pathlib import Path


def test_generate_preserves_model_defaults_and_rejects_short_audio():
    source = (Path(__file__).parents[1] / "nodes.py").read_text(encoding="utf-8")
    execute = source[source.index("class IndexTTS25LocalGenerate") : source.index("class IndexTTS25LocalTimelineMix")]
    assert "min_new_tokens" not in execute
    assert "num_beams=" not in execute
    assert "result_to_audio(result, minimum_seconds=minimum_seconds)" in execute
