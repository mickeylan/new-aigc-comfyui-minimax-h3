package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

type stubTextProvider struct {
	response string
	calls    int
}

func (s *stubTextProvider) Name() string                      { return "stub" }
func (s *stubTextProvider) HealthCheck(context.Context) error { return nil }
func (s *stubTextProvider) Chat(_, _ string) (string, error) {
	s.calls++
	return s.response, nil
}

type captureTextProvider struct {
	response string
	system   string
	user     string
}

func (s *captureTextProvider) Name() string                      { return "capture" }
func (s *captureTextProvider) HealthCheck(context.Context) error { return nil }
func (s *captureTextProvider) Chat(system, user string) (string, error) {
	s.system, s.user = system, user
	return s.response, nil
}

type sequenceTextProvider struct {
	responses []string
	calls     int
}

func (s *sequenceTextProvider) Name() string                      { return "sequence-stub" }
func (s *sequenceTextProvider) HealthCheck(context.Context) error { return nil }
func (s *sequenceTextProvider) Chat(_, _ string) (string, error) {
	if s.calls >= len(s.responses) {
		return "", fmt.Errorf("unexpected call %d", s.calls+1)
	}
	response := s.responses[s.calls]
	s.calls++
	return response, nil
}

func TestScriptFromPlanPromptMatchesStoryboardSchema(t *testing.T) {
	prompt := scriptFromPlanSystemPrompt(180, 25)
	for _, field := range []string{"visual_bible", "duration", "3~15", "180", "25"} {
		if !strings.Contains(prompt, field) {
			t.Fatalf("方案转分镜提示词缺少 %q", field)
		}
	}
}

func TestNormalizeVisualTypeDetectsMegastructure(t *testing.T) {
	if got := normalizeVisualType("", "云层环绕的巨塔延伸出画框"); got != "megastructure" {
		t.Fatalf("expected megastructure, got %q", got)
	}
	if got := normalizeVisualType("normal", "普通室内对话"); got != "normal" {
		t.Fatalf("expected normal, got %q", got)
	}
	if got := normalizeMegaType("mechanical"); got != "mechanical" {
		t.Fatalf("expected mechanical, got %q", got)
	}
}

func TestParseScriptJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"纯JSON", `{"script":"正文","scenes":[{"title":"s1","content":"c1","image_prompt":"p1"}]}`, 1},
		{"markdown包裹", "```json\n{\"script\":\"正文\",\"scenes\":[{\"title\":\"s1\",\"content\":\"c1\",\"image_prompt\":\"p1\"},{\"title\":\"s2\",\"content\":\"c2\",\"image_prompt\":\"p2\"}]}\n```", 2},
		{"前后杂文", `好的，这是剧本：
{"script":"正文","scenes":[{"title":"s1","content":"c1","image_prompt":"p1"}]}
希望你喜欢`, 1},
		{"字符串内原始换行", "{\"script\":\"第一行\n第二行\",\"scenes\":[{\"title\":\"s1\",\"content\":\"动作\n对白\",\"image_prompt\":\"p1\"}]}", 1},
		{"字符串值后混入文字", `{"script":"正文"舒,"scenes":[{"title":"s1","content":"c1","image_prompt":"p1"}]}`, 1},
		{"数组值后混入文字", `{"script":"正文","visual_bible":"基准","scenes":[{"title":"s1","content":"c1","image_prompt":"p1","characters":[]舒适氛围,"dialogues":[]}]}`, 1},
		{"数字值后混入文字", `{"script":"正文","visual_bible":"基准","scenes":[{"title":"s1","content":"c1","image_prompt":"p1","duration":5舒秒,"characters":[],"dialogues":[]}]}`, 1},
	}
	for _, tc := range cases {
		res, err := parseScriptJSON(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(res.Scenes) != tc.want {
			t.Fatalf("%s: 场景数 %d != %d", tc.name, len(res.Scenes), tc.want)
		}
		if res.Script == "" {
			t.Fatalf("%s: script 为空", tc.name)
		}
	}
}

func TestParseScriptJSONInvalid(t *testing.T) {
	if _, err := parseScriptJSON("这不是 JSON"); err == nil {
		t.Fatal("期望解析失败")
	}
}

func TestGenerateScriptCoreRepairsMalformedJSONOnce(t *testing.T) {
	ps := newTestProjectService(t)
	scenes := make([]map[string]any, 20)
	for i := range scenes {
		scenes[i] = map[string]any{"title": fmt.Sprintf("场景%d", i+1), "content": "动作", "image_prompt": "image", "duration": 9, "characters": []string{}, "dialogues": []any{}}
	}
	fixed, _ := json.Marshal(map[string]any{"script": "正文", "visual_bible": "视觉基准", "scenes": scenes})
	provider := &stubTextProvider{response: string(fixed)}
	ps.textProvider = provider
	p := models.Project{Title: "测试", Synopsis: "故事"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	_, got, err := ps.generateScriptCore(&p, 1, `{"script":"正文"雷}`, resScriptHandler(true))
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || len(got) != 20 {
		t.Fatalf("自动修复未生效: calls=%d scenes=%d", provider.calls, len(got))
	}
}

func TestGenerateScriptCoreRetriesTargetedRepairOnce(t *testing.T) {
	ps := newTestProjectService(t)
	scenes := make([]map[string]any, 20)
	for i := range scenes {
		scenes[i] = map[string]any{"title": fmt.Sprintf("场景%d", i+1), "content": "动作", "image_prompt": "image", "duration": 9, "characters": []string{}, "dialogues": []any{}}
	}
	fixed, _ := json.Marshal(map[string]any{"script": "正文", "visual_bible": "视觉基准", "scenes": scenes})
	provider := &sequenceTextProvider{responses: []string{
		`{"script":"正文"舒,"scenes":[]}`,
		string(fixed),
	}}
	ps.textProvider = provider
	p := models.Project{Title: "测试", Synopsis: "故事"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	_, got, err := ps.generateScriptCore(&p, 1, `{"script":"正文"雷}`, resScriptHandler(true))
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 || len(got) != 20 {
		t.Fatalf("targeted repair not used: calls=%d scenes=%d", provider.calls, len(got))
	}
}

func TestRebalanceScriptDurationsMeetsTarget(t *testing.T) {
	res := &scriptResult{Scenes: make([]scriptScene, 20)}
	for i := range res.Scenes {
		res.Scenes[i].Duration = 5
	}
	if !rebalanceScriptDurations(res, 180) {
		t.Fatal("expected durations to be rebalanced")
	}
	var total float64
	for _, scene := range res.Scenes {
		if scene.Duration < 3 || scene.Duration > 15 {
			t.Fatalf("duration out of range: %v", scene.Duration)
		}
		total += scene.Duration
	}
	if total < 179.9 || total > 180.1 {
		t.Fatalf("total duration = %v, want 180", total)
	}
}

func TestValidateScriptResultAcceptsFeasibleSceneCountBelowTargetTolerance(t *testing.T) {
	res := &scriptResult{Script: "正文", VisualBible: "视觉基准", Scenes: make([]scriptScene, 17)}
	for i := range res.Scenes {
		res.Scenes[i] = scriptScene{Content: "动作", ImagePrompt: "画面", Duration: 180.0 / 17}
	}
	if err := validateScriptResult(res, 180.0, 25); err != nil {
		t.Fatalf("17 scenes can carry 180 seconds within 3-15 seconds each: %v", err)
	}
}

func TestValidateScriptResultRejectsSceneCountThatCannotCarryDuration(t *testing.T) {
	res := &scriptResult{Script: "正文", VisualBible: "视觉基准", Scenes: make([]scriptScene, 10)}
	for i := range res.Scenes {
		res.Scenes[i] = scriptScene{Content: "动作", ImagePrompt: "画面", Duration: 15}
	}
	if err := validateScriptResult(res, 180.0, 25); err == nil {
		t.Fatal("10 scenes cannot carry 180 seconds within the 15-second maximum")
	}
}

func TestRebalanceScriptDurationsRejectsImpossibleTarget(t *testing.T) {
	res := &scriptResult{Scenes: make([]scriptScene, 10)}
	for i := range res.Scenes {
		res.Scenes[i].Duration = 5
	}
	if rebalanceScriptDurations(res, 180) {
		t.Fatal("10 scenes cannot reach 180 seconds within the 15-second maximum")
	}
}

func TestResultVideoOf(t *testing.T) {
	gpu := 3
	for _, resultFiles := range []string{
		`[{"type":"images","filename":"a.png","subfolder":""},{"type":"videos","filename":"minimax_00001_.mp4","subfolder":"minimax"}]`,
		`[{"type":"gifs","filename":"minimax_00001-audio.mp4","subfolder":"minimax"}]`,
	} {
		task := models.Task{ResultFiles: resultFiles, GPUIndex: &gpu}
		file, idx := resultVideoOf(&task)
		if !strings.HasPrefix(file, "minimax/") || !strings.HasSuffix(file, ".mp4") {
			t.Fatalf("file = %q for %s", file, resultFiles)
		}
		if idx == nil || *idx != 3 {
			t.Fatalf("gpu = %v", idx)
		}
	}
}

func TestEditorSceneVideoURLPrefersDownloadedProjectCopy(t *testing.T) {
	gpu := 3
	sc := &models.Scene{VideoInputFile: "scene_video.mp4", VideoFile: "remote/output.mp4", VideoGPU: &gpu}
	if got := editorSceneVideoURL(42, sc); got != "/api/input/42/scene_video.mp4" {
		t.Fatalf("video url = %q", got)
	}
	sc.VideoInputFile = ""
	if got := editorSceneVideoURL(42, sc); got != "/api/output/3/remote/output.mp4" {
		t.Fatalf("legacy output url = %q", got)
	}
}

func TestDetectImageExt(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	webp := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 0x00}
	if got := detectImageExt(png); got != ".png" {
		t.Fatalf("png: %s", got)
	}
	if got := detectImageExt(jpg); got != ".jpg" {
		t.Fatalf("jpg: %s", got)
	}
	if got := detectImageExt(webp); got != ".webp" {
		t.Fatalf("webp: %s", got)
	}
	if got := detectImageExt([]byte{0x00}); got != ".png" {
		t.Fatalf("unknown: %s", got)
	}
}

func TestUploadedSceneImageExtRejectsNonImage(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	jpg := []byte{0xff, 0xd8, 0xff, 0xe0}
	webp := []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}
	if uploadedSceneImageExt(png) != ".png" || uploadedSceneImageExt(jpg) != ".jpg" || uploadedSceneImageExt(webp) != ".webp" {
		t.Fatal("expected supported image signatures")
	}
	if got := uploadedSceneImageExt([]byte("not an image")); got != "" {
		t.Fatalf("expected invalid content to be rejected, got %q", got)
	}
}

func TestReplaceSceneImageInvalidatesVideoAndGenerationToken(t *testing.T) {
	ps := newTestProjectService(t)
	project := models.Project{Title: "p", Status: "ready"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	gpu := 1
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, ImageFile: "old.png", ImageToken: "old-token", ImageTaskID: "image-task", VideoTaskID: "video-task", VideoFile: "old.mp4", VideoInputFile: "old-input.mp4", VideoGPU: &gpu, VideoFullPrompt: "stale", Status: "video_ready"}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.ReplaceSceneImage(&scene, "replacement.png"); err != nil {
		t.Fatal(err)
	}
	var got models.Scene
	if err := ps.db.First(&got, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ImageFile != "replacement.png" || got.ImageToken != "" || got.ImageTaskID != "" || got.Status != "image_ready" || got.Error != "" {
		t.Fatalf("unexpected image state: %+v", got)
	}
	if got.VideoTaskID != "" || got.VideoFile != "" || got.VideoInputFile != "" || got.VideoGPU != nil || got.VideoFullPrompt != "" {
		t.Fatalf("stale video state retained: %+v", got)
	}
}

// newTestProjectService 内存 SQLite 初始化
func newTestProjectService(t *testing.T) *ProjectService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Character{}, &models.Asset{}, &models.MergeTask{}, &models.Task{}, &models.GenerationCandidate{}, &models.Episode{}, &models.PromptPolicyOverride{}); err != nil {
		t.Fatal(err)
	}
	ps := NewProjectService(nil, db, nil, nil, nil, nil, nil, nil, nil)
	ps.stopped = make(chan struct{})
	return ps
}

func TestHasRecentGenerationTasksSkipsIdlePolling(t *testing.T) {
	ps := newTestProjectService(t)
	if ps.hasRecentGenerationTasks() {
		t.Fatal("idle database should not trigger output table scans")
	}
	task := models.Task{TaskID: "active", Status: "running"}
	if err := ps.db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if !ps.hasRecentGenerationTasks() {
		t.Fatal("active task should trigger output reconciliation")
	}
	if err := ps.db.Model(&task).Updates(map[string]any{"status": "success", "updated_at": time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if ps.hasRecentGenerationTasks() {
		t.Fatal("old terminal task should not keep polling output tables")
	}
}

func TestClaimPipelinePersistsTargetEpisode(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Plan: `{"episodes":[{"n":2}]}`, Status: "plan_done"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}

	current, err := ps.claimPipeline(&p, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if current.PipelineEpisode != 2 {
		t.Fatalf("流水线目标集 = %d，期望 2", current.PipelineEpisode)
	}
	if current.PipelineStage != "script" {
		t.Fatalf("已有创作方案应从剧本阶段开始，实际 %q", current.PipelineStage)
	}

	var restored models.Project
	if err := ps.db.First(&restored, p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.PipelineEpisode != 2 {
		t.Fatalf("重新读取后流水线目标集 = %d，期望 2", restored.PipelineEpisode)
	}
}

func TestClaimPipelineDefaultsToFirstEpisode(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Status: "draft"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}

	current, err := ps.claimPipeline(&p, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.PipelineEpisode != 1 || current.PipelineStage != "plan" {
		t.Fatalf("默认流水线目标不正确: episode=%d stage=%q", current.PipelineEpisode, current.PipelineStage)
	}
}

func TestValidateScriptResultAndDuration(t *testing.T) {
	scenes := make([]scriptScene, 25)
	for i := range scenes {
		scenes[i] = scriptScene{Title: "场景", Content: "动作", ImagePrompt: "画面", Duration: 7.2}
	}
	valid := &scriptResult{Script: "正文", VisualBible: "黑发少年，蓝色外套，国漫画风", Scenes: scenes}
	if err := validateScriptResult(valid); err != nil {
		t.Fatalf("合法剧本不应失败: %v", err)
	}
	valid.VisualBible = ""
	if err := validateScriptResult(valid); err == nil {
		t.Fatal("缺少视觉基准应校验失败")
	}
	if got := normalizeSceneDuration(0); got != 5 {
		t.Fatalf("默认时长 = %v", got)
	}
	if got := normalizeSceneDuration(1); got != 3 {
		t.Fatalf("最小时长 = %v", got)
	}
	if got := normalizeSceneDuration(20); got != 15 {
		t.Fatalf("最大时长 = %v", got)
	}
}

func TestSuccessfulTaskWithoutVideoFailsScene(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Status: "producing"}
	ps.db.Create(&p)
	task := models.Task{TaskID: "missing-output", Status: "success", ResultFiles: "[]"}
	ps.db.Create(&task)
	sc := models.Scene{ProjectID: p.ID, Order: 1, Status: "video_pending", VideoTaskID: task.TaskID, VideoRetries: 2}
	ps.db.Create(&sc)
	ps.syncSceneVideos()
	var got models.Scene
	ps.db.First(&got, sc.ID)
	if got.Status != "failed" || got.Error == "" {
		t.Fatalf("无视频输出的成功任务必须失败: %+v", got)
	}
}

func TestRecoverInterruptedProjectWork(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Status: "producing", PipelineStage: "script_running", AutoGenerate: true}
	ps.db.Create(&p)
	image := models.Scene{ProjectID: p.ID, Order: 1, Status: "image_pending", ImageToken: "token"}
	video := models.Scene{ProjectID: p.ID, Order: 2, Status: "video_creating", ImageFile: "scene.png"}
	ps.db.Create(&image)
	ps.db.Create(&video)
	merge := models.MergeTask{ProjectID: p.ID, Status: "running"}
	ps.db.Create(&merge)
	ps.recoverInterruptedProjects()
	ps.db.First(&image, image.ID)
	ps.db.First(&video, video.ID)
	ps.db.First(&merge, merge.ID)
	ps.db.First(&p, p.ID)
	if image.Status != "pending" || image.ImageToken != "" {
		t.Fatalf("图片任务未恢复: %+v", image)
	}
	if video.Status != "image_ready" {
		t.Fatalf("视频创建任务未恢复: %+v", video)
	}
	if merge.Status != "failed" || merge.Error == "" {
		t.Fatalf("中断的合并任务应明确失败并允许用户重试: %+v", merge)
	}
	if p.PipelineStage != "script" {
		t.Fatalf("剧本阶段未恢复: %+v", p)
	}
}

func TestPipelineFailureStatusIsNotOverwritten(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Status: "failed", PipelineStage: "failed"}
	ps.db.Create(&p)
	ps.db.Create(&models.Scene{ProjectID: p.ID, Order: 1, Status: "image_ready"})
	ps.updateProjectStatus(p.ID)
	ps.db.First(&p, p.ID)
	if p.Status != "failed" {
		t.Fatalf("流水线失败状态被覆盖为 %s", p.Status)
	}
}

