package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func newFaceDesignFixture(t *testing.T) (*ProjectService, models.Project, models.Character) {
	t.Helper()
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterFaceDesign{}, &models.CharacterFaceReference{}, &models.UploadFile{}, &models.Template{}, &models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	ps.upload = NewUploadManager(cfg, ps.db, remote)
	ps.skills = NewSkillService(ps.db)
	if err := ps.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	ps.tasks = &TaskService{} // Rejected submission tests never execute a real task.
	for _, name := range []string{"qwen_image_2_1_multi_edit.json", "minimax_h3_image_edit.json"} {
		tpl := loadTemplateForTest(t, name)
		if err := ps.db.Create(&tpl).Error; err != nil {
			t.Fatal(err)
		}
	}
	p := models.Project{Title: "人脸设计测试", Style: "国漫", Genre: "古典修仙"}
	if err := ps.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "新角色", Gender: "女性", Appearance: "12岁少女", ProfileStatus: models.ProfileStatusApproved, Portrait: "keep.png"}
	if err := ps.db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"father.png", "mother.png", "keep.png"} {
		if _, _, err := ps.upload.SaveFile(fmt.Sprint(p.ID), "image", file, []byte(file)); err != nil {
			t.Fatal(err)
		}
	}
	return ps, p, ch
}

func parentFaceInput(engine string) faceDesignInput {
	return faceDesignInput{Preset: "dual_parent", Engine: engine, Requirements: "眼型偏母亲", References: []models.CharacterFaceReference{{File: "father.png", Slot: "father"}, {File: "mother.png", Slot: "mother"}}}
}

