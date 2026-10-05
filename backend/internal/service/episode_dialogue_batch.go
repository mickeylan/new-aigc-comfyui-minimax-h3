package service

import (
	"errors"
	"fmt"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	dialogueBatchQueued    = "queued"
	dialogueBatchRunning   = "running"
	dialogueBatchCompleted = "completed"
	dialogueBatchFailed    = "failed"
)

type DialogueSynthesisBatchDetail struct {
	Batch models.DialogueSynthesisBatch  `json:"batch"`
	Items []models.DialogueSynthesisItem `json:"items"`
}

// episodeDialoguesForSynthesis returns only Dialogue rows owned by the latest
// scene generation for this project episode.
func (s *ProjectService) episodeDialoguesForSynthesis(p *models.Project, episodeN int, staleOnly bool) (uint, []models.Dialogue, error) {
	generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
	if err != nil {
		return 0, nil, err
	}
	var sceneIDs []uint
	if err := s.db.Model(&models.Scene{}).
		Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).
		Order("`order`, id").Pluck("id", &sceneIDs).Error; err != nil {
		return 0, nil, err
	}
	if len(sceneIDs) == 0 {
		return generation, []models.Dialogue{}, nil
	}
	rows := make([]models.Dialogue, 0)
	for _, sceneID := range sceneIDs {
		var sceneRows []models.Dialogue
		if err := s.db.Where("project_id = ? AND scene_id = ?", p.ID, sceneID).Order("`order`, id").Find(&sceneRows).Error; err != nil {
			return 0, nil, err
		}
		rows = append(rows, sceneRows...)
	}
	eligible := make([]models.Dialogue, 0, len(rows))
	for i := range rows {
		d := rows[i]
		stale := d.AudioStale || d.AudioFile == "" || d.AudioHash == "" || d.AudioHash != s.effectiveDialogueAudioHash(d)
		if staleOnly && !stale {
			continue
		}
		if d.Status == "synthesizing" {
			continue
		}
		eligible = append(eligible, d)
	}
	return generation, eligible, nil
}

// CreateEpisodeDialogueSynthesisBatch persists the complete work list before
// dispatching synthesis, so progress and individual errors survive restarts.
func (s *ProjectService) CreateEpisodeDialogueSynthesisBatch(p *models.Project, episodeN int, staleOnly bool) (*DialogueSynthesisBatchDetail, error) {
	if episodeN <= 0 {
		return nil, fmt.Errorf("invalid episode number")
	}
	generation, dialogues, err := s.episodeDialoguesForSynthesis(p, episodeN, staleOnly)
	if err != nil {
		return nil, err
	}
	var active int64
	if err := s.db.Model(&models.DialogueSynthesisBatch{}).Where("project_id = ? AND episode_n = ? AND status IN ?", p.ID, episodeN, []string{dialogueBatchQueued, dialogueBatchRunning}).Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, fmt.Errorf("本集已有配音批次正在执行")
	}
	batch := models.DialogueSynthesisBatch{
		ProjectID: p.ID, EpisodeN: episodeN, Generation: generation, StaleOnly: staleOnly,
		Status: dialogueBatchQueued, TotalItems: len(dialogues),
	}
	items := make([]models.DialogueSynthesisItem, 0, len(dialogues))
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		for i := range dialogues {
			item := models.DialogueSynthesisItem{
				BatchID: batch.ID, DialogueID: dialogues[i].ID, SceneID: dialogues[i].SceneID,
				ItemOrder: i + 1, Status: dialogueBatchQueued,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			now := time.Now()
			batch.Status, batch.Progress, batch.FinishedAt = dialogueBatchCompleted, 1, &now
			return tx.Model(&batch).Updates(map[string]any{"status": batch.Status, "progress": batch.Progress, "finished_at": now}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		go s.runDialogueSynthesisBatch(batch.ID)
	}
	return &DialogueSynthesisBatchDetail{Batch: batch, Items: items}, nil
}

func (s *ProjectService) GetEpisodeDialogueSynthesisBatch(projectID uint, episodeN int, batchID uint) (*DialogueSynthesisBatchDetail, error) {
	var batch models.DialogueSynthesisBatch
	if err := s.db.Where("id = ? AND project_id = ? AND episode_n = ?", batchID, projectID, episodeN).First(&batch).Error; err != nil {
		return nil, err
	}
	var items []models.DialogueSynthesisItem
	if err := s.db.Where("batch_id = ?", batch.ID).Order("item_order").Find(&items).Error; err != nil {
		return nil, err
	}
	return &DialogueSynthesisBatchDetail{Batch: batch, Items: items}, nil
}

func (s *ProjectService) ListEpisodeDialogueSynthesisBatches(projectID uint, episodeN int) ([]models.DialogueSynthesisBatch, error) {
	var batches []models.DialogueSynthesisBatch
	err := s.db.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("id DESC").Find(&batches).Error
	return batches, err
}

// RetryEpisodeDialogueSynthesisBatch queues only failed items; successful takes
// are never synthesized again.
func (s *ProjectService) RetryEpisodeDialogueSynthesisBatch(projectID uint, episodeN int, batchID uint) (*DialogueSynthesisBatchDetail, error) {
	var retryCount int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.DialogueSynthesisBatch
		if err := tx.Where("id = ? AND project_id = ? AND episode_n = ?", batchID, projectID, episodeN).First(&batch).Error; err != nil {
			return err
		}
		if batch.Status == dialogueBatchQueued || batch.Status == dialogueBatchRunning {
			return fmt.Errorf("batch is still running")
		}
		if err := tx.Model(&models.DialogueSynthesisItem{}).Where("batch_id = ? AND status = ?", batch.ID, dialogueBatchFailed).Count(&retryCount).Error; err != nil {
			return err
		}
		if retryCount == 0 {
			return fmt.Errorf("batch has no failed items")
		}
		if err := tx.Model(&models.DialogueSynthesisItem{}).Where("batch_id = ? AND status = ?", batch.ID, dialogueBatchFailed).
			Updates(map[string]any{"status": dialogueBatchQueued, "error": "", "audio_token": "", "started_at": nil, "finished_at": nil}).Error; err != nil {
			return err
		}
		return tx.Model(&batch).Updates(map[string]any{"status": dialogueBatchQueued, "error": "", "failed_items": 0, "finished_at": nil}).Error
	})
	if err != nil {
		return nil, err
	}
	go s.runDialogueSynthesisBatch(batchID)
	return s.GetEpisodeDialogueSynthesisBatch(projectID, episodeN, batchID)
}

