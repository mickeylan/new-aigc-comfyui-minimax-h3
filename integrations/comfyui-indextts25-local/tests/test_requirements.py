from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_shared_requirements_are_additive_and_protect_protobuf():
    requirements = (ROOT / "requirements.txt").read_text(encoding="utf-8")
    for expected in (
        "protobuf>=5.29.6,<6",
        "openai-whisper>=20250625",
        "munch==4.0.0",
        "flatten-dict>=0.5,<1",
    ):
        assert expected in requirements
    active_lines = [line.strip() for line in requirements.splitlines() if line.strip() and not line.lstrip().startswith("#")]
    assert not any(line.startswith("descript-audiotools==") for line in active_lines)
    for host_package in ("numpy", "torch", "torchaudio", "transformers", "tokenizers", "huggingface-hub", "librosa", "scipy", "pandas", "matplotlib"):
        assert not any(line.lower().startswith(host_package + "==") for line in active_lines)
    assert "protobuf<3.20" in requirements


def test_audiotools_is_isolated_behind_no_deps_installer():
    audio = (ROOT / "requirements-audiotools.txt").read_text(encoding="utf-8")
    installer = (ROOT / "install-requirements.ps1").read_text(encoding="utf-8")
    verification = (ROOT / "verify_requirements.py").read_text(encoding="utf-8")
    assert "descript-audiotools==0.7.2" in audio
    assert "pip install --no-deps -r" in audio
    assert "-m pip install --no-deps -r $audioTools" in installer
    assert "verify_requirements.py" in installer
    assert "from google.protobuf.internal import builder" in verification
    assert "from google.protobuf import runtime_version" in verification
    assert '"protobuf": (5, 29, 6)' in verification
