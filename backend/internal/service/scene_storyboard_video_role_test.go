package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func storyboardRoleRequest(s *Service, projectID, sceneID uint, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PATCH("/api/projects/:id/scenes/:sid/storyboard-video-role", s.HandleUpdateSceneStoryboardVideoRole)
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/projects/%d/scenes/%d/storyboard-video-role", projectID, sceneID), bytes.NewBufferString(body))
	q.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, q)
	return w
}

func loadStoryboardRoleScene(t *testing.T, db *gorm.DB, id uint) models.Scene {
	t.Helper()
	var got models.Scene
	if err := db.First(&got, id).Error; err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStoryboardVideoRoleHTTPPreservesMediaAndInvalidUpdatesAreAtomic(t *testing.T) {
	s, db, scene := expandHTTPService(t, "http://unused.invalid")
	confirmed := time.Now().UTC().Truncate(time.Millisecond)
	seed := map[string]any{
		"image_file": "keep.png", "video_file": "keep.mp4", "video_task_id": "keep-task",
		"video_full_prompt": "manualDraft", "video_prompt_master_zh": "masterzh",
		"video_exec_source_hash": "source", "video_exec_confirmed_hash": "confirmed",
		"video_exec_confirmed_at": confirmed, "prompt_stale": false,
	}
	if err := db.Model(&models.Scene{}).Where("id = ?", scene.ID).Updates(seed).Error; err != nil {
		t.Fatal(err)
	}

	w := storyboardRoleRequest(s, scene.ProjectID, scene.ID, `{"storyboard_video_role":"end","storyboard_video_time":2}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body)
	}
	got := loadStoryboardRoleScene(t, db, scene.ID)
	if got.StoryboardVideoRole != "end" || got.StoryboardVideoTime != nil || !got.PromptStale {
		t.Fatalf("unexpected role save: %+v", got)
	}
	if got.VideoExecSourceHash != "" || got.VideoExecConfirmedHash != "" || got.VideoExecConfirmedAt != nil {
		t.Fatalf("confirmation not cleared: %+v", got)
	}
	if got.ImageFile != "keep.png" || got.VideoFile != "keep.mp4" || got.VideoTaskID != "keep-task" || got.VideoFullPrompt != "manualDraft" || got.VideoPromptMasterZH != "masterzh" {
		t.Fatalf("media/prompt changed: %+v", got)
	}

	w = storyboardRoleRequest(s, scene.ProjectID, scene.ID, `{"storyboard_video_role":"keyframe","storyboard_video_time":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("zero keyframe status=%d body=%s", w.Code, w.Body)
	}
	got = loadStoryboardRoleScene(t, db, scene.ID)
	if got.StoryboardVideoTime == nil || *got.StoryboardVideoTime != 0 {
		t.Fatalf("zero time not retained: %+v", got.StoryboardVideoTime)
	}

	for _, body := range []string{`{"storyboard_video_role":"bogus"}`, `{"storyboard_video_role":"keyframe","storyboard_video_time":-1}`, `{"storyboard_video_role":"keyframe","storyboard_video_time":5}`} {
		before := loadStoryboardRoleScene(t, db, scene.ID)
		w = storyboardRoleRequest(s, scene.ProjectID, scene.ID, body)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("body=%s status=%d response=%s", body, w.Code, w.Body)
		}
		after := loadStoryboardRoleScene(t, db, scene.ID)
		if after.StoryboardVideoRole != before.StoryboardVideoRole || !optionalFloatEqual(after.StoryboardVideoTime, before.StoryboardVideoTime) {
			t.Fatalf("invalid request wrote scene: before=%+v after=%+v", before, after)
		}
	}
}

func TestStoryboardVideoRoleHTTPProjectAndOptimisticLock(t *testing.T) {
	s, db, scene := expandHTTPService(t, "http://unused.invalid")
	original := loadStoryboardRoleScene(t, db, scene.ID)
	w := storyboardRoleRequest(s, scene.ProjectID+99, scene.ID, `{"storyboard_video_role":"start"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project status=%d body=%s", w.Code, w.Body)
	}
	if got := loadStoryboardRoleScene(t, db, scene.ID); got.StoryboardVideoRole != original.StoryboardVideoRole {
		t.Fatal("cross-project request wrote scene")
	}
	stale := original.UpdatedAt.Add(-time.Hour).Format(time.RFC3339Nano)
	w = storyboardRoleRequest(s, scene.ProjectID, scene.ID, fmt.Sprintf(`{"storyboard_video_role":"start","expected_updated_at":%q}`, stale))
	if w.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", w.Code, w.Body)
	}
	if got := loadStoryboardRoleScene(t, db, scene.ID); got.StoryboardVideoRole != original.StoryboardVideoRole {
		t.Fatal("stale request wrote scene")
	}
}

func TestSceneVideoReferenceFilesStoryboardRoles(t *testing.T) {
	ps := newTestProjectService(t)
	zero := 0.0
	cases := []struct {
		role    string
		tm      *float64
		present bool
		phrase  string
	}{
		{"start", nil, true, "0.00 秒开始画面"},
		{"keyframe", &zero, true, "约 0.00 秒"},
		{"end", nil, true, "结束状态参考"},
		{"none", nil, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			sc := models.Scene{ImageFile: "keep.png", StoryboardVideoRole: tc.role, StoryboardVideoTime: tc.tm}
			refs, lines := ps.sceneVideoReferenceFiles(&sc, "7")
			if tc.present {
				if len(refs) != 1 || refs[0].Name != "keep.png" || len(lines) != 1 || !strings.Contains(lines[0], tc.phrase) {
					t.Fatalf("refs=%+v lines=%+v", refs, lines)
				}
				if (openingPictureTag(lines) != "") != (tc.role == "start") {
					t.Fatalf("opening tag mismatch for %s: %q", tc.role, openingPictureTag(lines))
				}
			} else if len(refs) != 0 || len(lines) != 0 || openingPictureTag(lines) != "" {
				t.Fatalf("none emitted refs=%+v lines=%+v", refs, lines)
			}
		})
	}
	// Keyframe time is included in the real compiled prompt, including zero.
	for _, at := range []float64{0, 4.2} {
		sc := &models.Scene{ImageFile: "keep.png", Duration: 10, StoryboardVideoRole: "keyframe", StoryboardVideoTime: &at}
		_, refs := ps.sceneVideoReferenceFiles(sc, "7")
		prompt := buildMiniMaxH3RefPrompt(sc, nil, nil, refs)
		if !strings.Contains(prompt, "hard temporal frame lock") || strings.Contains(prompt, "exact full-frame opening") {
			t.Fatalf("keyframe temporal guidance lost: %s", prompt)
		}
	}
	// Explicit manual frames remain authoritative regardless of storyboard role.
	for _, role := range []string{"start", "keyframe", "end", "none"} {
		sc := &models.Scene{ImageFile: "story.png", StoryboardVideoRole: role, VideoFirstFrameImg: "manual-first.png", VideoLastFrameImg: "manual-last.png"}
		files := ps.buildVideoFilesForTemplate(sc, "7", "minimax_h3_first_last")
		if files["first_frame"][0].Name != "manual-first.png" || files["last_frame"][0].Name != "manual-last.png" {
			t.Fatalf("manual endpoints changed for %s: %+v", role, files)
		}
		sc.VideoFirstFrameImg = ""
		files = ps.buildVideoFilesForTemplate(sc, "7", "minimax_h3_i2v")
		if (len(files["first_frame"]) > 0) != (role == "start") {
			t.Fatalf("non-start silently used as first frame: %s %+v", role, files)
		}
	}
	// Empty persisted values are backward-compatible keyframes without a fabricated time phrase.
	_, lines := ps.sceneVideoReferenceFiles(&models.Scene{ImageFile: "keep.png"}, "7")
	b, _ := json.Marshal(lines)
	if len(lines) != 1 || !strings.Contains(lines[0], "关键帧") || strings.Contains(lines[0], "0.00") {
		t.Fatalf("default keyframe lines=%s", b)
	}
}
