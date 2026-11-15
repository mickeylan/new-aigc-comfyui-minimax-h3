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

func topviewGetter(enabled bool) *VolcClient {
	value := "false"
	if enabled {
		value = "true"
	}
	return NewVolcClientWithGetter(&mockGetter{settings: map[string]string{SettingExperimentalTopview: value}})
}

func TestTopviewSkillsAreSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var structure, dispatch models.Skill
	if err := db.Where("code = ?", topviewStructureSkillCode).First(&structure).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("code = ?", topviewEndDispatchSkillCode).First(&dispatch).Error; err != nil {
		t.Fatal(err)
	}
	if structure.Version != 1 || structure.Stage != models.SkillStageQwenImageEdit || structure.ModelFamilies != models.ModelFamilyQwenImage21 || !strings.Contains(structure.SystemPrompt, "画面中不出现任何人物") {
		t.Fatalf("structure skill=%+v", structure)
	}
	if dispatch.Version != 1 || dispatch.Stage != models.SkillStageStoryboard || !strings.HasSuffix(dispatch.SystemPrompt, skillOutputRule) ||
		!strings.Contains(dispatch.SystemPrompt, "禁止使用画面坐标") || !strings.Contains(dispatch.PromptTemplate, "{{previous_end}}") {
		t.Fatalf("dispatch skill=%+v", dispatch)
	}
}

func TestParseTopviewDispatchRejectsMalformedOutput(t *testing.T) {
	markers := []topviewMarker{{Name: "韩立", Color: "红"}, {Name: "南宫婉", Color: "蓝"}}
	valid := `{"summary":"两人隔石桌相对","characters":[{"name":"韩立","marker_color":"红","position":"门槛内侧","posture":"站立","facing":"面向南宫婉","holding":""},{"name":"南宫婉","marker_color":"蓝","position":"石桌东侧","posture":"","facing":"","holding":""}],"props":[{"name":"玉简","position":"石桌上"}]}`
	got, err := parseTopviewDispatch("```json\n"+valid+"\n```", markers)
	if err != nil || got.Characters[1].Position != "石桌东侧" {
		t.Fatalf("valid dispatch got=%+v err=%v", got, err)
	}
	for name, raw := range map[string]string{
		"not json":         "韩立站在门口",
		"missing person":   `{"summary":"","characters":[{"name":"韩立","marker_color":"红","position":"门槛内侧"}],"props":[]}`,
		"off whitelist":    strings.Replace(valid, `"南宫婉"`, `"厉飞雨"`, 1),
		"changed colour":   strings.Replace(valid, `"marker_color":"蓝"`, `"marker_color":"绿"`, 1),
		"screen position":  strings.Replace(valid, "石桌东侧", "画面左侧", 1),
		"appearance field": strings.Replace(valid, `"holding":""}]`, `"holding":"","appearance":"青衫"}]`, 1),
		"appearance words": strings.Replace(valid, "面向南宫婉", "身穿青衫面向南宫婉", 1),
		"empty position":   strings.Replace(valid, "门槛内侧", "", 1),
	} {
		if _, err := parseTopviewDispatch(raw, markers); err == nil {
			t.Errorf("%s: malformed dispatch was accepted", name)
		}
	}
}

func TestTopviewReferenceFillsOnlyAFreeSlotWithValidCache(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "俯视"}
	ps.db.Create(&p)
	asset := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "城门", Image: "gate.png", TopviewImage: "gate_top.png", TopviewSourceImage: "gate.png"}
	ps.db.Create(&asset)
	scene := models.Scene{ProjectID: p.ID, LocationName: "城门", ImageFile: "story.png"}
	ps.db.Create(&scene)
	pid := fmt.Sprint(p.ID)

	ps.volc = topviewGetter(false)
	if refs, _, _ := ps.sceneVideoContinuityReferences(&scene, pid); len(refs) != 2 {
		t.Fatalf("experiment off must not inject: %+v", refs)
	}
	ps.volc = topviewGetter(true)
	refs, lines, _ := ps.sceneVideoContinuityReferences(&scene, pid)
	if len(refs) != 3 || refs[2].Name != "gate_top.png" || !strings.Contains(lines[2], topviewReferenceLine) {
		t.Fatalf("enabled with cache: refs=%+v lines=%+v", refs, lines)
	}
	if tag := openingPictureTag(lines); tag != "" {
		t.Fatalf("top-view map must not be an opening frame: %q", tag)
	}
	defs, _, subjects := h3VideoSubjects(lines)
	if len(subjects) != 0 || !strings.Contains(defs[2], "never shows a top-down view") {
		t.Fatalf("definitions=%+v subjects=%+v", defs, subjects)
	}
	// The cache belongs to the image it was drawn from.
	ps.db.Model(&asset).Update("image", "gate2.png")
	if refs, _, _ := ps.sceneVideoContinuityReferences(&scene, pid); len(refs) != 2 {
		t.Fatalf("stale cache must not inject: %+v", refs)
	}
	ps.db.Model(&asset).Update("topview_source_image", "gate2.png")

	// Full H3 slots: the optional map is dropped, never another reference.
	names := []string{}
	for i := 0; i < maxSceneReferenceImages; i++ {
		name := fmt.Sprintf("角色%d", i)
		names = append(names, name)
		ps.db.Create(&models.Character{ProjectID: p.ID, Name: name, Sheet: fmt.Sprintf("sheet%d.png", i)})
	}
	ps.db.Model(&scene).Update("characters", strings.Join(names, ","))
	ps.db.First(&scene, scene.ID)
	refs, lines, _ = ps.sceneVideoContinuityReferences(&scene, pid)
	if len(refs) != maxSceneReferenceImages || strings.Contains(strings.Join(lines, "\n"), topviewReferenceLine) {
		t.Fatalf("full slots must skip the top-view map: %d refs, lines=%+v", len(refs), lines)
	}
}

