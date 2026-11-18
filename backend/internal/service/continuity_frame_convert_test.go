package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type frameConvertFixture struct {
	ps     *ProjectService
	upload *UploadManager
	source models.Scene
	frame  models.FrameCandidate
}

func newFrameConvertFixture(t *testing.T) frameConvertFixture {
	t.Helper()
	ps := newSheetGenerationService(t)
	// :memory: is private to each SQLite connection. Background Execute and the
	// next HTTP request must see the same migrated test database.
	sqlDB, err := ps.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := ps.db.AutoMigrate(&models.FrameCandidate{}, &models.SceneContinuity{}, &models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	tpl := loadTemplateForTest(t, "qwen_image_2_1_multi_edit.json")
	if err := ps.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, ps.db, remote)
	ps.upload = upload
	ps.continuity = NewContinuityService(cfg, ps.db, remote, upload)
	ps.skills = NewSkillService(ps.db)
	if err := ps.skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "续接"}
	ps.db.Create(&p)
	source := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, VideoTaskID: "video-1", Status: "video_ready"}
	ps.db.Create(&source)
	if _, _, err := upload.SaveFile(fmt.Sprint(p.ID), "image", "tail.png", testPNG(t, 64, 36)); err != nil {
		t.Fatal(err)
	}
	frame := models.FrameCandidate{ProjectID: p.ID, SceneID: source.ID, VideoTaskID: "video-1", Type: models.FrameCandidateSelected, FrameIndex: 3, ImageFile: "tail.png", Source: "extracted"}
	ps.db.Create(&frame)
	return frameConvertFixture{ps: ps, upload: upload, source: source, frame: frame}
}

func TestContinuityFrameConvertSkillsAreSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	for code, phrase := range map[string]string{continuityFrameClaySkillCode: "纯白哑光黏土模型渲染", continuityFrameLineartSkillCode: "黑线白底结构线稿"} {
		var skill models.Skill
		if err := db.Where("code = ?", code).First(&skill).Error; err != nil {
			t.Fatal(err)
		}
		if skill.Version != 1 || skill.Stage != models.SkillStageQwenImageEdit || skill.ModelFamilies != models.ModelFamilyQwenImage21 || !strings.Contains(skill.SystemPrompt, phrase) {
			t.Fatalf("%s skill=%+v", code, skill)
		}
	}
}

func TestClosestQwenAspectRejectsDistortingCanvas(t *testing.T) {
	if got, err := closestQwenAspect(1280.0 / 720); err != nil || got != "16:9 (Widescreen)" {
		t.Fatalf("16:9 frame got=%q err=%v", got, err)
	}
	if got, err := closestQwenAspect(1.83); err != nil || got != "16:9 (Widescreen)" {
		t.Fatalf("3%% drift must still map to 16:9, got=%q err=%v", got, err)
	}
	if _, err := closestQwenAspect(3.0); err == nil || !strings.Contains(err.Error(), "4%") {
		t.Fatalf("a 3:1 frame has no canvas within 4%%, err=%v", err)
	}
}

func TestStartFrameConversionKeepsSourceAndRejectsConcurrentConversion(t *testing.T) {
	f := newFrameConvertFixture(t)
	if _, err := f.ps.StartFrameConversion(f.source.ProjectID, f.source.ID, f.frame.ID, "sketch"); err == nil {
		t.Fatal("unknown reference form must be rejected")
	}
	got, err := f.ps.StartFrameConversion(f.source.ProjectID, f.source.ID, f.frame.ID, FrameReferenceClay)
	if err != nil {
		t.Fatal(err)
	}
	var task models.Task
	if err := f.ps.db.Where("task_id = ?", got.ConvertTaskID).First(&task).Error; err != nil {
		t.Fatalf("conversion task not recorded: %v", err)
	}
	if !strings.Contains(task.InputsJSON, "tail.png") || !strings.Contains(task.ParamsJSON, "16:9 (Widescreen)") || !strings.Contains(task.Prompt, "纯白哑光黏土模型渲染") {
		t.Fatalf("task inputs=%s params=%s prompt=%s", task.InputsJSON, task.ParamsJSON, task.Prompt)
	}
	if _, err := f.ps.StartFrameConversion(f.source.ProjectID, f.source.ID, f.frame.ID, FrameReferenceLineart); err != errFrameConvertActive {
		t.Fatalf("a second conversion of the same frame must conflict, err=%v", err)
	}
	var saved models.FrameCandidate
	f.ps.db.First(&saved, f.frame.ID)
	if saved.ImageFile != "tail.png" || saved.Source != "extracted" || saved.Type != models.FrameCandidateSelected || saved.ConvertSourceSHA256 == "" {
		t.Fatalf("source frame must stay untouched while converting: %+v", saved)
	}
}

func claimedFrame(t *testing.T, f frameConvertFixture, kind string) *models.FrameCandidate {
	t.Helper()
	sum := sha256.Sum256(testPNG(t, 64, 36))
	frame := f.frame
	frame.ConvertTaskID, frame.ConvertKind, frame.ConvertSourceSHA256 = "convert-task", kind, hex.EncodeToString(sum[:])
	return &frame
}

