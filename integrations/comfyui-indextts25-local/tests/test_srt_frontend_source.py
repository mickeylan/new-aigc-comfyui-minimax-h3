from pathlib import Path


SOURCE = (Path(__file__).parents[1] / "web" / "js" / "srt_ui.js").read_text(encoding="utf-8")
INIT = (Path(__file__).parents[1] / "__init__.py").read_text(encoding="utf-8")


def test_srt_frontend_registers_file_picker_and_local_summary():
    assert 'WEB_DIRECTORY = "./web"' in INIT
    assert "选择 SRT 文件" in SOURCE
    assert 'accept=".srt,application/x-subrip,text/plain"' in SOURCE
    assert "file.text()" in SOURCE
    assert "仅本地读取" in SOURCE
    assert "字幕 ${info.count} 条" in SOURCE
    assert "角色 ${info.roles.length} 个" in SOURCE


def test_srt_frontend_warns_for_multiple_roles_without_guessing_voice():
    assert "多角色字幕：请为每个角色复制一个单角色生成节点" in SOURCE
    assert 'setWidget(node,"character",info.roles[0])' in SOURCE
    assert "info.roles.length===1" in SOURCE
    assert "file.size>2*1024*1024" in SOURCE


def test_srt_frontend_has_full_local_editor_and_serializes_back_to_node():
    for label in ["新建 / 编辑 SRT", "＋ 新建字幕", "按时间排序", "重新编号", "导入 SRT", "导出 SRT", "保存到节点"]:
        assert label in SOURCE
    for field in ["start", "end", "character", "text", "voice", "emotion", "delivery", "speed", "language", "seed"]:
        assert f'data-field="{field}"' in SOURCE
    assert 'setWidget(node,"srt_text",text)' in SOURCE
    assert 'setWidget(node,"srt_path","")' in SOURCE
    assert "URL.createObjectURL" in SOURCE
    assert "end<=cue.start" in SOURCE
    assert "处重叠（允许混音）" in SOURCE
