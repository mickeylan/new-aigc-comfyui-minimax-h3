package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newSceneSupplementTestService(t *testing.T, llmReply string) (*Service, *gorm.DB, []models.Scene) {
	t.Helper()
	svc, db, _, _ := newShotRefineTestService(t, llmReply)
	project := models.Project{Title: "supplement"}
	db.Create(&project)
	scenes := []models.Scene{
		{ProjectID: project.ID, EpisodeN: 1, Order: 1, Title: "场景1 洞府", Content: "韩立盘坐洞府，灵力运转。", LocationName: "洞府", Duration: 8},
		{ProjectID: project.ID, EpisodeN: 1, Order: 2, Title: "场景2 山门", Content: "南宫婉立于山门。", LocationName: "山门", Duration: 6},
	}
	for i := range scenes {
		db.Create(&scenes[i])
	}
	return svc, db, scenes
}

func sceneSupplementRouter(s *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/supplement/draft", s.HandleSceneSupplementDraft)
	r.POST("/api/projects/:id/scenes/:sid/supplement/apply", s.HandleApplySceneSupplement)
	return r
}

func TestSceneSupplementSkillIsSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", sceneSupplementSkillCode).First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || !skill.Enabled || !strings.Contains(skill.SystemPrompt, "补镜描述是这一场唯一的剧情来源") || !strings.Contains(skill.SystemPrompt, "5到15秒能演完") {
		t.Fatalf("skill=%+v", skill)
	}
}

func TestNormalizeSceneSupplementDraftRejectsDialogueOutsideBrief(t *testing.T) {
	anchor := &models.Scene{LocationName: "洞府"}
	brief := "韩立收功起身，低声说：师姐，我该走了。"
	if _, _, err := normalizeSceneSupplementDraft(sceneSupplementDraft{Content: "韩立起身", Duration: 6, Dialogues: []sceneSupplementDialogue{{Character: "韩立", Text: "我不走了"}}}, brief, anchor); err == nil {
		t.Fatal("expected invented dialogue to be rejected")
	}
	draft, warnings, err := normalizeSceneSupplementDraft(sceneSupplementDraft{Content: "韩立收功起身", Duration: 40, Characters: []string{"韩立", " 韩立 "}, Dialogues: []sceneSupplementDialogue{{Character: "韩立", Text: "「师姐，我该走了！」"}}}, brief, anchor)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Location != "洞府" || draft.Duration != maxShotDuration || len(draft.Characters) != 1 || draft.Title != "补镜" || len(warnings) != 1 {
		t.Fatalf("draft=%+v warnings=%v", draft, warnings)
	}
	if _, _, err := normalizeSceneSupplementDraft(sceneSupplementDraft{Content: "  "}, brief, anchor); err == nil {
		t.Fatal("expected empty content to be rejected")
	}
}

func TestSceneSupplementDraftAndApplyInsertsAfterAnchor(t *testing.T) {
	reply := `{"title":"补镜 收功","content":"韩立缓缓收功，睁眼起身。","duration":6,"characters":["韩立"],"dialogues":[{"character":"韩立","text":"该走了。"}]}`
	svc, db, scenes := newSceneSupplementTestService(t, reply)
	r := sceneSupplementRouter(svc)
	base := fmt.Sprintf("/api/projects/%d/scenes/%d/supplement/", scenes[0].ProjectID, scenes[0].ID)
	brief := "韩立收功起身，自语：该走了。"
	w := shotRefineCall(r, http.MethodPost, base+"draft", map[string]string{"brief": brief})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"persisted":false`) {
		t.Fatalf("draft status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&models.Scene{}).Where("project_id = ?", scenes[0].ProjectID).Count(&count)
	if count != 2 {
		t.Fatalf("draft must not persist, scenes=%d", count)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	apply := map[string]any{"brief": brief, "source_hash": resp["source_hash"], "draft": resp["draft"]}
	if w := shotRefineCall(r, http.MethodPost, base+"apply", apply); w.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var active []models.Scene
	db.Where("project_id = ? AND (archived = ? OR archived IS NULL)", scenes[0].ProjectID, false).Order("`order`").Find(&active)
	if len(active) != 3 || active[0].ID != scenes[0].ID || active[2].ID != scenes[1].ID || active[1].Order != 2 || active[2].Order != 3 {
		t.Fatalf("active=%+v", active)
	}
	inserted := active[1]
	if inserted.LocationName != "洞府" || inserted.Status != "pending" || inserted.ImageFile != "" || inserted.VideoFile != "" || active[2].Title != "场景3 山门" {
		t.Fatalf("inserted=%+v next=%+v", inserted, active[2])
	}
	var dialogues []models.Dialogue
	db.Where("scene_id = ?", inserted.ID).Find(&dialogues)
	if len(dialogues) != 1 || dialogues[0].Text != "该走了。" {
		t.Fatalf("dialogues=%+v", dialogues)
	}
	// The same draft is now stale because the anchor's follower changed.
	if w := shotRefineCall(r, http.MethodPost, base+"apply", apply); w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for stale apply, got %d %s", w.Code, w.Body.String())
	}
}

func TestSceneSupplementDraftRejectsInventedDialogue(t *testing.T) {
	svc, db, scenes := newSceneSupplementTestService(t, `{"title":"补镜","content":"韩立起身。","duration":6,"characters":["韩立"],"dialogues":[{"character":"韩立","text":"我要杀了你"}]}`)
	r := sceneSupplementRouter(svc)
	w := shotRefineCall(r, http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/supplement/draft", scenes[0].ProjectID, scenes[0].ID), map[string]string{"brief": "韩立默默起身"})
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "不在补镜描述中") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&models.Scene{}).Where("project_id = ?", scenes[0].ProjectID).Count(&count)
	if count != 2 {
		t.Fatalf("scenes=%d", count)
	}
}
