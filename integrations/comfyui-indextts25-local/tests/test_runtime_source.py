from pathlib import Path


def test_runtime_patches_the_bigvgan_class_used_by_inference():
    source = (Path(__file__).parents[1] / "runtime.py").read_text(encoding="utf-8")
    assert "from indextts.infer_v2_5 import IndexTTS2, bigvgan" in source
    assert "install_bigvgan_compat(bigvgan.BigVGAN)" in source
    assert "install_gpt_generation_compat(model_v2)" not in source
    assert "coordinated IndexTTS compatibility stack" in source
    assert "from indextts.BigVGAN.bigvgan import BigVGAN" not in source
