from asr import build_asr_report, canonical_text, edit_distance


def test_asr_report_is_advisory_and_does_not_rewrite_expected_text():
    report = build_asr_report("相信姐姐。", "相信姐姐", 0.2)
    assert report["expected"] == "相信姐姐。"
    assert report["transcript"] == "相信姐姐"
    assert report["edit_distance"] == 1
    assert report["passed"] is True


def test_canonical_text_only_removes_whitespace():
    assert canonical_text("相 信\n姐姐。") == "相信姐姐。"
    assert edit_distance("甲", "乙") == 1
