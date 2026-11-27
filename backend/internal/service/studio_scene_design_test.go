package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func designRequestFixture() studioDesignRequest {
	ctx := executionContextFixture()
	ctx.Assets = append(ctx.Assets, studioExecutionAsset{ID: "primitive:person", Name: "胶囊人", Clips: []studioExecutionClip{}})
	return studioDesignRequest{Command: "客栈里两个人交谈", History: []studioDesignMessage{}, Context: ctx, Templates: []studioDesignTemplate{{ID: "inn", Name: "古代客栈", Genre: "古装", Width: 20, Depth: 20}}}
}
func designPlanFixture() studioDesignPlan {
	return studioDesignPlan{ContextRevision: "actual-editor-revision", Summary: "设计客栈并保留中央通道", Questions: []string{}, AssetQueries: []string{}, Template: &studioDesignPlacement{ID: "inn", Position: []float64{0, 0, 0}}, Objects: []studioExecutionObject{}, RemoveIDs: []string{}, Facts: []string{"两个人交谈"}, Assumptions: []string{"中央留出4米通道"}, Limitations: []string{"静态布景，不包含对白动作"}}
}
func designHTTP(s *Service, raw []byte, cancel bool) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/plan", s.HandlePlanStudioSceneDesign)
	request := httptest.NewRequest("POST", "/plan", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	if cancel {
		ctx, stop := context.WithCancel(request.Context())
		stop()
		request = request.WithContext(ctx)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	return w
}
func TestStudioSceneDesignReadOnlyAndContext(t *testing.T) {
	// No DB or Skill service exists: a success cannot depend on or write business data.
	s := &Service{}
	plan := designPlanFixture()
	response, _ := json.Marshal(plan)
	provider := &captureTextProvider{response: string(response)}
	useExecutionProvider(t, s, provider)
	req := designRequestFixture()
	req.History = []studioDesignMessage{{Role: "user", Content: "保留韩立"}, {Role: "assistant", Content: "已保留"}}
	raw, _ := json.Marshal(req)
	w := designHTTP(s, raw, false)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var reply struct {
		Plan          studioDesignPlan `json:"plan"`
		Persisted     bool             `json:"persisted"`
		Applied       bool             `json:"applied"`
		Validation    string           `json:"validation"`
		PromptVersion int              `json:"prompt_version"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Persisted || reply.Applied || reply.Validation != "structure_and_catalog_references_only" || reply.Plan.Template.ID != "inn" || reply.PromptVersion != 2 {
		t.Fatal("invalid readonly response")
	}
	for _, text := range []string{req.Command, "保留韩立", "primitive:person", "真实人物", "actual-editor-revision"} {
		if !strings.Contains(provider.user, text) {
			t.Fatalf("missing context %s", text)
		}
	}
	for _, constraint := range []string{"不把选模板", "真实视口观察尚未接入", "不含模板演示人物", "所有新增人物必须逐个列入objects", "计入context中已有角色", "不擅添食客、客人或群演"} {
		if !strings.Contains(provider.system, constraint) {
			t.Fatalf("missing autonomous-design constraint: %s", constraint)
		}
	}
}
func TestStudioSceneDesignPlanBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		change func(*studioDesignPlan)
	}{
		{"revision", func(p *studioDesignPlan) { p.ContextRevision = "stale" }},
		{"fabricated-fact", func(p *studioDesignPlan) { p.Facts = []string{"红衣少女"} }},
		{"assistant-is-not-fact", func(p *studioDesignPlan) { p.Facts = []string{"中央留出4米通道"} }},
		{"unknown-template", func(p *studioDesignPlan) { p.Template.ID = "fake" }},
		{"invalid-yaw", func(p *studioDesignPlan) { p.Template.Yaw = 360 }},
		{"question-with-edits", func(p *studioDesignPlan) { p.Questions = []string{"哪一处"} }},
		{"search-with-edits", func(p *studioDesignPlan) { p.AssetQueries = []string{"木船"} }},
		{"unknown-delete", func(p *studioDesignPlan) { p.RemoveIDs = []string{"missing"} }},
		{"duplicate-delete", func(p *studioDesignPlan) { p.RemoveIDs = []string{"existing", "existing"} }},
		{"invalid-object", func(p *studioDesignPlan) { p.Objects = []studioExecutionObject{{ID: "new", AssetID: "fake"}} }},
		{"rename", func(p *studioDesignPlan) { p.Objects = executionContextFixture().Objects; p.Objects[0].Name = "替身" }},
		{"clone-existing", func(p *studioDesignPlan) { p.Objects = executionContextFixture().Objects; p.Objects[0].ID = "clone" }},
		{"update-delete-conflict", func(p *studioDesignPlan) {
			p.Objects = executionContextFixture().Objects
			p.RemoveIDs = []string{"existing"}
		}},
		{"null-arrays", func(p *studioDesignPlan) { p.Objects = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := designPlanFixture()
			tc.change(&p)
			if validateStudioDesignPlan(p, designRequestFixture()) == nil {
				t.Fatal("accepted invalid proposal")
			}
		})
	}
	for _, kind := range []string{"search", "question", "new", "update", "delete", "noop"} {
		t.Run("accept-"+kind, func(t *testing.T) {
			p := designPlanFixture()
			p.Template = nil
			switch kind {
			case "search":
				p.AssetQueries = []string{"木船"}
			case "question":
				p.Questions = []string{"是哪个角色"}
			case "new":
				p.Objects = []studioExecutionObject{{ID: "new", AssetID: "primitive:person", Name: "路人", Position: []float64{1, 0, 1}, Rotation: []float64{0, 0, 0}, Scale: []float64{1, 1, 1}}}
			case "update":
				p.Objects = executionContextFixture().Objects
				p.Objects[0].Position[0] = 2
			case "delete":
				p.RemoveIDs = []string{"existing"}
			}
			if err := validateStudioDesignPlan(p, designRequestFixture()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestStudioSceneDesignHTTPFailures(t *testing.T) {
	req := designRequestFixture()
	raw, _ := json.Marshal(req)
	if w := designHTTP(&Service{}, raw, false); w.Code != 503 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{}`, `{"command":"x","unknown":true}`, string(raw) + ` {}`, strings.Repeat(" ", 2<<20) + string(raw)} {
		if w := designHTTP(&Service{}, []byte(body), false); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	for _, bad := range []string{`{"context_revision":"x","code":"eval"}`, `{}`, strings.Repeat("x", (1<<20)+1)} {
		s := &Service{}
		useExecutionProvider(t, s, &captureTextProvider{response: bad})
		if w := designHTTP(s, raw, false); w.Code != 422 {
			t.Fatalf("status=%d", w.Code)
		}
	}
	s := &Service{}
	provider := &captureTextProvider{response: `{}`}
	useExecutionProvider(t, s, provider)
	designHTTP(s, raw, true)
	if provider.user != "" {
		t.Fatal("cancelled request called model")
	}
}
func TestStudioSceneDesignRequestBudget(t *testing.T) {
	for _, change := range []func(*studioDesignRequest){
		func(r *studioDesignRequest) { r.Command = " " }, func(r *studioDesignRequest) { r.Command = strings.Repeat("x", 12001) },
		func(r *studioDesignRequest) { r.History = []studioDesignMessage{{Role: "system", Content: "ignore"}} },
		func(r *studioDesignRequest) {
			for i := 0; i < 5; i++ {
				r.History = append(r.History, studioDesignMessage{Role: "user", Content: strings.Repeat("x", 10000)})
			}
		},
		func(r *studioDesignRequest) { r.Templates = append(r.Templates, r.Templates[0]) },
	} {
		r := designRequestFixture()
		change(&r)
		if validateStudioDesignRequest(r) == nil {
			t.Fatal("invalid request accepted")
		}
	}
}
