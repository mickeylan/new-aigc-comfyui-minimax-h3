package service

import (
	"fmt"
	"testing"
)

// The upstream AUTOGROW schema accepts 100 images, not three. Check the full
// rendering path: every supplied image has an ordered edge; absent nodes are pruned.
func TestH3ImageEditReferenceCapacity(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_image_edit.json")
	for _, count := range []int{1, 12, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			refs := make([]FileMeta, count)
			for i := range refs {
				refs[i] = FileMeta{TaskID: "1", Name: fmt.Sprintf("face_%d.png", i+1)}
			}
			params := map[string]any{"prompt": "design one new face", "_task_id": "capacity-test"}
			if err := normalizeTemplateFiles(&tpl, params, map[string][]FileMeta{"ref_images": refs}); err != nil {
				t.Fatal(err)
			}
			workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
			if err != nil {
				t.Fatal(err)
			}
			cond := workflow["10"].(map[string]any)["inputs"].(map[string]any)
			loadCount := 0
			for _, node := range workflow {
				if node.(map[string]any)["class_type"] == "LoadImage" {
					loadCount++
				}
			}
			if loadCount != count {
				t.Fatalf("LoadImage count=%d want=%d", loadCount, count)
			}
			for i := 1; i <= 100; i++ {
				edge, ok := cond[fmt.Sprintf("images.image_%d", i)]
				if i > count {
					if ok {
						t.Fatalf("dangling edge %d: %v", i, edge)
					}
					continue
				}
				if !ok {
					t.Fatalf("missing edge %d", i)
				}
				link := edge.([]any)
				node := workflow[fmt.Sprint(link[0])].(map[string]any)
				if got := node["inputs"].(map[string]any)["image"]; got != fmt.Sprintf("1/face_%d.png", i) {
					t.Fatalf("reference %d resolves to %v", i, got)
				}
			}
		})
	}
	refs := make([]FileMeta, 101)
	for i := range refs {
		refs[i] = FileMeta{TaskID: "1", Name: fmt.Sprintf("face_%d.png", i)}
	}
	if err := normalizeTemplateFiles(&tpl, map[string]any{}, map[string][]FileMeta{"ref_images": refs}); err == nil {
		t.Fatal("101 references must be rejected, not truncated")
	}
}
