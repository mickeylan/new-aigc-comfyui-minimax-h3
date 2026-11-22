package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
)

// TestScene3DRefVideoWiring 场景选了 3D 参考视频时：模板走 ref2v_3d，文件进 ref_videos。
func TestScene3DRefVideoWiring(t *testing.T) {
	if !isH3Ref2VTemplate(h3Ref2V3DTemplate) {
		t.Fatalf("%s must be treated as an H3 Ref2V template", h3Ref2V3DTemplate)
	}
	// 没选视频：不能凭空造出 ref_videos（模板里那两个节点会被裁掉）。
	empty := &models.Scene{}
	if scene3DRefVideo(empty) != "" || scene3DRefVideoFiles(empty) != nil {
		t.Fatal("scene without 3D reference video must not produce ref_videos files")
	}
	if scene3DRefVideo(nil) != "" {
		t.Fatal("nil scene must be safe")
	}

	scene := &models.Scene{Video3DRefVideo: "  clay-seq-20261122-174038.mp4  "}
	if got := scene3DRefVideo(scene); got != "clay-seq-20261122-174038.mp4" {
		t.Fatalf("scene3DRefVideo = %q", got)
	}
	files := scene3DRefVideoFiles(scene)
	refs := files["ref_videos"]
	if len(refs) != 1 || refs[0].TaskID != "materials" || refs[0].Name != "clay-seq-20261122-174038.mp4" {
		t.Fatalf("ref_videos files = %+v", files)
	}
}

// TestH3VideoSelectionAccepts3DTemplate 新模板要能通过模型目录的能力校验（不能一提交就被挡）。
func TestH3VideoSelectionAccepts3DTemplate(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v_3d.json")
	if !isH3Ref2VTemplate(tpl.Code) {
		t.Fatalf("template code %q is not routed as Ref2V", tpl.Code)
	}
	// 模板声明的输入里必须有 videos 类型的 ref_videos，并且允许不传（否则没 3D 视频时会被挡）。
	var declared []TemplateInput
	if err := json.Unmarshal([]byte(tpl.InputsJSON), &declared); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, input := range declared {
		if input.Key != "ref_videos" {
			continue
		}
		found = true
		if input.Type != "videos" {
			t.Fatalf("ref_videos type = %q, want videos", input.Type)
		}
		if input.Required {
			t.Fatal("ref_videos must stay optional so scenes without a 3D reference still submit")
		}
	}
	if !found {
		t.Fatal("ref_videos input missing from template")
	}
}