func TestUpdateSceneLogic(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "测试", Synopsis: "创意", Status: "script_done"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{
		ProjectID: p.ID, Order: 1, Title: "t1", Content: "c1", ImagePrompt: "p1",
		ImageFile: "scene_1.png", VideoTaskID: "task-x", VideoFile: "minimax/1.mp4",
		Status: "video_ready",
	}
	if err := ps.db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}

	// 仅修改正文：保留画面，清空视频，状态回 image_ready
	if err := ps.UpdateScene(&sc, "", "c2", "", sc.Duration); err != nil {
		t.Fatal(err)
	}
	var after models.Scene
	ps.db.First(&after, sc.ID)
	if after.ImageFile == "" || after.VideoTaskID != "" || after.VideoFile != "" {
		t.Fatalf("content-only 编辑应保留画面清空视频: %+v", after)
	}
	if after.Status != "image_ready" {
		t.Fatalf("status = %s", after.Status)
	}

	// 修改时长：保留画面、清空视频，并使用新时长。
	if err := ps.UpdateScene(&after, "", "", "", 15); err != nil {
		t.Fatal(err)
	}
	var timed models.Scene
	ps.db.First(&timed, sc.ID)
	if timed.Duration != 15 || timed.ImageFile == "" || timed.VideoFile != "" || timed.VideoTaskID != "" || timed.Status != "image_ready" {
		t.Fatalf("修改时长后的失效状态错误: %+v", timed)
	}

	// 修改画面提示词：画面+视频全部清空，状态回 pending
	if err := ps.UpdateScene(&after, "", "", "p2", after.Duration); err != nil {
		t.Fatal(err)
	}
	var after2 models.Scene
	ps.db.First(&after2, sc.ID)
	if after2.ImageFile != "" || after2.VideoTaskID != "" || after2.VideoFile != "" {
		t.Fatalf("image_prompt 编辑应清空全部产物: %+v", after2)
	}
	if after2.Status != "pending" {
		t.Fatalf("status = %s", after2.Status)
	}
}

func TestMaterialAutoSaveAndList(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Material{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	ms := &MaterialService{db: db}

	pid := uint(7)
	sc := &models.Scene{ID: 11, ProjectID: pid, Order: 2, Title: "场景2", ImagePrompt: "prompt"}
	ms.SaveGeneratedImage(sc, "7/scene_2.png", 12345)

	var mats []models.Material
	db.Find(&mats)
	if len(mats) != 1 {
		t.Fatalf("自动入库失败: %d", len(mats))
	}
	m := mats[0]
	if m.Type != "image" || m.Source != "scene" || m.ProjectID == nil || *m.ProjectID != pid {
		t.Fatalf("入库字段错误: %+v", m)
	}
	if m.SceneID == nil || *m.SceneID != 11 {
		t.Fatalf("场景关联错误: %+v", m)
	}
	if m.Path != "7/scene_2.png" || m.Prompt != "prompt" {
		t.Fatalf("path/prompt 错误: %+v", m)
	}

	// List 按类型/项目筛选
	list, _ := ms.List("video", nil)
	if len(list) != 0 {
		t.Fatal("类型筛选失败")
	}
	list, _ = ms.List("image", nil)
	if len(list) != 1 {
		t.Fatal("类型筛选失败")
	}
	list, _ = ms.List("", &pid)
	if len(list) != 1 {
		t.Fatal("项目筛选失败")
	}
	if got := TaskIDOf("7/scene_2.png"); got != "7" {
		t.Fatalf("TaskIDOf = %s", got)
	}
	if got := FileNameOf("7/scene_2.png"); got != "scene_2.png" {
		t.Fatalf("FileNameOf = %s", got)
	}
}

func TestUpdateProjectStatus(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Status: "producing"}
	ps.db.Create(&p)
	gpu := 0
	for i := 1; i <= 3; i++ {
		status := "video_ready"
		if i == 3 {
			status = "pending"
		}
		ps.db.Create(&models.Scene{
			ProjectID: p.ID, Order: i, Status: status, VideoGPU: &gpu,
		})
	}
	// 2/3 就绪 → producing
	ps.updateProjectStatus(p.ID)
	var p1 models.Project
	ps.db.First(&p1, p.ID)
	if p1.Status != "producing" {
		t.Fatalf("2/3 就绪应 producing, got %s", p1.Status)
	}
	// 全部就绪 → ready
	ps.db.Model(&models.Scene{}).Where("project_id = ?", p.ID).Update("status", "video_ready")
	ps.updateProjectStatus(p.ID)
	ps.db.First(&p1, p.ID)
	if p1.Status != "ready" {
		t.Fatalf("全部就绪应 ready, got %s", p1.Status)
	}
	// finished 不被覆盖
	ps.db.Model(&p1).Update("status", "finished")
	ps.db.Model(&models.Scene{}).Where("project_id = ?", p.ID).Update("status", "pending")
	ps.updateProjectStatus(p.ID)
	ps.db.First(&p1, p.ID)
	if p1.Status != "finished" {
		t.Fatalf("finished 状态不应被覆盖, got %s", p1.Status)
	}
}

func TestEnsurePlanCharactersExtractsFromStoryWhenPlanOmittedThem(t *testing.T) {
	provider := &stubTextProvider{response: `{"characters":[{"name":"林夏","role":"女主","arc":"从逃避到担当","trait":"长发杏眼","style":"白风衣","appearance":"二十多岁女性，黑色长发，杏眼，清瘦","personality":"坚韧","background":"普通职员","relationships":"受陆川控制","emotions":"克制","habits":"握项链","wardrobe_detail":"白色羊毛风衣，银色项链","lighting_mood":"柔和冷光","color_palette":"白灰银"},{"name":"陆川","role":"反派总裁","arc":"控制欲逐渐失控","trait":"短发窄眼","style":"黑西装","appearance":"三十岁男性，短发，窄眼，高大","personality":"偏执","background":"集团继承人","relationships":"控制林夏","emotions":"冷峻","habits":"整理袖扣","wardrobe_detail":"黑色羊毛西装，金色袖扣","lighting_mood":"硬朗侧光","color_palette":"黑金"}]}`}
	ps := &ProjectService{textProvider: provider}
	p := &models.Project{Synopsis: "林夏被陆川控制后反抗并逃离", Genre: "都市悬疑", Style: "真人写实"}
	plan := &dramaPlan{Logline: "逃离控制", Core: "自由与控制"}

	if err := ps.ensurePlanCharacters(p, plan); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || len(plan.Characters) != 2 || plan.Characters[0].Name != "林夏" {
		t.Fatalf("角色自动抽取失败: calls=%d characters=%+v", provider.calls, plan.Characters)
	}
}

func TestEnsurePlanCharactersKeepsCompletePlanCharacters(t *testing.T) {
	provider := &stubTextProvider{response: `{}`}
	ps := &ProjectService{textProvider: provider}
	plan := &dramaPlan{Characters: []planCharacter{
		{Name: "林夏", Role: "女主", Trait: "长发杏眼", Style: "白风衣", Appearance: "年轻女性", Personality: "坚韧", Background: "职员", Relationships: "对手", Emotions: "克制", Habits: "握项链", WardrobeDetail: "羊毛风衣", LightingMood: "柔光", ColorPalette: "白灰"},
		{Name: "陆川", Role: "反派", Trait: "短发窄眼", Style: "黑西装", Appearance: "高大男性", Personality: "偏执", Background: "总裁", Relationships: "对手", Emotions: "冷峻", Habits: "理袖扣", WardrobeDetail: "羊毛西装", LightingMood: "侧光", ColorPalette: "黑金"},
	}}
	if err := ps.ensurePlanCharacters(&models.Project{}, plan); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatalf("完整角色不应再次调用 LLM，calls=%d", provider.calls)
	}
}

func TestAIStoryboardDetailUsesH3SubjectsAndDropsDialogueText(t *testing.T) {
	detail := `[Shot 1] 舒寒环抱上官若琳，上官若琳嘴唇微张，正说出那句：“元婴再生之力！你突破元婴了？！”中近景双人构图。`
	lines := []string{
		"- <Picture 1>：角色「舒寒」四视图",
		"- <Picture 2>：角色「上官若琳」四视图",
		"- <Picture 3>：场景「玉霄宫·内殿」参考图",
	}
	got := useSubjectTags(sanitizeStoryboardAIDetail(detail), lines)
	for _, want := range []string{"[Shot 1]", "<Subject 1>环抱<Subject 2>", "<Subject 2>嘴唇微张", "中近景双人构图"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized storyboard missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"舒寒", "上官若琳", "元婴再生之力", "突破元婴", "正说出那句", "“", "”"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized storyboard retained %q: %s", forbidden, got)
		}
	}
}

func TestRedesignSceneImagePromptUsesQwenContractWhenSelected(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &captureTextProvider{response: `{"rewritten_prompt":"国风写实画面中，林舒站在宗门大殿长阶中央，白色交领仙裙被晨光勾勒，青玉玉佩悬于腰间，低机位中近景构图，石柱与云雾形成纵深。","wh_ratio":"9:16","ratio_follow":""}`}
	ps.textProvider = provider
	project := models.Project{Title: "问仙", Genre: "古典修仙", Style: "国风写实", AspectRatio: "9:16"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, Title: "入殿", Content: "林舒进入宗门大殿", ImageEngine: ImageEngineQwen21, Characters: "林舒", LocationName: "宗门大殿"}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	prompt, err := ps.RedesignSceneImagePrompt(&scene)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "subject_definitions:") || strings.Contains(prompt, "[Shot 1]") {
		t.Fatalf("Qwen prompt contains H3 format: %s", prompt)
	}
	if !strings.Contains(prompt, "林舒") || !strings.Contains(provider.system, "Qwen-Image-2.1") {
		t.Fatalf("prompt=%q system=%q", prompt, provider.system)
	}
}

func TestRedesignQwenSceneUsesOnlyFirstShotStaticVisualFacts(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &captureTextProvider{response: `{"rewritten_prompt":"真人写实古风画面中，上官若彤身着淡紫宫装站在桃花林内，侧脸焦急而克制，暖色侧光照亮眉眼，中景平视构图。","wh_ratio":"16:9","ratio_follow":""}`}
	ps.textProvider = provider
	if err := ps.db.AutoMigrate(&models.Shot{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "舒寒", Genre: "修仙", Style: "真人写实", AspectRatio: "16:9"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, Title: "姐妹重逢 · Native段2", Content: "[Shot 1] 第一镜。\n[Shot 2] 第二镜。", ImageEngine: ImageEngineQwen21, Characters: "上官若彤"}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	shots := []models.Shot{
		{SceneID: scene.ID, Order: 1, Description: "第一镜", CameraMovement: "static", TransitionType: models.ShotTransitionCut, StartState: "准备说话", EndState: "说完", Dialogue: "原对白", PromptSubject: "淡紫宫装女子上官若彤侧脸", PromptAction: "眉宇焦急，嘴唇微张", PromptCamera: "中景平视", PromptLighting: "暖色侧光", PromptStyle: "真人写实古风", Emotion: "焦急"},
		{SceneID: scene.ID, Order: 2, Description: "第二镜", CameraMovement: "slow_tracking", StartState: "后续开始", EndState: "后续结束", PromptSubject: "姐姐上官若琳", PromptAction: "转身"},
	}
	for i := range shots {
		if err := ps.db.Create(&shots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ps.RedesignSceneImagePrompt(&scene); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"淡紫宫装女子上官若彤侧脸", "眉宇焦急", "中景平视", "暖色侧光"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("missing first-shot fact %q: %s", want, provider.user)
		}
	}
	for _, forbidden := range []string{"第二镜", "slow_tracking", "准备说话", "说完", "原对白", "转场"} {
		if strings.Contains(provider.user, forbidden) {
			t.Fatalf("retained non-static metadata %q: %s", forbidden, provider.user)
		}
	}
}

func TestRedesignSceneImagePromptUsesProjectAndAssetContext(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &stubTextProvider{response: "subject_definitions:\n<Subject 1> 是 <Picture 1> 中的素材名。\n\nsummary:\n[reference generation] 林舒进入古典宗门大殿\n\nretention_analysis:\n<Subject 1> (出现在 [Shot 1]): fully_preserved - 已提供的主体信息。\n\ndetailed_description:\n[Shot 1] 林舒右脚刚踏上长阶，电影级全景，低机位纵深构图，晨雾体积光，国风写实\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"}
	ps.textProvider = provider
	project := models.Project{Title: "问仙", Genre: "古典修仙", Style: "国风写实", Synopsis: "宗门试炼"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, Title: "入殿", Content: "林舒进入宗门大殿", ImagePrompt: "一个人在房间", Characters: "林舒", LocationName: "宗门大殿", Props: "元婴玉佩"}
	if err := ps.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	ps.db.Create(&models.Character{ProjectID: project.ID, Name: "林舒", Appearance: "22岁女性，黑色长发", WardrobeDetail: "白色交领仙裙，中式绣鞋"})
	ps.db.Create(&models.Asset{ProjectID: project.ID, Kind: AssetKindLocation, Name: "宗门大殿", Description: "石柱、长阶、云雾", Image: "hall.png"})
	ps.db.Create(&models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "元婴玉佩", Description: "青玉材质、金色纹路", Image: "jade.png"})
	prompt, err := ps.RedesignSceneImagePrompt(&scene)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !strings.Contains(prompt, "宗门大殿") {
		t.Fatalf("prompt=%q calls=%d", prompt, provider.calls)
	}
	for _, forbidden := range []string{"素材名", "已提供的主体信息"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt retained placeholder %q: %s", forbidden, prompt)
		}
	}
	for _, want := range []string{"<Subject 1> 是 <Picture 1>", "场景「宗门大殿」", "<Subject 2> 是 <Picture 2>", "道具「元婴玉佩」"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing deterministic binding %q: %s", want, prompt)
		}
	}
}

func TestGenerateReferencePromptKeepsFaceFocusedPortrait(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "问仙", Genre: "古典修仙", Style: "国风仙侠"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林舒", Role: "女主", Appearance: "22岁女性，黑色长发", WardrobeDetail: "白色交领仙裙，银色腰封", ProfileStatus: models.ProfileStatusApproved}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	prompt, err := NewCharacterProfileService(ps.db, nil).GenerateReferencePrompt(&ch, &p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"单人正面大头贴", "22岁女性", "黑色长发", "肩部以上构图", "白色交领仙裙", "银色腰封", "衣料完整覆盖肩部与胸口"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("reference portrait missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"正面全身立姿", "禁止显老", "法令纹"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("reference portrait mixed %q: %s", forbidden, prompt)
		}
	}
}

