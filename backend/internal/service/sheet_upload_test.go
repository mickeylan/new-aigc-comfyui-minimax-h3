package service

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// newSheetUploadService builds a Service whose UploadManager writes to a temp asset
// root, so四视图上传 can be exercised end to end without ComfyUI or a task service.
func newSheetUploadService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.Episode{}, &models.Scene{}, &models.Shot{},
		&models.Character{}, &models.CharacterLook{}, &models.SceneCharacterLook{},
		&models.CharacterOutfit{}, &models.SceneCharacterOutfit{},
		&models.ShotCharacterLook{}, &models.ShotCharacterOutfit{},
		&models.Asset{}, &models.AssetVariant{}, &models.UploadFile{},
		&models.SceneContinuity{}, &models.GenerationCandidate{}, &models.FrameCandidate{},
	); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Storage.AssetDir = t.TempDir()
	return &Service{
		DB:       db,
		Cfg:      cfg,
		Upload:   NewUploadManager(cfg, db, NewRemoteExec(config.RemoteConfig{})),
		Projects: &ProjectService{db: db},
	}, db
}

func sheetUploadContext(t *testing.T, params gin.Params, filename string, data []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Params = params
	return c, recorder
}

func uintParam(key string, value uint) gin.Param {
	return gin.Param{Key: key, Value: strconv.FormatUint(uint64(value), 10)}
}

// A hand-drawn four-view sheet must be usable directly. Before these routes existed
// only Krea2 generation could fill Character.Sheet / Asset.Sheet, so a supplied sheet
// could not become the identity reference the storyboard and H3 paths prefer.
func TestUploadedFourViewSheetReplacesGeneratedOne(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}

	t.Run("character sheet upload needs no portrait and clears the task", func(t *testing.T) {
		svc, db := newSheetUploadService(t)
		project := models.Project{Title: "p"}
		db.Create(&project)
		// No portrait on purpose: it is only the source image for generation.
		ch := models.Character{ProjectID: project.ID, Name: "柳石", SheetTaskID: "stale-task", SheetError: "上一次生成失败"}
		db.Create(&ch)

		c, w := sheetUploadContext(t, gin.Params{uintParam("id", project.ID), uintParam("cid", ch.ID)}, "sheet.png", png)
		svc.HandleUploadCharacterSheet(c)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}

		var saved models.Character
		if err := db.First(&saved, ch.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Sheet == "" {
			t.Fatal("四视图字段未写入")
		}
		if saved.SheetTaskID != "" || saved.SheetError != "" {
			t.Fatalf("残留生成状态 task=%q err=%q", saved.SheetTaskID, saved.SheetError)
		}

		var variant models.AssetVariant
		if err := db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, VariantCharacterSheet, ch.ID).First(&variant).Error; err != nil {
			t.Fatalf("四视图未进入版本历史: %v", err)
		}
		if !variant.Selected || variant.Provenance != "manual_upload" || variant.File != saved.Sheet {
			t.Fatalf("variant=%+v sheet=%q", variant, saved.Sheet)
		}
	})

	t.Run("prop sheet upload records an asset_sheet variant", func(t *testing.T) {
		svc, db := newSheetUploadService(t)
		project := models.Project{Title: "p"}
		db.Create(&project)
		asset := models.Asset{ProjectID: project.ID, Kind: AssetKindProp, Name: "旧拨浪鼓"}
		db.Create(&asset)

		c, w := sheetUploadContext(t, gin.Params{uintParam("id", project.ID), {Key: "kind", Value: AssetKindProp}, uintParam("aid", asset.ID)}, "sheet.png", png)
		svc.HandleUploadPropSheet(c)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}

		var saved models.Asset
		if err := db.First(&saved, asset.ID).Error; err != nil {
			t.Fatal(err)
		}
		if saved.Sheet == "" {
			t.Fatal("道具四视图字段未写入")
		}
		var variant models.AssetVariant
		if err := db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", project.ID, VariantAssetSheet, asset.ID).First(&variant).Error; err != nil {
			t.Fatalf("道具四视图未进入版本历史: %v", err)
		}
		if !variant.Selected || variant.Provenance != "manual_upload" {
			t.Fatalf("variant=%+v", variant)
		}
	})

	// models.go documents that location assets do not use Sheet, and StartPropSheet
	// refuses them; the upload path must agree instead of writing an orphan field.
	t.Run("location asset is refused", func(t *testing.T) {
		svc, db := newSheetUploadService(t)
		project := models.Project{Title: "p"}
		db.Create(&project)
		asset := models.Asset{ProjectID: project.ID, Kind: AssetKindLocation, Name: "山道"}
		db.Create(&asset)

		c, w := sheetUploadContext(t, gin.Params{uintParam("id", project.ID), {Key: "kind", Value: AssetKindLocation}, uintParam("aid", asset.ID)}, "sheet.png", png)
		svc.HandleUploadPropSheet(c)
		if w.Code != 400 {
			t.Fatalf("场景资产应被拒绝，实际 code=%d body=%s", w.Code, w.Body.String())
		}
		var saved models.Asset
		db.First(&saved, asset.ID)
		if saved.Sheet != "" {
			t.Fatalf("场景资产写入了四视图: %q", saved.Sheet)
		}
	})

	t.Run("empty upload is refused", func(t *testing.T) {
		svc, db := newSheetUploadService(t)
		project := models.Project{Title: "p"}
		db.Create(&project)
		ch := models.Character{ProjectID: project.ID, Name: "柳石"}
		db.Create(&ch)

		c, w := sheetUploadContext(t, gin.Params{uintParam("id", project.ID), uintParam("cid", ch.ID)}, "sheet.png", nil)
		svc.HandleUploadCharacterSheet(c)
		if w.Code != 400 {
			t.Fatalf("空文件应被拒绝，实际 code=%d body=%s", w.Code, w.Body.String())
		}
		var saved models.Character
		db.First(&saved, ch.ID)
		if saved.Sheet != "" {
			t.Fatalf("空文件写入了四视图: %q", saved.Sheet)
		}
	})
}
