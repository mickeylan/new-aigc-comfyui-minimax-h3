package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHandleUpdateSceneContentWithExistingShotsPreservesImageAndReferences(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Shot{}, &models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "save content"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	refs := `[{"source_type":"asset","source_id":17,"variant":"image","use_krea2":true,"use_h3":true}]`
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Content: "旧正文", ImagePrompt: "保留画面提示", Duration: 8, ImageFile: "storyboard.png", ReferenceImagesJSON: refs, ImageLocked: true, Status: "image_ready"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Shot{SceneID: scene.ID, Order: 1, ShotType: "近景", Duration: 3, Description: "现有镜头"}).Error; err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"title":"场景","content":"仅修改后的正文","image_prompt":"保留画面提示","image_engine":"minimax_h3","scene_mode":"文戏","duration":8,"visual_type":"normal"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/projects/1/scenes/1", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(project.ID), 10)}, {Key: "sid", Value: strconv.FormatUint(uint64(scene.ID), 10)}}
	(&Service{DB: db, Projects: &ProjectService{db: db}}).HandleUpdateScene(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Content != "仅修改后的正文" || saved.ImageFile != "storyboard.png" || saved.ReferenceImagesJSON != refs || !saved.ImageLocked || saved.Status != "image_ready" {
		t.Fatalf("content save cleared scene media: %+v", saved)
	}
	var shots []models.Shot
	if err := db.Where("scene_id = ?", scene.ID).Find(&shots).Error; err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 || shots[0].Description != "现有镜头" {
		t.Fatalf("content save changed existing shots: %+v", shots)
	}
}