func TestRedesignScenePromptUsesShotsLooksAndH3StartFrameFormat(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.CharacterLook{}, &models.SceneCharacterLook{}); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "subject_definitions:\n无参考图\n\nsummary:\n[reference generation] 林舒挡在陆川身前\n\nretention_analysis:\n保持主体\n\ndetailed_description:\n[Shot 1] 剑刚出鞘，中近景低机位，冷月逆光\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"}
	ps.textProvider = provider
	p := models.Project{Title: "问仙", Genre: "修仙", Style: "国风写实", Synopsis: "宗门试炼"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, Title: "护卫", Content: "林舒拔剑挡在陆川身前", Characters: "林舒,陆川", LocationName: "山门"}
	if err := ps.db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 1, ShotType: "中近景", CameraAngle: "低机位", CameraMovement: "缓慢推进", Description: "林舒拔剑护住陆川", Emotion: "警觉"}).Error; err != nil {
		t.Fatal(err)
	}
	look := models.CharacterLook{ProjectID: p.ID, CharacterID: 1, Name: "战斗绣鞋", Category: "shoes", Description: "黑色云纹软底靴", AuditStatus: models.LookStatusApproved}
	if err := ps.db.Create(&look).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.SceneCharacterLook{SceneID: sc.ID, LookID: look.ID}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := ps.RedesignSceneImagePrompt(&sc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if !strings.Contains(result, want) {
			t.Fatalf("result missing %q: %s", want, result)
		}
	}
	for _, want := range []string{"林舒拔剑护住陆川", "中近景", "低机位", "缓慢推进"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("AI context missing %q: %s", want, provider.user)
		}
	}
	if !strings.Contains(provider.system, "MiniMax H3 SelfLift") || !strings.Contains(provider.system, "真实身份绑定由系统") || !strings.Contains(provider.system, "不得写“素材名”") || !strings.Contains(provider.system, "避免远景、大远景") {
		t.Fatalf("wrong system prompt: %s", provider.system)
	}
}

func TestExplicitSceneReferencesControlKrea2AndH3Order(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterLook{}, &models.SceneCharacterLook{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "refs"}
	ps.db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, ImageFile: "start.png"}
	ps.db.Create(&sc)
	ch := models.Character{ProjectID: p.ID, Name: "林舒", Portrait: "face.png", Sheet: "sheet.png"}
	ps.db.Create(&ch)
	a := models.Asset{ProjectID: p.ID, Kind: AssetKindProp, Name: "灵剑", Image: "sword.png"}
	ps.db.Create(&a)
	selected := []SceneReferenceSelection{
		{SourceType: "asset", SourceID: a.ID, Variant: "image", UseKrea2: true, UseH3: true},
		{SourceType: "character", SourceID: ch.ID, Variant: "portrait", UseKrea2: true, UseH3: false},
	}
	if err := ps.SaveSceneReferences(&sc, selected); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&sc, sc.ID)
	sc.ImageFile = "start.png"
	sc.ImageTaskID = "image-task"
	sc.VideoFile = "old.mp4"
	sc.VideoTaskID = "video-task"
	sc.Status = "video_ready"
	ps.db.Save(&sc)
	if err := ps.SaveSceneReferences(&sc, selected); err != nil {
		t.Fatal(err)
	}
	var unchanged models.Scene
	ps.db.First(&unchanged, sc.ID)
	if unchanged.ImageFile != "start.png" || unchanged.ImageTaskID != "image-task" || unchanged.VideoFile != "old.mp4" || unchanged.Status != "video_ready" {
		t.Fatalf("重复保存相同参考图不应清空产物: %+v", unchanged)
	}
	imageRefs, imageLines, explicit := ps.selectedSceneReferenceFiles(&unchanged, "krea2")
	if !explicit || len(imageRefs) != 2 || imageRefs[0].Name != "face.png" || imageRefs[1].Name != "sword.png" || !strings.Contains(imageLines[0], "<Picture 1>") {
		t.Fatalf("SelfLift refs=%+v lines=%v", imageRefs, imageLines)
	}
	videoRefs, videoLines := ps.sceneVideoReferenceFiles(&sc, fmt.Sprint(p.ID))
	if len(videoRefs) != 2 || videoRefs[0].Name != "sword.png" || videoRefs[1].Name != "start.png" || !strings.Contains(videoLines[0], "<Picture 1>") || !strings.Contains(videoLines[1], "<Picture 2>") || !strings.Contains(videoLines[1], "当前分镜画面") {
		t.Fatalf("H3 must keep selected references first and storyboard last: refs=%+v lines=%v", videoRefs, videoLines)
	}
}

func TestSceneVideoContinuityReferencesReplaceStoryboardWithTailFrame(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.FrameCandidate{}, &models.SceneContinuity{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	ps.db.Create(&project)
	source := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, VideoTaskID: "video-1", Status: "video_ready"}
	current := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2, ImageFile: "storyboard.png", Status: "image_ready"}
	ps.db.Create(&source)
	ps.db.Create(&current)
	frame := models.FrameCandidate{ProjectID: project.ID, SceneID: source.ID, VideoTaskID: source.VideoTaskID, Type: models.FrameCandidateSelected, ImageFile: "tail.png"}
	ps.db.Create(&frame)
	ps.db.Create(&models.SceneContinuity{SceneID: current.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &source.ID, SelectedFrameID: &frame.ID, SourceVideoTaskID: source.VideoTaskID, Status: "ready"})
	ps.continuity = NewContinuityService(nil, ps.db, nil)

	files, lines, cfg := ps.sceneVideoContinuityReferences(&current, fmt.Sprint(project.ID))
	if cfg == nil || len(files) != 1 || files[0].Name != "tail.png" {
		t.Fatalf("continuity refs must replace storyboard with tail frame: files=%+v cfg=%+v", files, cfg)
	}
	if strings.Contains(strings.Join(lines, "\n"), "当前分镜画面") || !strings.Contains(lines[0], "本镜 0.00 秒唯一开始画面") {
		t.Fatalf("storyboard and tail frame must be mutually exclusive: %v", lines)
	}
	prompt := buildMiniMaxH3RefPrompt(&current, &project, nil, lines)
	for _, required := range []string{"begins from <Picture 1> as its exact 0.00-second first frame", "not as a loose visual reference"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("continuity prompt missing %q: %s", required, prompt)
		}
	}
}

func TestExplicitSceneReferencesHonorUserSelectionWhileAutomaticReferencesStayRelevant(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "穿越"}
	ps.db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, Characters: "雷晓飞,雷婶", Content: "雷晓飞独坐面馆"}
	ps.db.Create(&sc)
	lead := models.Character{ProjectID: p.ID, Name: "雷晓飞", Portrait: "lead-face.png", Sheet: "lead-sheet.png"}
	aunt := models.Character{ProjectID: p.ID, Name: "雷婶", Portrait: "aunt-face.png", Sheet: "aunt-sheet.png"}
	ps.db.Create(&lead)
	ps.db.Create(&aunt)
	ps.db.Create(&models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "雷记面馆", Image: "noodle-shop.png"})
	sc.LocationName = "雷记面馆"
	ps.db.Save(&sc)
	ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 1, PromptSubject: "雷晓飞独坐桌旁"})
	refs := []SceneReferenceSelection{
		{SourceType: "character", SourceID: lead.ID, Variant: "sheet", UseKrea2: true, UseH3: true},
		{SourceType: "character", SourceID: aunt.ID, Variant: "sheet", UseKrea2: true, UseH3: true},
	}
	if err := ps.SaveSceneReferences(&sc, refs); err != nil {
		t.Fatal(err)
	}
	files, lines, explicit := ps.selectedSceneReferenceFiles(&sc, "krea2")
	if !explicit || len(files) != 2 || files[0].Name != "lead-sheet.png" || files[1].Name != "aunt-sheet.png" {
		t.Fatalf("explicit user selections must all be submitted: files=%+v lines=%v", files, lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "雷婶") {
		t.Fatalf("explicitly selected character was silently dropped: %v", lines)
	}
	autoFiles, autoLines := ps.sceneImageReferenceFiles(&sc)
	if len(autoFiles) != 2 || autoFiles[0].Name != "lead-sheet.png" || autoFiles[1].Name != "noodle-shop.png" || strings.Contains(strings.Join(autoLines, "\n"), "雷婶") {
		t.Fatalf("automatic references must match explicit order: visible character first, environment second: files=%+v lines=%v", autoFiles, autoLines)
	}
	if !strings.Contains(autoLines[0], "四视图") || !strings.Contains(autoLines[1], "环境与起始构图") {
		t.Fatalf("reference bindings must match uploaded files: %v", autoLines)
	}
}

func TestCharacterRoleClassificationOnlyRequiresVisibleReferences(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "分类"}
	ps.db.Create(&p)
	visible := models.Character{ProjectID: p.ID, Name: "林采微", Sheet: "lin-sheet.png"}
	mentioned := models.Character{ProjectID: p.ID, Name: "雷晓飞"}
	ps.db.Create(&visible)
	ps.db.Create(&mentioned)
	sc := models.Scene{
		ProjectID: p.ID, Characters: "林采微", VisibleCharacters: "林采微",
		VoiceCharacters: "林采微", MentionedCharacters: "雷晓飞", CharacterRolesSet: true,
		Content: "林采微望着雷晓飞离开的方向。",
	}
	ps.db.Create(&sc)
	if ps.sceneHasCharacter(&sc, "雷晓飞") {
		t.Fatal("mentioned-only character was treated as visible")
	}
	missing := ps.missingSceneCharacterReferences(&sc, []FileMeta{{TaskID: "1", Name: "lin-sheet.png"}})
	if len(missing) != 0 {
		t.Fatalf("mentioned-only character incorrectly required a sheet: %v", missing)
	}
	refs, lines := ps.sceneImageReferenceFiles(&sc)
	if len(refs) != 1 || refs[0].Name != "lin-sheet.png" || strings.Contains(strings.Join(lines, "\n"), "雷晓飞") {
		t.Fatalf("visual refs must include only visible cast: refs=%+v lines=%v", refs, lines)
	}
}

func TestCharacterRoleClassificationSupportsNoVisibleCast(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "画外音"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "雷晓飞"})
	sc := models.Scene{ProjectID: p.ID, CharacterRolesSet: true, VoiceCharacters: "雷晓飞", MentionedCharacters: "雷婶"}
	ps.db.Create(&sc)
	if ps.sceneHasCharacter(&sc, "雷晓飞") || len(ps.missingSceneCharacterReferences(&sc, nil)) != 0 {
		t.Fatal("voice-only scene must not require a visual character reference")
	}
}

func TestExplicitSceneReferencesDoNotRestoreCancelledCharacterSheet(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "穿越"}
	ps.db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, Characters: "雷晓飞", Content: "雷晓飞独坐面馆", LocationName: "雷记面馆"}
	ps.db.Create(&sc)
	lead := models.Character{ProjectID: p.ID, Name: "雷晓飞", Portrait: "lead-face.png", Sheet: "lead-sheet.png"}
	ps.db.Create(&lead)
	location := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "雷记面馆", Image: "noodle-shop.png"}
	ps.db.Create(&location)
	ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 1, PromptSubject: "雷晓飞独坐桌旁"})
	if err := ps.SaveSceneReferences(&sc, []SceneReferenceSelection{{SourceType: "asset", SourceID: location.ID, Variant: "image", UseKrea2: true}}); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&sc, sc.ID)
	files, lines, explicit := ps.selectedSceneReferenceFiles(&sc, "krea2")
	if !explicit || len(files) != 1 || files[0].Name != "noodle-shop.png" {
		t.Fatalf("explicit references must not restore cancelled character sheet: files=%+v lines=%v", files, lines)
	}
	prompt := buildH3StoryboardPrompt(&sc, &p, lines)
	for _, want := range []string{"<Subject 1> 是 <Picture 1>", "雷记面馆", "[reference generation] <Subject 1>"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "<Subject 2>") || strings.Contains(prompt, "lead-sheet") {
		t.Fatalf("cancelled character reference was restored: %s", prompt)
	}
}

func TestBuildH3StoryboardPromptIsIdempotent(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「雷晓飞」四视图", "- <Picture 2>：场景「雷记面馆」参考图"}
	sc := &models.Scene{Content: "雷晓飞站在面馆", ImagePrompt: "[Shot 1] 画面中的参考主体为<Subject 1>、<Subject 2>。画面中的参考主体为<Subject 1>、<Subject 2>。<Subject 1>站在桌旁。\n视觉风格：3D国漫\n视觉风格：3D国漫"}
	prompt := buildH3StoryboardPrompt(sc, &models.Project{Style: "3D国漫"}, lines)
	detail := h3PromptSection(prompt, "detailed_description:")
	if strings.Count(detail, "画面中的参考主体为") != 1 {
		t.Fatalf("subject prefix duplicated: %s", detail)
	}
	if strings.Count(detail, "视觉风格：3D国漫") != 1 {
		t.Fatalf("style must appear exactly once: %s", detail)
	}
}

