package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 缩略图后缀按字节魔数决定：前端已换成 JPEG，历史素材仍是 PNG，
// 认不出来时退回 PNG（改动前的行为），不能因为嗅探失败丢缩略图。
func TestSaveStudioModelPicksThumbExtensionByMagicBytes(t *testing.T) {
	svc, _, _ := proxyTestService(t)
	cases := []struct {
		name   string
		thumb  []byte
		suffix string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F'}, "_thumb.jpg"},
		{"png", []byte("\x89PNG\r\n\x1a\n-thumb"), "_thumb.png"},
		{"unknown", []byte("not-an-image"), "_thumb.png"},
	}
	for i, c := range cases {
		model := []string{"scene-a.glb", "scene-b.glb", "scene-c.glb"}[i]
		mat, err := svc.Materials.SaveStudioModel(nil, model, []byte("glTF-binary"), c.thumb)
		if err != nil {
			t.Fatalf("%s: 保存失败：%v", c.name, err)
		}
		want := strings.TrimSuffix(model, filepath.Ext(model)) + c.suffix
		if !strings.HasSuffix(mat.ThumbPath, want) {
			t.Fatalf("%s: 缩略图名应为 %q，实际 %q", c.name, want, mat.ThumbPath)
		}
		// 记录里写的路径必须真有文件，否则素材页会显示裂图
		if _, err := os.Stat(filepath.Join(svc.Upload.InputDir(), mat.ThumbPath)); err != nil {
			t.Fatalf("%s: 缩略图文件没落盘：%v", c.name, err)
		}
	}
}

// 没缩略图时不该凭空造路径。
func TestSaveStudioModelWithoutThumbKeepsThumbPathEmpty(t *testing.T) {
	svc, _, _ := proxyTestService(t)
	mat, err := svc.Materials.SaveStudioModel(nil, "scene-d.glb", []byte("glTF-binary"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if mat.ThumbPath != "" {
		t.Fatalf("没有缩略图就不该写路径，实际 %q", mat.ThumbPath)
	}
}