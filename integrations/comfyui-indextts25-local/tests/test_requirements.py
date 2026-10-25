from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_shared_requirements_pin_verified_stack_without_audiotools():
    requirements = (ROOT / "requirements.txt").read_text(encoding="utf-8")
    for expected in (
        "numpy==1.26.4",
        "protobuf==5.29.6",
        "transformers==5.10.1",
        "tokenizers==0.22.2",
        "huggingface-hub==1.33.0",
        "openai-whisper==20250625",
        "munch==4.0.0",
        "flatten-dict==0.5.0",
    ):
        assert expected in requirements
    active_lines = [line.strip() for line in requirements.splitlines() if line.strip() and not line.lstrip().startswith("#")]
    assert not any(line.startswith("descript-audiotools==") for line in active_lines)
    assert "protobuf<3.20" in requirements


def test_audiotools_is_isolated_behind_no_deps_installer():
    audio = (ROOT / "requirements-audiotools.txt").read_text(encoding="utf-8")
    installer = (ROOT / "install-requirements.ps1").read_text(encoding="utf-8")
    assert "descript-audiotools==0.7.2" in audio
    assert "pip install --no-deps -r" in audio
    assert "-m pip install --no-deps -r $audioTools" in installer
    assert '"protobuf": "5.29.6"' in installer
    assert "from google.protobuf.internal import builder" in installer
    assert "from google.protobuf import runtime_version" in installer
