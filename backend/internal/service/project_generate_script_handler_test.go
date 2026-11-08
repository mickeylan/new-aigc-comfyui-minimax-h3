package service

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func generateScriptRequest(s *Service, projectID uint, query, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/script", s.HandleGenerateScript)
	path := fmt.Sprintf("/api/projects/%d/script%s", projectID, query)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGenerateScriptRequiresExplicitConsistentEpisodeBeforeClaim(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "safe", Status: "script_done", PipelineStage: "", PipelineEpisode: 7}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Projects: &ProjectService{db: db}}
	for name, request := range map[string]struct {
		query string
		body  string
	}{
		"missing":  {body: `{}`},
		"invalid":  {query: `?episode_n=nope`, body: `{"episode_n":2}`},
		"conflict": {query: `?episode_n=2`, body: `{"episode_n":1}`},
	} {
		t.Run(name, func(t *testing.T) {
			w := generateScriptRequest(svc, project.ID, request.query, request.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var after models.Project
			if err := db.First(&after, project.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.PipelineStage != "" || after.PipelineEpisode != 7 || after.Status != "script_done" {
				t.Fatalf("invalid request claimed or mutated project: %+v", after)
			}
		})
	}
}

func TestClaimManualScriptPersistsRequestedEpisode(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "safe", Status: "script_done"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	service := &ProjectService{db: db}
	if err := service.ClaimManualScript(&project, 2); err != nil {
		t.Fatal(err)
	}
	var after models.Project
	if err := db.First(&after, project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.PipelineStage != "script_manual" || after.PipelineEpisode != 2 || after.Status != "producing" {
		t.Fatalf("wrong claimed episode: %+v", after)
	}
}

// 一次失败后按钮变灰的根因之一是 script_manual 锁长期残留：
// 新的请求永远收到“项目已有生成任务正在进行”。陈旧锁必须可自愈，
// 但仍要拒绝抢占仍在进行中的新鲜锁。
func TestClaimManualScriptReclaimsOnlyStaleManualLock(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "stale-claim", Status: "script_done"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	service := &ProjectService{db: db}
	if err := service.ClaimManualScript(&project, 2); err != nil {
		t.Fatal(err)
	}
	// 新鲜锁必须继续阻止并发提交。
	if err := service.ClaimManualScript(&project, 3); err == nil {
		t.Fatal("fresh script_manual claim must reject a concurrent request")
	}
	if err := db.Model(&models.Project{}).Where("id = ?", project.ID).UpdateColumn("updated_at", time.Now().Add(-manualScriptClaimTTL-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.ClaimManualScript(&project, 4); err != nil {
		t.Fatalf("stale manual claim was not reclaimed: %v", err)
	}
	var after models.Project
	if err := db.First(&after, project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.PipelineStage != "script_manual" || after.PipelineEpisode != 4 || after.Status != "producing" {
		t.Fatalf("stale claim was not refreshed: %+v", after)
	}
}

// 生成失败（包括 panic）必须释放 script_manual 锁，不能把项目永久卡在“生成中”。
func TestGenerateScriptFailureReleasesClaim(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "release", Status: "script_done", Synopsis: "故事"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Projects: &ProjectService{db: db}}
	w := generateScriptRequest(svc, project.ID, "?episode_n=2", `{"episode_n":2}`)
	if w.Code == http.StatusOK {
		t.Fatalf("generation without a text provider must not report success: %s", w.Body.String())
	}
	var after models.Project
	if err := db.First(&after, project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.PipelineStage == "script_manual" {
		t.Fatalf("failed generation kept the manual claim: %+v status=%d body=%s", after, w.Code, w.Body.String())
	}
}