func TestStartTopviewStructureIsGatedCachedAndConflictsWhileActive(t *testing.T) {
	ps := newSheetGenerationService(t)
	tpl := loadTemplateForTest(t, "qwen_image_2_1_multi_edit.json")
	if err := ps.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "俯视"}
	ps.db.Create(&p)
	asset := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "城门", Image: "gate.png"}
	ps.db.Create(&asset)
	ps.db.Create(&models.UploadFile{TaskID: fmt.Sprint(p.ID), Type: "image", Name: "gate.png", Path: fmt.Sprint(p.ID) + "/gate.png"})

	ps.volc = topviewGetter(false)
	if _, err := ps.StartTopviewStructure(&asset, false); err != errTopviewDisabled {
		t.Fatalf("disabled experiment err=%v", err)
	}
	ps.volc = topviewGetter(true)
	cached, err := ps.StartTopviewStructure(&asset, false)
	if err != nil || cached {
		t.Fatalf("cached=%v err=%v", cached, err)
	}
	var saved models.Asset
	ps.db.First(&saved, asset.ID)
	var task models.Task
	if err := ps.db.Where("task_id = ?", saved.TopviewTaskID).First(&task).Error; err != nil {
		t.Fatalf("topview task not recorded: %v", err)
	}
	if !strings.Contains(task.InputsJSON, "gate.png") || !strings.Contains(task.ParamsJSON, "1:1 (Square)") || !strings.Contains(task.Prompt, "正俯视结构图") || saved.TopviewSourceImage != "gate.png" {
		t.Fatalf("task inputs=%s params=%s prompt=%s asset=%+v", task.InputsJSON, task.ParamsJSON, task.Prompt, saved)
	}

	active := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "山门", Image: "hill.png", TopviewTaskID: "active-topview"}
	ps.db.Create(&active)
	ps.db.Create(&models.Task{TaskID: "active-topview", Status: "pending"})
	if _, err := ps.StartTopviewStructure(&active, true); err != errTopviewActive {
		t.Fatalf("an active task must conflict, err=%v", err)
	}
	done := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "河岸", Image: "river.png", TopviewImage: "river_top.png", TopviewSourceImage: "river.png"}
	ps.db.Create(&done)
	if cached, err := ps.StartTopviewStructure(&done, false); err != nil || !cached {
		t.Fatalf("valid cache must be reused, cached=%v err=%v", cached, err)
	}
	if _, err := ps.StartTopviewStructure(&models.Asset{Kind: AssetKindProp, Image: "x.png"}, false); err == nil {
		t.Fatal("props must not get a top-view map")
	}
}

