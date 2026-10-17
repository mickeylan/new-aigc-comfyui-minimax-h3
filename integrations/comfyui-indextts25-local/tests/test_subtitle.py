from pathlib import Path

import pytest

from subtitle import parse_structured_srt, read_srt_input


SRT = """\ufeff1
00:00:01,000 --> 00:00:03.500
[角色=上官若琳; 情绪=克制而欣喜; 语速=1.0; 语言=ZH; 种子=42]
姐姐，终于等到你了。

2
00:00:04.000 --> 00:00:05,500
{\"character\":\"舒寒\",\"delivery\":\"低声\"}
我回来了。
"""


def test_parse_structured_srt_preserves_text_timing_and_metadata():
    cues, diagnostics = parse_structured_srt(SRT)
    assert diagnostics == []
    assert len(cues) == 2
    assert cues[0].start == 1.0 and cues[0].end == 3.5
    assert cues[0].text == "姐姐，终于等到你了。"
    assert cues[0].character == "上官若琳"
    assert cues[0].emotion == "克制而欣喜"
    assert cues[0].language == "ZH" and cues[0].seed == 42
    assert cues[1].character == "舒寒" and cues[1].delivery == "低声"


def test_parse_structured_srt_reports_bad_blocks_without_rewriting_valid_cues():
    cues, diagnostics = parse_structured_srt(SRT + "\n\n3\ninvalid\n坏块")
    assert len(cues) == 2
    assert diagnostics == ["block 3: invalid timestamp"]


def test_read_srt_input_accepts_utf8_file_and_rejects_ambiguous_sources(tmp_path: Path):
    path = tmp_path / "dialogue.srt"
    path.write_text(SRT, encoding="utf-8-sig")
    assert "上官若琳" in read_srt_input("", str(path))
    with pytest.raises(ValueError, match="either"):
        read_srt_input(SRT, str(path))


def test_parse_structured_srt_rejects_empty_or_invalid_input():
    with pytest.raises(ValueError, match="required"):
        parse_structured_srt("")
    with pytest.raises(ValueError, match="no valid cues"):
        parse_structured_srt("1\nbad\ntext")
