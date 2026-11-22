package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestH3Ref2V3DTemplateWiresVideoReference 守住新模板的两件事：
// ① 传了 3D 参考视频时，LoadVideo → GetVideoComponents → ref_videos.ref_video_0 接上；
// ② 没传视频时，这两个节点连同引用一起消失，工作流退回普通 Ref2V（不能留孤儿节点）。
func TestH3Ref2V3DTemplateWiresVideoReference(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v_3d.json")

	t.Run("with reference video", func(t *testing.T) {
		params := map[string]any{"prompt": "p", "_task_id": "task-a"}
		files := map[string][]FileMeta{
			"ref_images": {{TaskID: "1", Name: "a.png"}, {TaskID: "1", Name: "b.png"}},
			"ref_videos": {{TaskID: "materials", Name: "clay-seq-20261122-174038.mp4"}},
		}
		if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
			t.Fatal(err)
		}
		rendered, err := (&TaskService{}).RenderWorkflow(&tpl, params)
		if err != nil {
			t.Fatal(err)
		}
		load, ok := rendered["9001"].(map[string]any)
		if !ok {
			t.Fatalf("video loader node missing: %v", keysOf(rendered))
		}
		t.Logf("rendered nodes: %v", keysOf(rendered))
		inputs := load["inputs"].(map[string]any)
		if inputs["file"] != "materials/clay-seq-20261122-174038.mp4" {
			t.Fatalf("loader file = %v", inputs["file"])
		}
		components := rendered["9002"].(map[string]any)["inputs"].(map[string]any)
		if link, _ := components["video"].([]any); len(link) != 2 || link[0] != "9001" {
			t.Fatalf("components must read the loader output: %v", components["video"])
		}
		r2v := rendered["416"].(map[string]any)["inputs"].(map[string]any)
		link, _ := r2v["ref_videos.ref_video_0"].([]any)
		if len(link) != 2 || link[0] != "9002" {
			t.Fatalf("ref_videos.ref_video_0 must point at the frame extractor: %v", r2v["ref_videos.ref_video_0"])
		}
		// 参考图通路不受影响
		if _, ok := r2v["ref_images.ref_image_1"]; !ok {
			t.Fatalf("ref_images wiring was disturbed: %v", r2v)
		}
	})

	t.Run("without reference video", func(t *testing.T) {
		params := map[string]any{"prompt": "p", "_task_id": "task-b"}
		files := map[string][]FileMeta{
			"ref_images": {{TaskID: "1", Name: "a.png"}},
		}
		if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
			t.Fatal(err)
		}
		rendered, err := (&TaskService{}).RenderWorkflow(&tpl, params)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := rendered["9001"]; ok {
			t.Fatal("video loader must be pruned when no reference video is supplied")
		}
		// 级联：只摘掉 9001 的引用会留下缺必填 video 的孤儿节点，ComfyUI 校验会失败。
		if _, ok := rendered["9002"]; ok {
			t.Fatal("frame extractor must be pruned together with its loader")
		}
		r2v := rendered["416"].(map[string]any)["inputs"].(map[string]any)
		for key := range r2v {
			if strings.HasPrefix(key, "ref_videos") {
				t.Fatalf("dangling ref_videos input left behind: %s", key)
			}
		}
		if _, ok := r2v["ref_images.ref_image_0"]; !ok {
			t.Fatalf("ref_images wiring was disturbed: %v", r2v)
		}
		// 模板里不该残留未替换的占位符
		raw, err := json.Marshal(rendered)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "{{") {
			t.Fatalf("unresolved placeholder in rendered workflow: %s", string(raw))
		}
	})
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}