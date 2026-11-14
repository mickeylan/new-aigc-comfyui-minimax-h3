package service

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	shotSplitQueued    = "queued"
	shotSplitRunning   = "running"
	shotSplitPaused    = "paused"
	shotSplitCompleted = "completed"
	shotSplitFailed    = "failed"
	shotSplitCancelled = "cancelled"
	shotSplitSaved     = "saved"
	shotSplitSkipped   = "skipped"
)

var shotSplitWorkingStatuses = []string{"image_pending", "video_pending", "video_creating", "video_running"}

type ShotSplitBatchDetail struct {
	Batch models.ShotSplitBatch  `json:"batch"`
	Items []models.ShotSplitItem `json:"items"`
}

func shotSplitContentHash(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(content))))
}

// shotSplitSkipReason protects Scenes the user already directed, produced or is
// generating. It is re-checked right before saving, not only when the batch is created.
func (s *Service) shotSplitSkipReason(scene *models.Scene) (string, error) {
	var shots int64
	if err := s.DB.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Count(&shots).Error; err != nil {
		return "", err
	}
	switch {
	case shots > 0:
		return "已有导演Shot", nil
	case scene.Status == "video_ready":
		return "视频已就绪", nil
	case slices.Contains(shotSplitWorkingStatuses, scene.Status):
		return "正在生成", nil
	case strings.TrimSpace(scene.Content) == "":
		return "场景正文为空", nil
	}
	return "", nil
}

func (s *Service) shotSplitDraft(scene *models.Scene) (int, gin.H) {
	if s.shotSplitDraftFn != nil {
		return s.shotSplitDraftFn(scene)
	}
	return s.generateSceneDirectorDraft(scene, sceneDirectorDraftRequest{Mode: "standard"})
}

func (s *Service) launchShotSplitBatch(batchID uint) {
	if s.shotSplitLaunchFn != nil {
		s.shotSplitLaunchFn(batchID)
		return
	}
	go s.runShotSplitBatch(batchID)
}

// CreateShotSplitBatch persists every Scene of the episode's current generation as an
// item, so progress, skips and per-Scene errors survive page reloads and restarts.
func (s *Service) CreateShotSplitBatch(projectID uint, episodeN int) (*ShotSplitBatchDetail, error) {
	var active int64
	if err := s.DB.Model(&models.ShotSplitBatch{}).Where("project_id = ? AND episode_n = ? AND status IN ?", projectID, episodeN, []string{shotSplitQueued, shotSplitRunning, shotSplitPaused}).Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, fmt.Errorf("本集已有未结束的拆镜批次，请继续或取消后再发起")
	}
	generation, err := latestEpisodeGeneration(s.DB, projectID, episodeN)
	if err != nil {
		return nil, err
	}
	var scenes []models.Scene
	if err := s.DB.Where("project_id = ? AND episode_n = ? AND generation = ? AND (archived = ? OR archived IS NULL)", projectID, episodeN, generation, false).
		Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	reasons := make([]string, len(scenes))
	for i := range scenes {
		if reasons[i], err = s.shotSplitSkipReason(&scenes[i]); err != nil {
			return nil, err
		}
	}
	batch := models.ShotSplitBatch{ProjectID: projectID, EpisodeN: episodeN, Generation: generation, Status: shotSplitQueued, Revision: 1, TotalItems: len(scenes)}
	items := make([]models.ShotSplitItem, 0, len(scenes))
	queued := 0
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		for i := range scenes {
			reason := reasons[i]
			item := models.ShotSplitItem{BatchID: batch.ID, SceneID: scenes[i].ID, ItemOrder: i + 1, Status: shotSplitQueued, ContentHash: shotSplitContentHash(scenes[i].Content)}
			if reason != "" {
				item.Status, item.SkipReason = shotSplitSkipped, reason
			} else {
				queued++
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			items = append(items, item)
		}
		batch.SkippedItems = len(scenes) - queued
		updates := map[string]any{"skipped_items": batch.SkippedItems}
		if queued == 0 {
			now := time.Now()
			batch.Status, batch.FinishedAt = shotSplitCompleted, &now
			updates["status"], updates["finished_at"] = batch.Status, now
		}
		return tx.Model(&batch).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	if queued > 0 {
		s.launchShotSplitBatch(batch.ID)
	}
	return &ShotSplitBatchDetail{Batch: batch, Items: items}, nil
}