func TestFaceDesignPreviewBindsFilesAndUsesGenericSkill(t *testing.T) {
	ps, p, ch := newFaceDesignFixture(t)
	for _, engine := range []string{AgeStagePortraitQwenEdit, AgeStagePortraitH3Edit} {
		token1, token2 := "<image1>", "<image2>"
		if engine == AgeStagePortraitH3Edit {
			token1, token2 = "<Picture 1>", "<Picture 2>"
		}
		provider := &captureTextProvider{response: token1 + "父亲，" + token2 + "母亲，设计12岁新角色的正面标准像"}
		ps.textProvider = provider
		in := parentFaceInput(engine)
		row, err := ps.PreviewFaceDesign(p.ID, ch.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if row.ID == 0 || len(row.References) != 2 || row.References[0].SHA256 != sha256Hex([]byte("father.png")) || row.References[1].Position != 2 {
			t.Fatalf("missing concrete version/order: %+v", row)
		}
		if !strings.Contains(row.Skill, "角色设定标准") || !strings.Contains(provider.system, "【当前项目 Skill】") || !strings.Contains(provider.user, "不是遗传预测") || !strings.Contains(provider.user, "眼型偏母亲") {
			t.Fatalf("skill/request missing: %+v %s", row, provider.user)
		}
		if strings.Contains(provider.user, "请为以下角色生成完整的结构化档案") {
			t.Fatal("full profile template leaked")
		}
		var saved models.CharacterFaceDesign
		if err := ps.db.Preload("References").First(&saved, row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if len(saved.References) != 2 || saved.ProfileHash == "" {
			t.Fatal("preview not persisted")
		}
		// Changing reference order creates a new record, never edits the old bundle.
		in.References[0], in.References[1] = in.References[1], in.References[0]
		next, err := ps.PreviewFaceDesign(p.ID, ch.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if next.ID == row.ID || next.References[0].File != "mother.png" {
			t.Fatal("order not preserved in new revision")
		}
	}
	var got models.Character
	ps.db.First(&got, ch.ID)
	if got.Portrait != "keep.png" || got.PortraitTaskID != "" {
		t.Fatal("preview changed selected portrait or started generation")
	}
	for _, e := range ps.faceDesignEngines() {
		if e.Code == AgeStagePortraitH3Edit && e.MaxCount != 100 {
			t.Fatalf("capacity not read from template: %+v", e)
		}
	}
}

func TestFaceDesignValidationAndStaleSubmission(t *testing.T) {
	ps, p, ch := newFaceDesignFixture(t)
	ps.textProvider = &captureTextProvider{response: "<image1>父亲与<image2>母亲参考，设计一个新角色"}
	invalid := parentFaceInput(AgeStagePortraitQwenEdit)
	invalid.References[1].Slot = "father"
	if _, err := ps.PreviewFaceDesign(p.ID, ch.ID, invalid); err == nil {
		t.Fatal("ambiguous parent roles accepted")
	}
	invalid = parentFaceInput(AgeStagePortraitQwenEdit)
	invalid.References[0].File = "../father.png"
	if _, err := ps.PreviewFaceDesign(p.ID, ch.ID, invalid); err == nil {
		t.Fatal("unsafe path accepted")
	}
	invalid = parentFaceInput(AgeStagePortraitQwenEdit)
	invalid.References[0].File = "foreign.png"
	if err := ps.db.Create(&models.UploadFile{TaskID: "999", Type: "image", Name: "foreign.png"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.PreviewFaceDesign(p.ID, ch.ID, invalid); err == nil {
		t.Fatal("foreign project reference accepted")
	}
	row, err := ps.PreviewFaceDesign(p.ID, ch.ID, parentFaceInput(AgeStagePortraitQwenEdit))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.StartFaceDesign(p.ID, ch.ID, row.ID, "missing reference tokens"); err == nil {
		t.Fatal("missing token accepted")
	}
	if err := ps.db.Model(&ch).Update("appearance", "13岁少女").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ps.StartFaceDesign(p.ID, ch.ID, row.ID, row.Prompt); err == nil || !strings.Contains(err.Error(), "资料已改变") {
		t.Fatalf("stale profile accepted: %v", err)
	}
	ps.db.Model(&ch).Update("appearance", "12岁少女")
	path := ps.upload.ResolveReadPath(fmt.Sprint(p.ID), "father.png")
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.StartFaceDesign(p.ID, ch.ID, row.ID, row.Prompt); err == nil || !strings.Contains(err.Error(), "内容改变") {
		t.Fatalf("changed image accepted: %v", err)
	}
	if _, err := ps.StartFaceDesign(p.ID+1, ch.ID, row.ID, row.Prompt); err == nil {
		t.Fatal("cross-project design accepted")
	}
	if used, err := faceDesignUsesFile(ps.db, p.ID, "father.png"); err != nil || !used {
		t.Fatalf("reference not protected: %v %v", used, err)
	}
	var tasks int64
	ps.db.Model(&models.Task{}).Count(&tasks)
	if tasks != 0 {
		t.Fatal("rejected submit created tasks")
	}
}

func TestFaceFusionSharesValidationAndPreservesDistinctIntent(t *testing.T) {
	in := parentFaceInput(AgeStagePortraitH3Edit)
	in.Preset = "multi_face_fusion"
	if err := validateFaceDesignInput(in, 100); err != nil {
		t.Fatal(err)
	}
	text := faceDesignRequest(&models.Project{}, &models.Character{Appearance: "40岁男性"}, in)
	for _, want := range []string{"独立的新人物", "40岁男性", "<Picture 1>", "<Picture 2>", "不是机械叠脸"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	in.References = append(in.References, in.References[0])
	if err := validateFaceDesignInput(in, 100); err == nil {
		t.Fatal("same face repeated")
	}
	in.References = in.References[:2]
	if err := validateFaceDesignInput(in, 1); err == nil {
		t.Fatal("capacity exceeded")
	}
}

func TestFaceDesignReferencesSurviveHistoryCleanup(t *testing.T) {
	ps, p, ch := newFaceDesignFixture(t)
	ps.textProvider = &captureTextProvider{response: "<image1>父亲，<image2>母亲，设计新角色"}
	if _, err := ps.PreviewFaceDesign(p.ID, ch.ID, parentFaceInput(AgeStagePortraitQwenEdit)); err != nil {
		t.Fatal(err)
	}
	if err := NewGeneratedMediaHistoryService(ps.db, ps.upload).removeUnreferencedProjectFile(p.ID, "father.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ps.upload.ResolveReadPath(fmt.Sprint(p.ID), "father.png")); err != nil {
		t.Fatalf("design reference deleted by cleanup: %v", err)
	}
	if err := validateProjectImageUpload(ps.db, p.ID, "father.png"); err != nil {
		t.Fatal("protected upload registration lost")
	}
}

func TestFaceDesignTaskFailureKeepsOldSelectedPortrait(t *testing.T) {
	ps, p, ch := newFaceDesignFixture(t)
	ps.tasks = nil
	row := models.CharacterFaceDesign{ProjectID: p.ID, CharacterID: ch.ID, TaskID: "face-failed"}
	if err := ps.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&models.Task{TaskID: "face-failed", Status: "failed", Error: "fixture failure"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Model(&ch).Update("portrait_task_id", "face-failed").Error; err != nil {
		t.Fatal(err)
	}
	ps.syncCharacterPortraits()
	var got models.Character
	if err := ps.db.First(&got, ch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Portrait != "keep.png" || got.PortraitTaskID != "" || got.PortraitError != "fixture failure" {
		t.Fatalf("failed new design must clear task without losing old image: %+v", got)
	}
}