func TestApplyFrameConversionAddsDerivedRowAndRejectsDriftOrChangedSource(t *testing.T) {
	f := newFrameConvertFixture(t)
	pid := fmt.Sprint(f.source.ProjectID)
	f.upload.SaveFile(pid, "image", "clay.png", testPNG(t, 66, 36))
	f.upload.SaveFile(pid, "image", "square.png", testPNG(t, 36, 36))
	if err := f.ps.applyFrameConversion(claimedFrame(t, f, FrameReferenceClay), "square.png"); err == nil || !strings.Contains(err.Error(), "4%") {
		t.Fatalf("ratio drift must discard the result, err=%v", err)
	}
	if err := f.ps.applyFrameConversion(claimedFrame(t, f, FrameReferenceClay), "clay.png"); err != nil {
		t.Fatal(err)
	}
	derived, err := f.ps.continuity.DerivedReference(&f.frame, FrameReferenceClay)
	if err != nil || derived == nil {
		t.Fatalf("derived=%+v err=%v", derived, err)
	}
	if derived.ImageFile != "clay.png" || derived.Type != models.FrameCandidateDerived || derived.FrameIndex <= f.frame.FrameIndex || derived.DerivedSourceFile != "tail.png" {
		t.Fatalf("derived row=%+v", derived)
	}
	var source models.FrameCandidate
	f.ps.db.First(&source, f.frame.ID)
	if source.ImageFile != "tail.png" || source.Source != "extracted" {
		t.Fatalf("source frame changed: %+v", source)
	}
	// Derived rows are not tail candidates and cannot be selected.
	if frames, _ := f.ps.continuity.ListFrames(f.source.ProjectID, f.source.ID); len(frames) != 1 || frames[0].ID != f.frame.ID {
		t.Fatalf("listed frames=%+v", frames)
	}
	if _, err := f.ps.continuity.SelectFrame(f.source.ProjectID, f.source.ID, derived.ID); err == nil {
		t.Fatal("a derived reference must not be selectable as the tail frame")
	}
	// The source image changed after submission: the result must be discarded.
	f.upload.SaveFile(pid, "image", "tail.png", testPNG(t, 128, 72))
	if err := f.ps.applyFrameConversion(claimedFrame(t, f, FrameReferenceLineart), "clay.png"); err == nil || !strings.Contains(err.Error(), "已变化") {
		t.Fatalf("changed source must discard the result, err=%v", err)
	}
	if d, _ := f.ps.continuity.DerivedReference(&f.frame, FrameReferenceLineart); d != nil {
		t.Fatalf("discarded result was recorded: %+v", d)
	}
}

func TestClayReferenceFormReplacesStoryboardSlotWithoutBecomingOpeningFrame(t *testing.T) {
	f := newFrameConvertFixture(t)
	current := models.Scene{ProjectID: f.source.ProjectID, EpisodeN: 1, Generation: 1, Order: 2, ImageFile: "story.png", StoryboardVideoRole: "start"}
	f.ps.db.Create(&current)
	if _, err := f.ps.continuity.Configure(current.ProjectID, current.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeBridge, SourceSceneID: f.source.ID, FrameID: f.frame.ID, ReferenceForm: FrameReferenceClay}); err == nil {
		t.Fatal("bridge mode must reject a clay reference form")
	}
	if _, err := f.ps.continuity.Configure(current.ProjectID, current.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeContinue, SourceSceneID: f.source.ID, FrameID: f.frame.ID, ReferenceForm: FrameReferenceClay}); err != nil {
		t.Fatal(err)
	}
	f.ps.db.First(&current, current.ID)
	if err := f.ps.continuity.PrepareScene(&current); err == nil || !strings.Contains(err.Error(), "尚无白模") {
		t.Fatalf("submission without a clay result must be blocked, err=%v", err)
	}
	f.upload.SaveFile(fmt.Sprint(f.source.ProjectID), "image", "clay.png", testPNG(t, 64, 36))
	if err := f.ps.applyFrameConversion(claimedFrame(t, f, FrameReferenceClay), "clay.png"); err != nil {
		t.Fatal(err)
	}
	if err := f.ps.continuity.PrepareScene(&current); err != nil {
		t.Fatal(err)
	}
	refs, lines, _ := f.ps.sceneVideoContinuityReferences(&current, fmt.Sprint(current.ProjectID))
	if len(refs) != 1 || refs[0].Name != "clay.png" || !strings.Contains(lines[0], "上一镜尾帧白模") {
		t.Fatalf("clay must take the storyboard slot: refs=%+v lines=%+v", refs, lines)
	}
	if tag := openingPictureTag(lines); tag != "" {
		t.Fatalf("clay reference must never be the opening frame, got %q", tag)
	}
}

func TestHandleConvertFrameCandidateReturnsConflictForInFlightFrame(t *testing.T) {
	f := newFrameConvertFixture(t)
	f.ps.db.Model(&models.FrameCandidate{}).Where("id = ?", f.frame.ID).Update("convert_task_id", "running-task")
	svc := &Service{DB: f.ps.db, Projects: f.ps, Continuity: f.ps.continuity}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/continuity/frames/:fid/convert", svc.HandleConvertFrameCandidate)
	url := fmt.Sprintf("/api/projects/%d/scenes/%d/continuity/frames/%d/convert", f.source.ProjectID, f.source.ID, f.frame.ID)
	if w := shotRefineCall(r, http.MethodPost, url, map[string]string{"kind": FrameReferenceLineart}); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "正在转换") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w := shotRefineCall(r, http.MethodPost, url, map[string]string{"kind": "bogus"}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad kind status=%d body=%s", w.Code, w.Body.String())
	}
}