func (s *Service) GetShotSplitBatch(projectID uint, episodeN int, batchID uint) (*ShotSplitBatchDetail, error) {
	var batch models.ShotSplitBatch
	if err := s.DB.Where("id = ? AND project_id = ? AND episode_n = ?", batchID, projectID, episodeN).First(&batch).Error; err != nil {
		return nil, err
	}
	var items []models.ShotSplitItem
	if err := s.DB.Where("batch_id = ?", batch.ID).Order("item_order").Find(&items).Error; err != nil {
		return nil, err
	}
	return &ShotSplitBatchDetail{Batch: batch, Items: items}, nil
}

func (s *Service) ListShotSplitBatches(projectID uint, episodeN int) ([]models.ShotSplitBatch, error) {
	var batches []models.ShotSplitBatch
	err := s.DB.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("id DESC").Limit(10).Find(&batches).Error
	return batches, err
}

// ControlShotSplitBatch applies pause/resume/cancel/retry. Every transition bumps
// Revision so a runner started under an older revision can no longer claim Scenes.
// The Scene already in flight finishes; pause keeps its result, cancel discards it.
func (s *Service) ControlShotSplitBatch(projectID uint, episodeN int, batchID uint, action string) (*ShotSplitBatchDetail, error) {
	launch := false
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var batch models.ShotSplitBatch
		if err := tx.Where("id = ? AND project_id = ? AND episode_n = ?", batchID, projectID, episodeN).First(&batch).Error; err != nil {
			return err
		}
		now := time.Now()
		updates := map[string]any{"revision": batch.Revision + 1}
		switch action {
		case "pause":
			if batch.Status != shotSplitQueued && batch.Status != shotSplitRunning {
				return fmt.Errorf("只有进行中的批次可以暂停")
			}
			updates["status"] = shotSplitPaused
		case "resume":
			if batch.Status != shotSplitPaused {
				return fmt.Errorf("只有已暂停的批次可以继续")
			}
			updates["status"], updates["error"], updates["finished_at"] = shotSplitQueued, "", nil
			launch = true
		case "cancel":
			if batch.Status != shotSplitQueued && batch.Status != shotSplitRunning && batch.Status != shotSplitPaused {
				return fmt.Errorf("批次已结束")
			}
			if err := tx.Model(&models.ShotSplitItem{}).Where("batch_id = ? AND status = ?", batch.ID, shotSplitQueued).
				Updates(map[string]any{"status": shotSplitCancelled, "finished_at": now}).Error; err != nil {
				return err
			}
			updates["status"], updates["finished_at"] = shotSplitCancelled, now
		case "retry":
			if batch.Status != shotSplitCompleted && batch.Status != shotSplitFailed {
				return fmt.Errorf("批次尚未结束，不能重试")
			}
			retry := tx.Model(&models.ShotSplitItem{}).Where("batch_id = ? AND status = ?", batch.ID, shotSplitFailed).
				Updates(map[string]any{"status": shotSplitQueued, "error": "", "started_at": nil, "finished_at": nil})
			if retry.Error != nil {
				return retry.Error
			}
			if retry.RowsAffected == 0 {
				return fmt.Errorf("批次没有失败的场景")
			}
			updates["status"], updates["error"], updates["finished_at"] = shotSplitQueued, "", nil
			launch = true
		default:
			return fmt.Errorf("未知操作：%s", action)
		}
		return tx.Model(&batch).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	s.reconcileShotSplitBatch(batchID)
	if launch {
		s.launchShotSplitBatch(batchID)
	}
	return s.GetShotSplitBatch(projectID, episodeN, batchID)
}