func TestBuildH3StoryboardPromptRemovesRedundantWardrobeText(t *testing.T) {
	sc := &models.Scene{Content: "雷晓飞独坐面馆", ImagePrompt: "subject_definitions:\n旧绑定\n\ndetailed_description:\n雷晓飞独自坐于八仙桌旁，身着符合角色设定的现代服装，右手轻敲桌面。"}
	prompt := buildH3StoryboardPrompt(sc, &models.Project{Style: "3D国漫"}, []string{"<Picture 1>：场景雷记面馆", "<Picture 2>：角色雷晓飞四视图"})
	if strings.Contains(prompt, "符合角色设定") || strings.Contains(prompt, "现代服装") {
		t.Fatalf("redundant wardrobe text leaked: %s", prompt)
	}
	if !strings.Contains(prompt, "雷晓飞独自坐于八仙桌旁，右手轻敲桌面") {
		t.Fatalf("visible action was lost: %s", prompt)
	}
	if strings.Contains(h3PromptSection(prompt, "summary:"), sc.Content) {
		t.Fatalf("static storyboard summary must not repeat multi-step scene narrative: %s", prompt)
	}
	for _, want := range []string{
		"<Subject 1> 是 <Picture 1> 中的场景雷记面馆。",
		"<Subject 2> 是 <Picture 2> 中的角色雷晓飞四视图。",
		"[reference generation] <Subject 1>、<Subject 2>",
		"<Subject 1> (出现在 [Shot 1]): fully_preserved",
		"<Subject 2> (出现在 [Shot 1]): fully_preserved",
		"detailed_description:\n[Shot 1] 画面中的参考主体为<Subject 1>、<Subject 2>。",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("H3 storyboard prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildCharacterSheetPromptLocksHistoricalFootwear(t *testing.T) {
	p := &models.Project{Genre: "古典修仙", Style: "国风仙侠", Synopsis: "中国古代宗门修炼故事"}
	ch := &models.Character{Name: "林舒", Appearance: "22岁女性", WardrobeDetail: "白色交领仙裙，银色腰封"}
	prompt := buildCharacterSheetPrompt(p, ch)
	for _, want := range []string{"白色交领仙裙", "软底云头绣鞋", "正面全身、侧面全身、背面全身三个视图必须穿完全相同的鞋", "双脚必须完整穿鞋", "高跟鞋", "日式木屐"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("sheet prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildCharacterSheetPromptUsesDeclaredFootwear(t *testing.T) {
	p := &models.Project{Genre: "古典修仙"}
	ch := &models.Character{WardrobeDetail: "玄色长袍，黑色云纹长靴"}
	prompt := buildCharacterSheetPrompt(p, ch)
	if !strings.Contains(prompt, "指定鞋履：玄色长袍，黑色云纹长靴") || !strings.Contains(prompt, "三个视图必须穿完全相同的鞋") {
		t.Fatalf("declared footwear not preserved: %s", prompt)
	}
}

func TestRedesignLocationDescriptionUsesProjectContext(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &stubTextProvider{response: "中国古典修仙宗门主殿，青石长阶通向朱漆殿门，两侧云纹石柱，殿内中央祭坛与铜制灯架，晨雾穿过格窗形成冷暖交错光束，固定空间布局，无人物"}
	ps.textProvider = provider
	project := &models.Project{Title: "问仙", Genre: "古典修仙", Style: "国风写实", Synopsis: "宗门试炼"}
	description, err := ps.RedesignAssetDescription(project, AssetKindLocation, "宗门大殿", "很大的大殿")
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !strings.Contains(description, "青石长阶") {
		t.Fatalf("description=%q calls=%d", description, provider.calls)
	}
}

func TestBuildLocationAssetPromptSupportsMegastructure(t *testing.T) {
	p := &models.Project{Style: "真人写实", AspectRatio: "16:9"}
	location := &models.Asset{Kind: AssetKindLocation, Name: "天空巨城", Description: "云海中的庞大城市", VisualType: "megastructure", MegaType: "architecture"}
	prompt := buildAssetPrompt(p, location)
	for _, want := range []string{"巨构场景专项", "建筑巨构", "尺度参照", "大气分层", "重量感", "不增加剧情人物"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("megastructure location prompt missing %q: %s", want, prompt)
		}
	}
	prop := &models.Asset{Kind: AssetKindProp, Name: "巨构钥匙", VisualType: "megastructure", MegaType: "architecture"}
	if strings.Contains(buildAssetPrompt(p, prop), "巨构场景专项") {
		t.Fatal("prop must not use location megastructure mode")
	}
}

func TestBuildAssetPromptAndSizeUseKrea2ReferenceConventions(t *testing.T) {
	p := &models.Project{Style: "真人写实", AspectRatio: "16:9"}
	prop := &models.Asset{Kind: AssetKindProp, Name: "元婴玉佩", Description: "青玉材质，金色纹路"}
	location := &models.Asset{Kind: AssetKindLocation, Name: "宗门大殿", Description: "石柱与长阶"}
	propPrompt := buildAssetPrompt(p, prop)
	locationPrompt := buildAssetPrompt(p, location)
	for _, want := range []string{"无生命道具", "只是物品专名", "不是人物", "不是婴儿", "画面中人物数量必须为零", "青玉材质"} {
		if !strings.Contains(propPrompt, want) {
			t.Fatalf("prop prompt missing %q: %s", want, propPrompt)
		}
	}
	for _, want := range []string{"场景空镜参考图", "无人物", "石柱与长阶"} {
		if !strings.Contains(locationPrompt, want) {
			t.Fatalf("location prompt missing %q: %s", want, locationPrompt)
		}
	}
	if w, h := assetImageSize(p, AssetKindProp); w != 1024 || h != 1024 {
		t.Fatalf("prop size = %dx%d", w, h)
	}
	if w, h := assetImageSize(p, AssetKindLocation); w != 1344 || h != 768 {
		t.Fatalf("location size = %dx%d", w, h)
	}
	for _, tc := range []struct {
		aspect string
		w, h   int
	}{{"16:9", 1920, 1080}, {"9:16", 1080, 1920}, {"1:1", 1920, 1920}} {
		p.AspectRatio = tc.aspect
		if w, h := sceneImageSize(p); w != tc.w || h != tc.h {
			t.Fatalf("scene %s size = %dx%d, want %dx%d", tc.aspect, w, h, tc.w, tc.h)
		}
	}
}

func TestValidateQwenScenePromptRejectsGenericInstructionAndMissingReferences(t *testing.T) {
	if err := validateQwenScenePrompt("设计一个古风仙侠风格的静止分镜画面", 0); err == nil {
		t.Fatal("generic image-generation instruction accepted as Qwen prompt")
	}
	if err := validateQwenScenePrompt("上官若彤站在桃林中，柔和侧光照亮面部。", 2); err == nil || !strings.Contains(err.Error(), "<image1>") {
		t.Fatalf("missing ordered reference was not rejected: %v", err)
	}
	valid := "以<image1>中的上官若彤身份和服饰为人物外观来源，以<image2>中的桃花林为环境来源，人物在画面中央温柔望向姐姐。"
	if err := validateQwenScenePrompt(valid, 2); err != nil {
		t.Fatalf("valid Qwen edit prompt rejected: %v", err)
	}
}

func TestEnsureQwenSceneReferenceBindingsDeterministicallyAddsMissingImages(t *testing.T) {
	got := ensureQwenSceneReferenceBindings(
		"以<image2>中的桃花林作为环境来源，上官若彤在暖色侧光下神情焦急。",
		[]string{"- <Picture 1>：角色「上官若琳」四视图", "- <Picture 2>：场景「梵心桃花林」参考图", "- <Picture 3>：角色「上官若彤」四视图"},
	)
	for _, tag := range []string{"<image1>", "<image2>", "<image3>"} {
		if !strings.Contains(got, tag) {
			t.Fatalf("missing %s: %s", tag, got)
		}
	}
	if strings.Count(got, "<image2>") != 1 {
		t.Fatalf("existing binding duplicated: %s", got)
	}
	if strings.Contains(got, "<Picture") {
		t.Fatalf("H3 picture tag leaked: %s", got)
	}
	if !strings.Contains(got, "不要求该素材中的主体必须出现在画面中") {
		t.Fatalf("optional visibility constraint missing: %s", got)
	}
	if err := validateQwenScenePrompt(got, 3); err != nil {
		t.Fatalf("completed prompt invalid: %v", err)
	}
}

func TestBuildQwenSceneExecutionPromptKeepsLocationOnlyAndForbidsPeople(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "问仙"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Character{ProjectID: p.ID, Name: "舒寒", Appearance: "黑色长发"}).Error; err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, ImageEngine: ImageEngineQwen21, ImagePrompt: "天阙宗玉霄峰巍峨耸立于云海之上", ReferenceImagesJSON: `[{"source_type":"asset","source_id":22,"variant":"image","use_krea2":true,"use_h3":false}]`}
	got := ps.buildQwenSceneExecutionPrompt(&sc, []string{"- <Picture 1>：场景「玉霄宫」参考图"})
	for _, want := range []string{"<image1>: 场景「玉霄宫」参考图", "天阙宗玉霄峰", "零人物", "No people"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{"舒寒：", "主要角色外貌特征", "当前项目 Skill", "masterpiece", "居中构图、中景、半身像"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("prompt retained unrelated %q: %s", forbidden, got)
		}
	}
}

func TestBuildQwenSceneExecutionPromptAllowsSelectedCharacter(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "问仙"}
	_ = ps.db.Create(&p).Error
	sc := models.Scene{ProjectID: p.ID, ImagePrompt: "舒寒站在殿前", ReferenceImagesJSON: `[{"source_type":"character","source_id":1,"variant":"portrait","use_krea2":true,"use_h3":false}]`}
	got := ps.buildQwenSceneExecutionPrompt(&sc, []string{"- <Picture 1>：角色「舒寒」标准像"})
	if strings.Contains(got, "零人物") {
		t.Fatalf("character scene incorrectly forbidden: %s", got)
	}
}

func TestBuildPortraitPromptDoesNotAppendNegativePrompt(t *testing.T) {
	p := &models.Project{Style: "真人写实"}
	ch := &models.Character{Appearance: "22岁女性，黑色长发", ReferencePrompt: "22岁青年女性，超写实真人照片风格"}
	prompt := buildPortraitPrompt(p, ch)
	if strings.Count(prompt, "超写实真人照片风格") != 1 {
		t.Fatalf("style duplicated: %s", prompt)
	}
	for _, forbidden := range []string{"禁止显老", "法令纹", "眼袋", "负面提示词"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt should not append %q at cfg=1: %s", forbidden, prompt)
		}
	}
}

func TestStartCharacterPortraitRequiresApprovedProfileAndPrompt(t *testing.T) {
	ps := newTestProjectService(t)
	ch := &models.Character{Name: "林夏", ProfileStatus: models.ProfileStatusDraft}
	if err := ps.StartCharacterPortrait(ch); err == nil || !strings.Contains(err.Error(), "审核通过") {
		t.Fatalf("草稿档案应拒绝生图，err=%v", err)
	}
	ch.ProfileStatus = models.ProfileStatusApproved
	if err := ps.StartCharacterPortrait(ch); err == nil || !strings.Contains(err.Error(), "参考像提示词") {
		t.Fatalf("缺少提示词应拒绝生图，err=%v", err)
	}
}

func TestUpdateCharacterInvalidatesPortrait(t *testing.T) {
	ps := newTestProjectService(t)
	ch := models.Character{ProjectID: 1, Name: "林夏", Trait: "长发", ReferencePrompt: "旧提示词", Portrait: "old.png", ProfileStatus: models.ProfileStatusApproved, ReviewNote: "通过"}
	ps.db.Create(&ch)
	if err := ps.UpdateCharacter(&ch, models.Character{Name: "林夏", Trait: "短发"}); err != nil {
		t.Fatal(err)
	}
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.Portrait != "" || got.ReferencePrompt != "" || got.ReviewNote != "" || got.ProfileStatus != models.ProfileStatusDraft {
		t.Fatalf("角色形象编辑未使派生状态失效: %+v", got)
	}
}

func TestPlanBackfillInvalidatesDerivedProfileState(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主", ReferencePrompt: "旧提示词", Portrait: "old.png", ProfileStatus: models.ProfileStatusApproved, ReviewNote: "通过"}
	ps.db.Create(&ch)
	ps.upsertCharactersFromPlan(&p, &dramaPlan{Characters: []planCharacter{{Name: "林夏", Appearance: "补全外貌"}}})
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.Appearance != "补全外貌" || got.ReferencePrompt != "" || got.Portrait != "" || got.ReviewNote != "" || got.ProfileStatus != models.ProfileStatusDraft {
		t.Fatalf("方案回填未清除旧派生状态: %+v", got)
	}
}

// TestUpsertCharactersFromPlanIdempotent 验证重复抽取不覆盖用户手动编辑与已生成标准像
func TestUpsertCharactersFromPlanIdempotent(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Status: "plan_done"}
	ps.db.Create(&p)

	planJSON := `{"title":"t","logline":"l","episodes":[{"n":1,"title":"e1"}],"characters":[{"name":"林夏","role":"女主","trait":"长发","style":"白风衣"},{"name":"陆川","role":"男主","trait":"寸头","style":"黑夹克"}]}`
	var plan dramaPlan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatal(err)
	}

	// 首次抽取：全部新建，source=auto
	ps.upsertCharactersFromPlan(&p, &plan)
	var chars []models.Character
	ps.db.Where("project_id = ?", p.ID).Order("id").Find(&chars)
	if len(chars) != 2 || chars[0].Name != "林夏" || chars[0].Source != "auto" || chars[0].Trait != "长发" {
		t.Fatalf("首次抽取失败: %+v", chars)
	}

	// 用户手动编辑林夏的 trait 并生成标准像，标记 manual
	ps.db.Model(&models.Character{}).Where("project_id = ? AND name = ?", p.ID, "林夏").
		Updates(map[string]any{"trait": "短发(用户改)", "portrait": "lin.png", "source": "manual"})

	// 再次抽取：不应覆盖用户编辑与标准像
	ps.upsertCharactersFromPlan(&p, &plan)
	var lin models.Character
	ps.db.Where("project_id = ? AND name = ?", p.ID, "林夏").First(&lin)
	if lin.Trait != "短发(用户改)" || lin.Portrait != "lin.png" || lin.Source != "manual" {
		t.Fatalf("重复抽取覆盖了用户编辑: %+v", lin)
	}
}

// TestCharacterContextForScene 验证出场角色权威设定拼装
func TestCharacterContextForScene(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "林夏", Trait: "长发", Style: "白风衣"})
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "陆川", Trait: "寸头", Style: "黑夹克"})

	sc := &models.Scene{ProjectID: p.ID, Characters: "林夏, 陆川"}
	ctx := ps.characterContextForScene(sc)
	if !strings.Contains(ctx, "林夏：长发；白风衣") || !strings.Contains(ctx, "陆川：寸头；黑夹克") {
		t.Fatalf("角色设定拼装错误: %q", ctx)
	}
	// 无出场角色 → 空
	if got := ps.characterContextForScene(&models.Scene{ProjectID: p.ID, Characters: ""}); got != "" {
		t.Fatalf("无角色应返回空, got %q", got)
	}
}

// TestBuildSceneImagePrompt 验证完整提示词拼装：强画风约束 + 角色设定 + 视觉基准 + 分镜
func TestBuildSceneImagePrompt(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", Style: "国漫", VisualBible: "统一暗色调"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "林夏", Trait: "长发", Style: "白风衣"})
	sc := &models.Scene{ProjectID: p.ID, Characters: "林夏", ImagePrompt: "林夏站在月台"}
	prompt := ps.buildSceneImagePrompt(sc)
	for _, want := range []string{"国漫插画风格", "严禁真实照片风格", "角色设定", "- 林夏：长发", "人物景别硬约束", "优先中景、中近景或近景", "避免远景、大远景", "统一暗色调", "画风：国漫", "当前分镜：林夏站在月台"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("拼装缺少 %q: %q", want, prompt)
		}
	}
}

func TestParseAndJoinSceneCharacters(t *testing.T) {
	got := parseSceneCharacters("林夏, 陆川 ,, ")
	if len(got) != 2 || got[0] != "林夏" || got[1] != "陆川" {
		t.Fatalf("parse: %v", got)
	}
	if parseSceneCharacters("") != nil {
		t.Fatal("空应返回 nil")
	}
	if g := joinSceneCharacters([]string{"林夏", " 陆川 ", ""}); g != "林夏,陆川" {
		t.Fatalf("join: %q", g)
	}
}

// TestAspectSizeMapping 画幅 + 分辨率档位 → 视频/文生图尺寸映射
func TestAspectSizeMapping(t *testing.T) {
	cases := []struct {
		aspect, resolution string
		vw, vh             int
		img                string
	}{
		{"16:9", "480p", 832, 480, "2560x1440"},
		{"16:9", "720p", 1280, 704, "2560x1440"},
		{"16:9", "1080p", 1920, 1088, "2560x1440"},
		{"16:9", "2k", 2560, 1440, "2560x1440"},
		{"9:16", "480p", 480, 832, "1440x2560"},
		{"9:16", "720p", 704, 1280, "1440x2560"},
		{"9:16", "1080p", 1088, 1920, "1440x2560"},
		{"1:1", "480p", 512, 512, "1920x1920"},
		{"1:1", "720p", 1024, 1024, "1920x1920"},
		{"1:1", "1080p", 1920, 1920, "1920x1920"},
		{"", "720p", 1280, 704, "2560x1440"}, // 默认横屏
	}
	for _, c := range cases {
		w, h := aspectVideoSize(c.aspect, c.resolution)
		if w != c.vw || h != c.vh {
			t.Fatalf("aspect %s %s video: %dx%d != %dx%d", c.aspect, c.resolution, w, h, c.vw, c.vh)
		}
		if got := aspectImageSize(c.aspect); got != c.img {
			t.Fatalf("aspect %s image: %s != %s", c.aspect, got, c.img)
		}
	}
}

// TestBuildSceneVideoSpec 验证有分镜图时默认走 SelfLift Ref2VA 多参考模板。
func TestBuildSceneVideoSpec(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", AspectRatio: "16:9"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "林夏", Portrait: "lin.png", Sheet: "lin-sheet.png"})
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "陆川", Portrait: "lu.png"})

	sc := &models.Scene{ProjectID: p.ID, ImageFile: "scene_1.png", Characters: "林夏, 陆川", VideoTemplate: ""}
	code, prompt, files := ps.buildSceneVideoSpec(sc, "1", "")
	if code != "minimax_h3_ref2v" || len(files) != 0 {
		t.Fatalf("正式视频默认必须走 ref2v，多参考文件在提交阶段统一装配, got %s %v", code, files)
	}
	for _, want := range []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:", "<Subject 1>", "<Picture 1>", "[reference generation]", "[Shot 1]"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("H3 prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "【参考图绑定】") {
		t.Fatalf("Ref2VA prompt must use structured subject definitions, not an appended binding block: %s", prompt)
	}
}

// TestFormatSRTTime SRT 时间码格式（HH:MM:SS,mmm），负数归零
func TestFormatSRTTime(t *testing.T) {
	cases := []struct {
		sec  float64
		want string
	}{
		{0, "00:00:00,000"},
		{1.5, "00:00:01,500"},
		{65.25, "00:01:05,250"},
		{3661.999, "01:01:01,999"},
		{-1, "00:00:00,000"},
	}
	for _, c := range cases {
		if got := formatSRTTime(c.sec); got != c.want {
			t.Fatalf("formatSRTTime(%v) = %s, want %s", c.sec, got, c.want)
		}
	}
}

