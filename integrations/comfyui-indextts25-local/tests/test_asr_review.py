from asr_review import build_asr_report, canonical_text, edit_distance


def test_canonical_text_only_ignores_whitespace():
    assert canonical_text("姐 姐\n不会") == "姐姐不会"
    assert canonical_text("姐姐，不会") == "姐姐，不会"


def test_edit_distance_is_deterministic():
    assert edit_distance("姐姐不会", "姐姐不会") == 0
    assert edit_distance("姐姐不会", "姐姐会") == 1


def test_asr_report_is_advisory_and_does_not_rewrite_expected_text():
    report = build_asr_report("姐姐无论如何也不会让你去。", "姐姐无论如何也不会让你。", 0.05)
    assert report["passed"] is False
    assert report["expected"] == "姐姐无论如何也不会让你去。"
    assert report["transcript"] == "姐姐无论如何也不会让你。"
    assert report["edit_distance"] == 1


def test_asr_report_passes_exact_dialogue():
    report = build_asr_report("相信姐姐，", "相信姐姐，", 0.0)
    assert report["passed"] is True
    assert report["error_rate"] == 0
