package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
)

// TestResultVideoOfSkipsEchoedInput 3D 参考视频节点会把输入 mp4 回显到 outputs，
// 取成片时不能把它当成产物（否则会去 type=output 下载一个 input 文件 → 404）。
func TestResultVideoOfSkipsEchoedInput(t *testing.T) {
	inputs, _ := json.Marshal(map[string][]map[string]string{
		"ref_images": {{"task_id": "1", "name": "loc_22.png"}, {"task_id": "1", "name": "scene_g2.png"}},
		"ref_videos": {{"task_id": "materials", "name": "clay-seq-20261122-174919.mp4"}},
	})
	gpu := 0
	task := &models.Task{GPUIndex: &gpu, InputsJSON: string(inputs)}

	// 新行：ComfyUI 原始的 source_type 已保留。
	task.ResultFiles = `[
		{"type":"images","source_type":"input","subfolder":"materials","filename":"clay-seq-20261122-174919.mp4"},
		{"type":"gifs","source_type":"output","subfolder":"minimax","filename":"20261122-184207-3ff79981625b_00001-audio.mp4"}
	]`
	if got, _ := resultVideoOf(task); got != "minimax/20261122-184207-3ff79981625b_00001-audio.mp4" {
		t.Fatalf("picked %q", got)
	}

	// 旧行（本次修复前入库的结果没有 source_type）：只能靠「名字等于输入」剔除。
	task.ResultFiles = `[
		{"type":"images","subfolder":"materials","filename":"clay-seq-20261122-174919.mp4"},
		{"type":"gifs","subfolder":"minimax","filename":"20261122-184207-3ff79981625b_00001-audio.mp4"}
	]`
	if got, _ := resultVideoOf(task); got != "minimax/20261122-184207-3ff79981625b_00001-audio.mp4" {
		t.Fatalf("legacy row picked %q", got)
	}

	// 没有输入信息时保持原行为：第一条视频就是产物。
	plain := &models.Task{GPUIndex: &gpu, ResultFiles: `[{"type":"gifs","subfolder":"minimax","filename":"out.mp4"}]`}
	if got, _ := resultVideoOf(plain); got != "minimax/out.mp4" {
		t.Fatalf("plain task picked %q", got)
	}

	// 只有回显输入、没有产物：宁可返回空（走「结果待取回」告警）也不能下载输入文件。
	task.ResultFiles = `[{"type":"images","source_type":"input","subfolder":"materials","filename":"clay-seq-20261122-174919.mp4"}]`
	if got, _ := resultVideoOf(task); got != "" {
		t.Fatalf("input-only result picked %q", got)
	}
}

// TestHasUnsavedSceneOutputs 兜底对账的触发条件：还有场景停在生成中才算「有未取回结果」。
func TestHasUnsavedSceneOutputs(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Scene{}); err != nil {
		t.Fatal(err)
	}
	ps := &ProjectService{db: db}
	if ps.hasUnsavedSceneOutputs() {
		t.Fatal("empty database must report no pending scene output")
	}
	sc := models.Scene{ProjectID: 1, EpisodeN: 1, Order: 1, Status: "video_ready"}
	if err := db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	if ps.hasUnsavedSceneOutputs() {
		t.Fatal("video_ready scene must not trigger reconciliation")
	}
	db.Model(&models.Scene{}).Where("id = ?", sc.ID).Update("status", "video_running")
	if !ps.hasUnsavedSceneOutputs() {
		t.Fatal("video_running scene must trigger reconciliation")
	}
}
