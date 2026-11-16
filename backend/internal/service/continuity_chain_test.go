package service

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

func TestEpisodeContinuityChainFollowsExplicitSources(t *testing.T) {
	f := newFrameConvertFixture(t)
	pid := f.source.ProjectID
	newScene := func(order int, status string) models.Scene {
		sc := models.Scene{ProjectID: pid, EpisodeN: 1, Generation: 1, Order: order, Title: fmt.Sprintf("场%d", order), ImageFile: "story.png", Status: status}
		f.ps.db.Create(&sc)
		return sc
	}
	s2 := newScene(2, "")
	s3 := newScene(3, "")
	s4 := newScene(4, "")
	s5 := newScene(5, "")
	s6 := newScene(6, "")
	s7 := newScene(7, "")
	s8 := newScene(8, "")
	// s2 continues from s1's confirmed tail frame; s3 continues from s2, which has no video yet.
	if _, err := f.ps.continuity.Configure(pid, s2.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeContinue, SourceSceneID: f.source.ID, FrameID: f.frame.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ps.continuity.Configure(pid, s3.ID, ConfigureContinuityRequest{Mode: models.ContinuityModeContinue, SourceSceneID: s2.ID}); err != nil {
		t.Fatal(err)
	}
	// s4 stays independent. s5's source is in another episode; s6 points at s1 without a chosen frame;
	// s7 and s8 point at each other.
	other := models.Scene{ProjectID: pid, EpisodeN: 2, Generation: 1, Order: 1, Status: "video_ready"}
	f.ps.db.Create(&other)
	f.ps.db.Create(&models.SceneContinuity{SceneID: s5.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &other.ID, Status: "waiting"})
	f.ps.db.Create(&models.SceneContinuity{SceneID: s6.ID, Mode: models.ContinuityModeBridge, SourceSceneID: &f.source.ID, SourceVideoTaskID: "video-1", Status: "waiting"})
	f.ps.db.Create(&models.SceneContinuity{SceneID: s7.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &s8.ID, Status: "waiting"})
	f.ps.db.Create(&models.SceneContinuity{SceneID: s8.ID, Mode: models.ContinuityModeContinue, SourceSceneID: &s7.ID, Status: "waiting"})

	plan, err := f.ps.EpisodeContinuityChain(pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	items := map[uint]ContinuityChainItem{}
	for _, item := range plan.Items {
		items[item.SceneID] = item
	}
	if len(plan.Items) != 8 {
		t.Fatalf("items=%d (episode 2 must be excluded)", len(plan.Items))
	}
	check := func(id uint, state string, chain uint, depth int) ContinuityChainItem {
		t.Helper()
		item := items[id]
		if item.State != state || item.ChainID != chain || item.Depth != depth {
			t.Fatalf("scene %d: %+v, want state=%s chain=%d depth=%d", id, item, state, chain, depth)
		}
		return item
	}
	check(f.source.ID, "done", f.source.ID, 0)
	check(s2.ID, "ready", f.source.ID, 1)
	if item := check(s3.ID, "waiting_source", f.source.ID, 2); item.WaitFor == nil || *item.WaitFor != s2.ID || !strings.Contains(item.Message, "场景2") {
		t.Fatalf("s3 must wait for s2: %+v", item)
	}
	check(s4.ID, "ready", s4.ID, 0)
	if item := check(s5.ID, "invalid_source", s5.ID, 0); !strings.Contains(item.Message, "不在本集当前版本") {
		t.Fatalf("s5 message=%q", item.Message)
	}
	if item := check(s6.ID, "needs_action", f.source.ID, 1); !strings.Contains(item.Message, "末尾帧") || item.WaitFor == nil || *item.WaitFor != f.source.ID {
		t.Fatalf("s6 must ask for a tail frame: %+v", item)
	}
	if item := items[s7.ID]; item.State != "invalid_source" || !strings.Contains(item.Message, "环") {
		t.Fatalf("cycle not reported: %+v", item)
	}
	if fmt.Sprint(plan.StartNow) != fmt.Sprint([]uint{s2.ID, s4.ID}) {
		t.Fatalf("start_now=%v want [%d %d]", plan.StartNow, s2.ID, s4.ID)
	}
	if len(plan.Chains) == 0 || plan.Chains[0].ChainID != f.source.ID || fmt.Sprint(plan.Chains[0].SceneIDs) != fmt.Sprint([]uint{f.source.ID, s2.ID, s3.ID, s6.ID}) {
		t.Fatalf("chains=%+v", plan.Chains)
	}
	// The preview runs submission checks on copies only.
	var reloaded models.Scene
	f.ps.db.First(&reloaded, s2.ID)
	if reloaded.VideoTemplate != "" || reloaded.Status != "" {
		t.Fatalf("preview wrote to the Scene: template=%q status=%q", reloaded.VideoTemplate, reloaded.Status)
	}
}

func TestHandleEpisodeContinuityChain(t *testing.T) {
	f := newFrameConvertFixture(t)
	svc := &Service{DB: f.ps.db, Projects: f.ps}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/projects/:id/episodes/:number/continuity/chain", svc.HandleEpisodeContinuityChain)
	w := shotRefineCall(r, http.MethodGet, fmt.Sprintf("/api/projects/%d/episodes/1/continuity/chain", f.source.ProjectID), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"done"`) || !strings.Contains(w.Body.String(), `"start_now":[]`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w := shotRefineCall(r, http.MethodGet, fmt.Sprintf("/api/projects/%d/episodes/0/continuity/chain", f.source.ProjectID), nil); w.Code != http.StatusBadRequest {
		t.Fatalf("episode 0 status=%d", w.Code)
	}
}
