package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newShotRefineTestService(t *testing.T, llmReply string) (*Service, *gorm.DB, models.Scene, []models.Shot) {
	t.Helper()
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "refine"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Duration: 8, Content: "韩立推门而入，对南宫婉说：「师姐，我回来了。」"}
	db.Create(&scene)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "韩立", SpeechType: "dialogue", Text: "师姐，我回来了。"})
	shots := []models.Shot{
		{SceneID: scene.ID, Order: 1, ShotType: "全景", CameraAngle: "平视", CameraMovement: "固定", Duration: 3, Description: "推门", PromptAction: "韩立推门"},
		{SceneID: scene.ID, Order: 2, ShotType: "中景", CameraAngle: "平视", CameraMovement: "固定", Duration: 5, Description: "开口", Dialogue: "师姐，我回来了。", Blocking: "韩立站门口"},
	}
	for i := range shots {
		db.Create(&shots[i])
	}
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := json.Marshal(llmReply)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%s}}]}`, content)
	}))
	t.Cleanup(ai.Close)
	g := &mockGetter{settings: map[string]string{SettingTextProvider: "llama", SettingLlamaBaseURL: ai.URL, SettingLlamaModel: "mock"}}
	return &Service{DB: db, Skills: NewSkillService(db), TextProviderFact: NewTextProviderFactory(g)}, db, scene, shots
}

func shotRefineRouter(s *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/shots/refine-patch", s.HandleShotRefinePatchDraft)
	r.POST("/api/projects/:id/scenes/:sid/shots/refine-patch/apply", s.HandleApplyShotRefinePatch)
	r.GET("/api/projects/:id/scenes/:sid/shots/refine-patches", s.HandleListShotRefinePatches)
	r.POST("/api/projects/:id/scenes/:sid/shots/refine-patches/:vid/rollback", s.HandleRollbackShotRefinePatch)
	return r
}

func shotRefineCall(r *gin.Engine, method, url string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestShotRefinePatchSkillIsSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", shotRefinePatchSkillCode).First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || !skill.Enabled || !strings.Contains(skill.SystemPrompt, "Shot顺序、时长、对白及其分配、可见人物、道具都已锁定") {
		t.Fatalf("skill=%+v", skill)
	}
}

func TestCheckShotRefineEditsRejectsLockedFieldsAndInventedDialogue(t *testing.T) {
	shots := []models.Shot{{ID: 7, Order: 1, Blocking: "站门口", Dialogue: "师姐，我回来了。"}}
	spoken := "韩立说：「师姐，我回来了。」"
	rationale := shotRefineRationale{Issue: "太平"}
	for name, edit := range map[string]shotRefineEdit{
		"locked dialogue": {ShotID: 7, Field: "dialogue", Replacement: "新台词", Rationale: rationale},
		"locked duration": {ShotID: 7, Field: "duration", Replacement: "4", Rationale: rationale},
		"foreign shot":    {ShotID: 99, Field: "blocking", Replacement: "x", Rationale: rationale},
		"new quote":       {ShotID: 7, Field: "performance", Replacement: "他低声说「我不走了」", Rationale: rationale},
		"no rationale":    {ShotID: 7, Field: "blocking", Replacement: "向前一步"},
		"empty value":     {ShotID: 7, Field: "blocking", Replacement: "  ", Rationale: rationale},
	} {
		if _, err := checkShotRefineEdits([]shotRefineEdit{edit}, shots, spoken, true); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	edits, err := checkShotRefineEdits([]shotRefineEdit{
		{ShotID: 7, Field: "performance", Replacement: "说出「师姐，我回来了。」时垂目", Rationale: rationale},
		{ShotID: 7, Field: "blocking", Replacement: "站门口", Rationale: rationale},
	}, shots, spoken, true)
	if err != nil || len(edits) != 1 || edits[0].Field != "performance" || edits[0].ShotOrder != 1 {
		t.Fatalf("edits=%+v err=%v", edits, err)
	}
}

func TestParseShotRefinePatchMapsOrderAndHandlesUnsupported(t *testing.T) {
	shots := []models.Shot{{ID: 11}, {ID: 12}}
	patch, err := parseShotRefinePatch("```json\n{\"schema\":\"shot_refine.patch.v1\",\"action\":\"apply\",\"edits\":[{\"shot_order\":2,\"field\":\"reaction\",\"replacement\":\"抬眼\"}]}\n```", shots)
	if err != nil || patch.Edits[0].ShotID != 12 {
		t.Fatalf("patch=%+v err=%v", patch, err)
	}
	if _, err := parseShotRefinePatch(`{"action":"apply","edits":[{"shot_order":3,"field":"reaction","replacement":"x"}]}`, shots); err == nil {
		t.Fatal("out-of-range shot order must be rejected")
	}
	patch, err = parseShotRefinePatch(`{"action":"unsupported","edits":[{"shot_order":1}]}`, shots)
	if err != nil || patch.Action != "unsupported" || patch.Edits != nil {
		t.Fatalf("patch=%+v err=%v", patch, err)
	}
}

