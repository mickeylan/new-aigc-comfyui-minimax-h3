package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

// Offline upload/Scene contract followed by a real CreateTask path with a held
// executor and controlled localhost HTTP completion (no model or ComfyUI started).
// Exercise registered input resolution, immutable snapshots and output identity.
func TestStudioVideoSceneHandoffPreservesDerivation(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(fmt.Sprintf("project_scoped_%v", scoped), func(t *testing.T) {
			f := newTranslationHTTPFixture(t)
			local, cfg, _ := proxyTestService(t)
			if err := f.db.AutoMigrate(&models.Material{}, &models.GenerationSnapshot{}, &models.Task{}); err != nil {
				t.Fatal(err)
			}
			f.svc.Upload = NewUploadManager(cfg, f.db, local.Remote)
			f.svc.Materials = NewMaterialService(cfg, f.db, local.Remote, f.svc.Upload)
			f.svc.Projects.upload = f.svc.Upload
			router := gin.New()
			router.POST("/api/materials", f.svc.HandleUploadMaterial)
			router.GET("/api/projects/:id/scenes/:sid/generation-snapshots", f.svc.HandleListGenerationSnapshots)

			manifest := materialDerivationFixture(t)
			payload := []byte("synthetic-webm-contract-bytes-not-a-decoder-test")
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			for key, value := range map[string]string{"type": "video", "derivation_manifest": manifest} {
				if err := form.WriteField(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if scoped {
				if err := form.WriteField("project_id", fmt.Sprint(f.p.ID)); err != nil {
					t.Fatal(err)
				}
			}
			file, err := form.CreateFormFile("file", "studio-reviewed.webm")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := form.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/materials", &body)
			req.Header.Set("Content-Type", form.FormDataContentType())
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			var material models.Material
			if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &material) != nil || material.ID == 0 {
				t.Fatalf("upload=%d %s", out.Code, out.Body.String())
			}
			if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{"video_first_frame_img": "first.png", "video_last_frame_img": "last.png", "status": "image_ready"}).Error; err != nil {
				t.Fatal(err)
			}
			update, _ := json.Marshal(map[string]any{"master_prompt_zh": translationMaster, "ref3d_video": material.Name})
			out = translationRequest(f.svc, http.MethodPut, f.p.ID, f.sc.ID, "/master", string(update))
			if out.Code != 200 {
				t.Fatalf("Scene save=%d %s", out.Code, out.Body.String())
			}
			out = translationRequest(f.svc, http.MethodGet, f.p.ID, f.sc.ID, "", "")
			if out.Code != 200 || decodeTranslationBody(t, out)["ref3d_video"] != material.Name {
				t.Fatalf("Scene read=%d %s", out.Code, out.Body.String())
			}
			var saved models.Scene
			if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.VideoFirstFrameImg != "first.png" || saved.VideoLastFrameImg != "last.png" || saved.Status != "image_ready" || saved.VideoTaskID != "" {
				t.Fatal("manual reference save changed frame bindings or generation state")
			}
			files, err := f.svc.Projects.resolveScene3DRefVideoFiles(&saved)
			if err != nil {
				t.Fatal(err)
			}
			ref := files["ref_videos"][0]
			if ref.TaskID+"/"+ref.Name != material.Path {
				t.Fatalf("selected input=%s/%s actual material path=%s", ref.TaskID, ref.Name, material.Path)
			}
			actual, err := os.ReadFile(f.svc.Upload.ResolveReadPath(ref.TaskID, ref.Name))
			if err != nil || !bytes.Equal(actual, payload) {
				t.Fatalf("resolved input is not the uploaded bytes: %v", err)
			}
			// This deliberately uses a diagnostic fake task, not CreateTask or GenerateSceneVideo.
			f.svc.Projects.recordGenerationSnapshot(&saved, "video", &models.Task{TaskID: "offline-studio-contract", Prompt: "reviewed prompt", ParamsJSON: `{}`}, &models.Template{Code: h3Ref2V3DTemplate}, files)
			// A later material edit must not rewrite the already-recorded audit declaration.
			if err := f.db.Model(&material).Update("derivation_manifest", "").Error; err != nil {
				t.Fatal(err)
			}
			out = httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/projects/%d/scenes/%d/generation-snapshots?kind=video", f.p.ID, f.sc.ID), nil))
			var response struct {
				Items []models.GenerationSnapshot `json:"items"`
			}
			if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &response) != nil || len(response.Items) != 1 {
				t.Fatalf("snapshot read=%d %s", out.Code, out.Body.String())
			}
			var refs []generationSnapshotRef
			if err := json.Unmarshal([]byte(response.Items[0].RefsJSON), &refs); err != nil || len(refs) != 1 {
				t.Fatalf("snapshot refs: %v", err)
			}
			if refs[0].MaterialID != material.ID || string(refs[0].DerivationManifest) != manifest || refs[0].SHA256 != sha256Hex(payload) || refs[0].File != material.Path {
				t.Fatalf("snapshot lost source link: %+v", refs[0])
			}
			var taskCount int64
			if err := f.db.Model(&models.Task{}).Count(&taskCount).Error; err != nil || taskCount != 0 || f.calls.Load() != 0 {
				t.Fatalf("offline handoff unexpectedly generated: tasks=%d AI calls=%d err=%v", taskCount, f.calls.Load(), err)
			}
			t.Logf("STUDIO_SCENE_HANDOFF_OFFLINE=PASS scoped=%v upload=200 Scene=roundtrip frames=unchanged input=%s snapshot=retained tasks=0 providerCalls=0", scoped, material.Path)
			assertSceneMaterialTaskProvenance(t, f, material, manifest)
		})
	}
}

func TestScene3DRefVideoDirectoryIsolation(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	f.sc.Video3DRefVideo = "same.webm"
	for _, task := range []string{"material_p999", "materials", fmt.Sprintf("material_p%d", f.p.ID)} {
		if err := f.db.Create(&models.UploadFile{TaskID: task, Type: "video", Name: "same.webm", Path: task + "/same.webm"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	files, err := f.svc.Projects.resolveScene3DRefVideoFiles(&f.sc)
	if err != nil || files["ref_videos"][0].TaskID != fmt.Sprintf("material_p%d", f.p.ID) {
		t.Fatalf("current project must win over shared or unrelated upload: %+v %v", files, err)
	}
	f.db.Where("task_id = ?", fmt.Sprintf("material_p%d", f.p.ID)).Delete(&models.UploadFile{})
	files, err = f.svc.Projects.resolveScene3DRefVideoFiles(&f.sc)
	if err != nil || files["ref_videos"][0].TaskID != "materials" {
		t.Fatalf("shared fallback mismatch: %+v %v", files, err)
	}
	f.sc.Video3DRefVideo = "../escape.webm"
	if _, err := f.svc.Projects.resolveScene3DRefVideoFiles(&f.sc); err == nil {
		t.Fatal("unsafe video filename accepted")
	}
}
