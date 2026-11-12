package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/models"
)

// A crashed generation must not block the scene forever, but a generation that is
// genuinely running must still refuse a second click.
func TestClaimSceneImageTakesOverDeadClaimButRefusesLiveOne(t *testing.T) {
	newScene := func(t *testing.T, ps *ProjectService, status, token, taskID string) *models.Scene {
		t.Helper()
		sc := models.Scene{ProjectID: 1, EpisodeN: 1, Generation: 1, Order: 1, ImageFile: "old.png"}
		if err := ps.db.Create(&sc).Error; err != nil {
			t.Fatal(err)
		}
		if err := ps.db.Model(&models.Scene{}).Where("id = ?", sc.ID).Updates(map[string]any{
			"status": status, "image_token": token, "image_task_id": taskID,
		}).Error; err != nil {
			t.Fatal(err)
		}
		var stored models.Scene
		if err := ps.db.First(&stored, sc.ID).Error; err != nil {
			t.Fatal(err)
		}
		return &stored
	}
	freshToken := func(id uint) string { return fmt.Sprintf("%d-%d", id, time.Now().UnixNano()) }
	staleToken := func(id uint) string {
		return fmt.Sprintf("%d-%d", id, time.Now().Add(-2*sceneImageClaimStallTimeout).UnixNano())
	}

	t.Run("crashed before task submission is taken over", func(t *testing.T) {
		ps := newTestProjectService(t)
		sc := newScene(t, ps, "image_pending", staleToken(1), "")
		token, err := ps.claimSceneImage(sc)
		if err != nil || token == "" {
			t.Fatalf("stale pre-submit claim must be taken over, got err=%v", err)
		}
		var got models.Scene
		ps.db.First(&got, sc.ID)
		if got.ImageToken != token || got.Status != "image_pending" {
			t.Fatalf("takeover did not rebind the claim: %+v", got)
		}
	})

	t.Run("claim whose task record is gone is taken over", func(t *testing.T) {
		ps := newTestProjectService(t)
		sc := newScene(t, ps, "image_pending", freshToken(1), "ghost-task")
		if _, err := ps.claimSceneImage(sc); err != nil {
			t.Fatalf("claim with missing task must be taken over, got %v", err)
		}
	})

	t.Run("claim whose task already finished is taken over", func(t *testing.T) {
		for _, status := range []string{"success", "failed", "cancelled"} {
			ps := newTestProjectService(t)
			sc := newScene(t, ps, "image_pending", freshToken(1), "done-task")
			if err := ps.db.Create(&models.Task{TaskID: "done-task", Status: status}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := ps.claimSceneImage(sc); err != nil {
				t.Fatalf("task status %q is terminal and must be taken over, got %v", status, err)
			}
		}
	})

	t.Run("genuinely running generation is refused", func(t *testing.T) {
		for _, status := range []string{"pending", "queued", "running"} {
			ps := newTestProjectService(t)
			sc := newScene(t, ps, "image_pending", freshToken(1), "live-task")
			if err := ps.db.Create(&models.Task{TaskID: "live-task", Status: status}).Error; err != nil {
				t.Fatal(err)
			}
			_, err := ps.claimSceneImage(sc)
			if err == nil || !strings.Contains(err.Error(), "正在生成中") {
				t.Fatalf("task status %q is live and must be refused, got %v", status, err)
			}
			var got models.Scene
			ps.db.First(&got, sc.ID)
			if got.ImageToken != sc.ImageToken || got.ImageTaskID != "live-task" {
				t.Fatalf("refused claim must not disturb the live generation: %+v", got)
			}
		}
	})

	t.Run("fresh pre-submit claim is refused inside the stall window", func(t *testing.T) {
		ps := newTestProjectService(t)
		sc := newScene(t, ps, "image_pending", freshToken(1), "")
		if _, err := ps.claimSceneImage(sc); err == nil || !strings.Contains(err.Error(), "正在生成中") {
			t.Fatalf("a just-claimed scene must still refuse a double click, got %v", err)
		}
	})

	t.Run("live video phase is refused without touching scene state", func(t *testing.T) {
		for _, status := range []string{"video_creating", "video_pending", "video_running"} {
			ps := newTestProjectService(t)
			sc := newScene(t, ps, status, "", "")
			_, err := ps.claimSceneImage(sc)
			if err == nil || !strings.Contains(err.Error(), "视频") {
				t.Fatalf("status %q must refuse an image claim, got %v", status, err)
			}
			var got models.Scene
			ps.db.First(&got, sc.ID)
			if got.Status != status {
				t.Fatalf("refused claim changed status from %q to %q", status, got.Status)
			}
		}
	})

	t.Run("idle statuses still claim normally", func(t *testing.T) {
		for _, status := range []string{"pending", "failed", "image_ready", "video_ready"} {
			ps := newTestProjectService(t)
			sc := newScene(t, ps, status, "", "")
			if _, err := ps.claimSceneImage(sc); err != nil {
				t.Fatalf("status %q must be claimable, got %v", status, err)
			}
		}
	})
}