func TestShotRefinePatchDraftApplyAndRollback(t *testing.T) {
	reply := `{"schema":"shot_refine.patch.v1","action":"apply","edits":[{"shot_order":2,"field":"blocking","replacement":"韩立跨过门槛停在南宫婉身前一步","rationale":{"issue":"距离感","problem":"站位过远","change_reason":"拉近距离","result_reason":"重逢更有张力"}},{"shot_order":1,"field":"camera_movement","replacement":"缓推","rationale":{"issue":"开场","problem":"固定机位平淡","change_reason":"跟随推门","result_reason":"引出人物"}}]}`
	s, db, scene, shots := newShotRefineTestService(t, reply)
	r := shotRefineRouter(s)
	base := fmt.Sprintf("/api/projects/%d/scenes/%d/shots", scene.ProjectID, scene.ID)

	w := shotRefineCall(r, "POST", base+"/refine-patch", map[string]string{"instruction": "两人距离太远了吧？"})
	if w.Code != http.StatusOK {
		t.Fatalf("draft status=%d %s", w.Code, w.Body.String())
	}
	var draft struct {
		Patch      shotRefinePatch `json:"patch"`
		SourceHash string          `json:"source_hash"`
		Persisted  bool            `json:"persisted"`
	}
	json.Unmarshal(w.Body.Bytes(), &draft)
	if len(draft.Patch.Edits) != 2 || draft.Persisted || draft.Patch.Edits[0].Original != "韩立站门口" {
		t.Fatalf("draft=%+v", draft)
	}
	var unchanged models.Shot
	db.First(&unchanged, shots[1].ID)
	if unchanged.Blocking != "韩立站门口" {
		t.Fatal("draft must not persist")
	}

	// Accept only the first edit.
	w = shotRefineCall(r, "POST", base+"/refine-patch/apply", map[string]any{"source_hash": draft.SourceHash, "instruction": "两人距离太远了吧？", "edits": draft.Patch.Edits[:1]})
	if w.Code != http.StatusOK {
		t.Fatalf("apply status=%d %s", w.Code, w.Body.String())
	}
	var applied struct {
		VersionID uint `json:"version_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &applied)
	var after models.Shot
	db.First(&after, shots[1].ID)
	var first models.Shot
	db.First(&first, shots[0].ID)
	if after.Blocking != "韩立跨过门槛停在南宫婉身前一步" || after.Dialogue != "师姐，我回来了。" || after.Duration != 5 || first.CameraMovement != "固定" {
		t.Fatalf("after=%+v first=%+v", after, first)
	}

	// The old hash is now stale; a second apply must be refused.
	w = shotRefineCall(r, "POST", base+"/refine-patch/apply", map[string]any{"source_hash": draft.SourceHash, "edits": draft.Patch.Edits[1:]})
	if w.Code != http.StatusConflict {
		t.Fatalf("stale apply status=%d %s", w.Code, w.Body.String())
	}

	w = shotRefineCall(r, "POST", fmt.Sprintf("%s/refine-patches/%d/rollback", base, applied.VersionID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("rollback status=%d %s", w.Code, w.Body.String())
	}
	db.First(&after, shots[1].ID)
	if after.Blocking != "韩立站门口" {
		t.Fatalf("rollback blocking=%q", after.Blocking)
	}
	w = shotRefineCall(r, "POST", fmt.Sprintf("%s/refine-patches/%d/rollback", base, applied.VersionID), nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("double rollback status=%d", w.Code)
	}
	w = shotRefineCall(r, "GET", base+"/refine-patches", nil)
	if !strings.Contains(w.Body.String(), `"state":"rolled_back"`) || !strings.Contains(w.Body.String(), "两人距离太远了吧？") {
		t.Fatalf("list=%s", w.Body.String())
	}
}

func TestShotRefineRollbackRefusesAfterLaterManualEdit(t *testing.T) {
	reply := `{"action":"apply","edits":[{"shot_order":2,"field":"reaction","replacement":"南宫婉眼眶一红","rationale":{"issue":"反应"}}]}`
	s, db, scene, shots := newShotRefineTestService(t, reply)
	r := shotRefineRouter(s)
	base := fmt.Sprintf("/api/projects/%d/scenes/%d/shots", scene.ProjectID, scene.ID)
	w := shotRefineCall(r, "POST", base+"/refine-patch", map[string]string{"instruction": "加点反应"})
	var draft struct {
		Patch      shotRefinePatch `json:"patch"`
		SourceHash string          `json:"source_hash"`
	}
	json.Unmarshal(w.Body.Bytes(), &draft)
	w = shotRefineCall(r, "POST", base+"/refine-patch/apply", map[string]any{"source_hash": draft.SourceHash, "edits": draft.Patch.Edits})
	var applied struct {
		VersionID uint `json:"version_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &applied)
	if _, err := NewShotService(db).UpdateShot(shots[1].ID, map[string]any{"reaction": "人工改过"}); err != nil {
		t.Fatal(err)
	}
	w = shotRefineCall(r, "POST", fmt.Sprintf("%s/refine-patches/%d/rollback", base, applied.VersionID), nil)
	var current models.Shot
	db.First(&current, shots[1].ID)
	if w.Code != http.StatusConflict || current.Reaction != "人工改过" {
		t.Fatalf("status=%d reaction=%q", w.Code, current.Reaction)
	}
}

func TestShotRefineAppliedVersionRecordsVerifiedSkillProvenance(t *testing.T) {
	reply := `{"action":"apply","edits":[{"shot_order":2,"field":"reaction","replacement":"南宫婉眼眶一红","rationale":{"issue":"反应"}}]}`
	s, db, scene, _ := newShotRefineTestService(t, reply)
	r := shotRefineRouter(s)
	base := fmt.Sprintf("/api/projects/%d/scenes/%d/shots", scene.ProjectID, scene.ID)
	draftAndApply := func(provenance func(*SkillProvenance) *SkillProvenance) map[string]any {
		w := shotRefineCall(r, "POST", base+"/refine-patch", map[string]string{"instruction": "加点反应"})
		var draft struct {
			Patch           shotRefinePatch  `json:"patch"`
			SourceHash      string           `json:"source_hash"`
			SkillProvenance *SkillProvenance `json:"skill_provenance"`
		}
		json.Unmarshal(w.Body.Bytes(), &draft)
		if draft.SkillProvenance == nil || draft.SkillProvenance.Code != shotRefinePatchSkillCode || draft.SkillProvenance.Version != 1 || len(draft.SkillProvenance.InputSHA256) != 64 {
			t.Fatalf("draft provenance=%+v body=%s", draft.SkillProvenance, w.Body.String())
		}
		w = shotRefineCall(r, "POST", base+"/refine-patch/apply", map[string]any{"source_hash": draft.SourceHash, "edits": draft.Patch.Edits, "skill_provenance": provenance(draft.SkillProvenance)})
		var applied struct {
			VersionID uint `json:"version_id"`
		}
		json.Unmarshal(w.Body.Bytes(), &applied)
		var version models.PromptVersion
		if err := db.First(&version, applied.VersionID).Error; err != nil {
			t.Fatalf("apply status=%d %s err=%v", w.Code, w.Body.String(), err)
		}
		var meta map[string]any
		json.Unmarshal([]byte(version.Metadata), &meta)
		// Roll back so the next round starts from the original values.
		shotRefineCall(r, "POST", fmt.Sprintf("%s/refine-patches/%d/rollback", base, applied.VersionID), nil)
		return meta
	}
	var audit models.SkillAuditLog
	meta := draftAndApply(func(p *SkillProvenance) *SkillProvenance { return p })
	db.Order("id DESC").First(&audit)
	if meta["skill_code"] != shotRefinePatchSkillCode || meta["skill_version"] != float64(1) || meta["input_sha256"] != audit.InputHash {
		t.Fatalf("verified metadata=%v audit hash=%s", meta, audit.InputHash)
	}
	meta = draftAndApply(func(p *SkillProvenance) *SkillProvenance {
		forged := *p
		forged.Version = 99
		return &forged
	})
	if _, ok := meta["skill_version"]; ok || meta["skill_code"] != shotRefinePatchSkillCode {
		t.Fatalf("unverified provenance must not be recorded: %v", meta)
	}
	w := shotRefineCall(r, "GET", base+"/refine-patches", nil)
	if !strings.Contains(w.Body.String(), `"skill_version":1`) {
		t.Fatalf("history must expose the verified skill version: %s", w.Body.String())
	}
}

func TestShotRefineDraftRejectsWholePatchOnLockedField(t *testing.T) {
	reply := `{"action":"apply","edits":[{"shot_order":1,"field":"reaction","replacement":"抬眼","rationale":{"issue":"x"}},{"shot_order":2,"field":"dialogue","replacement":"改台词","rationale":{"issue":"x"}}]}`
	s, _, scene, _ := newShotRefineTestService(t, reply)
	w := shotRefineCall(shotRefineRouter(s), "POST", fmt.Sprintf("/api/projects/%d/scenes/%d/shots/refine-patch", scene.ProjectID, scene.ID), map[string]string{"instruction": "改"})
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "整体拒绝") {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
}
