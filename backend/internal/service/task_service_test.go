package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func loadTemplateForTest(t *testing.T, filename string) models.Template {
	t.Helper()
	data, err := templateFS.ReadFile("templates/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var tf templateFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatal(err)
	}
	inputs, _ := json.Marshal(tf.Inputs)
	workflow, _ := json.Marshal(tf.Workflow)
	return models.Template{Name: tf.Name, Code: tf.Code, InputsJSON: string(inputs), WorkflowJSON: string(workflow), Enabled: true}
}

func baseParams() map[string]any {
	return map[string]any{
		"prompt": "测试镜头", "width": float64(1280), "height": float64(704),
		"duration": float64(5), "steps": float64(20), "seed": float64(1),
		"cfg": float64(1), "fps": float64(24), "ref_image_size": "match", "_task_id": "test-task",
	}
}

func TestMiniMaxH3TemplatesRenderWithoutPlaceholders(t *testing.T) {
	tests := []struct {
		file  string
		files map[string][]FileMeta
	}{
		{file: "minimax_h3_t2v.json"},
		{file: "minimax_h3_i2v.json", files: map[string][]FileMeta{
			"first_frame": {{TaskID: "draft", Name: "first.png"}},
		}},
		{file: "minimax_h3_first_last.json", files: map[string][]FileMeta{
			"first_frame": {{TaskID: "draft", Name: "first.png"}},
			"last_frame":  {{TaskID: "draft", Name: "last.png"}},
		}},
		{file: "minimax_h3_ref2v.json", files: map[string][]FileMeta{
			"ref_images": {{TaskID: "draft", Name: "ref.png"}},
			"ref_videos": {{TaskID: "draft", Name: "ref.mp4"}},
			"ref_audios": {{TaskID: "draft", Name: "ref.wav"}},
		}},
		{file: "krea2_character_portrait.json"},
		{file: "krea2_character_sheet.json", files: map[string][]FileMeta{
			"source_image": {{TaskID: "draft", Name: "portrait.png"}},
		}},
		{file: "minimax_h3_storyboard_candidates_selflift.json", files: map[string][]FileMeta{
			"ref_images": {{TaskID: "draft", Name: "character.png"}, {TaskID: "draft", Name: "location.png"}},
		}},
		{file: "minimax_h3_look_reference.json", files: map[string][]FileMeta{
			"ref_images": {{TaskID: "draft", Name: "portrait.png"}, {TaskID: "draft", Name: "clothing.png"}},
		}},
	}

	service := &TaskService{}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			tpl := loadTemplateForTest(t, tt.file)
			params := baseParams()
			if err := normalizeTemplateFiles(&tpl, params, tt.files); err != nil {
				t.Fatal(err)
			}
			workflow, err := service.RenderWorkflow(&tpl, params)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(workflow)
			if strings.Contains(string(encoded), "{{") {
				t.Fatalf("工作流仍有未渲染占位符: %s", encoded)
			}
		})
	}
}

func TestTemplateScalarDefaultsCanBeOverridden(t *testing.T) {
	tpl := loadTemplateForTest(t, "krea2_character_portrait.json")
	params := baseParams()
	if err := normalizeTemplateFiles(&tpl, params, nil); err != nil {
		t.Fatal(err)
	}
	if params["unet_name"] == nil {
		t.Fatal("模型默认值未注入")
	}
	params["unet_name"] = "Krea2/new-version.safetensors"
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(workflow)
	if !strings.Contains(string(encoded), "Krea2/new-version.safetensors") {
		t.Fatalf("模型覆盖未生效: %s", encoded)
	}
}

func TestReferenceFilesExpandToIndexedPlaceholders(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v.json")
	params := baseParams()
	files := map[string][]FileMeta{
		"ref_images": {
			{TaskID: "draft", Name: "one.png"},
			{TaskID: "draft", Name: "two.png"},
		},
	}
	if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"one.png", "two.png"} {
		key := fmt.Sprintf("ref_image_%d", i)
		ref, ok := params[key].(map[string]any)
		if !ok || ref["name"] != name {
			t.Fatalf("%s 未正确展开: %#v", key, params[key])
		}
	}
}

