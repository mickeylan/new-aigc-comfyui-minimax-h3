package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// TestH3Video1TagReachesSubmission 选了 3D 结构参考视频时，最终提交给 H3 的提示词里必须有 <Video 1>；
// 没选时必须一个字都不加（不能让普通 ref2v 也带上一条不存在的参考）。
func TestH3Video1TagReachesSubmission(t *testing.T) {
	for _, tc := range []struct {
		name     string
		refVideo string
		wantTag  bool
	}{
		{name: "with 3d reference", refVideo: "clay-seq-20261122-174919.mp4", wantTag: true},
		{name: "without 3d reference", refVideo: "", wantTag: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTranslationHTTPFixture(t)
			pool, err := f.db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			configureDefaultRef2VFixture(t, f)
			if err := f.db.AutoMigrate(&models.Task{}, &models.Setting{}, &models.Instance{}, &models.UploadFile{}); err != nil {
				t.Fatal(err)
			}
			// 模板行必须带真实 inputs/workflow：目录能力校验会解析它们（fixture 默认的 "{}" 会让普通 ref2v 判无效）。
			for _, file := range []string{"minimax_h3_ref2v.json", "minimax_h3_ref2v_3d.json"} {
				tpl := loadTemplateForTest(t, file)
				if err := f.db.Where("code = ?", tpl.Code).Delete(&models.Template{}).Error; err != nil {
					t.Fatal(err)
				}
				if err := f.db.Create(&models.Template{Name: tpl.Name, Code: tpl.Code, WorkflowJSON: tpl.WorkflowJSON, InputsJSON: tpl.InputsJSON, Enabled: true}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"aqing.png", "teahouse.png", "start.png"} {
				if err := f.db.Create(&models.UploadFile{TaskID: "1", Type: "image", Name: name, Path: "1/" + name}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if tc.refVideo != "" {
				if err := f.db.Create(&models.UploadFile{TaskID: "materials", Type: "video", Name: tc.refVideo, Path: "materials/" + tc.refVideo}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{
				"duration": 10, "video_full_prompt": bilingualRef2VEnglish,
				"video_3d_ref_video": tc.refVideo, "status": "image_ready",
			}).Error; err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{}
			f.svc.Projects.tasks = NewTaskService(cfg, f.db, NewInstanceManager(cfg, f.db, nil), nil, NewHub(), nil)

			var sc models.Scene
			if err := f.db.First(&sc, f.sc.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.svc.Projects.GenerateSceneVideo(&f.p, &sc); err != nil {
				t.Fatalf("submit failed: %v", err)
			}
			var task models.Task
			if err := f.db.Where("task_id = ?", sceneVideoTaskID(t, f.db, sc.ID)).First(&task).Error; err != nil {
				t.Fatal(err)
			}
			hasTag := strings.Contains(task.Prompt, "<Video 1>")
			if hasTag != tc.wantTag {
				t.Fatalf("prompt <Video 1> presence = %v, want %v\nprompt:\n%s", hasTag, tc.wantTag, task.Prompt)
			}
			if tc.wantTag {
				if !strings.Contains(task.Prompt, "<Video 1> (appears in [Shot 1]): structure_only") {
					t.Fatalf("retention line missing:\n%s", task.Prompt)
				}
				if !strings.Contains(task.InputsJSON, "ref_videos") || !strings.Contains(task.InputsJSON, tc.refVideo) {
					t.Fatalf("ref_videos not wired: %s", task.InputsJSON)
				}
				var saved models.Scene
				if err := f.db.First(&saved, sc.ID).Error; err != nil {
					t.Fatal(err)
				}
				if saved.VideoFullPrompt != bilingualRef2VEnglish {
					t.Fatal("injection must not rewrite the user's saved execution draft")
				}
			}
		})
	}
}
