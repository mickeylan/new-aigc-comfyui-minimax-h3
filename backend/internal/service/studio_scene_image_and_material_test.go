package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// 2D 画布写回：新图成为当前分镜图候选，旧图降级但保留可切回；
// 已生成的视频不被删除（用户仍可在候选中切回），但在跑的成片合并必须标失败。
func TestSaveStudioSceneImageCandidateKeepsOldImageAndFailsPendingMerge(t *testing.T) {
	db := newStudioCandidateDB(t)
	project := models.Project{Title: "studio-image"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{
		ProjectID: project.ID, EpisodeN: 1, Order: 1,
		ImageFile: "old.png", ImageTaskID: "old-image-task",
		VideoFile: "old.mp4", VideoInputFile: "old.mp4", VideoTaskID: "old-video-task",
		Status: "video_ready",
	}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	oldImage := models.GenerationCandidate{ProjectID: project.ID, EntityType: "scene", EntityID: scene.ID, MediaType: "image", TaskID: "old-image-task", File: "old.png", IsCurrent: true}
	if err := db.Create(&oldImage).Error; err != nil {
		t.Fatal(err)
	}
	merge := models.MergeTask{ProjectID: project.ID, EpisodeN: 1, Status: "running"}
	if err := db.Create(&merge).Error; err != nil {
		t.Fatal(err)
	}

	svc := &ProjectService{db: db}
	candidate, err := svc.SaveStudioSceneImageCandidate(&scene, "/tmp/canvas/画布精修.png", "画布精修.png")
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.IsCurrent || candidate.MediaType != "image" || candidate.File != "画布精修.png" {
		t.Fatalf("新图必须是当前的图片候选：%+v", candidate)
	}
	if !strings.HasPrefix(candidate.TaskID, "studio-image-") {
		t.Fatalf("画布候选应带独立 task id：%q", candidate.TaskID)
	}
	var provenance map[string]any
	if err := json.Unmarshal([]byte(candidate.ProvenanceJSON), &provenance); err != nil {
		t.Fatalf("provenance 不是合法 JSON：%v", err)
	}
	if provenance["source"] != "studio_canvas" || provenance["source_filename"] != "画布精修.png" {
		t.Fatalf("候选来源不可追溯：%v", provenance)
	}

	var demoted models.GenerationCandidate
	if err := db.First(&demoted, oldImage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if demoted.IsCurrent {
		t.Fatal("旧分镜图应降级为历史候选，不能被静默丢弃")
	}

	var saved models.Scene
	if err := db.First(&saved, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ImageFile != "画布精修.png" || saved.Status != "video_ready" {
		t.Fatalf("分镜图应切到精修版且保留已完成视频：%q %q", saved.ImageFile, saved.Status)
	}
	if saved.VideoInputFile != "old.mp4" {
		t.Fatalf("写回分镜图不应删除已生成的视频：%q", saved.VideoInputFile)
	}
	var merged models.MergeTask
	if err := db.First(&merged, merge.ID).Error; err != nil {
		t.Fatal(err)
	}
	if merged.Status != "failed" {
		t.Fatalf("在跑的成片合并必须标失败等待重新确认，实际 %q", merged.Status)
	}

	// 空文件/空场景必须拒绝，避免写出没有来源的候选。
	if _, err := svc.SaveStudioSceneImageCandidate(&saved, "  ", "空.png"); err == nil {
		t.Fatal("空文件名应被拒绝")
	}
	if _, err := svc.SaveStudioSceneImageCandidate(nil, "a.png", "a.png"); err == nil {
		t.Fatal("空场景应被拒绝")
	}
}

// HTTP 层：画布写回用 multipart 的 as_candidate=1 触发候选通道，没这个字段时保持原来的替换语义。
func TestHandleUploadSceneImageCandidateFlagChoosesCandidatePath(t *testing.T) {
	db := newStudioCandidateDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "upload-candidate"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, ImageFile: "old.png", ImageTaskID: "old-task", Status: "image_ready"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")},
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	svc := &Service{
		DB:       db,
		Upload:   NewUploadManager(cfg, db, remote),
		Projects: &ProjectService{db: db},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/scenes/:sid/image/upload", svc.HandleUploadSceneImage)

	upload := func(asCandidate bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "canvas.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("\x89PNG\r\n\x1a\ncanvas")); err != nil {
			t.Fatal(err)
		}
		if asCandidate {
			if err := writer.WriteField("as_candidate", "1"); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/scenes/%d/image/upload", project.ID, scene.ID), &body)
		q.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, q)
		return w
	}

	w := upload(true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "candidate_id") {
		t.Fatalf("候选写回 status=%d body=%s", w.Code, w.Body.String())
	}
	var candidates []models.GenerationCandidate
	if err := db.Where("entity_id = ? AND media_type = ?", scene.ID, "image").Find(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || !candidates[0].IsCurrent {
		t.Fatalf("候选写回应只新建一条当前候选：%+v", candidates)
	}

	// 不带标记仍走原来的「直接替换」，不额外产生候选。
	w = upload(false)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "candidate_id") {
		t.Fatalf("普通上传 status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	if err := db.Model(&models.GenerationCandidate{}).Where("entity_id = ? AND media_type = ?", scene.ID, "image").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("普通上传不应新增候选，实际 %d 条", count)
	}
}

// HTTP 层：3D 素材的缩略图字段与扩展名白名单（素材页和 3D 编辑器都走这个接口）。
func TestHandleUploadMaterial3DAcceptsThumbnailAndRejectsOtherExtensions(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.UploadFile{}, &models.Material{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")},
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	svc := &Service{DB: db, Upload: upload, Materials: NewMaterialService(cfg, db, remote, upload)}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/materials", svc.HandleUploadMaterial)

	send := func(name string, withThumb bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("type", "3d"); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("glTF-binary")); err != nil {
			t.Fatal(err)
		}
		if withThumb {
			thumb, err := writer.CreateFormFile("thumbnail", "thumb.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := thumb.Write([]byte("\x89PNG\r\n\x1a\nthumb")); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		q := httptest.NewRequest(http.MethodPost, "/api/materials", &body)
		q.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, q)
		return w
	}

	w := send("scene.glb", true)
	if w.Code != http.StatusOK {
		t.Fatalf("3D 上传 status=%d body=%s", w.Code, w.Body.String())
	}
	var mat models.Material
	if err := json.Unmarshal(w.Body.Bytes(), &mat); err != nil {
		t.Fatalf("响应不是素材 JSON：%v %s", err, w.Body.String())
	}
	if mat.Type != "3d" || mat.Source != "studio_3d" || mat.ThumbPath == "" {
		t.Fatalf("3D 素材应带缩略图且来源为 studio_3d：%+v", mat)
	}
	if w := send("note.txt", false); w.Code != http.StatusBadRequest {
		t.Fatalf("非 3D 扩展名应被拒绝，status=%d body=%s", w.Code, w.Body.String())
	}
}

// 3D 编辑器存入素材库：模型按 type=3d 记一条素材，缩略图与模型同目录保存。
func TestSaveStudioModelStoresThumbnailForMaterialLibrary(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.UploadFile{}, &models.Material{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")},
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	materials := NewMaterialService(cfg, db, remote, upload)

	projectID := uint(7)
	png := []byte("\x89PNG\r\n\x1a\n-thumb")
	mat, err := materials.SaveStudioModel(&projectID, "scene-1700000000.glb", []byte("glTF-binary"), png)
	if err != nil {
		t.Fatal(err)
	}
	if mat.Type != "3d" || mat.Source != "studio_3d" {
		t.Fatalf("3D 素材类型/来源不对：%+v", mat)
	}
	if mat.ThumbPath != "material_p7/scene-1700000000_thumb.png" {
		t.Fatalf("缩略图路径不对：%q", mat.ThumbPath)
	}
	for _, rel := range []string{mat.Path, mat.ThumbPath} {
		if _, err := os.Stat(filepath.Join(cfg.AssetsRoot(), filepath.FromSlash(rel))); err != nil {
			t.Fatalf("素材文件未落盘 %s: %v", rel, err)
		}
	}
	// 素材列表按 type 过滤时能取到这条 3D 素材。
	list, err := materials.List("3d", &projectID)
	if err != nil || len(list) != 1 || list[0].ID != mat.ID {
		t.Fatalf("3D 素材列表 %+v %v", list, err)
	}
	// SaveFile 的白名单必须接受 3d，否则上传会被「文件类型无效」挡住。
	if _, _, err := upload.SaveFile("materials", "3d", "mesh.obj", []byte("o")); err != nil {
		t.Fatalf("SaveFile 应接受 3d 类型：%v", err)
	}
	if _, _, err := upload.SaveFile("materials", "script", "x.txt", []byte("x")); err == nil {
		t.Fatal("未知类型仍应被拒绝")
	}
}