func TestTopviewDispatchDraftAndApplyWriteBlockingStartAndEnd(t *testing.T) {
	reply := `{"summary":"两人隔石桌相对","characters":[{"name":"韩立","marker_color":"蓝","position":"门槛内侧","posture":"站立","facing":"面向南宫婉","holding":""},{"name":"南宫婉","marker_color":"红","position":"石桌东侧","posture":"","facing":"","holding":""}],"props":[]}`
	svc, db, scene, _ := newShotRefineTestService(t, reply)
	db.Model(&scene).Update("characters", "韩立,南宫婉")
	// The previous Scene's confirmed end is this Scene's start, and 南宫婉 keeps her colour.
	prevContent := "南宫婉在石桌旁等候。"
	prevDispatch, _ := json.Marshal(topviewDispatch{SourceContentHash: sceneDialogueContentHash(prevContent), Summary: "南宫婉独坐",
		Characters: []topviewDispatchCharacter{{Name: "南宫婉", Color: "红", Position: "石桌东侧", Posture: "坐"}}})
	db.Create(&models.Scene{ProjectID: scene.ProjectID, EpisodeN: scene.EpisodeN, Order: 0, Content: prevContent, TopviewDispatchJSON: string(prevDispatch)})
	svc.Projects = &ProjectService{db: db, volc: topviewGetter(false)}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/topview/dispatch", svc.HandleTopviewDispatchDraft)
	r.PUT("/api/projects/:id/scenes/:sid/topview/dispatch", svc.HandleApplyTopviewDispatch)
	url := fmt.Sprintf("/api/projects/%d/scenes/%d/topview/dispatch", scene.ProjectID, scene.ID)
	if w := shotRefineCall(r, http.MethodPost, url, nil); w.Code != http.StatusForbidden {
		t.Fatalf("disabled draft status=%d body=%s", w.Code, w.Body.String())
	}
	svc.Projects.volc = topviewGetter(true)
	w := shotRefineCall(r, http.MethodPost, url, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("draft status=%d body=%s", w.Code, w.Body.String())
	}
	var draft struct {
		Dispatch topviewDispatch `json:"dispatch"`
		Markers  []topviewMarker `json:"markers"`
	}
	json.Unmarshal(w.Body.Bytes(), &draft)
	if draft.Markers[0] != (topviewMarker{"韩立", "蓝"}) || draft.Markers[1] != (topviewMarker{"南宫婉", "红"}) {
		t.Fatalf("markers must keep earlier colours: %+v", draft.Markers)
	}
	var unchanged models.Scene
	db.First(&unchanged, scene.ID)
	if unchanged.TopviewDispatchJSON != "" || unchanged.BlockingJSON != "" {
		t.Fatal("draft must not write the Scene")
	}
	stale := draft.Dispatch
	stale.SourceContentHash = "old"
	if w := shotRefineCall(r, http.MethodPut, url, stale); w.Code != http.StatusConflict {
		t.Fatalf("stale apply status=%d body=%s", w.Code, w.Body.String())
	}
	if w := shotRefineCall(r, http.MethodPut, url, draft.Dispatch); w.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.Scene
	db.First(&saved, scene.ID)
	blocking, err := parseSceneBlocking(saved.BlockingJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blocking.Start, "南宫婉（红）：石桌东侧，坐") || !strings.Contains(blocking.End, "韩立（蓝）：门槛内侧") || saved.TopviewDispatchJSON == "" || !saved.PromptStale {
		t.Fatalf("blocking=%+v saved=%+v", blocking, saved)
	}
}

func TestVideoPromptSourceHashCoversSubmittedRef2VReferences(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	configureDefaultRef2VFixture(t, f)
	f.svc.Projects.volc = topviewGetter(true)
	hash := func() string {
		t.Helper()
		var sc models.Scene
		f.db.First(&sc, f.sc.ID)
		h, err := f.svc.sceneVideoPromptSourceHash(&sc)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	base := hash()
	if hash() != base {
		t.Fatal("source hash must be stable")
	}
	// A new top-view map is a new submitted reference.
	f.db.Model(&models.Asset{}).Where("name = ?", "茶馆").Updates(map[string]any{"topview_image": "top1.png", "topview_source_image": "teahouse.png"})
	withTop := hash()
	if withTop == base {
		t.Fatal("adding the top-view map must change the source hash")
	}
	f.db.Model(&models.Asset{}).Where("name = ?", "茶馆").Update("topview_image", "top2.png")
	regenerated := hash()
	if regenerated == withTop {
		t.Fatal("a regenerated top-view map must change the source hash")
	}
	// Other Ref2V reference images were not hashed before either.
	f.db.Model(&models.Character{}).Where("name = ?", "阿青").Update("portrait", "aqing-v2.png")
	if hash() == regenerated {
		t.Fatal("a replaced character reference image must change the source hash")
	}
}

func TestTopviewMapChangeMarksSavedPromptsOfThatLocationStale(t *testing.T) {
	ps := newTestProjectService(t)
	p := models.Project{Title: "俯视"}
	ps.db.Create(&p)
	gate := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "城门", Image: "gate.png", TopviewImage: "gate_top.png", TopviewSourceImage: "gate.png"}
	ps.db.Create(&gate)
	ps.db.Create(&models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "河岸", Image: "river.png"})
	saved := models.Scene{ProjectID: p.ID, Order: 1, LocationName: "城门", VideoFullPrompt: "reviewed"}
	unsaved := models.Scene{ProjectID: p.ID, Order: 2, LocationName: "城门"}
	elsewhere := models.Scene{ProjectID: p.ID, Order: 3, LocationName: "河岸", VideoFullPrompt: "reviewed"}
	for _, sc := range []*models.Scene{&saved, &unsaved, &elsewhere} {
		ps.db.Create(sc)
	}
	stale := func(id uint) bool {
		var sc models.Scene
		ps.db.First(&sc, id)
		return sc.PromptStale
	}
	ps.volc = topviewGetter(false)
	ps.markTopviewScenesStale(&gate)
	if stale(saved.ID) {
		t.Fatal("experiment off must not touch prompts")
	}
	ps.volc = topviewGetter(true)
	ps.markTopviewScenesStale(&gate)
	if !stale(saved.ID) || stale(unsaved.ID) || stale(elsewhere.ID) {
		t.Fatalf("stale saved=%v unsaved=%v elsewhere=%v", stale(saved.ID), stale(unsaved.ID), stale(elsewhere.ID))
	}
	var kept models.Scene
	ps.db.First(&kept, saved.ID)
	if kept.VideoFullPrompt != "reviewed" {
		t.Fatal("the saved prompt must be kept for re-confirmation")
	}
}
