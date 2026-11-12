package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestAssetReferenceEngineAcceptsMossOnlyForAssets(t *testing.T) {
	if got, err := normalizeAssetReferenceEngine(ImageEngineMoss); err != nil || got != ImageEngineMoss {
		t.Fatalf("asset reference must accept Moss: %s %v", got, err)
	}
	// 道具/场景默认 Moss：Krea2 生成的中文基本不可读。
	if got, _ := normalizeAssetReferenceEngine(""); got != ImageEngineMoss {
		t.Fatalf("asset reference default=%s", got)
	}
	if got, _ := normalizeAssetReferenceEngine(ImageEngineKrea2); got != ImageEngineKrea2 {
		t.Fatalf("explicit Krea2 must stay selectable, got %s", got)
	}
	// 标准照与换装没有 Moss 工作流，必须拒绝而不是静默改用 Krea2。
	if _, err := normalizeAssetImageEngine(ImageEngineMoss); err == nil {
		t.Fatal("portrait/outfit engine accepted Moss")
	}
}

func TestNewAssetsDefaultToMoss(t *testing.T) {
	ps := newTestProjectService(t)
	project := models.Project{Title: "问仙"}
	ps.db.Create(&project)
	manual, err := ps.CreateAsset(models.Asset{ProjectID: project.ID, Kind: AssetKindLocation, Name: "天阙宗山门"})
	if err != nil {
		t.Fatal(err)
	}
	if manual.ImageEngine != ImageEngineMoss {
		t.Fatalf("manual asset engine=%s", manual.ImageEngine)
	}
	if err := ps.upsertProductionEntitiesFromScenes(project.ID, []scriptScene{{Location: "临江客栈", Props: []string{"青玉令牌"}}}); err != nil {
		t.Fatal(err)
	}
	var auto []models.Asset
	ps.db.Where("project_id = ? AND source = ?", project.ID, "auto").Find(&auto)
	if len(auto) != 2 {
		t.Fatalf("auto assets=%+v", auto)
	}
	for _, a := range auto {
		if a.ImageEngine != ImageEngineMoss {
			t.Fatalf("auto %s %s engine=%s", a.Kind, a.Name, a.ImageEngine)
		}
	}
}

func TestStartAssetImageUsesMossTextToImage(t *testing.T) {
	// 每种类型独立建库：后台执行协程会占用内存 SQLite 连接。
	for _, tc := range []struct {
		kind, ratio string
	}{{AssetKindProp, "1:1 (Square)"}, {AssetKindLocation, "16:9 (Widescreen)"}} {
		ps := newSheetGenerationService(t)
		tpl := loadTemplateForTest(t, "moss_image_t2i.json")
		if err := ps.db.Create(&tpl).Error; err != nil {
			t.Fatal(err)
		}
		p := models.Project{Title: "问仙", Style: "国风写实", AspectRatio: "16:9"}
		ps.db.Create(&p)
		a, err := ps.CreateAsset(models.Asset{ProjectID: p.ID, Kind: tc.kind, Name: "青铜古镜" + tc.kind, Description: "铜绿斑驳", ImageEngine: ImageEngineMoss})
		if err != nil {
			t.Fatal(err)
		}
		if a.ImageEngine != ImageEngineMoss {
			t.Fatalf("Moss engine not persisted: %s", a.ImageEngine)
		}
		if err := ps.StartAssetImage(a); err != nil {
			t.Fatal(err)
		}
		var saved models.Asset
		ps.db.First(&saved, a.ID)
		var task models.Task
		if err := ps.db.Where("task_id = ?", saved.ImageTaskID).First(&task).Error; err != nil {
			t.Fatalf("asset task not recorded: %v", err)
		}
		if task.TemplateName != tpl.Name {
			t.Fatalf("%s must use the Moss template, got %s", tc.kind, task.TemplateName)
		}
		var params map[string]any
		if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
			t.Fatal(err)
		}
		if params["aspect_ratio"] != tc.ratio {
			t.Fatalf("%s Moss ratio=%v want %s", tc.kind, params["aspect_ratio"], tc.ratio)
		}
		workflow, err := ps.tasks.RenderWorkflow(&tpl, params)
		if err != nil {
			t.Fatalf("Moss asset workflow must render: %v", err)
		}
		encoded, _ := json.Marshal(workflow)
		if strings.Contains(string(encoded), "{{") || !strings.Contains(string(encoded), "moss_image") {
			t.Fatalf("Moss asset workflow left placeholders or lost its CLIP type: %s", encoded)
		}
	}
}
