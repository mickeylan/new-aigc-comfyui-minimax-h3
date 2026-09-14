package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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
	task := models.Task{
		ResultFiles: `[{"type":"images","filename":"a.png","subfolder":""},{"type":"videos","filename":"minimax_00001_.mp4","subfolder":"minimax"}]`,
		GPUIndex:    &gpu,
	}
	file, idx := resultVideoOf(&task)
	if file != "minimax/minimax_00001_.mp4" {
		t.Fatalf("file = %q", file)
	}
	if idx == nil || *idx != 3 {
		t.Fatalf("gpu = %v", idx)
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

// newTestProjectService 内存 SQLite 初始化
func newTestProjectService(t *testing.T) *ProjectService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Character{}, &models.Asset{}, &models.MergeTask{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	ps := NewProjectService(nil, db, nil, nil, nil, nil, nil, nil, nil)
	ps.stopped = make(chan struct{})
	return ps
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
	if merge.Status != "pending" {
		t.Fatalf("合并任务未恢复: %+v", merge)
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
	if err := ps.UpdateScene(&sc, "", "c2", ""); err != nil {
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

	// 修改画面提示词：画面+视频全部清空，状态回 pending
	if err := ps.UpdateScene(&after, "", "", "p2"); err != nil {
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

func TestRedesignSceneImagePromptUsesProjectAndAssetContext(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &stubTextProvider{response: "电影级全景，古典宗门大殿，林舒身穿白色仙裙，青玉佩悬于腰间，晨雾体积光，低机位纵深构图"}
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
	ps.db.Create(&models.Asset{ProjectID: project.ID, Kind: AssetKindLocation, Name: "宗门大殿", Description: "石柱、长阶、云雾"})
	ps.db.Create(&models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "元婴玉佩", Description: "青玉材质、金色纹路"})
	prompt, err := ps.RedesignSceneImagePrompt(&scene)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !strings.Contains(prompt, "宗门大殿") {
		t.Fatalf("prompt=%q calls=%d", prompt, provider.calls)
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
	if !strings.Contains(prompt, "正面半身头像") || strings.Contains(prompt, "正面全身立姿") {
		t.Fatalf("reference portrait should remain face-focused: %s", prompt)
	}
}

func TestCharacterAnchorAndSheetLockAccessories(t *testing.T) {
	p := &models.Project{Genre: "古典修仙", Style: "国风仙侠"}
	ch := &models.Character{Role: "女主", Appearance: "22岁女性，发间金色步摇，耳戴珍珠耳坠", WardrobeDetail: "白色交领仙裙，银色腰封，颈戴青玉项链，右腕银镯"}
	for name, prompt := range map[string]string{"anchor": buildCharacterAnchorPrompt(p, ch), "sheet": buildCharacterSheetPrompt(p, ch)} {
		for _, want := range []string{"金色步摇", "珍珠耳坠", "青玉项链", "右腕银镯", "位置、数量、左右侧", "不得丢失、增减"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing %q: %s", name, want, prompt)
			}
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
}

func TestBuildPortraitPromptDoesNotDuplicateStyleAndKeepsYouthAnchor(t *testing.T) {
	p := &models.Project{Style: "真人写实"}
	ch := &models.Character{Appearance: "22岁女性，黑色长发", ReferencePrompt: "22岁青年女性，年龄视觉必须严格锁定为22岁，超写实真人照片风格"}
	prompt := buildPortraitPrompt(p, ch)
	if strings.Count(prompt, "超写实真人照片风格") != 1 {
		t.Fatalf("style duplicated: %s", prompt)
	}
	for _, want := range []string{"22岁青年女性", "禁止显老", "法令纹", "眼袋"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
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
	for _, want := range []string{"国漫插画风格", "严禁真实照片风格", "角色设定", "- 林夏：长发", "统一暗色调", "画风：国漫", "当前分镜：林夏站在月台"} {
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
		{"16:9", "720p", 1280, 704, "2560x1440"},
		{"16:9", "1080p", 1920, 1088, "2560x1440"},
		{"16:9", "2k", 2560, 1440, "2560x1440"},
		{"9:16", "720p", 704, 1280, "1440x2560"},
		{"9:16", "1080p", 1088, 1920, "1440x2560"},
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

// TestBuildSceneVideoSpec 验证有权威角色参考时走 ref2v，否则保持首帧 i2v。
func TestBuildSceneVideoSpec(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "t", Synopsis: "s", AspectRatio: "16:9"}
	ps.db.Create(&p)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "林夏", Portrait: "lin.png", Sheet: "lin-sheet.png"})
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "陆川", Portrait: "lu.png"})

	// 当前分镜 + 两个角色参考图 → ref2v；四视图优先，标准像兜底。
	sc := &models.Scene{ProjectID: p.ID, ImageFile: "scene_1.png", Characters: "林夏, 陆川"}
	code, _, files := ps.buildSceneVideoSpec(sc, "1")
	if code != "minimax_h3_ref2v" {
		t.Fatalf("有角色参考应走 ref2v, got %s", code)
	}
	refs := files["ref_images"]
	if len(refs) != 3 || refs[0].Name != "scene_1.png" || refs[1].Name != "lin-sheet.png" || refs[2].Name != "lu.png" {
		t.Fatalf("参考图顺序或回退错误: %+v", refs)
	}

	// 无出场角色 → i2v，first_frame 单图
	sc2 := &models.Scene{ProjectID: p.ID, ImageFile: "scene_2.png", Characters: ""}
	code2, _, files2 := ps.buildSceneVideoSpec(sc2, "1")
	if code2 != "minimax_h3_i2v" || len(files2["first_frame"]) != 1 {
		t.Fatalf("无角色应走 i2v/first_frame, got %s %v", code2, files2)
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