func (s *ProjectService) runDialogueSynthesisBatch(batchID uint) {
	var batch models.DialogueSynthesisBatch
	if err := s.db.First(&batch, batchID).Error; err != nil {
		return
	}
	generation, err := latestEpisodeGeneration(s.db, batch.ProjectID, batch.EpisodeN)
	if err != nil || generation != batch.Generation {
		if err == nil {
			err = fmt.Errorf("本集已生成新版本，旧配音批次已失效")
		}
		s.failDialogueBatch(batchID, err)
		_ = s.db.Model(&models.DialogueSynthesisItem{}).Where("batch_id = ? AND status = ?", batchID, dialogueBatchQueued).Updates(map[string]any{"status": dialogueBatchFailed, "error": err.Error(), "finished_at": time.Now()}).Error
		return
	}
	now := time.Now()
	claim := s.db.Model(&models.DialogueSynthesisBatch{}).Where("id = ? AND status = ?", batchID, dialogueBatchQueued).
		Updates(map[string]any{"status": dialogueBatchRunning, "started_at": now, "finished_at": nil, "error": ""})
	if claim.Error != nil || claim.RowsAffected != 1 {
		return
	}
	var items []models.DialogueSynthesisItem
	if err := s.db.Where("batch_id = ? AND status = ?", batchID, dialogueBatchQueued).Order("item_order").Find(&items).Error; err != nil {
		s.failDialogueBatch(batchID, err)
		return
	}
	for i := range items {
		if s.projectServiceStopped() {
			return
		}
		s.runDialogueSynthesisItem(&items[i])
		s.reconcileDialogueSynthesisBatch(batchID)
	}
	s.reconcileDialogueSynthesisBatch(batchID)
	s.pushProject(nil)
}