func (s *Service) runShotSplitBatch(batchID uint) {
	var batch models.ShotSplitBatch
	if err := s.DB.First(&batch, batchID).Error; err != nil {
		return
	}
	now := time.Now()
	claim := s.DB.Model(&models.ShotSplitBatch{}).Where("id = ? AND status = ? AND revision = ?", batchID, shotSplitQueued, batch.Revision).
		Updates(map[string]any{"status": shotSplitRunning, "started_at": now, "finished_at": nil, "error": ""})
	if claim.Error != nil || claim.RowsAffected != 1 {
		return
	}
	for {
		if s.Projects != nil && s.Projects.projectServiceStopped() {
			return
		}
		var item models.ShotSplitItem
		result := s.DB.Where("batch_id = ? AND status = ?", batchID, shotSplitQueued).Order("item_order").Limit(1).Find(&item)
		if result.Error != nil || result.RowsAffected == 0 {
			break
		}
		// The item claim and the batch fence are one statement: after pause/cancel
		// bumps Revision, this runner cannot start another paid draft call.
		started := time.Now()
		itemClaim := s.DB.Model(&models.ShotSplitItem{}).
			Where("id = ? AND status = ? AND EXISTS (SELECT 1 FROM shot_split_batches WHERE id = ? AND status = ? AND revision = ?)", item.ID, shotSplitQueued, batchID, shotSplitRunning, batch.Revision).
			Updates(map[string]any{"status": shotSplitRunning, "attempts": gorm.Expr("attempts + 1"), "started_at": started, "finished_at": nil, "error": ""})
		if itemClaim.Error != nil || itemClaim.RowsAffected != 1 {
			break
		}
		s.runShotSplitItem(&batch, &item)
		s.reconcileShotSplitBatch(batchID)
		if s.Projects != nil {
			s.Projects.pushProject(nil)
		}
	}
	s.reconcileShotSplitBatch(batchID)
}

func (s *Service) runShotSplitItem(batch *models.ShotSplitBatch, item *models.ShotSplitItem) {
	var scene models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", item.SceneID, batch.ProjectID).First(&scene).Error; err != nil {
		s.finishShotSplitItem(item.ID, shotSplitFailed, "场景不存在", 0)
		return
	}
	if scene.Archived || scene.Generation != batch.Generation {
		s.finishShotSplitItem(item.ID, shotSplitSkipped, "场景已归档或已重新生成", 0)
		return
	}
	if reason, err := s.shotSplitSkipReason(&scene); err != nil || reason != "" {
		if err != nil {
			s.finishShotSplitItem(item.ID, shotSplitFailed, err.Error(), 0)
		} else {
			s.finishShotSplitItem(item.ID, shotSplitSkipped, reason, 0)
		}
		return
	}
	hash := shotSplitContentHash(scene.Content)
	_ = s.DB.Model(&models.ShotSplitItem{}).Where("id = ?", item.ID).Update("content_hash", hash).Error
	status, response := s.shotSplitDraft(&scene)
	if status != http.StatusOK {
		message, _ := response["error"].(string)
		if message == "" {
			message = fmt.Sprintf("导演草稿生成失败（HTTP %d）", status)
		}
		s.finishShotSplitItem(item.ID, shotSplitFailed, message, 0)
		return
	}
	draft, _ := response["draft"].(*sceneDirectorDraft)
	snapshot, _ := response["dialogue_snapshot"].(string)
	shots, err := shotsFromDirectorDraft(draft)
	if err != nil {
		s.finishShotSplitItem(item.ID, shotSplitFailed, err.Error(), 0)
		return
	}
	var current models.ShotSplitBatch
	if err := s.DB.First(&current, batch.ID).Error; err != nil || current.Status == shotSplitCancelled {
		s.finishShotSplitItem(item.ID, shotSplitCancelled, "批次已取消，草稿未保存", 0)
		return
	}
	// The model call can take minutes; re-read the Scene so edits made meanwhile win.
	if err := s.DB.First(&scene, scene.ID).Error; err != nil {
		s.finishShotSplitItem(item.ID, shotSplitFailed, "场景不存在", 0)
		return
	}
	if shotSplitContentHash(scene.Content) != hash {
		s.finishShotSplitItem(item.ID, shotSplitFailed, "场景正文在拆镜期间被修改，草稿未保存，可重试", 0)
		return
	}
	if reason, err := s.shotSplitSkipReason(&scene); err != nil || reason != "" {
		if err != nil {
			s.finishShotSplitItem(item.ID, shotSplitFailed, err.Error(), 0)
		} else {
			s.finishShotSplitItem(item.ID, shotSplitSkipped, reason+"（拆镜期间发生变化，草稿未保存）", 0)
		}
		return
	}
	saved, err := s.Shots.ReplaceShotsWithDialogueSnapshot(scene.ID, shots, snapshot)
	if err != nil {
		s.finishShotSplitItem(item.ID, shotSplitFailed, err.Error(), 0)
		return
	}
	if s.Projects != nil {
		s.Projects.CancelStaleSceneTasks(scene.ID, scene.ImageTaskID, scene.VideoTaskID)
	}
	s.finishShotSplitItem(item.ID, shotSplitSaved, "", len(saved))
}

