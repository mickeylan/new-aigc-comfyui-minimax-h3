package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func newInstanceTestManager(t *testing.T) *InstanceManager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Instance{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	return NewInstanceManager(&config.Config{}, db, NewRemoteExec(config.RemoteConfig{}))
}

func countInstances(t *testing.T, mgr *InstanceManager) int64 {
	t.Helper()
	var n int64
	if err := mgr.db.Model(&models.Instance{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRegisterEndpointCreatesUnmanagedInstanceWithOwnSlot(t *testing.T) {
	mgr := newInstanceTestManager(t)

	inst, err := mgr.RegisterEndpoint(" 10.0.0.5 ", 8199)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Host != "10.0.0.5" {
		t.Fatalf("host 未去空格：%q", inst.Host)
	}
	if inst.Port != 8199 {
		t.Fatalf("port 不符：%d", inst.Port)
	}
	if inst.Managed || inst.EnableManager {
		t.Fatalf("外部端点不能是平台管理实例：managed=%v enable_manager=%v", inst.Managed, inst.EnableManager)
	}
	if inst.GPUIndex != 0 {
		t.Fatalf("首个外部端点应占槽位 0，实际 %d", inst.GPUIndex)
	}

	// 库里也必须真的写着 false：GORM 零值插入会落成 default:true，这里专门覆盖那个坑。
	var stored models.Instance
	if err := mgr.db.First(&stored, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Managed || stored.EnableManager {
		t.Fatalf("落库后 managed=%v enable_manager=%v，说明零值被默认值覆盖", stored.Managed, stored.EnableManager)
	}

	// 第二个端点拿到不同槽位，避免唯一索引冲突。
	second, err := mgr.RegisterEndpoint("10.0.0.6", 8199)
	if err != nil {
		t.Fatal(err)
	}
	if second.GPUIndex == inst.GPUIndex {
		t.Fatalf("两个端点占用了同一槽位 %d", second.GPUIndex)
	}
}

func TestRegisterEndpointIsIdempotentForSameEndpoint(t *testing.T) {
	mgr := newInstanceTestManager(t)

	first, err := mgr.RegisterEndpoint("10.0.0.7", 8200)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mgr.RegisterEndpoint("10.0.0.7", 8200)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("同一 host:port 重复登记应复用记录：first=%d second=%d", first.ID, second.ID)
	}
	if n := countInstances(t, mgr); n != 1 {
		t.Fatalf("重复登记产生了多余记录，共 %d 条", n)
	}
}

func TestRegisterEndpointReusesLocalInstanceForLoopbackHost(t *testing.T) {
	mgr := newInstanceTestManager(t)

	local := models.Instance{GPUIndex: 0, Host: "", Port: 8188, Managed: true, Status: "running"}
	if err := mgr.db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}

	// 空 host 属于非法输入（下面单独断言），回环等价只针对显式回环写法。
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		inst, err := mgr.RegisterEndpoint(host, 8188)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if inst.ID != local.ID {
			t.Fatalf("回环地址 %s 应复用本机实例 %d，实际拿到 %d", host, local.ID, inst.ID)
		}
	}
	if n := countInstances(t, mgr); n != 1 {
		t.Fatalf("回环地址登记出多余实例，共 %d 条", n)
	}
}

func TestRegisterEndpointRejectsInvalidInput(t *testing.T) {
	mgr := newInstanceTestManager(t)

	if _, err := mgr.RegisterEndpoint("   ", 8199); err == nil {
		t.Fatal("空 host 必须报错")
	}
	if _, err := mgr.RegisterEndpoint("10.0.0.8", 0); err == nil {
		t.Fatal("端口 0 必须报错")
	}
	if _, err := mgr.RegisterEndpoint("10.0.0.8", 70000); err == nil {
		t.Fatal("越界端口必须报错")
	}
	if n := countInstances(t, mgr); n != 0 {
		t.Fatalf("非法输入不应落库，实际 %d 条", n)
	}
}

func TestDeleteInstanceRefusesManagedOrBusyEndpoint(t *testing.T) {
	mgr := newInstanceTestManager(t)

	managed := models.Instance{GPUIndex: 0, Host: "10.0.0.9", Port: 8201, Managed: true, Status: "running"}
	if err := mgr.db.Create(&managed).Error; err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeleteInstance(managed.ID); err == nil || !strings.Contains(err.Error(), "平台管理") {
		t.Fatalf("平台管理的实例必须拒绝删除，实际 err=%v", err)
	}

	// 外部端点走真实登记路径创建：db.Create 直接写 managed=false 会被 GORM 的
	// default:true 覆盖，RegisterEndpoint 内部用 map 更新才是实际生效写法。
	external, err := mgr.RegisterEndpoint("10.0.0.10", 8202)
	if err != nil {
		t.Fatal(err)
	}
	busy := models.Task{InstanceID: &external.ID, Status: "running"}
	if err := mgr.db.Create(&busy).Error; err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeleteInstance(external.ID); err == nil || !strings.Contains(err.Error(), "在跑") {
		t.Fatalf("有在跑任务的实例必须拒绝删除，实际 err=%v", err)
	}

	// 任务结束后可以删除。
	if err := mgr.db.Model(&models.Task{}).Where("id = ?", busy.ID).Update("status", "success").Error; err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeleteInstance(external.ID); err != nil {
		t.Fatalf("空闲外部实例应可删除：%v", err)
	}
	if n := countInstances(t, mgr); n != 1 {
		t.Fatalf("删除后只应剩平台实例，实际 %d 条", n)
	}
}

func TestResolveOutputFileFallsBackToStandardOutputDir(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("AnimateDiff", "clip_00001.mp4")
	standard := filepath.Join(root, "output", rel)
	if err := os.MkdirAll(filepath.Dir(standard), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(standard, []byte("glb"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		Cfg:    &config.Config{Comfy: config.ComfyConfig{ComfyDir: root}},
		Remote: NewRemoteExec(config.RemoteConfig{}),
	}

	got, err := svc.resolveOutputFile("0", rel)
	if err != nil {
		t.Fatalf("默认 output 目录下的产物应能找到：%v", err)
	}
	if got != standard {
		t.Fatalf("解析结果不符：got=%q want=%q", got, standard)
	}

	// worker 目录优先：两处都有时取 output_workers/gpuN。
	worker := filepath.Join(root, "output_workers", "gpu0", rel)
	if err := os.MkdirAll(filepath.Dir(worker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worker, []byte("glb"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = svc.resolveOutputFile("0", rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != worker {
		t.Fatalf("worker 目录应优先：got=%q want=%q", got, worker)
	}

	if _, err := svc.resolveOutputFile("0", filepath.Join("missing", "nope.glb")); err == nil {
		t.Fatal("不存在的产物必须报错，而不是回落到某个目录继续读")
	}
}

func TestFindTaskOutputsClassifies3DFiles(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "task-3d")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// findTaskOutputs 靠文件名包含 taskID 匹配（模板产物名形如 character_<id>.glb），
	// 所以文件名里必须带上任务 ID。
	for _, name := range []string{"mesh_task-3d.glb", "mesh_task-3d.gltf", "cloud_task-3d.ply", "splat_task-3d.splat", "shot_task-3d.mp4"} {
		if err := os.WriteFile(filepath.Join(taskDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	svc := &TaskService{
		cfg:    &config.Config{Comfy: config.ComfyConfig{ComfyDir: root}},
		remote: NewRemoteExec(config.RemoteConfig{}),
	}
	outputs := svc.findTaskOutputs(root, "task-3d")

	byType := map[string]int{}
	for _, item := range outputs {
		byType[item["type"]]++
	}
	if byType["3d"] != 4 {
		t.Fatalf("3D 产物应识别 4 个，实际 %d（%v）", byType["3d"], byType)
	}
	if byType["videos"] != 1 {
		t.Fatalf("视频产物应识别 1 个，实际 %d（%v）", byType["videos"], byType)
	}
}