// TestWriteSRTEntry SRT 字幕条目格式（含说话人前缀）
func TestBuildMiniMaxH3PromptUsesSixSectionContract(t *testing.T) {
	sc := &models.Scene{
		Title: "重逢", Content: "舒寒抬起右手，指尖触碰上官若琳的脸颊",
		Characters: "舒寒, 上官若琳", LocationName: "玉霄宫内殿", Props: "元婴玉佩", Duration: 8,
	}
	p := &models.Project{Style: "古风修仙写实"}
	prompt := buildMiniMaxH3Prompt(sc, p, nil)
	fields := []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"}
	last := -1
	for _, field := range fields {
		pos := strings.Index(prompt, field)
		if pos < 0 {
			t.Fatalf("prompt missing %s: %s", field, prompt)
		}
		if pos <= last {
			t.Fatalf("field order invalid at %s", field)
		}
		last = pos
	}
	for _, want := range []string{"唯一视觉基准", "舒寒, 上官若琳", "玉霄宫内殿", "元婴玉佩", "[Shot 1]", "短暂静止后", "N/A"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestH3RetentionListsOnlyVisibleShots(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「姐姐」四视图", "- <Picture 2>：角色「妹妹」四视图", "- <Picture 3>：场景「庭院」参考图"}
	sc := &models.Scene{VideoPrompt: "[Shot 1] 姐姐 raises her hand. [Shot 2] 妹妹 listens silently.", Duration: 8}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, nil, lines)
	retention := h3PromptSection(prompt, "retention_analysis:")
	for _, want := range []string{"<Subject 1> (appears in [Shot 1])", "<Subject 2> (appears in [Shot 2])", "<Subject 3> (appears in [Shot 1], [Shot 2])"} {
		if !strings.Contains(retention, want) {
			t.Fatalf("missing %q: %s", want, retention)
		}
	}
}

func TestBuildMiniMaxH3RefPromptUsesStoryboardAsOptionalLastReference(t *testing.T) {
	sc := &models.Scene{Content: "雷晓飞敲击桌面", VideoPrompt: "[Shot 1] 雷晓飞抬起手指后再次落向桌面。", Duration: 9}
	lines := []string{"- <Picture 1>：角色「雷晓飞」四视图", "- <Picture 2>：场景「雷记面馆」参考图", "- <Picture 3>：当前分镜画面（可选构图与动作状态参考）"}
	prompt := buildMiniMaxH3RefPrompt(sc, &models.Project{Style: "3D国漫"}, nil, lines)
	for _, want := range []string{
		"<Subject 1> is the character named 「雷晓飞」 shown in the four-view reference from <Picture 1>",
		"<Subject 3> is the storyboard composition and action-state reference from <Picture 3>",
		"[reference generation] Generate a 9-second video from the defined references and shot timeline",
		"<Subject 1>抬起手指",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("Ref2VA prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "动作必须表现为") || strings.Contains(prompt, "摄影机运动必须写明") {
		t.Fatalf("system instructions leaked into final prompt: %s", prompt)
	}
}

func TestClassifyH3SceneModeAndInstructions(t *testing.T) {
	drama := &models.Scene{Content: "姐姐看着妹妹，停顿后继续解释。"}
	if got := classifyH3SceneMode(drama, nil, []models.Dialogue{{Character: "姐姐", Text: "相信我。"}}); got != h3SceneDrama {
		t.Fatalf("drama mode=%s", got)
	}
	action := &models.Scene{Content: "两人拔剑交锋，姐姐格挡后反击。"}
	if got := classifyH3SceneMode(action, nil, nil); got != h3SceneAction {
		t.Fatalf("action mode=%s", got)
	}
	if got := classifyH3SceneMode(action, nil, []models.Dialogue{{Character: "姐姐", Text: "退后。"}}); got != h3SceneMixed {
		t.Fatalf("mixed mode=%s", got)
	}
	for mode, want := range map[h3SceneMode]string{h3SceneDrama: "视线、停顿、距离变化", h3SceneAction: "运动方向、攻防对象", h3SceneMixed: "对白与反应"} {
		if !strings.Contains(h3SceneModeInstruction(mode), want) {
			t.Fatalf("%s instruction missing %q", mode, want)
		}
	}
}

func TestGenerateSceneVideoActionDoesNotRecycleOldVisualDescription(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	provider := &captureTextProvider{response: "[Shot 1] Starting from the current composition, 雷晓飞 taps the table once with his right index finger, then turns toward the doorway as the camera pushes in with small amplitude at slow speed."}
	ps.textProvider = provider
	project := models.Project{Title: "测试"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Character{ProjectID: project.ID, Name: "雷晓飞", Sheet: "leixiaofei-sheet.png"}).Error; err != nil {
		t.Fatal(err)
	}
	sc := &models.Scene{ProjectID: project.ID, Characters: "雷晓飞", ImageFile: "storyboard.png", Content: "雷晓飞轻敲桌面，随后警觉地转头", VideoPrompt: "雷晓飞穿着蓝色粗布短褐，暖黄阳光照亮八仙桌。"}
	if err := ps.db.Create(sc).Error; err != nil {
		t.Fatal(err)
	}
	ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 1, Description: "雷晓飞听见门外脚步后敲桌示警", ShotType: "中近景", CameraAngle: "平视", CameraMovement: "缓慢推进", PromptAction: "右手食指敲击桌面一次后转头看门口", Emotion: "警觉"})
	ps.db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: sc.ID, Order: 1, Character: "雷晓飞", Text: "谁在门外？"})
	out, err := ps.GenerateSceneVideoAction(sc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(provider.user, "蓝色粗布短褐") || strings.Contains(provider.user, "暖黄阳光") || strings.Contains(provider.user, "当前动作草稿") {
		t.Fatalf("old visual draft must not be recycled: %s", provider.user)
	}
	for _, want := range []string{"镜头指令，不是剧本复述", "一个主要动作", "25至45个英文单词", "不得复述剧情背景", "无结构化对白时，人物保持闭口", "不得从参考图反推剧情", "必须全部使用简洁自然的英文"} {
		if !strings.Contains(provider.system, want) {
			t.Fatalf("system missing %q: %s", want, provider.system)
		}
	}
	for _, want := range []string{"雷晓飞轻敲桌面，随后警觉地转头", "雷晓飞听见门外脚步后敲桌示警", "右手食指敲击桌面一次后转头看门口", "中近景", "缓慢推进", "雷晓飞：谁在门外？"} {
		if !strings.Contains(provider.user, want) {
			t.Fatalf("video AI context missing %q: %s", want, provider.user)
		}
	}
	if strings.Contains(out, "雷晓飞") || !strings.Contains(out, "<Subject 1> taps the table once") {
		t.Fatalf("character must use Subject binding: %s", out)
	}
}

func TestGenerateSceneVideoActionFallsBackToStructuredShotsForMalformedAI(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Shot{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	ps.textProvider = &captureTextProvider{response: "[Shot 1] Shangguan Ruolin stands close. [Shot 1] duplicate. [Shot 2] At 99:00.000, Shangguan Ruotong listens."}
	p := models.Project{Title: "p"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "上官若琳", Sheet: "a.png"})
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "上官若彤", Sheet: "b.png"})
	sc := models.Scene{ProjectID: p.ID, Characters: "上官若琳, 上官若彤", Content: "姐妹交谈", Duration: 11, ImageFile: "story.png"}
	ps.db.Create(&sc)
	ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 1, ActType: models.ShotActSetup, ShotType: "近景", Duration: 5, PromptSubject: "上官若琳正面近景", PromptAction: "目光由坚定转为温柔", PromptCamera: "镜头缓慢后拉", PromptLighting: "柔和正面光", PromptStyle: "真人写实"})
	ps.db.Create(&models.Shot{SceneID: sc.ID, Order: 2, ActType: models.ShotActRising, ShotType: "侧面近景", Duration: 6, PromptSubject: "上官若彤侧身倾听", PromptAction: "泪光逐渐平复", PromptCamera: "镜头轻微推进", PromptLighting: "柔和侧光", PromptStyle: "真人写实"})
	got, err := ps.GenerateSceneVideoAction(&sc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Shot 1]", "[Shot 2] At 00:05.000,", "<Subject 1>front-facing close-up", "<Subject 2>listens in profile"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fallback missing %q: %s", want, got)
		}
	}
	for _, bad := range []string{"Shangguan", "99:00.000"} {
		if strings.Contains(got, bad) {
			t.Fatalf("untrusted model structure retained %q: %s", bad, got)
		}
	}
}

func TestBuildMiniMaxH3RefPromptUsesSubjectTagWithoutForcingStoryboardStart(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「雷晓飞」四视图", "- <Picture 2>：场景「雷记面馆」参考图", "- <Picture 3>：当前分镜画面（可选构图与动作状态参考）"}
	sc := &models.Scene{VideoPrompt: "[Shot 1] 雷晓飞侧身坐在桌旁，雷晓飞抬起右手。", Duration: 9}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, nil, lines)
	detail := h3PromptSection(prompt, "detailed_description:")
	if strings.Contains(detail, "本段视频从 <Picture 3> 的静止画面开始") {
		t.Fatalf("storyboard start must not be forced: %s", detail)
	}
	if strings.Contains(detail, "雷晓飞") || strings.Count(detail, "<Subject 1>") != 2 {
		t.Fatalf("character name must be replaced by Subject tag: %s", detail)
	}
}

func TestNestedFullPromptIsReducedToInnermostAction(t *testing.T) {
	inner := "[Shot 1] <Subject 3>勾起<Subject 2>的脸庞，深吻其红唇。本段无对白。"
	nested := "subject_definitions:\nouter\n\ndetailed_description:\n[Shot 1] subject_definitions:\ninner\n\nsummary:\nx\n\nretention_analysis:\nx\n\ndetailed_description:\n" + inner + "\n\noverall_soundscape:\nx\n\nnon_diegetic_music:\nN/A"
	if got := normalizeVideoActionPrompt(nested); got != inner {
		t.Fatalf("normalized action = %q, want %q", got, inner)
	}
	lines := []string{"- <Picture 1>：当前分镜画面", "- <Picture 2>：角色「上官若琳」四视图", "- <Picture 3>：角色「舒寒」四视图"}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: nested, Duration: 8}, nil, nil, lines)
	for _, heading := range []string{"subject_definitions:", "summary:", "retention_analysis:", "detailed_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if strings.Count(prompt, heading) != 1 {
			t.Fatalf("heading %s duplicated in %s", heading, prompt)
		}
	}
}

func TestValidateFullH3PromptAllowsUserEditedStructure(t *testing.T) {
	for _, prompt := range []string{
		"人物转身看向门口，镜头跟随。",
		"subject_definitions:\nx\ndetailed_description:\n用户自定义正文\nsubject_definitions:\n用户保留的补充说明",
	} {
		if issues := ValidateFullH3Prompt(prompt); len(issues) != 0 {
			t.Fatalf("user-edited prompt was rejected: %v", issues)
		}
	}
}

func TestCharacterNamesMapToActualReferenceSubjects(t *testing.T) {
	lines := []string{
		"- <Picture 1>：角色「舒寒」四视图",
		"- <Picture 2>：角色「上官若琳」四视图",
		"- <Picture 3>：场景「玉霄宫·内殿」参考图",
		"- <Picture 4>：当前分镜画面（可选构图与动作状态参考）",
	}
	sc := &models.Scene{VideoPrompt: "[Shot 1] 舒寒 embraces 上官若琳 and leans close to her ear.", Duration: 15}
	dubs := []models.Dialogue{{Character: "舒寒", Text: "这不是太想你了吗。"}, {Character: "上官若琳", Text: "今晚有得是时间。"}}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, dubs, lines)
	for _, want := range []string{
		"<Subject 1> is the character named 「舒寒」 shown in the four-view reference from <Picture 1>",
		"<Subject 2> is the character named 「上官若琳」 shown in the four-view reference from <Picture 2>",
		"<Subject 4> is the storyboard composition and action-state reference from <Picture 4>",
		"<Subject 1> embraces <Subject 2>",
		"<Subject 1> (S1) says: <d>[Chinese] 这不是太想你了吗。</d>",
		"<Subject 2> (S2) says: <d>[Chinese] 今晚有得是时间。</d>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("character mapping missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "<Subject 4>环抱") {
		t.Fatalf("storyboard reference was used as a character: %s", prompt)
	}
	for _, dialogue := range []string{"这不是太想你了吗。", "今晚有得是时间。"} {
		if strings.Count(prompt, dialogue) != 1 {
			t.Fatalf("dialogue %q must appear exactly once: %s", dialogue, prompt)
		}
	}
}

func TestEmptySpeakerPlotTextIsNeverConvertedToNarration(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「雷晓飞」四视图", "- <Picture 2>：角色「林采微」四视图"}
	dubs := []models.Dialogue{{Character: "", Text: "四目相对，一瞬间的静默，却似有千言万语"}}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 雷晓飞与林采微四目相对。", Duration: 8}, nil, dubs, lines)
	for _, forbidden := range []string{"旁白者", "The narrator", "<d>", "千言万语", "(S1)"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("empty-speaker plot text became narration as %q: %s", forbidden, prompt)
		}
	}
	for _, want := range []string{"No dialogue", "human voice", "narration"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("silent contract missing %q: %s", want, prompt)
		}
	}
}

func TestExplicitNarrationAndMonologueRemainAllowed(t *testing.T) {
	got := validSceneDialogues([]models.Dialogue{
		{Character: "旁白", SpeechType: "narration", Text: "夜色笼罩山城。"},
		{Character: "雷晓飞", SpeechType: "monologue", Text: "我必须找到他。"},
	})
	if len(got) != 2 || got[0].SpeechType != "narration" || got[1].SpeechType != "monologue" {
		t.Fatalf("explicit narration/monologue was removed: %+v", got)
	}
	prompt := appendStructuredDialogue("[Shot 1] 雷晓飞站在门前。", got, []string{"- <Picture 1>：角色「雷晓飞」四视图"})
	for _, want := range []string{"旁白 (S1) says in an off-screen voiceover: <d>[Chinese] 夜色笼罩山城。</d> while their lips remain completely closed", "<Subject 1> (S2) delivers an internal monologue: <d>[Chinese] 我必须找到他。</d> while their lips remain completely closed"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("explicit speech missing %q: %s", want, prompt)
		}
	}
}

func TestStandardSceneSpeechFormat(t *testing.T) {
	sc := &models.Scene{Characters: "林采微, 雷晓飞", Content: "【动作】林采微望向门外。\n【对白｜雷晓飞】我去看看。\n【旁白】夜色渐深。\n【内心独白｜林采微】他还会回来吗？"}
	dubs := explicitSceneSpeech(sc)
	if len(dubs) != 3 {
		t.Fatalf("standard speech count = %d: %+v", len(dubs), dubs)
	}
	want := []models.Dialogue{
		{Character: "雷晓飞", SpeechType: "dialogue", Text: "我去看看。"},
		{Character: "旁白", SpeechType: "narration", Text: "夜色渐深。"},
		{Character: "林采微", SpeechType: "monologue", Text: "他还会回来吗？"},
	}
	for i := range want {
		if dubs[i].Character != want[i].Character || dubs[i].SpeechType != want[i].SpeechType || dubs[i].Text != want[i].Text {
			t.Fatalf("standard speech %d = %+v, want %+v", i, dubs[i], want[i])
		}
	}
}

func TestNaturalQuotedThoughtFallbackIsConservative(t *testing.T) {
	sc := &models.Scene{Characters: "林采微", Content: `林采微望着门外，心想：“他还会回来吗？”随后眼眶微红。`}
	dubs := explicitSceneSpeech(sc)
	if len(dubs) != 1 || dubs[0].Character != "林采微" || dubs[0].SpeechType != "monologue" || dubs[0].Text != "他还会回来吗？" {
		t.Fatalf("natural quoted thought = %+v", dubs)
	}
	unmarked := explicitSceneSpeech(&models.Scene{Characters: "林采微", Content: "林采微望着门外，心绪起伏，眼眶微红。"})
	if len(unmarked) != 0 {
		t.Fatalf("unquoted psychology became speech: %+v", unmarked)
	}
}

