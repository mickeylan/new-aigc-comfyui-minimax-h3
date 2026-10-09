package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func combatReferenceTestService(t *testing.T, root string) (*Service, models.Project) {
	t.Helper()
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "combat references"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	references, err := NewCombatReferenceService()
	if err != nil {
		t.Fatal(err)
	}
	return &Service{DB: db, Cfg: &config.Config{CombatReferenceDir: root}, CombatReferences: references}, project
}

func TestCombatReferenceHandlersEnforceProjectOwnershipAndExposeVersion(t *testing.T) {
	service, project := combatReferenceTestService(t, "")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/projects/:id/combat-references/search", service.HandleSearchCombatReferences)
	router.GET("/api/projects/:id/combat-references/:scope/:rid", service.HandleGetCombatReference)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/projects/"+strconv.FormatUint(uint64(project.ID), 10)+"/combat-references/search?scope=design&query=%E7%94%B5%E5%BD%B1%E7%BA%A7%E7%A8%B3%E5%AE%9A%E8%BF%9E%E7%BB%AD%E5%8A%A8%E4%BD%9C%E5%9B%A0%E6%9E%9C", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var search CombatReferenceSearchResult
	if err := json.Unmarshal(response.Body.Bytes(), &search); err != nil {
		t.Fatal(err)
	}
	if search.Primary == nil || search.Primary.ID != "02" || len(search.SourceCommit) != 40 {
		t.Fatalf("search=%+v", search)
	}

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/projects/999999/combat-references/design/02", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing project status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func TestCombatShowcaseHandlerServesOnlyRegisteredGIF(t *testing.T) {
	root := t.TempDir()
	relative := filepath.FromSlash("reference/action-storyboard-design/招式库/招式展示样本/27-居合拔刀与重兵器抢线专项.gif")
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	gif := []byte("GIF89a-test")
	if err := os.WriteFile(path, gif, 0o644); err != nil {
		t.Fatal(err)
	}
	service, project := combatReferenceTestService(t, root)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/projects/:id/combat-references/:scope/:rid/showcase", service.HandleGetCombatShowcase)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/projects/"+strconv.FormatUint(uint64(project.ID), 10)+"/combat-references/moves/27/showcase", nil))
	if response.Code != http.StatusOK || response.Body.String() != string(gif) {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "image/gif") {
		t.Fatalf("content-type=%q", response.Header().Get("Content-Type"))
	}

	noSample := httptest.NewRecorder()
	router.ServeHTTP(noSample, httptest.NewRequest(http.MethodGet, "/api/projects/"+strconv.FormatUint(uint64(project.ID), 10)+"/combat-references/design/02/showcase", nil))
	if noSample.Code != http.StatusNotFound {
		t.Fatalf("no sample status=%d", noSample.Code)
	}
}
