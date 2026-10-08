import json

from srt import parse_structured_srt, preview_report


def test_structured_srt_supports_roles_emotion_and_bilingual_metadata():
    source = """1
00:00:00,250 --> 00:00:02,500
[voice-studio role=dialogue character=林夏 voice=warm speed=1.15 emotion=悲伤 delivery=克制 language=zh-CN seed=42]
第一行
第二行

2
00:00:02.000 --> 00:00:04.250
{\"role\":\"offscreen_dialogue\",\"角色\":\"阿宁\",\"感情\":\"焦急\",\"种子\":7}
门外有人吗？
"""
    cues, diagnostics = parse_structured_srt(source)
    assert len(cues) == 2
    assert cues[0].text == "第一行\n第二行"
    assert cues[0].emotion == "悲伤" and cues[0].delivery == "克制" and cues[0].seed == 42
    assert cues[1].role == "offscreen_dialogue" and cues[1].character == "阿宁"
    assert any(item["code"] == "overlap" for item in diagnostics)


def test_srt_rejects_unknown_role_and_missing_character():
    source = """1
00:00:00,000 --> 00:00:01,000
[role=dialogue]
你好

2
00:00:01,000 --> 00:00:02,000
[role=unsupported character=甲]
测试
"""
    report = preview_report(source)
    assert report["valid"] is False
    assert report["cue_count"] == 0
    assert {item["code"] for item in report["diagnostics"]} == {"character", "role"}


def test_srt_narration_defaults_to_narrator_and_is_read_only_preview():
    report = preview_report("""1
00:00:00.000 --> 00:00:01.500
[role=narration emotion=庄重]
夜色笼罩山城。
""")
    assert report["valid"] is True
    assert report["dialogue_authority"] == "external-read-only-preview"
    assert report["cues"][0]["character"] == "旁白"
    assert json.dumps(report, ensure_ascii=False)
