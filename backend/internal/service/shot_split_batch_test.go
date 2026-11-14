package service

import (
	"net/http"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newShotSplitTestService(t *testing.T) (*Service, *gorm.DB, models.Project) {
	t.Helper()
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.ShotSplitBatch{}, &models.ShotSplitItem{}); err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	svc := &Service{DB: db, Shots: NewShotService(db)}
	svc.shotSplitLaunchFn = svc.runShotSplitBatch
	project := models.Project{Title: "p"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	return svc, db, project
}

func shotSplitScene(t *testing.T, db *gorm.DB, projectID uint, order int, content string, extra func(*models.Scene)) models.Scene {
	t.Helper()
	scene := models.Scene{ProjectID: projectID, EpisodeN: 1, Generation: 1, Order: order, Content: content, Duration: 8, Status: "pending"}
	if extra != nil {
		extra(&scene)
	}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	return scene
}

func shotSplitDraftResponse(descriptions ...string) (int, gin.H) {
	draft := &sceneDirectorDraft{}
	for _, description := range descriptions {
		draft.Shots = append(draft.Shots, sceneDirectorDraftShot{ShotType: "中景", Duration: 4, Description: description, Checks: []string{"audit"}})
	}
	return http.StatusOK, gin.H{"draft": draft, "dialogue_snapshot": ""}
}

func shotSplitItems(t *testing.T, db *gorm.DB, batchID uint) map[uint]models.ShotSplitItem {
	t.Helper()
	var items []models.ShotSplitItem
	if err := db.Where("batch_id = ?", batchID).Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	out := map[uint]models.ShotSplitItem{}
	for _, item := range items {
		out[item.SceneID] = item
	}
	return out
}

func sceneShotCount(t *testing.T, db *gorm.DB, sceneID uint) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestShotSplitBatchSkipsProtectedScenesAndRecordsPerSceneFailures(t *testing.T) {
	svc, db, p := newShotSplitTestService(t)
	ok := shotSplitScene(t, db, p.ID, 1, "柳石推门而入", nil)
	directed := shotSplitScene(t, db, p.ID, 2, "已有镜头", nil)
	db.Create(&models.Shot{SceneID: directed.ID, Order: 1, ShotType: "近景", Duration: 3})
	ready := shotSplitScene(t, db, p.ID, 3, "已出片", func(s *models.Scene) { s.Status = "video_ready" })
	working := shotSplitScene(t, db, p.ID, 4, "生成中", func(s *models.Scene) { s.Status = "video_running" })
	empty := shotSplitScene(t, db, p.ID, 5, "  ", nil)
	broken := shotSplitScene(t, db, p.ID, 6, "模型失败", nil)
	archived := shotSplitScene(t, db, p.ID, 7, "已归档", func(s *models.Scene) { s.Archived = true })
	var calls []uint
	svc.shotSplitDraftFn = func(scene *models.Scene) (int, gin.H) {
		calls = append(calls, scene.ID)
		if scene.ID == broken.ID {
			return http.StatusBadGateway, gin.H{"error": "provider failed"}
		}
		return shotSplitDraftResponse("推门", "对视")
	}
	detail, err := svc.CreateShotSplitBatch(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != ok.ID || calls[1] != broken.ID {
		t.Fatalf("draft calls=%v", calls)
	}
	items := shotSplitItems(t, db, detail.Batch.ID)
	if _, found := items[archived.ID]; found {
		t.Fatal("archived scene must not be part of the batch")
	}
	want := map[uint]string{directed.ID: "已有导演Shot", ready.ID: "视频已就绪", working.ID: "正在生成", empty.ID: "场景正文为空"}
	for sceneID, reason := range want {
		if items[sceneID].Status != shotSplitSkipped || items[sceneID].SkipReason != reason {
			t.Fatalf("scene %d item=%+v want skip %q", sceneID, items[sceneID], reason)
		}
	}
	if items[ok.ID].Status != shotSplitSaved || items[ok.ID].ShotCount != 2 || sceneShotCount(t, db, ok.ID) != 2 {
		t.Fatalf("saved item=%+v shots=%d", items[ok.ID], sceneShotCount(t, db, ok.ID))
	}
	if items[broken.ID].Status != shotSplitFailed || items[broken.ID].Error != "provider failed" || sceneShotCount(t, db, broken.ID) != 0 {
		t.Fatalf("failed item=%+v", items[broken.ID])
	}
	var batch models.ShotSplitBatch
	db.First(&batch, detail.Batch.ID)
	if batch.Status != shotSplitFailed || batch.TotalItems != 6 || batch.SavedItems != 1 || batch.SkippedItems != 4 || batch.FailedItems != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	if _, err := svc.CreateShotSplitBatch(p.ID, 1); err != nil {
		t.Fatalf("a finished batch must not block a new one: %v", err)
	}
}

func TestShotSplitBatchDiscardsDraftWhenSceneChangesDuringGeneration(t *testing.T) {
	svc, db, p := newShotSplitTestService(t)
	edited := shotSplitScene(t, db, p.ID, 1, "原始正文", nil)
	directed := shotSplitScene(t, db, p.ID, 2, "用户手动导演", nil)
	svc.shotSplitDraftFn = func(scene *models.Scene) (int, gin.H) {
		switch scene.ID {
		case edited.ID:
			db.Model(&models.Scene{}).Where("id = ?", edited.ID).Update("content", "用户改过的正文")
		case directed.ID:
			db.Create(&models.Shot{SceneID: directed.ID, Order: 1, ShotType: "近景", Duration: 3, Description: "人工"})
		}
		return shotSplitDraftResponse("A")
	}
	detail, err := svc.CreateShotSplitBatch(p.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	items := shotSplitItems(t, db, detail.Batch.ID)
	if items[edited.ID].Status != shotSplitFailed || !strings.Contains(items[edited.ID].Error, "正文在拆镜期间被修改") || sceneShotCount(t, db, edited.ID) != 0 {
		t.Fatalf("edited item=%+v", items[edited.ID])
	}
	if items[directed.ID].Status != shotSplitSkipped || sceneShotCount(t, db, directed.ID) != 1 {
		t.Fatalf("manual shots must win: item=%+v shots=%d", items[directed.ID], sceneShotCount(t, db, directed.ID))
	}
}

func TestShotSplitBatchPauseFencesRunnerAndResumeFinishes(t *testing.T) {
	svc, db, p := newShotSplitTestService(t)
	first := shotSplitScene(t, db, p.ID, 1, "第一场", nil)
	second := shotSplitScene(t, db, p.ID, 2, "第二场", nil)
	var batchID uint
	calls := 0
	svc.shotSplitLaunchFn = func(id uint) { batchID = id }
	svc.shotSplitDraftFn = func(scene *models.Scene) (int, gin.H) {
		calls++
		if scene.ID == first.ID {
			if _, err := svc.ControlShotSplitBatch(p.ID, 1, batchID, "pause"); err != nil {
				t.Fatal(err)
			}
		}
		return shotSplitDraftResponse("A")
	}
	if _, err := svc.CreateShotSplitBatch(p.ID, 1); err != nil {
		t.Fatal(err)
	}
	svc.runShotSplitBatch(batchID)
	items := shotSplitItems(t, db, batchID)
	if calls != 1 || items[first.ID].Status != shotSplitSaved || items[second.ID].Status != shotSplitQueued {
		t.Fatalf("pause must keep the in-flight result and stop further calls: calls=%d items=%+v", calls, items)
	}
	var batch models.ShotSplitBatch
	db.First(&batch, batchID)
	if batch.Status != shotSplitPaused {
		t.Fatalf("batch status=%s", batch.Status)
	}
	svc.shotSplitLaunchFn = svc.runShotSplitBatch
	if _, err := svc.ControlShotSplitBatch(p.ID, 1, batchID, "resume"); err != nil {
		t.Fatal(err)
	}
	items = shotSplitItems(t, db, batchID)
	db.First(&batch, batchID)
	if calls != 2 || items[second.ID].Status != shotSplitSaved || batch.Status != shotSplitCompleted || batch.SavedItems != 2 {
		t.Fatalf("resume: calls=%d items=%+v batch=%+v", calls, items, batch)
	}
}

func TestShotSplitBatchCancelDiscardsInFlightDraft(t *testing.T) {
	svc, db, p := newShotSplitTestService(t)
	first := shotSplitScene(t, db, p.ID, 1, "第一场", nil)
	second := shotSplitScene(t, db, p.ID, 2, "第二场", nil)
	var batchID uint
	svc.shotSplitLaunchFn = func(id uint) { batchID = id }
	svc.shotSplitDraftFn = func(scene *models.Scene) (int, gin.H) {
		if _, err := svc.ControlShotSplitBatch(p.ID, 1, batchID, "cancel"); err != nil {
			t.Fatal(err)
		}
		return shotSplitDraftResponse("A")
	}
	if _, err := svc.CreateShotSplitBatch(p.ID, 1); err != nil {
		t.Fatal(err)
	}
	svc.runShotSplitBatch(batchID)
	items := shotSplitItems(t, db, batchID)
	if items[first.ID].Status != shotSplitCancelled || sceneShotCount(t, db, first.ID) != 0 || items[second.ID].Status != shotSplitCancelled {
		t.Fatalf("cancel items=%+v shots=%d", items, sceneShotCount(t, db, first.ID))
	}
	var batch models.ShotSplitBatch
	db.First(&batch, batchID)
	if batch.Status != shotSplitCancelled {
		t.Fatalf("batch status=%s", batch.Status)
	}
}

func TestRecoverShotSplitBatchesPausesWithoutRerunningPaidCalls(t *testing.T) {
	svc, db, p := newShotSplitTestService(t)
	interrupted := shotSplitScene(t, db, p.ID, 1, "第一场", nil)
	pending := shotSplitScene(t, db, p.ID, 2, "第二场", nil)
	batch := models.ShotSplitBatch{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Status: shotSplitRunning, Revision: 3, TotalItems: 2}
	db.Create(&batch)
	db.Create(&models.ShotSplitItem{BatchID: batch.ID, SceneID: interrupted.ID, ItemOrder: 1, Status: shotSplitRunning, Attempts: 1})
	db.Create(&models.ShotSplitItem{BatchID: batch.ID, SceneID: pending.ID, ItemOrder: 2, Status: shotSplitQueued})
	calls := 0
	svc.shotSplitDraftFn = func(*models.Scene) (int, gin.H) { calls++; return shotSplitDraftResponse("A") }
	if err := svc.RecoverShotSplitBatches(); err != nil {
		t.Fatal(err)
	}
	items := shotSplitItems(t, db, batch.ID)
	db.First(&batch, batch.ID)
	if calls != 0 || batch.Status != shotSplitPaused || batch.Revision != 4 || items[interrupted.ID].Status != shotSplitFailed || items[pending.ID].Status != shotSplitQueued || batch.FailedItems != 1 {
		t.Fatalf("recover: calls=%d batch=%+v items=%+v", calls, batch, items)
	}
	if _, err := svc.ControlShotSplitBatch(p.ID, 1, batch.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	items = shotSplitItems(t, db, batch.ID)
	db.First(&batch, batch.ID)
	if calls != 1 || items[pending.ID].Status != shotSplitSaved || items[interrupted.ID].Status != shotSplitFailed || batch.Status != shotSplitFailed {
		t.Fatalf("resume after recover: calls=%d batch=%+v items=%+v", calls, batch, items)
	}
	if _, err := svc.ControlShotSplitBatch(p.ID, 1, batch.ID, "retry"); err != nil {
		t.Fatal(err)
	}
	items = shotSplitItems(t, db, batch.ID)
	db.First(&batch, batch.ID)
	if calls != 2 || items[interrupted.ID].Status != shotSplitSaved || items[interrupted.ID].Attempts != 2 || batch.Status != shotSplitCompleted {
		t.Fatalf("retry: calls=%d batch=%+v items=%+v", calls, batch, items)
	}
}

func TestShotsFromDirectorDraftDropsAuditChecks(t *testing.T) {
	_, response := shotSplitDraftResponse("A")
	shots, err := shotsFromDirectorDraft(response["draft"].(*sceneDirectorDraft))
	if err != nil || len(shots) != 1 || shots[0].ShotType != "中景" || shots[0].Duration != 4 || shots[0].DirectorSource != "" {
		t.Fatalf("shots=%+v err=%v", shots, err)
	}
	if _, err := shotsFromDirectorDraft(&sceneDirectorDraft{}); err == nil {
		t.Fatal("empty draft must fail")
	}
}
