package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func blockingDraftRequest(s *Service, pid, sid uint, kind, action string, body any) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/:id/:sid/:kind/preview", s.HandlePreviewSceneBlockingPrompt)
	r.POST("/:id/:sid/:kind/apply", s.HandleApplySceneBlockingPrompt)
	raw, _ := json.Marshal(body)
	q := httptest.NewRequest("POST", fmt.Sprintf("/%d/%d/%s/%s", pid, sid, kind, action), bytes.NewReader(raw))
	q.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func TestBlockingDraftPreviewReadOnlyAndAtomicAdoption(t *testing.T) {
	for _, kind := range []string{"image", "video"} {
		t.Run(kind, func(t *testing.T) {
			f := newTranslationHTTPFixture(t)
			if err := f.db.AutoMigrate(&models.Episode{}, &models.PromptPolicyOverride{}); err != nil {
				t.Fatal(err)
			}
			provider := &captureTextProvider{response: "[Shot 1] A stationary figure stands beside the door, seen in profile with soft daylight."}
			f.svc.Projects.textProvider = provider
			var before models.Scene
			f.db.First(&before, f.sc.ID)
			draft := SceneBlocking{SchemaVersion: 2, Facts: []string{"原文事实保留"}, Suggestions: []string{"【3D预演建议，非原文事实】甲位于X2、Y0、Z0米"}, Characters: []SceneBlockingCharacter{{Name: "甲", Position: "X2、Y0、Z0米", Facing: "面向乙，相对镜头侧面", Gaze: "原视线"}}, Camera: "双方侧面", Start: "原起始状态", End: "原结束状态"}
			w := blockingDraftRequest(f.svc, f.p.ID, f.sc.ID, kind, "preview", map[string]any{"blocking": draft})
			if w.Code != 200 {
				t.Fatalf("preview=%d %s", w.Code, w.Body.String())
			}
			out := decodeTranslationBody(t, w)
			if !strings.Contains(provider.user, "相对镜头侧面") || !strings.Contains(provider.user, "原文事实保留") {
				t.Fatalf("draft not in model context: %s", provider.user)
			}
			var after models.Scene
			f.db.First(&after, f.sc.ID)
			if preparationJSONVersion(before) != preparationJSONVersion(after) {
				t.Fatal("preview wrote Scene")
			}
			payload := map[string]any{"prompt": "reviewed prompt", "blocking": out["blocking"], "blocking_hash": out["blocking_hash"], "source_version": out["source_version"], "confirm_replace": true}
			if kind == "video" {
				payload["prompt"] = "detailed_description:\n[Shot 1] A person stands beside the door."
			}
			w = blockingDraftRequest(f.svc, f.p.ID, f.sc.ID, kind, "apply", payload)
			if w.Code != 200 {
				t.Fatalf("adopt=%d %s", w.Code, w.Body.String())
			}
			f.db.First(&after, f.sc.ID)
			if after.BlockingJSON == "" || after.ImageFile != before.ImageFile || after.VideoFile != before.VideoFile || after.Content != before.Content {
				t.Fatal("adoption lost blocking or changed authority/media")
			}
			if kind == "image" {
				if after.ImagePrompt != "reviewed prompt" || after.BlockingImageAppliedHash == "" || after.VideoFullPrompt != before.VideoFullPrompt {
					t.Fatal("image adoption mismatch")
				}
			} else if after.BlockingVideoAppliedHash == "" || after.VideoFullPrompt != payload["prompt"] {
				t.Fatal("video adoption mismatch")
			}
			w = blockingDraftRequest(f.svc, f.p.ID, f.sc.ID, kind, "apply", payload)
			if w.Code != 409 {
				t.Fatalf("obsolete preview reused: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestBlockingDraftRejectsChangedAuthorityAndRequiresConfirmation(t *testing.T) {
	for _, change := range []string{"scene", "shot", "dialogue", "project", "confirmation", "blocking"} {
		t.Run(change, func(t *testing.T) {
			f := newTranslationHTTPFixture(t)
			var scene models.Scene
			f.db.First(&scene, f.sc.ID)
			version, err := f.svc.blockingDraftSource(f.db, &scene)
			if err != nil {
				t.Fatal(err)
			}
			draft := SceneBlocking{SourceContentHash: sceneDialogueContentHash(scene.Content), Camera: "side"}
			payload := map[string]any{"prompt": "reviewed replacement", "blocking": draft, "blocking_hash": blockingDigest(blockingJSON(draft)), "source_version": version, "confirm_replace": true}
			switch change {
			case "scene":
				f.db.Model(&scene).Update("content", "changed")
			case "shot":
				f.db.Model(&models.Shot{}).Where("scene_id=?", scene.ID).Update("description", "changed")
			case "dialogue":
				f.db.Create(&models.Dialogue{SceneID: scene.ID, ProjectID: f.p.ID, Text: "new words", Order: 1})
			case "project":
				f.db.Model(&f.p).Update("style", "changed")
			case "confirmation":
				payload["confirm_replace"] = false
			case "blocking":
				payload["blocking_hash"] = "obsolete"
			}
			var before models.Scene
			f.db.First(&before, scene.ID)
			w := blockingDraftRequest(f.svc, f.p.ID, scene.ID, "image", "apply", payload)
			expected := 409
			if change == "confirmation" {
				expected = 400
			}
			if w.Code != expected {
				t.Fatalf("status=%d want=%d body=%s", w.Code, expected, w.Body.String())
			}
			var after models.Scene
			f.db.First(&after, scene.ID)
			if preparationJSONVersion(before) != preparationJSONVersion(after) {
				t.Fatal("rejected adoption wrote Scene")
			}
		})
	}
}