func TestRef2VWorkflowConnectsAllThreeImagesInOrder(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v.json")
	params := baseParams()
	files := map[string][]FileMeta{"ref_images": {
		{TaskID: "1", Name: "char-sheet.png"},
		{TaskID: "1", Name: "location.png"},
		{TaskID: "1", Name: "storyboard.png"},
	}}
	if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
		t.Fatal(err)
	}
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	nodeIDs := []string{"137", "139", "144"}
	for i, tc := range []struct{ node, name string }{{"137", "1/char-sheet.png"}, {"139", "1/location.png"}, {"144", "1/storyboard.png"}} {
		node := workflow[tc.node].(map[string]any)
		inputs := node["inputs"].(map[string]any)
		if inputs["image"] != tc.name {
			t.Fatalf("ref_image_%d rendered as %#v, want %s", i, inputs["image"], tc.name)
		}
	}
	refInputs := workflow["416"].(map[string]any)["inputs"].(map[string]any)
	for i, nodeID := range nodeIDs {
		ref := refInputs[fmt.Sprintf("ref_images.ref_image_%d", i)].([]any)
		if ref[0] != nodeID {
			t.Fatalf("ref_image_%d connects node %#v, want %s", i, ref[0], nodeID)
		}
	}
	for _, nodeID := range []string{"156", "334", "394", "417", "333", "398", "399", "400", "397"} {
		if workflow[nodeID] == nil {
			t.Fatalf("verified SelfLift node %s missing", nodeID)
		}
	}
	if workflow["7"] != nil {
		t.Fatal("legacy KSampler node must not remain in SelfLift ref2v template")
	}
}

func TestStoryboardSelfLiftWorkflowConnectsReferenceImages(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_storyboard_candidates_selflift.json")
	params := baseParams()
	files := map[string][]FileMeta{"ref_images": {
		{TaskID: "1", Name: "char-sheet.png"},
		{TaskID: "1", Name: "location.png"},
	}}
	if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
		t.Fatal(err)
	}
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ node, name string }{{"137", "1/char-sheet.png"}, {"139", "1/location.png"}} {
		node := workflow[tc.node].(map[string]any)
		inputs := node["inputs"].(map[string]any)
		if inputs["image"] != tc.name {
			t.Fatalf("ref_image_%d rendered as %#v, want %s", i, inputs["image"], tc.name)
		}
	}
	refInputs := workflow["416"].(map[string]any)["inputs"].(map[string]any)
	for i, nodeID := range []string{"137", "139"} {
		key := fmt.Sprintf("ref_images.ref_image_%d", i)
		ref, ok := refInputs[key].([]any)
		if !ok || ref[0] != nodeID {
			t.Fatalf("%s not connected to node %s: %#v", key, nodeID, refInputs[key])
		}
	}
	if _, nested := refInputs["ref_images"]; nested {
		t.Fatal("SelfLift API workflow must use flattened Autogrow keys, not nested ref_images")
	}
}

func TestRequiredTemplateFilesAreValidated(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_first_last.json")
	err := normalizeTemplateFiles(&tpl, baseParams(), map[string][]FileMeta{
		"first_frame": {{TaskID: "draft", Name: "first.png"}},
	})
	if err == nil || !strings.Contains(err.Error(), "尾帧图片") {
		t.Fatalf("预期缺少尾帧校验错误，实际为 %v", err)
	}
}

func TestUploadedFileTypeMustMatchTemplateInputFamily(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	upload := models.UploadFile{TaskID: "draft", Type: "audio", Name: "spoof.png", Path: "draft/spoof.png"}
	if err := db.Create(&upload).Error; err != nil {
		t.Fatal(err)
	}
	tpl := models.Template{InputsJSON: `[{"key":"first_frame","type":"image","label":"首帧"}]`}
	err = (&TaskService{db: db}).validateUploadedFiles(&tpl, map[string][]FileMeta{
		"first_frame": {{TaskID: upload.TaskID, Name: upload.Name}},
	})
	if err == nil || !strings.Contains(err.Error(), "类型不匹配") {
		t.Fatalf("expected type-family mismatch, got %v", err)
	}
}

func TestRankInstanceLoadsPrefersQueueThenVRAM(t *testing.T) {
	loads := []instanceLoad{
		{inst: models.Instance{GPUIndex: 0}, queueLen: 1, vramFree: 80, up: true},
		{inst: models.Instance{GPUIndex: 1}, queueLen: 0, vramFree: 40, up: true},
		{inst: models.Instance{GPUIndex: 2}, queueLen: 0, vramFree: 60, up: true},
		{inst: models.Instance{GPUIndex: 3}, queueLen: 0, vramFree: 90, up: false},
	}
	rankInstanceLoads(loads)
	want := []int{2, 1, 0, 3}
	for i, gpu := range want {
		if loads[i].inst.GPUIndex != gpu {
			t.Fatalf("位置 %d: 期望 GPU %d，实际 GPU %d", i, gpu, loads[i].inst.GPUIndex)
		}
	}
}

