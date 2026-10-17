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
    assert 'setWidget(node, "character", info.roles[0])' in SOURCE
    assert "info.roles.length === 1" in SOURCE
    assert "file.size > 2 * 1024 * 1024" in SOURCE
