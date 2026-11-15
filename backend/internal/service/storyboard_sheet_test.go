package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestStoryboardSheetSkillIsSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", storyboardSheetSkillCode).First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || !skill.Enabled || !strings.Contains(skill.PromptTemplate, "不机械地每个 Shot 一格") || !strings.Contains(skill.SystemPrompt, "只输出锚点行") {
		t.Fatalf("skill=%+v", skill)
	}
}

func TestParseStoryboardAnchorsRequiresConsecutiveNumbering(t *testing.T) {
	anchors, err := parseStoryboardAnchors("```\nP1：韩立推门\n\np2: 南宫婉回头\n```")
	if err != nil || len(anchors) != 2 || anchors[1] != "P2：南宫婉回头" {
		t.Fatalf("anchors=%v err=%v", anchors, err)
	}
	for _, raw := range []string{"", "P1：a\nP3：b", "P2：a", "P1：a\n说明：多余", strings.Repeat("x", 0)} {
		if _, err := parseStoryboardAnchors(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
	var lines []string
	for i := 1; i <= 16; i++ {
		lines = append(lines, fmt.Sprintf("P%d：动作%d", i, i))
	}
	if _, err := parseStoryboardAnchors(strings.Join(lines, "\n")); err == nil || !strings.Contains(err.Error(), "不能超过15") {
		t.Fatalf("16 anchors must be rejected, err=%v", err)
	}
}

func TestBuildStoryboardSheetPromptFillsGridAndReferences(t *testing.T) {
	prompt := buildStoryboardSheetPrompt([]string{"P1：a", "P2：b", "P3：c", "P4：d", "P5：e"}, []string{"<image1>是角色「韩立」的四视图"})
	for _, want := range []string{"P1 到 P5", "3列×2行", "横向 4:3", "参考图：<image1>是角色「韩立」的四视图。", "\nP5：e"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "{{") {
		t.Fatalf("unfilled placeholder:\n%s", prompt)
	}
	if storyboardSheetGrid(15) != "5列×3行" || storyboardSheetGrid(1) != "2列×2行" {
		t.Fatal("grid mapping changed")
	}
}

func TestStoryboardSheetAnchorsDraftIsNotPersisted(t *testing.T) {
	svc, db, scene, _ := newShotRefineTestService(t, "P1：韩立推门而入，中景，人物在画面左侧朝右\nP2：韩立对南宫婉开口，近景")
	if err := db.AutoMigrate(&models.StoryboardSheet{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/storyboard-sheet/anchors", svc.HandleStoryboardSheetAnchors)
	r.GET("/api/projects/:id/scenes/:sid/storyboard-sheets", svc.HandleListStoryboardSheets)
	base := fmt.Sprintf("/api/projects/%d/scenes/%d/", scene.ProjectID, scene.ID)
	w := shotRefineCall(r, http.MethodPost, base+"storyboard-sheet/anchors", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Anchors    string `json:"anchors"`
		SourceHash string `json:"source_hash"`
		Persisted  bool   `json:"persisted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Persisted || !strings.HasPrefix(resp.Anchors, "P1：韩立推门") || len(resp.SourceHash) != 64 {
		t.Fatalf("resp=%+v", resp)
	}
	var count int64
	db.Model(&models.StoryboardSheet{}).Count(&count)
	if count != 0 {
		t.Fatalf("anchor draft must not persist, rows=%d", count)
	}
	w = shotRefineCall(r, http.MethodGet, base+"storyboard-sheets", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), resp.SourceHash) {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStoryboardSheetAnchorsRejectsMalformedOutput(t *testing.T) {
	svc, _, scene, _ := newShotRefineTestService(t, "以下是锚点：\nP1：韩立推门")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/storyboard-sheet/anchors", svc.HandleStoryboardSheetAnchors)
	w := shotRefineCall(r, http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard-sheet/anchors", scene.ProjectID, scene.ID), nil)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "非锚点行") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStartStoryboardSheetUsesCharacterSheetsOnlyAndStaysReviewOnly(t *testing.T) {
	ps := newSheetGenerationService(t)
	if err := ps.db.AutoMigrate(&models.StoryboardSheet{}); err != nil {
		t.Fatal(err)
	}
	edit := loadTemplateForTest(t, "qwen_image_2_1_multi_edit.json")
	if err := ps.db.Create(&edit).Error; err != nil {
		t.Fatal(err)
	}
	ch := createSheetCharacter(t, ps, "", "")
	ps.db.Model(ch).Update("sheet", "sheet.png")
	ps.db.Create(&models.UploadFile{TaskID: fmt.Sprint(ch.ProjectID), Type: "image", Name: "sheet.png", Path: fmt.Sprint(ch.ProjectID) + "/sheet.png"})
	scene := models.Scene{ProjectID: ch.ProjectID, EpisodeN: 1, Order: 1, Characters: ch.Name, Content: "林舒拔剑", ImageFile: "frame.png"}
	ps.db.Create(&scene)
	if _, err := ps.StartStoryboardSheet(&scene, "P1：林舒拔剑\nP3：错号", "h"); err == nil {
		t.Fatal("malformed anchors must be rejected before any task is created")
	}
	row, err := ps.StartStoryboardSheet(&scene, "P1：林舒拔剑\nP2：林舒收剑", "h")
	if err != nil {
		t.Fatal(err)
	}
	var task models.Task
	if err := ps.db.Where("task_id = ?", row.TaskID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.TemplateName != edit.Name || !strings.Contains(task.InputsJSON, "sheet.png") || strings.Contains(task.InputsJSON, "frame.png") {
		t.Fatalf("task must use Qwen edit with only the character sheet: %s %s", task.TemplateName, task.InputsJSON)
	}
	if !strings.Contains(task.ParamsJSON, "4:3 (Standard)") || !strings.Contains(task.Prompt, "<image1>是角色「林舒」的四视图") || !strings.Contains(task.Prompt, "P2：林舒收剑") {
		t.Fatalf("params=%s prompt=%s", task.ParamsJSON, task.Prompt)
	}
	if _, err := ps.StartStoryboardSheet(&scene, "P1：林舒拔剑", "h"); err == nil || !strings.Contains(err.Error(), "正在生成") {
		t.Fatalf("an active sheet task must block a second submission, err=%v", err)
	}
	var saved models.Scene
	ps.db.First(&saved, scene.ID)
	if saved.ImageFile != "frame.png" || saved.ImageTaskID != "" || saved.VideoTaskID != "" {
		t.Fatalf("storyboard sheet must not touch the Scene image/video: %+v", saved)
	}
}