func TestQuotedOffscreenCharacterDialogueIsRecoveredFromSceneContent(t *testing.T) {
	sc := &models.Scene{
		Characters:        "雷晓飞",
		VisibleCharacters: "雷晓飞",
		VoiceCharacters:   "", // 即使上游漏填画外角色，也必须从原文明确信息恢复
		Content:           `雷晓飞刚迈出几步，门外突然传来林采薇清脆悦耳的声音说道："雷叔、雷婶，早上好，我过来了。"那声音如山间清泉，又似黄莺出谷。雷晓飞脚步一顿，回首望向门口方向。`,
		VideoPrompt:       "[Shot 1] 雷晓飞迈出几步后停下，回头看向门口。",
		Duration:          8,
	}
	dubs := mergeExplicitSceneSpeech(sc, nil)
	if len(dubs) != 1 || dubs[0].Character != "林采薇" || dubs[0].SpeechType != "dialogue" || dubs[0].Text != "雷叔、雷婶，早上好，我过来了。" {
		t.Fatalf("off-screen quoted dialogue extraction = %+v", dubs)
	}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, dubs, []string{"- <Picture 1>：角色「雷晓飞」四视图"})
	for _, want := range []string{"门外的林采薇，声音清脆悦耳 (S1) says: <d>[Chinese] 雷叔、雷婶，早上好，我过来了。</d>", "<Subject 1>迈出几步后停下"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("off-screen dialogue missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "山间清泉") || strings.Contains(prompt, "黄莺出谷") {
		t.Fatalf("voice description leaked into spoken text: %s", prompt)
	}
}

func TestExplicitSceneSpeakerCorrectsWrongStoredSpeaker(t *testing.T) {
	sc := &models.Scene{
		Characters:        "雷晓飞",
		VisibleCharacters: "雷晓飞",
		VoiceCharacters:   "林采薇",
		Content:           `门外传来林采薇的声音说道："雷叔、雷婶，早上好，我过来了。"`,
	}
	stored := []models.Dialogue{{Character: "雷晓飞", SpeechType: "dialogue", Text: "雷叔、雷婶，早上好，我过来了。", Order: 1}}
	got := mergeExplicitSceneSpeech(sc, stored)
	if len(got) != 1 || got[0].Character != "林采薇" || got[0].SpeechType != "dialogue" {
		t.Fatalf("explicit source speaker did not correct stored speaker: %+v", got)
	}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, got, []string{"- <Picture 1>：角色「雷晓飞」四视图"})
	if !strings.Contains(prompt, "门外的林采薇 (S1) says: <d>[Chinese] 雷叔、雷婶，早上好，我过来了。</d>") || strings.Contains(prompt, "雷晓飞 (S1) says") {
		t.Fatalf("prompt used wrong speaker: %s", prompt)
	}
}

func TestOffscreenFemaleVoiceUsesCharacterProfile(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "测试"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "林采微", Role: "女主", Appearance: "年轻女性"})
	sc := &models.Scene{ProjectID: p.ID, Content: `门外传来林采薇清脆悦耳的声音说道："我过来了。"`}
	ps.db.Create(sc)
	dubs := ps.sceneVideoDialogues(sc)
	if len(dubs) != 1 || !strings.Contains(dubs[0].H3VoiceDescription, "年轻女性林采薇") || !strings.Contains(dubs[0].H3VoiceDescription, "清脆悦耳的女声") {
		t.Fatalf("female voice identity missing: %+v", dubs)
	}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, dubs, nil)
	if !strings.Contains(prompt, "门外的年轻女性林采薇，使用清脆悦耳的女声 (S1) says: <d>[Chinese] 我过来了。</d>") {
		t.Fatalf("female voice direction missing: %s", prompt)
	}
}

func TestExplicitMonologueInSceneContentIsIncludedInH3Prompt(t *testing.T) {
	sc := &models.Scene{
		Characters:  "林采微",
		Content:     `雷晓飞离开后，林采微望着他的背影，心绪起伏。她长这么大，头一次见有人一听她的名字就说出含义。内心独白："采微是母亲临终前为她取的，寓意盼望父亲速归……“，想到此处，她不禁眼眶微红。`,
		VideoPrompt: "[Shot 1] 林采微站在门边，目光追随雷晓飞离去的背影，眼眶微红。",
		Duration:    8,
	}
	dubs := mergeExplicitSceneSpeech(sc, nil)
	if len(dubs) != 1 || dubs[0].SpeechType != "monologue" || dubs[0].Character != "林采微" || dubs[0].Text != "采微是母亲临终前为她取的，寓意盼望父亲速归……" {
		t.Fatalf("explicit monologue extraction = %+v", dubs)
	}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, dubs, []string{"- <Picture 1>：角色「林采微」四视图"})
	want := `<Subject 1> (S1) delivers an internal monologue: <d>[Chinese] 采微是母亲临终前为她取的，寓意盼望父亲速归……</d> while their lips remain completely closed`
	if !strings.Contains(prompt, want) {
		t.Fatalf("explicit monologue missing from H3 prompt: %s", prompt)
	}
}

func TestExplicitNarrationInSceneContentIsIncludedButNotInvented(t *testing.T) {
	sc := &models.Scene{Characters: "林采微", Content: `旁白：“夜色渐深。” 林采微心绪起伏。`}
	dubs := mergeExplicitSceneSpeech(sc, nil)
	if len(dubs) != 1 || dubs[0].SpeechType != "narration" || dubs[0].Text != "夜色渐深。" {
		t.Fatalf("explicit narration extraction = %+v", dubs)
	}
}

func TestUnmarkedPlotTextIsNotGuessedAsNarration(t *testing.T) {
	got := validSceneDialogues([]models.Dialogue{{Character: "", Text: "夜色笼罩山城。"}})
	if len(got) != 0 {
		t.Fatalf("unmarked plot text became narration: %+v", got)
	}
}

func TestActionDescriptionIsNeverConvertedToDialogue(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「舒寒」四视图", "- <Picture 2>：场景「内殿」参考图"}
	dubs := []models.Dialogue{{Character: "舒寒", Text: "(动作描写：抚摸脸庞，输送力量)"}}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 舒寒抬手抚摸对方脸庞。", Duration: 8}, nil, dubs, lines)
	for _, forbidden := range []string{"<d>", "动作描写", "抚摸脸庞，输送力量", "(S1)"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("action direction leaked into spoken dialogue as %q: %s", forbidden, prompt)
		}
	}
	for _, want := range []string{"No dialogue", "human voice", "narration"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("silent contract missing %q: %s", want, prompt)
		}
	}
}

func TestDialogueKeepsSpeechAndDropsInlineStageDirection(t *testing.T) {
	got := validSceneDialogues([]models.Dialogue{{Character: "舒寒", Text: "（轻抚她的脸庞）你终于醒了。"}})
	if len(got) != 1 || got[0].Text != "你终于醒了。" {
		t.Fatalf("dialogue normalization = %+v", got)
	}
}

func TestSceneWithoutStructuredDialogueForbidsVoice(t *testing.T) {
	lines := []string{"- <Picture 1>：当前分镜画面", "- <Picture 2>：角色「上官若琳」四视图"}
	sc := &models.Scene{VideoPrompt: "[Shot 1] <Subject 2>轻轻转头。", Duration: 8}
	prompt := buildMiniMaxH3RefPrompt(sc, nil, nil, lines)
	if strings.Contains(prompt, "<d>") {
		t.Fatalf("dialogue tag appeared without structured dialogue: %s", prompt)
	}
	for _, want := range []string{"No dialogue", "human voice", "narration"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("no-dialogue constraint missing %q: %s", want, prompt)
		}
	}
}

func TestStructuredDialogueIsAlwaysIncluded(t *testing.T) {
	lines := []string{"- <Picture 1>：当前分镜画面"}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 人物抬头。", Duration: 8}, nil, []models.Dialogue{{Character: "林夏", Text: "你来了"}}, lines)
	for _, want := range []string{"林夏 (S1) says: <d>[Chinese] 你来了</d>", "no additional voices", "narration", "Dialogue appears only in the shot timeline"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("structured dialogue missing %q: %s", want, prompt)
		}
	}
}

func TestSoundscapeContainsNoFakeDialogueTagAndSpeakerIDsAreStable(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「林夏」四视图", "- <Picture 2>：角色「陆川」四视图"}
	dubs := []models.Dialogue{{Character: "林夏", Text: "第一句。"}, {Character: "陆川", Text: "回应。"}, {Character: "林夏", Text: "第二句。"}}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 林夏看向陆川。", Duration: 8}, nil, dubs, lines)
	soundscape := h3PromptSection(prompt, "overall_soundscape:")
	if strings.Contains(soundscape, "<d>") || strings.Contains(soundscape, "</d>") {
		t.Fatalf("soundscape must not contain fake dialogue tags: %s", soundscape)
	}
	if strings.Count(prompt, "<Subject 1> (S1)") != 2 || strings.Count(prompt, "<Subject 2> (S2)") != 1 || strings.Contains(prompt, "(S3)") {
		t.Fatalf("speaker IDs must be stable by speaker: %s", prompt)
	}
}

func TestNormalizeSavedH3AudioPreservesUserEditedVisualPrompt(t *testing.T) {
	custom := "KEEP_MY_CUSTOM_CAMERA_MOVE"
	prompt := "subject_definitions:\nsubject\n\nsummary:\nsummary\n\nretention_analysis:\nretention\n\ndetailed_description:\n[Shot 1] " + custom + "。错误旧对白：<d>[Chinese] 不要保留</d>\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"
	dubs := []models.Dialogue{{Character: "林夏", Text: "正确对白"}}
	got := normalizeSavedH3Audio(prompt, dubs, []string{"- <Picture 1>：角色「林夏」四视图"})
	for _, want := range []string{custom, "subject_definitions:\nsubject", "summary:\nsummary", "retention_analysis:\nretention", "<Subject 1> (S1)", "<d>[Chinese] 正确对白</d>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("normalized prompt lost %q: %s", want, got)
		}
	}
	if strings.Contains(got, "不要保留") || strings.Contains(h3PromptSection(got, "overall_soundscape:"), "<d>") {
		t.Fatalf("stale dialogue/audio leaked: %s", got)
	}
}

func TestNormalizeSavedH3AudioNeverCompactsDialogueText(t *testing.T) {
	line := "她身着红金宫装，神情从悲伤转向信任与期待。"
	prompt := "subject_definitions:\nsubject\n\nsummary:\nsummary\n\nretention_analysis:\nretention\n\ndetailed_description:\n[Shot 1] 人物停下。\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"
	got := normalizeSavedH3Audio(prompt, []models.Dialogue{{Character: "林夏", Text: line}}, nil)
	if !strings.Contains(got, "<d>[Chinese] "+line+"</d>") {
		t.Fatalf("authoritative dialogue compacted: %s", got)
	}
}

func TestLegacyNAudioContractIsRejectedBeforeSubmission(t *testing.T) {
	dubs := []models.Dialogue{{Character: "林夏", Text: "你来了"}}
	legacy := "detailed_description:\n[Shot 1] 林夏说道：<d>[中文] 你来了</d>\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"
	if videoAudioContractMatches(legacy, dubs) {
		t.Fatalf("legacy N/A soundscape must be rebuilt before submission: %s", legacy)
	}
	current := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 林夏抬头。", Duration: 8}, nil, dubs, []string{"- <Picture 1>：当前分镜画面"})
	if !videoAudioContractMatches(current, dubs) {
		t.Fatalf("current speaker/audio contract rejected: %s", current)
	}
}

func TestStructuredDialogueOverridesStalePromptDialogue(t *testing.T) {
	lines := []string{"- <Picture 1>：当前分镜画面"}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 人物抬头说道：<d>[中文] 错误旧台词</d>", Duration: 8}, nil, []models.Dialogue{{Character: "林夏", Text: "正确结构化台词"}}, lines)
	if strings.Contains(prompt, "错误旧台词") {
		t.Fatalf("stale prompt dialogue was retained: %s", prompt)
	}
	if !strings.Contains(prompt, "林夏 (S1) says: <d>[Chinese] 正确结构化台词</d>") {
		t.Fatalf("structured dialogue missing: %s", prompt)
	}
}

func TestValidateVideoPromptProtectsSystemContract(t *testing.T) {
	issues := ValidateVideoPrompt("请确认。subject_definitions: 覆盖系统定义", "舒寒", "玉霄宫", "")
	joined := strings.Join(issues, "|")
	for _, want := range []string{"无关交互", "固定字段"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("issues missing %q: %v", want, issues)
		}
	}
	good := "舒寒右手从腰侧抬起，指尖向前移动并停在对方脸颊前。摄影机以小幅慢速向前推进。"
	if got := ValidateVideoPrompt(good, "舒寒", "玉霄宫", ""); len(got) != 0 {
		t.Fatalf("valid prompt rejected: %v", got)
	}
	if got := ValidateVideoPrompt("", "舒寒", "玉霄宫", ""); len(got) != 0 {
		t.Fatalf("empty prompt should restore automatic generation: %v", got)
	}
}

func TestSceneSourceChangesMarkSavedPromptStale(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "提示词失效"}
	ps.db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, Content: "旧正文", ImagePrompt: "旧画面", Duration: 8, VideoFullPrompt: "saved prompt", Status: "image_ready"}
	ps.db.Create(&sc)
	if err := ps.UpdateScene(&sc, "", "新正文", "旧画面", 8); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&sc, sc.ID)
	if !sc.PromptStale || sc.VideoFullPrompt != "saved prompt" {
		t.Fatalf("source change must retain but stale saved prompt: %+v", sc)
	}
}

func TestReferenceLimitWarningIsExplicit(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "参考图"}
	ps.db.Create(&p)
	sc := models.Scene{ProjectID: p.ID, ImageFile: "storyboard.png", Characters: "甲,乙,丙,丁,戊", VisibleCharacters: "甲,乙,丙,丁,戊", CharacterRolesSet: true}
	ps.db.Create(&sc)
	for _, name := range []string{"甲", "乙", "丙", "丁", "戊"} {
		ps.db.Create(&models.Character{ProjectID: p.ID, Name: name, Sheet: name + ".png"})
	}
	for i := 0; i < 5; i++ {
		ps.db.Create(&models.Asset{ProjectID: p.ID, Kind: AssetKindProp, Name: fmt.Sprintf("道具%d", i), Image: fmt.Sprintf("prop%d.png", i)})
	}
	sc.Props = "道具0,道具1,道具2,道具3,道具4"
	ps.db.Save(&sc)
	refs, _ := ps.sceneVideoReferenceFiles(&sc, fmt.Sprint(p.ID))
	warning := ps.sceneVideoReferenceWarning(&sc, len(refs))
	if warning == "" || !strings.Contains(warning, "实际提交") {
		t.Fatalf("reference truncation must be visible: refs=%d warning=%q", len(refs), warning)
	}
}

func TestResolveRef2VSubmissionPromptUsesLatestSavedTextVerbatim(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「林夏」四视图", "- <Picture 2>：场景「庭院」参考图"}
	scene := &models.Scene{VideoPrompt: "[Shot 1] 旧动作。", Duration: 8}
	latest := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] 最新人工修改的动作，右手抬起并停在胸前。", Duration: 8}, nil, nil, lines)
	scene.VideoFullPrompt = latest
	got := resolveRef2VSubmissionPrompt(scene, nil, nil, lines)
	if got != latest {
		t.Fatalf("saved prompt changed before submission:\nwant:\n%s\n\ngot:\n%s", latest, got)
	}
	if strings.Contains(got, "旧动作") {
		t.Fatalf("stale action leaked into submission: %s", got)
	}
}

func TestH3KeyframePromptValidationDoesNotForceGeneratedFormat(t *testing.T) {
	for _, template := range []string{"minimax_h3_t2v", "minimax_h3_i2v", "minimax_h3_first_last"} {
		for _, prompt := range []string{
			"人物从画面左侧走向门口，镜头缓慢跟随。",
			"integrated_multimodal_description:\n用户自定义正文，没有固定首行",
			"integrated_multimodal_description:\n第一段\nintegrated_multimodal_description:\n用户明确保留的第二段",
		} {
			if issues := validateH3KeyframePrompt(prompt, template); len(issues) != 0 {
				t.Fatalf("template=%s prompt=%q unexpectedly rejected: %v", template, prompt, issues)
			}
		}
	}
	if issues := validateH3KeyframePrompt("  ", "minimax_h3_first_last"); len(issues) != 1 || issues[0] != "视频提示词不能为空" {
		t.Fatalf("empty prompt validation=%v", issues)
	}
}