func (s *ProjectService) runDialogueSynthesisItem(item *models.DialogueSynthesisItem) {
	now := time.Now()
	claim := s.db.Model(&models.DialogueSynthesisItem{}).Where("id = ? AND status = ?", item.ID, dialogueBatchQueued).
		Updates(map[string]any{"status": dialogueBatchRunning, "attempts": gorm.Expr("attempts + 1"), "started_at": now, "finished_at": nil, "error": ""})
	if claim.Error != nil || claim.RowsAffected != 1 {
		return
	}
	var dialogue models.Dialogue
	if err := s.db.First(&dialogue, item.DialogueID).Error; err != nil {
		s.finishDialogueSynthesisItem(item.ID, dialogueBatchFailed, err.Error())
		return
	}
	token, inputHash, err := s.startDialogueTTS(&dialogue)
	if err != nil {
		s.finishDialogueSynthesisItem(item.ID, dialogueBatchFailed, err.Error())
		return
	}
	if err := s.db.Model(&models.DialogueSynthesisItem{}).Where("id = ? AND status = ?", item.ID, dialogueBatchRunning).
		Updates(map[string]any{"audio_token": token, "input_hash": inputHash}).Error; err != nil {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.projectServiceStopped() {
			return
		}
		var current models.Dialogue
		if err := s.db.First(&current, item.DialogueID).Error; err != nil {
			s.finishDialogueSynthesisItem(item.ID, dialogueBatchFailed, err.Error())
			return
		}
		switch {
		case current.Status == "failed" && current.AudioToken == token:
			s.finishDialogueSynthesisItem(item.ID, dialogueBatchFailed, current.Error)
			return
		case current.Status == "ready" && current.AudioToken == "" && current.AudioHash == inputHash:
			s.finishDialogueSynthesisItem(item.ID, dialogueBatchCompleted, "")
			return
		case current.AudioToken != token:
			s.finishDialogueSynthesisItem(item.ID, dialogueBatchFailed, "dialogue synthesis was superseded")
			return
		}
		<-ticker.C
	}
}

func (s *ProjectService) finishDialogueSynthesisItem(itemID uint, status, message string) {
	now := time.Now()
	_ = s.db.Model(&models.DialogueSynthesisItem{}).Where("id = ? AND status = ?", itemID, dialogueBatchRunning).
		Updates(map[string]any{"status": status, "error": message, "finished_at": now}).Error
}

func (s *ProjectService) reconcileDialogueSynthesisBatch(batchID uint) {
	var total, completed, failed, active int64
	q := s.db.Model(&models.DialogueSynthesisItem{}).Where("batch_id = ?", batchID)
	if q.Count(&total).Error != nil || q.Where("status = ?", dialogueBatchCompleted).Count(&completed).Error != nil ||
		s.db.Model(&models.DialogueSynthesisItem{}).Where("batch_id = ? AND status = ?", batchID, dialogueBatchFailed).Count(&failed).Error != nil ||
		s.db.Model(&models.DialogueSynthesisItem{}).Where("batch_id = ? AND status IN ?", batchID, []string{dialogueBatchQueued, dialogueBatchRunning}).Count(&active).Error != nil {
		return
	}
	progress := float64(1)
	if total > 0 {
		progress = float64(completed+failed) / float64(total)
	}
	updates := map[string]any{"total_items": total, "completed_items": completed, "failed_items": failed, "progress": progress}
	if active == 0 {
		now := time.Now()
		updates["finished_at"] = now
		if failed > 0 {
			updates["status"] = dialogueBatchFailed
			updates["error"] = fmt.Sprintf("%d dialogue item(s) failed", failed)
		} else {
			updates["status"] = dialogueBatchCompleted
			updates["error"] = ""
		}
	}
	_ = s.db.Model(&models.DialogueSynthesisBatch{}).Where("id = ?", batchID).Updates(updates).Error
}

func (s *ProjectService) failDialogueBatch(batchID uint, err error) {
	now := time.Now()
	_ = s.db.Model(&models.DialogueSynthesisBatch{}).Where("id = ?", batchID).
		Updates(map[string]any{"status": dialogueBatchFailed, "error": err.Error(), "finished_at": now}).Error
}

func (s *ProjectService) projectServiceStopped() bool {
	if s.stopped == nil {
		return false
	}
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
}

// RecoverDialogueSynthesisBatches releases only CAS claims owned by interrupted
// batch items, then resumes queued work. Unrelated Dialogue claims are untouched.
func (s *ProjectService) RecoverDialogueSynthesisBatches() error {
	var running []models.DialogueSynthesisItem
	if err := s.db.Where("status = ?", dialogueBatchRunning).Find(&running).Error; err != nil {
		return err
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		for i := range running {
			item := running[i]
			if item.AudioToken != "" {
				if err := tx.Model(&models.Dialogue{}).Where("id = ? AND audio_token = ?", item.DialogueID, item.AudioToken).
					Updates(map[string]any{"status": "pending", "audio_token": "", "error": "synthesis interrupted by service restart"}).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&item).Updates(map[string]any{"status": dialogueBatchQueued, "audio_token": "", "error": "", "started_at": nil, "finished_at": nil}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.DialogueSynthesisBatch{}).Where("status = ?", dialogueBatchRunning).
			Updates(map[string]any{"status": dialogueBatchQueued, "error": "", "finished_at": nil}).Error
	}); err != nil {
		return err
	}
	var batches []models.DialogueSynthesisBatch
	if err := s.db.Where("status = ?", dialogueBatchQueued).Find(&batches).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	for i := range batches {
		go s.runDialogueSynthesisBatch(batches[i].ID)
	}
	return nil
}
