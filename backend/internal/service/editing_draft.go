package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Duration is browser preview metadata only. Export re-probes physical media.
type editingDraftClip struct {
	mergeEditClip
	Duration float64 `json:"duration,omitempty"`
}

var errEditingDraftConflict = errors.New("剪辑草稿已被其他窗口修改，请先读取最新服务端草稿")

func (s *Service) editingDraftTarget(c *gin.Context) (*models.Project, int, bool) {
	p, ok := s.loadProject(c)
	if !ok {
		return nil, 0, false
	}
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil || n < 1 || n > 100000 {
		c.JSON(400, gin.H{"error": "集数无效"})
		return nil, 0, false
	}
	return p, n, true
}
func (s *Service) HandleGetEditingDraft(c *gin.Context) {
	p, n, ok := s.editingDraftTarget(c)
	if !ok {
		return
	}
	var draft models.EditingDraft
	err := s.DB.Where("project_id = ? AND episode_n = ?", p.ID, n).First(&draft).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(200, gin.H{"draft": nil})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "读取剪辑草稿失败"})
		return
	}
	c.JSON(200, gin.H{"draft": draft})
}

// Drafts may contain zero or one clip. Final export still enforces its own full validation.
func validateEditingDraft(tx *gorm.DB, projectID uint, episode int, clips []editingDraftClip) error {
	if len(clips) > 500 {
		return fmt.Errorf("草稿最多500个片段")
	}
	for i, c := range clips {
		pair := []mergeEditClip{c.mergeEditClip, c.mergeEditClip}
		if !finite(c.Duration) || c.Duration < 0 {
			return fmt.Errorf("片段预览时长无效")
		}
		if _, err := validateMergeEditSequence(pair); err != nil {
			return err
		}
		if c.Transform != nil || len(c.VisualTracks) > 0 || len(c.VolumeEnvelope) > 0 || len(c.Markers) > 0 {
			end := c.Duration
			if c.SourceEnd != nil {
				end = *c.SourceEnd
			}
			duration := (end - c.SourceStart) / mergeClipSpeed(mergeResolvedVideo{Speed: c.Speed})
			if !finite(duration) || duration <= 0 {
				return fmt.Errorf("变换草稿需要有效片段时长")
			}
			if err := validateClipTracks(resolvedVideoFromClip(c.mergeEditClip, mergeResolvedVideo{}), duration); err != nil {
				return err
			}
			if err := validateClipTransform(c.Transform, duration); err != nil {
				return err
			}
		}
		var count int64
		if c.SceneID != nil {
			tx.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND episode_n = ? AND archived = ?", *c.SceneID, projectID, episode, false).Count(&count)
		} else {
			tx.Model(&models.Material{}).Where("id = ? AND project_id = ? AND type = ?", *c.MaterialID, projectID, "video").Count(&count)
		}
		if count != 1 {
			return fmt.Errorf("第%d个片段来源不属于当前项目/集或已不可用", i+1)
		}
		if c.OverlayMaterialID != nil {
			count = 0
			tx.Model(&models.Material{}).Where("id = ? AND project_id = ? AND type IN ?", *c.OverlayMaterialID, projectID, []string{"image", "video"}).Count(&count)
			if count != 1 {
				return fmt.Errorf("叠加素材不可用")
			}
		}
	}
	values := make([]mergeEditClip, len(clips))
	for i, c := range clips {
		values[i] = c.mergeEditClip
	}
	if _, err := fingerprintClipTracks(tx, projectID, values); err != nil {
		return err
	}
	return nil
}
func saveEditingDraft(db *gorm.DB, project uint, episode int, revision uint, clips []editingDraftClip, globalOptions ...[]clipVisualTrack) (models.EditingDraft, error) {
	var global []clipVisualTrack
	if len(globalOptions) > 0 {
		global = globalOptions[0]
	}
	var out models.EditingDraft
	err := db.Transaction(func(tx *gorm.DB) error {
		// Older clients omit visual_tracks; preserve rather than silently erase it.
		// An explicit [] is the only way to clear all global tracks.
		if revision > 0 && global == nil {
			var existing models.EditingDraft
			if err := tx.Where("project_id = ? AND episode_n = ? AND revision = ?", project, episode, revision).First(&existing).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errEditingDraftConflict
				}
				return err
			}
			var err error
			global, err = decodeGlobalVisualTracks(existing.VisualTracks)
			if err != nil {
				return err
			}
		}
		if err := validateEditingDraft(tx, project, episode, clips); err != nil {
			return err
		}
		duration := 0.0
		for i, c := range clips {
			end := c.Duration
			if c.SourceEnd != nil {
				end = *c.SourceEnd
			}
			duration += (end - c.SourceStart) / mergeClipSpeed(mergeResolvedVideo{Speed: c.Speed})
			if i < len(clips)-1 && c.Transition != "" && c.Transition != "cut" {
				duration -= c.TransitionDuration
			}
		}
		if err := validateGlobalVisualTracks(global, duration); err != nil {
			return err
		}
		if _, err := fingerprintClipTracks(tx, project, []mergeEditClip{{VisualTracks: global}}); err != nil {
			return err
		}
		globalJSON, _ := json.Marshal(global)
		encoded, err := json.Marshal(clips)
		if err != nil {
			return err
		}
		if revision == 0 {
			out = models.EditingDraft{ProjectID: project, EpisodeN: episode, Revision: 1, VisualTracks: string(globalJSON), EditSequence: string(encoded)}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&out)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errEditingDraftConflict
			}
			return nil
		}
		result := tx.Model(&models.EditingDraft{}).Where("project_id = ? AND episode_n = ? AND revision = ?", project, episode, revision).Updates(map[string]any{"revision": revision + 1, "visual_tracks": string(globalJSON), "edit_sequence": string(encoded), "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errEditingDraftConflict
		}
		return tx.Where("project_id = ? AND episode_n = ?", project, episode).First(&out).Error
	})
	return out, err
}
func (s *Service) HandleSaveEditingDraft(c *gin.Context) {
	p, n, ok := s.editingDraftTarget(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	var req struct {
		VisualTracks []clipVisualTrack  `json:"visual_tracks"`
		Revision     *uint              `json:"revision"`
		EditSequence []editingDraftClip `json:"edit_sequence"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.Revision == nil || req.EditSequence == nil {
		c.JSON(400, gin.H{"error": "草稿参数无效，必须包含revision和edit_sequence"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		c.JSON(400, gin.H{"error": "草稿JSON存在多余内容"})
		return
	}
	out, err := saveEditingDraft(s.DB, p.ID, n, *req.Revision, req.EditSequence, req.VisualTracks)
	if errors.Is(err, errEditingDraftConflict) {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"draft": out})
}