func newComfyLoadServer(t *testing.T, queueLen int, vramFree int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/queue":
			running := make([]any, queueLen)
			_ = json.NewEncoder(w).Encode(map[string]any{"queue_running": running, "queue_pending": []any{}})
		case "/system_stats":
			_ = json.NewEncoder(w).Encode(map[string]any{"devices": []any{map[string]any{
				"vram_free": vramFree, "vram_total": int64(96 << 30),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func comfyTestPort(server *httptest.Server) int {
	return server.Listener.Addr().(*net.TCPAddr).Port
}

func TestPickInstanceDiscoversReachableInstanceWithStaleStoppedStatus(t *testing.T) {
	idle := newComfyLoadServer(t, 0, 30<<30)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Instance{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	instance := models.Instance{GPUIndex: 0, Port: comfyTestPort(idle), Status: "stopped"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	remote := NewRemoteExec(config.RemoteConfig{})
	svc := &TaskService{cfg: cfg, db: db, manager: NewInstanceManager(cfg, db, remote)}
	selected, err := svc.pickInstance(nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.GPUIndex != 0 || selected.Status != "running" {
		t.Fatalf("selected = %+v", selected)
	}
	var stored models.Instance
	if err := db.First(&stored, instance.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "running" {
		t.Fatalf("stored status = %q", stored.Status)
	}
}

func TestPickInstanceRequiresPlatformAndComfyQueuesIdle(t *testing.T) {
	externalBusy := newComfyLoadServer(t, 1, 90<<30)
	idle := newComfyLoadServer(t, 0, 30<<30)
	platformBusy := newComfyLoadServer(t, 0, 80<<30)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Instance{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	instances := []models.Instance{
		{GPUIndex: 0, Port: comfyTestPort(externalBusy), Status: "running"},
		{GPUIndex: 1, Port: comfyTestPort(idle), Status: "running"},
		{GPUIndex: 2, Port: comfyTestPort(platformBusy), Status: "running"},
	}
	if err := db.Create(&instances).Error; err != nil {
		t.Fatal(err)
	}
	busyPort := instances[2].Port
	mismatchedGPU := 99
	if err := db.Create(&models.Task{
		TaskID: "platform-busy", Status: "running", Port: &busyPort, GPUIndex: &mismatchedGPU,
	}).Error; err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	remote := NewRemoteExec(config.RemoteConfig{})
	svc := &TaskService{cfg: cfg, db: db, manager: NewInstanceManager(cfg, db, remote)}
	selected, err := svc.pickInstance(nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected.GPUIndex != 1 {
		t.Fatalf("应跳过平台占用端口和 ComfyUI 非空队列，实际选择 GPU %d", selected.GPUIndex)
	}
}

func TestReserveInstanceDistributesConcurrentTasksAcrossFreeGPUs(t *testing.T) {
	gpu0 := newComfyLoadServer(t, 0, 60<<30)
	gpu1 := newComfyLoadServer(t, 0, 70<<30)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Instance{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	instances := []models.Instance{
		{GPUIndex: 0, Port: comfyTestPort(gpu0), Status: "running"},
		{GPUIndex: 1, Port: comfyTestPort(gpu1), Status: "running"},
	}
	if err := db.Create(&instances).Error; err != nil {
		t.Fatal(err)
	}
	tasks := []models.Task{{TaskID: "batch-a", Status: "pending"}, {TaskID: "batch-b", Status: "pending"}}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	remote := NewRemoteExec(config.RemoteConfig{})
	svc := &TaskService{cfg: cfg, db: db, manager: NewInstanceManager(cfg, db, remote)}

	start := make(chan struct{})
	assigned := make(chan int, len(tasks))
	errs := make(chan error, len(tasks))
	var wg sync.WaitGroup
	for i := range tasks {
		wg.Add(1)
		go func(task *models.Task) {
			defer wg.Done()
			<-start
			inst, err := svc.reserveInstance(task, false, nil)
			if err != nil {
				errs <- err
				return
			}
			assigned <- inst.GPUIndex
		}(&tasks[i])
	}
	close(start)
	wg.Wait()
	close(errs)
	close(assigned)
	for err := range errs {
		t.Fatalf("并发预占失败: %v", err)
	}
	got := map[int]bool{}
	for gpu := range assigned {
		got[gpu] = true
	}
	if len(got) != 2 || !got[0] || !got[1] {
		t.Fatalf("两个并发任务应分配到不同 GPU，实际为 %v", got)
	}
}

func TestQueueRetryWhenFreeSingleLoop(t *testing.T) {
	ts := &TaskService{retrying: map[string]bool{}}
	ts.queueRetryWhenFree("t1")
	// 同一任务重复触发不启动新循环
	ts.queueRetryWhenFree("t1")
	ts.retryMu.Lock()
	active := ts.retrying["t1"]
	ts.retryMu.Unlock()
	if !active {
		t.Fatalf("重试循环应处于运行状态")
	}
	// 模拟任务变为终态后循环退出
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatal(err)
	}
	ts.db = db
	ts.db.Create(&models.Task{TaskID: "t1", Status: "cancelled"})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ts.retryMu.Lock()
		done := !ts.retrying["t1"]
		ts.retryMu.Unlock()
		if done {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("任务取消后重试循环未退出")
}

func TestVideoConcurrencyLimit(t *testing.T) {
	if !isVideoTemplateCode("minimax_h3_i2v") || !isVideoTemplateCode("minimax_h3_ref2v") ||
		!isVideoTemplateCode("minimax_h3_t2v") || !isVideoTemplateCode("minimax_h3_first_last") {
		t.Fatal("视频模板识别错误")
	}
	if isVideoTemplateCode("seedream_text2img") {
		t.Fatal("非视频模板不应识别为视频")
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}, &models.Task{}, &models.Setting{}); err != nil {
		t.Fatal(err)
	}
	tpl := models.Template{Code: "minimax_h3_i2v", Enabled: true}
	db.Create(&tpl)
	db.Create(&models.Setting{Key: "video_concurrency", Value: "2"})

	ts := &TaskService{db: db, retrying: map[string]bool{}}
	if ts.videoConcurrency() != 2 {
		t.Fatalf("videoConcurrency 应读平台设置 2, got %d", ts.videoConcurrency())
	}
	// 2 个运行中任务 = 已达上限
	db.Create(&models.Task{TaskID: "a", TemplateID: tpl.ID, Status: "running"})
	db.Create(&models.Task{TaskID: "b", TemplateID: tpl.ID, Status: "queued"})
	if ts.runningVideoTaskCount() != 2 {
		t.Fatalf("runningVideoTaskCount = %d, want 2", ts.runningVideoTaskCount())
	}
	// 非视频模板任务不计入
	db.Create(&models.Template{Code: "img2img", Enabled: true})
	db.Create(&models.Task{TaskID: "c", Status: "running"})
	if ts.runningVideoTaskCount() != 2 {
		t.Fatalf("非视频任务不应计入, got %d", ts.runningVideoTaskCount())
	}
	// 设置缺失回退默认 4
	db.Delete(&models.Setting{}, "key = ?", "video_concurrency")
	if ts.videoConcurrency() != 4 {
		t.Fatalf("默认并发应为 4, got %d", ts.videoConcurrency())
	}
}

func TestRemoteLocalRoots(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "share")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRemoteExec(config.RemoteConfig{Host: "remote-host"}) // 模拟远程模式
	r.SetLocalRoots([]string{sub, "/nonexistent-root-xyz"})
	if !r.localPath(sub + "/input/x.png") {
		t.Fatal("命中本地根应返回 true")
	}
	if r.localPath("/opt/comfyUI/input/x.png") {
		t.Fatal("未挂载的路径不应命中本地")
	}
	// 本地直写直读
	content := []byte("hello")
	if err := r.WriteFile(filepath.Join(sub, "a/b.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := r.Open(filepath.Join(sub, "a/b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if string(data) != "hello" {
		t.Fatalf("本地直读内容不符: %q", data)
	}
	if fi, err := r.Stat(filepath.Join(sub, "a/b.txt")); err != nil || fi.Size() != 5 {
		t.Fatalf("本地 Stat 失败: %v %v", fi, err)
	}
}