func TestApplyShotTimelineDoesNotRewriteRetentionShotReferences(t *testing.T) {
	prompt := "retention_analysis:\n<Subject 1> (appears in [Shot 2]): fully_preserved.\n\ndetailed_description:\n[Shot 1] 开始。\n[Shot 2] At 00:09.000, 继续。\n\noverall_soundscape:\nx"
	got := applyShotTimeline(prompt, []models.Shot{{Duration: 5}, {Duration: 6}}, 11)
	if !strings.Contains(got, "appears in [Shot 2]") || strings.Contains(got, "appears in [Shot 2] At") {
		t.Fatalf("retention was mutated: %s", got)
	}
	if !strings.Contains(got, "[Shot 2] At 00:05.000,") {
		t.Fatalf("detail timeline not corrected: %s", got)
	}
}

func TestValidateGeneratedH3PromptRejectsDuplicateAndLegacyShotSyntax(t *testing.T) {
	bad := "subject_definitions:\nx\n\nsummary:\nx\n\nretention_analysis:\nx\n\ndetailed_description:\n[Shot 1] A.\n[Shot 1] B.\n[Shot 2] C.\n\noverall_soundscape:\nx\n\nnon_diegetic_music:\nN/A"
	joined := strings.Join(validateGeneratedH3Prompt(bad, "minimax_h3_ref2v", 10), "|")
	for _, want := range []string{"Shot 1 重复"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing validation %q: %s", want, joined)
		}
	}
	good := "subject_definitions:\nx\n\nsummary:\nx\n\nretention_analysis:\nx\n\ndetailed_description:\n[Shot 1] A medium shot frames the woman by the doorway.\n[Shot 2] At 00:05.000, the shot cuts to the younger sister listening.\n\noverall_soundscape:\nQuiet room tone.\n\nnon_diegetic_music:\nN/A"
	if issues := validateGeneratedH3Prompt(good, "minimax_h3_ref2v", 10); len(issues) != 0 {
		t.Fatalf("valid generated prompt rejected: %v", issues)
	}
}

func TestH3EnglishStyleRemovesChineseStyleLabel(t *testing.T) {
	if got := h3EnglishStyle("真人写实"); got != "live-action realistic" {
		t.Fatalf("style=%q", got)
	}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: "[Shot 1] The woman looks toward the doorway.", Duration: 5}, &models.Project{Style: "真人写实"}, nil, nil)
	if strings.Contains(prompt, "真人写实") || strings.Contains(prompt, "live-action realistic visual style") {
		t.Fatalf("redundant style leaked into concise Shot prompt: %s", prompt)
	}
}

func TestH3KeyframePromptContractsAndDialoguePlacement(t *testing.T) {
	sc := &models.Scene{VideoPrompt: "[Shot 1] 林夏抬头看向门口。", Duration: 8}
	dubs := []models.Dialogue{{Character: "林夏", Text: "你来了。"}}
	i2v := buildH3I2VAPrompt(sc, &models.Project{Style: "3D国漫"}, dubs)
	if !strings.HasPrefix(i2v, "For the target video, at 0.00 seconds into the target video, <Picture 1> (from [Shot 1]) is fully referenced.") {
		t.Fatalf("I2VA alignment instruction missing: %s", i2v)
	}
	for _, heading := range []string{"integrated_multimodal_description:", "overall_soundscape:", "non_diegetic_music:"} {
		if strings.Count(i2v, heading) != 1 {
			t.Fatalf("I2VA field %s missing or duplicated: %s", heading, i2v)
		}
	}
	if !strings.Contains(h3IntegratedDescription(i2v), "林夏 (S1) says: <d>[Chinese] 你来了。</d>") {
		t.Fatalf("dialogue must be in the timeline with stable speaker ID: %s", i2v)
	}
	if strings.Contains(h3PromptSection(i2v, "overall_soundscape:"), "你来了") {
		t.Fatalf("soundscape must not repeat dialogue: %s", i2v)
	}

	multiShot := &models.Scene{VideoPrompt: "[Shot 1] Opening. [Shot 2] Reaction. [Shot 3] Final pose.", Duration: 8}
	fl2v := buildH3FL2VAPrompt(multiShot, nil, nil)
	if !strings.HasPrefix(fl2v, "How the reference pictures align with the target video") || !strings.Contains(fl2v, "Picture 2 (from Shot 3) aligns with the 8.00-second mark") {
		t.Fatalf("FL2VA alignment instruction missing: %s", fl2v)
	}
	if !strings.Contains(fl2v, "[Shot 3] ends on") {
		t.Fatalf("FL2VA final frame not bound to final shot: %s", fl2v)
	}
}

func TestH3NarrationAndCameraConflictValidation(t *testing.T) {
	sc := &models.Scene{VideoPrompt: "[Shot 1] 人物保持闭口，镜头保持固定。", Duration: 5}
	prompt := buildH3T2VAPrompt(sc, nil, []models.Dialogue{{Character: "旁白", Text: "夜色降临。"}})
	detail := h3IntegratedDescription(prompt)
	for _, want := range []string{"旁白 (S1) says in an off-screen voiceover: <d>[Chinese] 夜色降临。</d> while their lips remain completely closed"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("narration rule missing %q: %s", want, prompt)
		}
	}
	issues := strings.Join(ValidateVideoPrompt("镜头保持固定，同时缓慢推进并跟随人物。", "", "", ""), "|")
	if !strings.Contains(issues, "运镜冲突") {
		t.Fatalf("camera conflict was not rejected: %s", issues)
	}
}

func TestNormalizeSavedH3AudioCoalescesDuplicateShotOneBeforeDialogue(t *testing.T) {
	prompt := "subject_definitions:\nsubject\n\nsummary:\nsummary\n\nretention_analysis:\nretention\n\ndetailed_description:\nThe target video uses a live-action visual style.\n[Shot 1] <Subject 1> (S1) says: <d>[Chinese] 你要代替姐姐去太运宗，其实太运宗倒是个不错的地方，</d>.\n[Shot 1] Starting from a close-up, <Subject 1> grips her sister's shoulder, then releases her hand as the camera slowly dollies backward. <Subject 1> (S1) says: <d>[Chinese] 你要代替姐姐去太运宗，其实太运宗倒是个不错的地方，</d>.\n[Shot 2] At 00:05.000, Side close-up of her sister listening silently as the camera gently pans and pushes in. <Subject 1> (S1)'s same line continues seamlessly across the cut; every visible non-speaking character keeps their lips completely closed.\n\noverall_soundscape:\nold\n\nnon_diegetic_music:\nN/A"
	dubs := []models.Dialogue{{Character: "上官若琳", Text: "你要代替姐姐去太运宗，其实太运宗倒是个不错的地方，"}}
	shots := []models.Shot{{Order: 1, Duration: 5, Dialogue: dubs[0].Text}, {Order: 2, Duration: 6}}
	got := normalizeSavedH3Audio(prompt, dubs, []string{"<Subject 1> 是 <Picture 1> 中的角色「上官若琳」四视图。"}, shots)
	if strings.Count(h3PromptSection(got, "detailed_description:"), "[Shot 1]") != 1 {
		t.Fatalf("duplicate Shot 1 retained: %s", got)
	}
	if strings.Count(got, "<d>") != 1 || strings.Count(got, dubs[0].Text) != 1 {
		t.Fatalf("dialogue duplicated: %s", got)
	}
	for _, want := range []string{"live-action visual style", "Starting from a close-up", "slowly dollies backward"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged visual content lost %q: %s", want, got)
		}
	}
}

func TestNormalizeSavedH3AudioRebuildsCollapsedShotsWithoutDuplicateDialogue(t *testing.T) {
	prompt := "subject_definitions:\n<Subject 2> 是角色参考。\n\nsummary:\n测试\n\nretention_analysis:\n测试\n\ndetailed_description:\n[Shot 1] <Subject 2> (S1) says: <d>[Chinese] 第一句。</d>。\n[Shot 1] <Subject 2> (S1) says: <d>[Chinese] 第一句。</d>。\n[Shot 1] 三镜动作坍缩。 <Subject 2> (S1) says: <d>[Chinese] 第一句。</d>。\n\noverall_soundscape:\n旧声音\n\nnon_diegetic_music:\nN/A"
	shots := []models.Shot{{Order: 1, Duration: 5, Description: "第一镜动作", Dialogue: "第一句。"}, {Order: 2, Duration: 4, Description: "第二镜动作", Dialogue: "第二句，"}, {Order: 3, Duration: 5, Description: "第三镜动作", Dialogue: "第三句，"}}
	dubs := []models.Dialogue{{Character: "上官若彤", SpeechType: "dialogue", Text: "第一句。"}, {Character: "上官若彤", SpeechType: "dialogue", Text: "第二句，"}, {Character: "上官若彤", SpeechType: "dialogue", Text: "第三句，"}}
	lines := []string{"<Subject 2> 是 <Picture 2> 中的角色「上官若彤」四视图。"}
	first := applyShotTimeline(normalizeSavedH3Audio(prompt, dubs, lines, shots), shots, 14)
	second := applyShotTimeline(normalizeSavedH3Audio(first, dubs, lines, shots), shots, 14)
	if first != second {
		t.Fatalf("normalization not idempotent:\n%s\n---\n%s", first, second)
	}
	for _, text := range []string{"第一句。", "第二句，", "第三句，"} {
		if strings.Count(first, text) != 1 {
			t.Fatalf("dialogue duplicated: %s", first)
		}
	}
	markers := []string{"[Shot 1]", "[Shot 2] At 00:05.000,", "[Shot 3] At 00:09.000,"}
	for _, marker := range markers {
		if strings.Count(first, marker) != 1 {
			t.Fatalf("shot marker %q invalid: %s", marker, first)
		}
	}
}

func TestAppendStructuredDialogueUsesH3CrossShotContinuationForOneSentence(t *testing.T) {
	body := "[Shot 1] The elder sister touches her sister's cheek.\n[Shot 2] At 00:04.500, the shot cuts to the younger sister listening.\n[Shot 3] At 00:10.500, the shot cuts back to the elder sister."
	shots := []models.Shot{{PromptSubject: "上官若琳近景", Dialogue: "相信姐姐，"}, {PromptSubject: "上官若彤反应镜头", Dialogue: "姐姐无论如何也不会让你去"}, {PromptSubject: "上官若琳特写", Dialogue: "罗刹魔域！"}}
	dubs := []models.Dialogue{{Character: "上官若琳", SpeechType: "dialogue", Text: "相信姐姐，"}, {Character: "上官若琳", SpeechType: "dialogue", Text: "姐姐无论如何也不会让你去"}, {Character: "上官若琳", SpeechType: "dialogue", Text: "罗刹魔域！"}}
	got := appendStructuredDialogueToShots(body, dubs, []string{"<Subject 1> 是 <Picture 1> 中的角色「上官若琳」四视图。"}, shots)
	for _, want := range []string{"<Subject 1> (S1) says: <d>[Chinese] 相信姐姐，</d> <scenetrans>", "<Subject 1> (S1)'s words carry over from the previous shot <scenetrans> 姐姐无论如何也不会让你去</d> <scenetrans>", "<Subject 1> (S1)'s words carry over from the previous shot <scenetrans> 罗刹魔域！</d>."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing official cross-cut form %q: %s", want, got)
		}
	}
	if strings.Count(got, "<d>") != 1 || strings.Count(got, "<scenetrans>") != 4 {
		t.Fatalf("cross-cut tags invalid: %s", got)
	}
	var actual strings.Builder
	for _, match := range h3AllSpokenTextPattern.FindAllStringSubmatch(got, -1) {
		fragment := ""
		if len(match) > 1 {
			fragment = match[1]
		}
		if fragment == "" && len(match) > 2 {
			fragment = match[2]
		}
		actual.WriteString(canonicalDialogueText(fragment))
	}
	if actual.String() != canonicalDialogueText("相信姐姐，姐姐无论如何也不会让你去罗刹魔域！") {
		t.Fatalf("cross-cut dialogue changed: %s", got)
	}
}

func TestAppendStructuredDialoguePlacesEachLineInsideItsShot(t *testing.T) {
	body := "[Shot 1] The first action begins.\n[Shot 2] At 00:05.000, the second action begins.\n[Shot 3] At 00:09.000, the third action begins."
	shots := []models.Shot{{Dialogue: "第一句。"}, {Dialogue: "第二句。"}, {Dialogue: "第三句。"}}
	dubs := []models.Dialogue{
		{Character: "上官若彤", SpeechType: "dialogue", Text: "第一句。"},
		{Character: "上官若彤", SpeechType: "dialogue", Text: "第二句。"},
		{Character: "上官若彤", SpeechType: "dialogue", Text: "第三句。"},
	}
	got := appendStructuredDialogueToShots(body, dubs, nil, shots)
	positions := []int{strings.Index(got, "第一句。"), strings.Index(got, "第二句。"), strings.Index(got, "第三句。")}
	markers := []int{strings.Index(got, "[Shot 1"), strings.Index(got, "[Shot 2"), strings.Index(got, "[Shot 3")}
	if !(markers[0] < positions[0] && positions[0] < markers[1] && markers[1] < positions[1] && positions[1] < markers[2] && markers[2] < positions[2]) {
		t.Fatalf("dialogues not placed in matching shots: %s", got)
	}
	if strings.Count(got, "<d>") != 3 {
		t.Fatalf("unexpected dialogue count: %s", got)
	}
}

func TestRebuildStructuredShotActionOmitsDescriptionAndAbstractNarration(t *testing.T) {
	shots := []models.Shot{{Description: "姐妹二人在桃花林中对望，夕阳余晖洒落，画面温馨。太运宗的威胁虽在，但姐妹情深，气氛中既有忧虑也有温情。", PromptSubject: "双人中景：红金与淡紫宫装女子并肩而立，相貌相似气质各异", PromptAction: "姐妹相视，气氛温馨", PromptCamera: "双人中景固定", PromptLighting: "夕阳余晖，温暖金色", PromptStyle: "古风仙侠，温馨结尾"}}
	got := rebuildStructuredShotAction(shots)
	for _, forbidden := range []string{"太运宗", "威胁虽在", "姐妹情深", "气氛", "温情", "温馨结尾", "画面温馨", "古风仙侠"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("retained abstract/repeated prose %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"medium two-shot", "the camera holds a static two-shot", "warm sunset light", "warm golden tones"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing visible fact %q: %s", want, got)
		}
	}
}

func TestApplyShotTimelineUsesPersistedDurationsAndEndsAtSceneDuration(t *testing.T) {
	shots := []models.Shot{{Duration: 4}, {Duration: 3}, {Duration: 7}}
	got := applyShotTimeline("[Shot 1] 建立画面。\n[Shot 2] 角色反应。\n[Shot 3 | 0-0秒] 收束。", shots, 14)
	for _, want := range []string{"[Shot 1]", "[Shot 2] At 00:04.000,", "[Shot 3] At 00:07.000,"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing timeline %q: %s", want, got)
		}
	}
	if strings.Count(got, "[Shot ") != 3 || strings.Contains(got, "秒]") {
		t.Fatalf("shot markers changed unexpectedly: %s", got)
	}
	if again := applyShotTimeline(got, shots, 14); again != got {
		t.Fatalf("timeline compilation is not idempotent:\n%s\n---\n%s", got, again)
	}
}

