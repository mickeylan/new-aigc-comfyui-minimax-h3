package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strings"
)

func shotFrameState(db *gorm.DB, sc *models.Scene, row *models.ShotPrevisFrame) (models.Shot, string, error) {
	var shot models.Shot
	err := db.Where("id=? AND scene_id=?", row.ShotID, sc.ID).First(&shot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return shot, "Shot已删除或不属于当前Scene", nil
	}
	if err != nil {
		return shot, "", err
	}
	if hashString(sc.Content) != row.SourceContentHash {
		return shot, "Scene正文已变化", nil
	}
	if preparationJSONVersion(shot) != row.ShotHash {
		return shot, "Shot内容或顺序已变化", nil
	}
	var previs models.ScenePrevisualization
	err = db.Where("scene_id=? AND project_id=?", sc.ID, sc.ProjectID).First(&previs).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return shot, "原预演已不存在", nil
	}
	if err != nil {
		return shot, "", err
	}
	if previs.Revision != row.PrevisRevision || previs.SourceContentHash != row.SourceContentHash || previs.DocumentJSON != row.DocumentJSON {
		return shot, "预演版本已变化", nil
	}
	var newer int64
	if err := db.Model(&models.ShotPrevisFrame{}).Where("project_id=? AND scene_id=? AND shot_id=? AND id>?", sc.ProjectID, sc.ID, row.ShotID, row.ID).Count(&newer).Error; err != nil {
		return shot, "", err
	}
	if newer > 0 {
		return shot, "同一Shot已有更新的帧绑定", nil
	}
	if hashString(row.FrameJSON) != row.FrameHash {
		return shot, "帧快照校验失败", nil
	}
	return shot, "", nil
}
func (s *Service) shotFrameSource(db *gorm.DB, sc *models.Scene, row *models.ShotPrevisFrame) (string, error) {
	_, reason, err := shotFrameState(db, sc, row)
	if err != nil {
		return "", err
	}
	if reason != "" {
		return "", gorm.ErrInvalidData
	}
	version, err := s.blockingDraftSource(db, sc)
	if err != nil {
		return "", err
	}
	return preparationJSONVersion("shot-previs-frame-v1", version, row.ID, row.ShotID, row.PrevisRevision, row.FrameHash, row.AppliedPromptHash), nil
}
func shotFrameResponse(row models.ShotPrevisFrame, reason string) gin.H {
	return gin.H{"id": row.ID, "project_id": row.ProjectID, "scene_id": row.SceneID, "shot_id": row.ShotID, "previs_revision": row.PrevisRevision, "time_ms": row.TimeMS, "source_content_hash": row.SourceContentHash, "shot_hash": row.ShotHash, "frame_hash": row.FrameHash, "document_json": row.DocumentJSON, "frame_json": row.FrameJSON, "applied_prompt_hash": row.AppliedPromptHash, "created_at": row.CreatedAt, "stale": reason != "", "stale_reason": reason}
}
func (s *Service) HandleListShotPrevisFrames(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var shots []models.Shot
	var rows []models.ShotPrevisFrame
	if s.DB.Where("scene_id=?", sc.ID).Order("order_num,id").Find(&shots).Error != nil || s.DB.Where("project_id=? AND scene_id=?", sc.ProjectID, sc.ID).Order("id DESC").Limit(200).Find(&rows).Error != nil {
		c.JSON(500, gin.H{"error": "读取Shot帧绑定失败"})
		return
	}
	items := []gin.H{}
	for _, row := range rows {
		_, reason, err := shotFrameState(s.DB, sc, &row)
		if err != nil {
			c.JSON(500, gin.H{"error": "读取帧状态失败"})
			return
		}
		items = append(items, shotFrameResponse(row, reason))
	}
	c.JSON(200, gin.H{"items": items, "shots": shots})
}
func (s *Service) HandleBindShotPrevisFrame(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		ShotID            uint   `json:"shot_id"`
		PrevisRevision    uint   `json:"previs_revision"`
		TimeMS            *int   `json:"time_ms"`
		SourceContentHash string `json:"source_content_hash"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&req) != nil || req.ShotID == 0 || req.PrevisRevision == 0 || req.TimeMS == nil {
		c.JSON(400, gin.H{"error": "请选择Shot、已保存预演版本和整数毫秒时间"})
		return
	}
	var row models.ShotPrevisFrame
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var current models.Scene
		var shot models.Shot
		var previs models.ScenePrevisualization
		if err := tx.Where("id=? AND project_id=?", sc.ID, sc.ProjectID).First(&current).Error; err != nil {
			return err
		}
		if err := tx.Where("id=? AND scene_id=?", req.ShotID, sc.ID).First(&shot).Error; err != nil {
			return err
		}
		if err := tx.Where("scene_id=? AND project_id=?", sc.ID, sc.ProjectID).First(&previs).Error; err != nil {
			return err
		}
		if previs.Revision != req.PrevisRevision || previs.SourceContentHash != hashString(current.Content) || req.SourceContentHash != previs.SourceContentHash {
			return gorm.ErrInvalidData
		}
		var doc previsDocument
		if json.Unmarshal([]byte(previs.DocumentJSON), &doc) != nil {
			return gorm.ErrInvalidData
		}
		frame, err := sampleShotPrevisFrame(doc, *req.TimeMS)
		if err != nil {
			return fmt.Errorf("%w: %s", gorm.ErrInvalidData, err)
		}
		raw, _ := json.Marshal(frame)
		row = models.ShotPrevisFrame{ProjectID: sc.ProjectID, SceneID: sc.ID, ShotID: shot.ID, PrevisRevision: previs.Revision, TimeMS: *req.TimeMS, SourceContentHash: previs.SourceContentHash, ShotHash: preparationJSONVersion(shot), FrameHash: hashString(string(raw)), DocumentJSON: previs.DocumentJSON, FrameJSON: string(raw)}
		return tx.Create(&row).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "Shot或已保存预演不存在或不属于此Scene"})
		return
	}
	if errors.Is(err, gorm.ErrInvalidData) {
		c.JSON(409, gin.H{"error": "正文、预演版本或选帧时间已失效，请刷新后重试"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "保存Shot绑定帧失败"})
		return
	}
	c.JSON(200, gin.H{"item": shotFrameResponse(row, "")})
}
func (s *Service) loadShotFrame(c *gin.Context, sc *models.Scene) (models.ShotPrevisFrame, bool) {
	var row models.ShotPrevisFrame
	err := s.DB.Where("id=? AND scene_id=? AND project_id=?", c.Param("fid"), sc.ID, sc.ProjectID).First(&row).Error
	if err != nil {
		code := 500
		if errors.Is(err, gorm.ErrRecordNotFound) {
			code = 404
		}
		c.JSON(code, gin.H{"error": "读取Shot帧绑定失败"})
		return row, false
	}
	return row, true
}
func (s *Service) HandlePreviewShotPrevisFramePrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	row, ok := s.loadShotFrame(c, sc)
	if !ok {
		return
	}
	version, err := s.shotFrameSource(s.DB, sc, &row)
	if err != nil {
		c.JSON(409, gin.H{"error": "帧绑定来源已变化，请重新绑定"})
		return
	}
	var shot models.Shot
	if s.DB.Where("id=? AND scene_id=?", row.ShotID, sc.ID).First(&shot).Error != nil {
		c.JSON(409, gin.H{"error": "Shot已变化"})
		return
	}
	prompt, err := s.shotFrameImagePrompt(sc, &shot, &row)
	if err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	var current models.Scene
	if s.DB.First(&current, sc.ID).Error != nil {
		c.JSON(409, gin.H{"error": "Scene已变化"})
		return
	}
	now, err := s.shotFrameSource(s.DB, &current, &row)
	if err != nil || now != version {
		c.JSON(409, gin.H{"error": "生成草稿期间来源已变化，请重新准备"})
		return
	}
	c.JSON(200, gin.H{"prompt": prompt, "source_version": version, "frame_hash": row.FrameHash, "item": shotFrameResponse(row, ""), "persisted": false})
}
func (s *Service) HandleApplyShotPrevisFramePrompt(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	row, ok := s.loadShotFrame(c, sc)
	if !ok {
		return
	}
	var req struct {
		Prompt         string `json:"prompt"`
		SourceVersion  string `json:"source_version"`
		ConfirmReplace bool   `json:"confirm_replace"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if c.ShouldBindJSON(&req) != nil || !req.ConfirmReplace || strings.TrimSpace(req.Prompt) == "" || req.SourceVersion == "" {
		c.JSON(400, gin.H{"error": "请审阅草稿并明确确认替换提示词"})
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var current models.Scene
		var binding models.ShotPrevisFrame
		if err := tx.Where("id=? AND project_id=?", sc.ID, sc.ProjectID).First(&current).Error; err != nil {
			return err
		}
		if err := tx.Where("id=? AND scene_id=? AND project_id=?", row.ID, sc.ID, sc.ProjectID).First(&binding).Error; err != nil {
			return err
		}
		version, err := s.shotFrameSource(tx, &current, &binding)
		if err != nil {
			return err
		}
		if version != req.SourceVersion {
			return gorm.ErrInvalidData
		}
		result := tx.Model(&models.Scene{}).Where("id=? AND project_id=? AND updated_at=?", current.ID, current.ProjectID, current.UpdatedAt).Updates(map[string]any{"image_prompt": req.Prompt, "blocking_image_applied_hash": "", "prompt_stale": true})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrInvalidData
		}
		binding.AppliedPromptHash = hashString(req.Prompt)
		if err := tx.Model(&models.ShotPrevisFrame{}).Where("id=?", binding.ID).Update("applied_prompt_hash", binding.AppliedPromptHash).Error; err != nil {
			return err
		}
		row = binding
		return nil
	})
	if errors.Is(err, gorm.ErrInvalidData) || errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(409, gin.H{"error": "预览来源已变化，请保留草稿并重新准备"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "采用Shot帧提示词失败"})
		return
	}
	c.JSON(200, gin.H{"prompt": req.Prompt, "item": shotFrameResponse(row, ""), "persisted": true})
}