// shotsFromDirectorDraft mirrors the manual save path: audit-only checks are dropped
// and provenance stays empty, exactly as HandleCreateShots does for non-combat drafts.
func shotsFromDirectorDraft(draft *sceneDirectorDraft) ([]models.Shot, error) {
	if draft == nil || len(draft.Shots) == 0 {
		return nil, fmt.Errorf("导演草稿没有镜头")
	}
	payload, err := json.Marshal(draft.Shots)
	if err != nil {
		return nil, err
	}
	var shots []models.Shot
	if err := json.Unmarshal(payload, &shots); err != nil {
		return nil, err
	}
	return shots, nil
}

func (s *Service) finishShotSplitItem(itemID uint, status, message string, shotCount int) {
	updates := map[string]any{"status": status, "error": message, "shot_count": shotCount, "finished_at": time.Now()}
	if status == shotSplitSkipped {
		updates["skip_reason"], updates["error"] = message, ""
	}
	_ = s.DB.Model(&models.ShotSplitItem{}).Where("id = ? AND status = ?", itemID, shotSplitRunning).Updates(updates).Error
}

func (s *Service) reconcileShotSplitBatch(batchID uint) {
	counts := map[string]int64{}
	var rows []struct {
		Status string
		N      int64
	}
	if err := s.DB.Model(&models.ShotSplitItem{}).Select("status, COUNT(*) AS n").Where("batch_id = ?", batchID).Group("status").Scan(&rows).Error; err != nil {
		return
	}
	var total int64
	for _, row := range rows {
		counts[row.Status] = row.N
		total += row.N
	}
	updates := map[string]any{"total_items": total, "saved_items": counts[shotSplitSaved], "skipped_items": counts[shotSplitSkipped], "failed_items": counts[shotSplitFailed]}
	_ = s.DB.Model(&models.ShotSplitBatch{}).Where("id = ?", batchID).Updates(updates).Error
	if counts[shotSplitQueued]+counts[shotSplitRunning] > 0 {
		return
	}
	final := map[string]any{"status": shotSplitCompleted, "error": "", "finished_at": time.Now()}
	if counts[shotSplitFailed] > 0 {
		final["status"], final["error"] = shotSplitFailed, fmt.Sprintf("%d 个场景拆镜失败", counts[shotSplitFailed])
	}
	_ = s.DB.Model(&models.ShotSplitBatch{}).Where("id = ? AND status = ?", batchID, shotSplitRunning).Updates(final).Error
}

// RecoverShotSplitBatches never re-runs an interrupted paid model call. The Scene that
// was in flight becomes a failed item, and unfinished batches wait paused for the user.
func (s *Service) RecoverShotSplitBatches() error {
	var batchIDs []uint
	if err := s.DB.Model(&models.ShotSplitBatch{}).Where("status IN ?", []string{shotSplitQueued, shotSplitRunning}).Pluck("id", &batchIDs).Error; err != nil {
		return err
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ShotSplitItem{}).Where("status = ?", shotSplitRunning).
			Updates(map[string]any{"status": shotSplitFailed, "error": "服务重启中断了该场景的导演草稿生成；为避免重复计费不会自动重试，可手动重试", "finished_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Model(&models.ShotSplitBatch{}).Where("id IN ?", batchIDs).
			Updates(map[string]any{"status": shotSplitPaused, "revision": gorm.Expr("revision + 1"), "error": "服务重启后已暂停，请手动继续"}).Error
	}); err != nil {
		return err
	}
	for _, id := range batchIDs {
		s.reconcileShotSplitBatch(id)
	}
	return nil
}

func shotSplitBatchIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("bid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid batch id"})
		return 0, false
	}
	return uint(id), true
}

func (s *Service) HandleCreateShotSplitBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	if s.shotSplitDraftFn == nil && !s.textProviderConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前文生文 provider 未配置"})
		return
	}
	detail, err := s.CreateShotSplitBatch(p.ID, number)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, detail)
}

func (s *Service) HandleListShotSplitBatches(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	batches, err := s.ListShotSplitBatches(p.ID, number)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, batches)
}

func (s *Service) HandleGetShotSplitBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	batchID, ok := shotSplitBatchIDParam(c)
	if !ok {
		return
	}
	detail, err := s.GetShotSplitBatch(p.ID, number, batchID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "拆镜批次不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (s *Service) HandleControlShotSplitBatch(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	batchID, ok := shotSplitBatchIDParam(c)
	if !ok {
		return
	}
	detail, err := s.ControlShotSplitBatch(p.ID, number, batchID, c.Param("action"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "拆镜批次不存在"})
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}