func TestH3ActionDoesNotLeaveTruncatedDescriptionPrefix(t *testing.T) {
	action := `[Shot 3] 说明太运宗即将派出更强弟子的威胁，眉心与目光的紧张感增强。；近景：淡紫宫装女子，眼神凝重。`
	got := stripStructuredDialogueFromAction(action, []models.Dialogue{{Character: "上官若彤", SpeechType: "dialogue", Text: "太运宗就会派更强的弟子，"}})
	for _, forbidden := range []string{"明太运宗", "说明太运宗", "。；", "；。", "；；"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("retained malformed fragment %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "唇部自然开合") || !strings.Contains(got, "近景：淡紫宫装女子") {
		t.Fatalf("visual content damaged: %s", got)
	}
}

func TestH3ActionConvertsSpeechStageDirectionsToSilentVisualCues(t *testing.T) {
	action := `[Shot 1] 镜头切到上官若彤正面近景，语气加重地陈述姐姐十年没有好好闭关修炼，说完后停顿，眉心紧锁。[Shot 2] 切换到侧面，语速稍快，眼中闪过焦急，强调不到四十年这一紧迫时间期限。[Shot 3] 近景固定，继续陈述威胁。`
	dubs := []models.Dialogue{{Character: "上官若彤", SpeechType: "dialogue", Text: "这十年你都没有怎么好好闭关修炼过。"}}
	got := stripStructuredDialogueFromAction(action, dubs)
	for _, forbidden := range []string{"语气加重地陈述", "说完后停顿", "语速稍快", "继续陈述威胁"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("retained speech-stage narration %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"眉心与目光的紧张感增强", "唇部停止开合并闭合，维持一瞬静止", "唇部自然开合"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing visual cue %q: %s", want, got)
		}
	}
	full := appendStructuredDialogue(got, dubs, nil)
	if strings.Count(full, "<d>") != 1 || !strings.Contains(full, "<d>[Chinese] 这十年你都没有怎么好好闭关修炼过。</d>") {
		t.Fatalf("structured dialogue not sole spoken text: %s", full)
	}
}

func TestH3HybridLanguageAndSubjectBindingContract(t *testing.T) {
	lines := []string{"- <Picture 1>：角色「上官若琳」四视图", "- <Picture 2>：角色「上官若彤」四视图"}
	visual := "[Shot 1] 上官若琳身着红金宫装正面站立，目光由坚定转为温柔，镜头缓慢后拉。\n[Shot 2] At 00:05.000, 上官若彤侧身倾听，双唇保持闭合。"
	bound := useSubjectTags(visual, lines)
	if strings.Contains(bound, "上官若琳") || strings.Contains(bound, "上官若彤") || !strings.Contains(bound, "<Subject 1>") || !strings.Contains(bound, "<Subject 2>") {
		t.Fatalf("subject binding failed: %s", bound)
	}
	full := "subject_definitions:\n<Subject 1> is the character 上官若琳 shown in the four-view reference from <Picture 1>.\n<Subject 2> is the character 上官若彤 shown in the four-view reference from <Picture 2>.\n\nsummary:\ntest\n\nretention_analysis:\ntest\n\ndetailed_description:\n" + bound + "\n\noverall_soundscape:\nOnly ambient room tone and physical action sounds that are visibly motivated are audible. There is no dialogue, human voice, narration, commentary, indistinct vocalization, speech, or singing; every visible person keeps their lips completely closed.\n\nnon_diegetic_music:\nN/A"
	if issues := validateGeneratedH3Prompt(full, "minimax_h3_ref2v", 8); len(issues) == 0 {
		t.Fatal("Chinese visual prose must be rejected by the official English contract")
	}
	englishVisual := strings.Replace(full, bound, "[Shot 1] <Subject 1> stands in a front-facing close-up and slowly pulls back.\n[Shot 2] At 00:05.000, <Subject 2> listens silently.", 1)
	if issues := validateGeneratedH3Prompt(englishVisual, "minimax_h3_ref2v", 8); len(issues) > 0 {
		t.Fatalf("language choice must not block formal submission: %v", issues)
	}
}

func TestH3VisualEnglishGateAcceptsDocumentedCrossCutDialogue(t *testing.T) {
	prompt := "[Shot 1] <Subject 1> speaks in a steady medium close-up while the camera slowly pushes in: <d>[Chinese] 前半句</d> <scenetrans>\n[Shot 2] At 00:05.000, the camera cuts to the listener. <Subject 1> (S1)'s words carry over from the previous shot <scenetrans> 后半句</d>."
	if !h3CarryoverDialoguePattern.MatchString(prompt) {
		t.Fatalf("carryover pattern did not match: %s", prompt)
	}
	if !h3VisualProseIsEnglish(prompt, "") {
		t.Fatalf("documented cross-cut dialogue rejected as visual prose: %s", prompt)
	}
}

func TestCrossShotDialogueFragmentsKeepSpeakerIdentityAndListenerSilent(t *testing.T) {
	body := `[Shot 1] <Subject 1> stands in a medium close-up and begins speaking.
[Shot 2] At 00:05.000, <Subject 2> listens with closed lips while facing <Subject 1>.`
	dubs := []models.Dialogue{{ID: 1, Character: "上官若琳", SpeechType: "dialogue", Text: "你要代替姐姐去太运宗，其实太运宗倒是个不错的地方，"}}
	full := []rune(canonicalDialogueText(dubs[0].Text))
	split := 11
	shots := []models.Shot{{Order: 1, PromptSubject: "上官若琳正面中近景", DialogueRanges: []models.ShotDialogueRange{{DialogueID: 1, GroupKey: "dialogue-group:1", StartRune: 0, EndRune: split}}}, {Order: 2, PromptSubject: "上官若彤侧面近景", DialogueRanges: []models.ShotDialogueRange{{DialogueID: 1, GroupKey: "dialogue-group:1", StartRune: split, EndRune: len(full)}}}}
	got := appendStructuredDialogueToShots(body, dubs, []string{"- <Picture 1>：角色「上官若琳」四视图", "- <Picture 2>：角色「上官若彤」四视图"}, shots)
	for _, want := range []string{"<Subject 1> (S1) says: <d>[Chinese] " + string(full[:split]) + "</d> <scenetrans>", "<Subject 1> (S1)'s words carry over from the previous shot <scenetrans> " + string(full[split:]) + "</d>.", "<Subject 2> keeps their lips completely closed while listening."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"says in an off-screen voiceover", "her lips moving naturally", "off-screen voice from the previous shot", "remains clearly visible", "only <Subject 1> lip-syncs"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("retained ambiguous cue %q:\n%s", forbidden, got)
		}
	}
	if strings.Count(got, "<d>") != 1 || strings.Count(got, "<scenetrans>") != 2 {
		t.Fatalf("official cross-cut tags invalid: %s", got)
	}
}

func TestResolveH3VisualConflictsCleansReportedFinalPromptVariants(t *testing.T) {
	input := "<Subject 1>正面近景，神情从坚决逐渐转为温柔，手从握妹妹肩膀缓缓垂落；镜头缓慢后拉，从特写过渡至中近景，从肩部特写回到中近景；At MM:SS.mmm 开始口型配合，随后继续口型；眉心与目光的紧张感增强；转场dissolve叠化过渡。<Subject 2>静静倾听姐姐话语。"
	got := resolveH3VisualConflicts(input)
	for _, want := range []string{"原本向前抬起的手臂缓缓放下，自然垂落至身侧", "从面部特写过渡至肩部以上中近景", "眉心舒展，目光逐渐柔和", "始终保持双唇闭合，目光专注地望向画外的说话者"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	for _, bad := range []string{"妹妹肩膀", "At MM:SS.mmm", "口型配合", "随后继续口型", "眉心与目光的紧张感增强", "dissolve", "叠化过渡", "静静倾听姐姐话语", "从肩部特写回到中近景"} {
		if strings.Contains(got, bad) {
			t.Fatalf("retained %q: %s", bad, got)
		}
	}
}

func TestResolveH3VisualConflictsRemovesDuplicateFramingAndProtectsOffscreenSpeaker(t *testing.T) {
	input := "神情从坚决转为温柔耐心，眉心与目光的紧张感增强；缓慢后拉，从特写过渡至中近景，从特写回到中近景；始终保持双唇闭合，目光专注地望向画外的说话者"
	got := visualiseSpeechPerformanceNarration(resolveH3VisualConflicts(input), true)
	for _, want := range []string{"眉心舒展，目光逐渐柔和", "从面部特写过渡至肩部以上中近景", "画外的说话者"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	for _, bad := range []string{"眉心与目光的紧张感增强", "从特写回到中近景", "画外的唇部自然开合"} {
		if strings.Contains(got, bad) {
			t.Fatalf("retained %q: %s", bad, got)
		}
	}
}

func TestCompactFinalH3PromptRemovesReportedFiller(t *testing.T) {
	body := `[Shot 1] <Subject 1>身着红金宫装，手从<Subject 2>肩膀缓缓垂落，神情从坚决转为温柔，平缓地唇部自然开合。正面机位缓慢后拉，柔和正面光勾勒柔和面部线条。dissolve。
[Shot 2] At 00:05.000, <Subject 2>侧身面向<Subject 1>，眼眶微红，泪光渐消，神情从悲伤转向信任与期待，静静倾听。侧面机位微微推进。侧光勾勒柔和轮廓。`
	got := compactFinalH3VisualBody(body)
	for _, want := range []string{"手从<Subject 2>肩膀缓缓垂落", "神情从坚决转为温柔", "正面机位缓慢后拉", "眼眶微红，泪光渐消", "静静倾听", "侧面机位微微推进"} {
		if !strings.Contains(got, want) {
			t.Fatalf("visible detail lost %q: %s", want, got)
		}
	}
	for _, bad := range []string{"身着红金宫装", "唇部自然开合", "信任与期待", "柔和面部线条", "勾勒柔和轮廓", "dissolve"} {
		if strings.Contains(got, bad) {
			t.Fatalf("filler retained %q: %s", bad, got)
		}
	}
}

func TestBuildH3PromptResolvesReviewedVisualConflicts(t *testing.T) {
	shots := []models.Shot{{Order: 1, PromptSubject: "上官若琳，红金宫装，中近景", PromptAction: "神情从坚决转为温柔耐心，眉心与目光的紧张感增强，手自然垂落", PromptCamera: "正面机位，缓慢后拉", PromptLighting: "柔和正面光", PromptStyle: "古风仙侠"}, {Order: 2, PromptSubject: "上官若彤，淡紫宫装侧面近景", PromptAction: "泪光渐消，静静倾听", PromptCamera: "侧面机位，轻微摇摄跟随，微微推进", PromptLighting: "柔和侧光", PromptStyle: "古风仙侠"}}
	action := rebuildStructuredShotAction(shots)
	lines := []string{"- <Picture 1>：角色「上官若琳」四视图", "- <Picture 2>：角色「上官若彤」四视图", "- <Picture 3>：场景「桃花林」参考图"}
	prompt := buildMiniMaxH3RefPrompt(&models.Scene{VideoPrompt: action, Duration: 11}, nil, []models.Dialogue{{ID: 1, Character: "上官若琳", SpeechType: "dialogue", Text: "第一段，"}, {ID: 2, Character: "上官若琳", SpeechType: "dialogue", Text: "第二段，"}}, lines)
	for _, want := range []string{"眉心舒展，目光逐渐柔和", "微微推进", "<Subject 3> remains visible in the background", "<Subject 3> (appears in [Shot 1], [Shot 2])"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q: %s", want, prompt)
		}
	}
	for _, bad := range []string{"眉心与目光的紧张感增强", "轻微摇摄跟随", "<Subject 2> are silent listeners", "古风仙侠", "温柔细腻", "柔美内敛", "色调红金偏暖"} {
		if strings.Contains(prompt, bad) {
			t.Fatalf("retained %q: %s", bad, prompt)
		}
	}
}

func TestH3ActionRemovesBareSpeechDirectionsReportedByUser(t *testing.T) {
	action := `[Shot 1] 语气加重，眉心微蹙，镜头固定不动，说完暂停，眉心紧锁。`
	dubs := []models.Dialogue{{Character: "上官若彤", SpeechType: "dialogue", Text: "还有不到四十年。"}}
	got := stripStructuredDialogueFromAction(action, dubs)
	for _, forbidden := range []string{"语气", "说完", "暂停", "对白", "结构化"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("retained vocal semantic %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{"眉心与目光的紧张感增强", "镜头固定不动", "唇部停止开合并闭合，维持一瞬静止", "眉心紧锁"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing visual state %q: %s", want, got)
		}
	}
}

func TestNormalizeSavedH3AudioRemovesImplicitDuplicateSpeechAndPreservesAlignment(t *testing.T) {
	prompt := `目标视频的参考图时间对齐：<Picture 1>对应0.00秒；<Picture 2>对应14.00秒。

subject_definitions:
<Subject 1> 是角色参考。

summary:
测试

retention_analysis:
测试

detailed_description:
[Shot 1] <Subject 1>从参考状态开始陈述，语气加重，眉心微蹙，镜头固定不动，说完暂停，眉心紧锁。
[Shot 2] 语速稍快，强调时间紧迫。
[Shot 3] 出威胁内容，语气沉重。 <Subject 1> (S1) says: <d>[Chinese] 旧错误对白</d>。

overall_soundscape:
旧声音

non_diegetic_music:
N/A`
	dubs := []models.Dialogue{{Character: "上官若彤", SpeechType: "dialogue", Text: "这十年你都没有怎么好好闭关修炼过。还有不到四十年，太运宗就会派更强的弟子，"}}
	got := normalizeSavedH3Audio(prompt, dubs, []string{"<Subject 1> 是 <Picture 1> 中的角色「上官若彤」四视图。"})
	if !strings.HasPrefix(got, "目标视频的参考图时间对齐") {
		t.Fatalf("alignment prefix lost: %s", got)
	}
	for _, forbidden := range []string{"开始陈述", "语气", "说完", "暂停", "语速", "强调时间", "出威胁内容", "旧错误对白"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("retained implicit/old speech %q: %s", forbidden, got)
		}
	}
	if strings.Count(got, "<d>") != 1 || strings.Count(got, dubs[0].Text) != 1 {
		t.Fatalf("dialogue was not rebuilt exactly once: %s", got)
	}
}

func TestH3CrossShotDialogueMarkers(t *testing.T) {
	dubs := []models.Dialogue{
		{Character: "林夏", Text: "我还没有说完。<scenetrans>"},
		{Character: "林夏", Text: "这句话会被截断——<cutoff>"},
	}
	body := appendStructuredDialogue("[Shot 1] 林夏向前走。[Shot 2] At 00:03.000, the shot cuts to her face.", dubs, nil)
	for _, want := range []string{"林夏 (S1)", "</d>.<scenetrans>", "<cutoff>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cross-shot dialogue missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "(S2)") {
		t.Fatalf("same speaker changed ID across shots: %s", body)
	}
	if strings.Contains(body, "对白音频跨镜头") {
		t.Fatalf("explanatory prose leaked into dialogue timeline: %s", body)
	}
}

func TestH3VoiceoverUsesOfficialPhraseAndClosesLips(t *testing.T) {
	body := appendStructuredDialogue("[Shot 1] The listener remains on screen.", []models.Dialogue{{Character: "林夏", SpeechType: "narration", Text: "我仍记得那条路。"}}, nil)
	for _, want := range []string{"says in an off-screen voiceover", "<d>[Chinese] 我仍记得那条路。</d>", "while their lips remain completely closed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("voiceover contract missing %q: %s", want, body)
		}
	}
}

func TestValidateDialogueSpeakerDoesNotGuessBlankCharacterAsNarration(t *testing.T) {
	if err := validateDialogueSpeaker(models.Dialogue{ID: 7, Text: "夜色深沉"}); err == nil || !strings.Contains(err.Error(), "未指定说话人") {
		t.Fatalf("err=%v", err)
	}
	if err := validateDialogueSpeaker(models.Dialogue{ID: 8, SpeechType: "narration", Text: "夜色深沉"}); err != nil {
		t.Fatalf("explicit narration rejected: %v", err)
	}
}

func TestWriteSRTEntry(t *testing.T) {
	var sb strings.Builder
	writeSRTEntry(&sb, 1, 0, 2.5, "林夏", "你来了。")
	out := sb.String()
	for _, want := range []string{"1\n", "00:00:00,000 --> 00:00:02,500", "林夏：你来了。"} {
		if !strings.Contains(out, want) {
			t.Fatalf("SRT 条目缺少 %q: %q", want, out)
		}
	}
	// 旁白（character 为空）不加前缀
	var sb2 strings.Builder
	writeSRTEntry(&sb2, 2, 3, 5, "", "夜色深沉")
	if !strings.Contains(sb2.String(), "夜色深沉") || strings.Contains(sb2.String(), "：夜色深沉") {
		t.Fatalf("旁白不应加说话人前缀: %q", sb2.String())
	}
}
