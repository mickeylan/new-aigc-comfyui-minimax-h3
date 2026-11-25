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
)

func useExecutionProvider(t *testing.T, s *Service, provider TextProvider) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Messages) < 2 {
			w.WriteHeader(400)
			return
		}
		output, err := provider.Chat(input.Messages[0].Content, input.Messages[1].Content)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(gin.H{"choices": []any{gin.H{"message": gin.H{"content": output}}}})
	}))
	t.Cleanup(server.Close)
	s.TextProviderFact = NewTextProviderFactory(&mockGetter{settings: map[string]string{SettingTextProvider: "llama", SettingLlamaBaseURL: server.URL, SettingLlamaModel: "mock"}})
}

func executionContextFixture() studioExecutionContext {
	return studioExecutionContext{Revision: "actual-editor-revision", Assets: []studioExecutionAsset{{ID: "actor", Name: "真实人物", Clips: []studioExecutionClip{{ID: "walk-0", Name: "walk", Duration: 1}}}}, Objects: []studioExecutionObject{{ID: "existing", AssetID: "actor", Name: "韩立", Position: []float64{0, 0, 0}, Rotation: []float64{0, 0, 0}, Scale: []float64{1, 1, 1}}}}
}
func executionPlanFixture(source studioExecutionSource, hash string) studioExecutionPlan {
	plan := studioExecutionPlan{SourceHash: hash, ContextRevision: "actual-editor-revision", Objects: []studioExecutionObject{}, Bindings: []studioExecutionBinding{}, Shots: []studioExecutionShot{}, Assumptions: []string{}, Blockers: []string{}}
	for _, shot := range source.Shots {
		plan.Shots = append(plan.Shots, studioExecutionShot{ShotID: shot.ID, Duration: shot.Duration, Camera: &studioExecutionCamera{Position: []float64{0, 2, 5}, Target: []float64{0, 1, 0}, FOV: 45}, Evidence: []string{source.Scene.Content}, Assumptions: []string{"机位为待审导演补充"}, Blockers: []string{}})
	}
	return plan
}
func executionHTTP(t *testing.T, s *Service, project, scene uint, method string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.GET("/:id/:sid/source", s.HandleGetStudioExecutionSource)
	r.POST("/:id/:sid/plan", s.HandlePlanStudioExecution)
	suffix := "source"
	if method == "POST" {
		suffix = "plan"
	}
	raw, _ := json.Marshal(body)
	request := httptest.NewRequest(method, fmt.Sprintf("/%d/%d/%s", project, scene, suffix), bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	return w
}
func TestStudioExecutionPlanReadOnlyAuditedAndSourcePreserved(t *testing.T) {
	s, db, scene, _ := newShotRefineTestService(t, "")
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan := executionPlanFixture(source, hash)
	raw, _ := json.Marshal(plan)
	provider := &captureTextProvider{response: string(raw)}
	useExecutionProvider(t, s, provider)
	beforeScene, _ := json.Marshal(source.Scene)
	beforeShots, _ := json.Marshal(source.Shots)
	get := executionHTTP(t, s, scene.ProjectID, scene.ID, "GET", nil)
	if get.Code != 200 || !strings.Contains(get.Body.String(), hash) {
		t.Fatalf("source %d %s", get.Code, get.Body.String())
	}
	w := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", gin.H{"source_hash": hash, "context": executionContextFixture()})
	if w.Code != 200 {
		t.Fatalf("plan %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(provider.system, studioExecutionSystemPrompt) || !strings.Contains(provider.user, scene.Content) || !strings.Contains(provider.user, "walk-0") {
		t.Fatal("did not use approved prompt, authoritative source and actual directory")
	}
	var result struct {
		Persisted  bool                  `json:"persisted"`
		Applied    bool                  `json:"applied"`
		Provenance *SkillProvenance      `json:"skill_provenance"`
		Source     studioExecutionSource `json:"source"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Persisted || result.Applied || result.Provenance == nil || result.Provenance.Code != studioExecutionSkillCode || result.Provenance.Version != 1 {
		t.Fatalf("false provenance/acceptance: %+v", result)
	}
	after, afterHash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterScene, _ := json.Marshal(after.Scene)
	afterShots, _ := json.Marshal(after.Shots)
	if afterHash != hash || !bytes.Equal(beforeScene, afterScene) || !bytes.Equal(beforeShots, afterShots) {
		t.Fatal("planning mutated source")
	}
	resultSource, _ := json.Marshal(result.Source)
	original, _ := json.Marshal(source)
	if !bytes.Equal(resultSource, original) {
		t.Fatal("response lost original director fields")
	}
	var logs int64
	db.Model(&models.SkillAuditLog{}).Where("skill_code = ?", studioExecutionSkillCode).Count(&logs)
	if logs != 1 {
		t.Fatalf("audit count=%d", logs)
	}
	var versions int64
	db.Model(&models.PromptVersion{}).Count(&versions)
	if versions != 0 {
		t.Fatalf("planning saved business drafts: %d", versions)
	}
}
func TestStudioExecutionPlanRejectsInvalidReferencesAndCoverage(t *testing.T) {
	s, _, scene, _ := newShotRefineTestService(t, "")
	source, hash, _ := s.studioExecutionSource(scene.ID)
	cases := []struct {
		name   string
		change func(*studioExecutionPlan)
	}{
		{"source", func(p *studioExecutionPlan) { p.SourceHash = "other" }},
		{"context", func(p *studioExecutionPlan) { p.ContextRevision = "old" }},
		{"missing-shot", func(p *studioExecutionPlan) { p.Shots = p.Shots[:1] }},
		{"reordered", func(p *studioExecutionPlan) { p.Shots[0], p.Shots[1] = p.Shots[1], p.Shots[0] }},
		{"duration", func(p *studioExecutionPlan) { p.Shots[0].Duration += 1 }},
		{"invented-evidence", func(p *studioExecutionPlan) { p.Shots[0].Evidence = []string{"凭空出现的巨龙"} }},
		{"invented-asset", func(p *studioExecutionPlan) { p.Objects = []studioExecutionObject{{ID: "x", AssetID: "invented"}} }},
		{"invented-person", func(p *studioExecutionPlan) {
			p.Bindings = []studioExecutionBinding{{Character: "画外旁白", ObjectID: "existing"}}
		}},
		{"invented-motion", func(p *studioExecutionPlan) {
			p.Shots[0].Motion = &studioExecutionMotion{ObjectID: "existing", ClipID: "sword", Start: []float64{0, 0, 0}, End: []float64{1, 0, 0}, Duration: 1}
		}},
		{"bad-camera", func(p *studioExecutionPlan) { p.Shots[0].Camera.Target = []float64{0, 2, 5} }},
		{"missing-diagnostics", func(p *studioExecutionPlan) { p.Shots[0].Blockers = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := executionPlanFixture(source, hash)
			tc.change(&p)
			if validateStudioExecutionPlan(&p, source, executionContextFixture(), hash) == nil {
				t.Fatal("accepted invalid plan")
			}
		})
	}
}
func TestStudioExecutionPlanDoesNotHideUnimplementedPerformance(t *testing.T) {
	source := studioExecutionSource{Scene: models.Scene{Content: "持剑迎敌", CharacterRolesSet: true, VisibleCharacters: "韩立"}, Shots: []models.Shot{{ID: 1, Duration: 3, CameraMovement: "环绕", Performance: "拔剑格挡", ActionTimeline: []models.ShotActionTimelineEntry{{Start: 0, End: 1}, {Start: 1, End: 3}}}}}
	p := executionPlanFixture(source, "hash")
	if err := validateStudioExecutionPlan(&p, source, executionContextFixture(), "hash"); err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 1 || len(p.Shots[0].Blockers) != 3 {
		t.Fatalf("missing blockers: %+v", p)
	}
	p = executionPlanFixture(source, "hash")
	p.Bindings = []studioExecutionBinding{{Character: "韩立", ObjectID: "existing"}}
	p.Shots[0].Motion = &studioExecutionMotion{ObjectID: "existing", ClipID: "walk-0", Start: []float64{0, 0, 0}, End: []float64{1, 0, 0}, Duration: 1}
	if err := validateStudioExecutionPlan(&p, source, executionContextFixture(), "hash"); err != nil {
		t.Fatal(err)
	}
	// Structural clip matching must never be sold as sword-motion semantic acceptance.
	if len(p.Shots[0].Blockers) < 2 {
		t.Fatal("lost unsupported camera/timeline blockers")
	}
}
func TestStudioExecutionPlanRejectsStaleSourceAndDisabledSkillBeforeModel(t *testing.T) {
	s, db, scene, _ := newShotRefineTestService(t, "")
	_, hash, _ := s.studioExecutionSource(scene.ID)
	provider := &stubTextProvider{}
	useExecutionProvider(t, s, provider)
	req := gin.H{"source_hash": hash, "context": executionContextFixture()}
	w := executionHTTP(t, s, scene.ProjectID+100, scene.ID, "POST", req)
	if w.Code != 404 {
		t.Fatalf("cross-project=%d", w.Code)
	}
	db.Model(&scene).Update("director_source_stale", true)
	w = executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", req)
	if w.Code != 409 {
		t.Fatalf("stale=%d", w.Code)
	}
	db.Model(&scene).Update("director_source_stale", false)
	_, hash, _ = s.studioExecutionSource(scene.ID)
	req["source_hash"] = hash
	db.Model(&models.Skill{}).Where("code = ?", studioExecutionSkillCode).Update("enabled", false)
	w = executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", req)
	if w.Code != 503 || provider.calls != 0 {
		t.Fatalf("disabled=%d calls=%d %s", w.Code, provider.calls, w.Body.String())
	}
}
func TestStudioExecutionPlanRejectsMalformedModelReply(t *testing.T) {
	s, _, scene, _ := newShotRefineTestService(t, "")
	source, hash, _ := s.studioExecutionSource(scene.ID)
	p := executionPlanFixture(source, hash)
	raw, _ := json.Marshal(p)
	for _, reply := range []string{string(raw) + "{}", strings.Replace(string(raw), "\"source_hash\"", "\"unexpected\"", 1), strings.Repeat("x", (1<<20)+1)} {
		useExecutionProvider(t, s, &stubTextProvider{response: reply})
		w := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", gin.H{"source_hash": hash, "context": executionContextFixture()})
		if w.Code != 422 {
			t.Fatalf("malformed=%d %s", w.Code, w.Body.String())
		}
	}
}
