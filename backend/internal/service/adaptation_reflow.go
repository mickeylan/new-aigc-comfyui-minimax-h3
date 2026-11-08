package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type adaptationReflowItem struct {
	Episode       int                      `json:"episode_n"`
	PreviousTitle string                   `json:"previous_title"`
	SourceEpisode int                      `json:"source_episode"`
	Title         string                   `json:"title"`
	Goal          string                   `json:"adaptation_goal"`
	Mapping       models.EpisodeAdaptation `json:"mapping"`
}
type adaptationReflowPreview struct {
	Token            string                 `json:"token"`
	Start            int                    `json:"start_episode"`
	Items            []adaptationReflowItem `json:"items"`
	UnplannedEpisode int                    `json:"unplanned_episode"`
}

// Reflow never invents a chapter assignment. It reuses reviewed successor mappings
// after a proven consumed-source conflict. A vacated tail is explicitly unplanned.
func (s *AdaptationService) previewReflow(projectID uint, start int) (*adaptationReflowPreview, []models.EpisodeAdaptation, error) {
	if start < 2 {
		return nil, nil, fmt.Errorf("必须保留前集，起点至少为第2集")
	}
	ctx, err := s.BuildContext(projectID, start, 0)
	if err != nil {
		return nil, nil, err
	}
	consumed, err := s.episodeSourceScopeConsumed(projectID, start, ctx)
	if err != nil {
		return nil, nil, err
	}
	if !consumed {
		return nil, nil, fmt.Errorf("未确认本集来源已被前集消费，不允许自动前移规划")
	}
	var rows []models.EpisodeAdaptation
	if err := s.db.Where("project_id=? AND episode_n>=?", projectID, start).Order("episode_n").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	if len(rows) < 2 {
		return nil, nil, fmt.Errorf("缺少后续已审核映射，不能虚构新剧情")
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id=? AND episode_n>=? AND (archived=? OR archived IS NULL)", projectID, start, false).Order("id").Find(&scenes).Error; err != nil {
		return nil, nil, err
	}
	for _, sc := range scenes {
		if sc.ImageFile != "" || sc.VideoFile != "" || sc.VideoInputFile != "" || sc.ImageTaskID != "" || sc.VideoTaskID != "" {
			return nil, nil, fmt.Errorf("第%d集已有媒体或生成任务，不允许自动重排", sc.EpisodeN)
		}
		if sc.EpisodeN > start {
			return nil, nil, fmt.Errorf("第%d集已有剧本Scene，不能当作未生产规划自动前移", sc.EpisodeN)
		}
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].EpisodeN != rows[i-1].EpisodeN+1 {
			return nil, nil, fmt.Errorf("后续集号不连续，不能自动前移")
		}
		if rows[i].Script != "" || rows[i].Status != "approved" {
			return nil, nil, fmt.Errorf("第%d集不是尚未生成的已审核映射", rows[i].EpisodeN)
		}
	}
	// Include preserved episodes and all project scene identities in the CAS token.
	// No confirmation made against an older production state can change mappings.
	var allRows []models.EpisodeAdaptation
	if err := s.db.Where("project_id=?", projectID).Order("episode_n").Find(&allRows).Error; err != nil {
		return nil, nil, err
	}
	var allScenes []models.Scene
	if err := s.db.Where("project_id=?", projectID).Order("id").Find(&allScenes).Error; err != nil {
		return nil, nil, err
	}
	var dialogues []models.Dialogue
	if err := s.db.Where("project_id=?", projectID).Order("id").Find(&dialogues).Error; err != nil {
		return nil, nil, err
	}
	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, nil, err
	}
	if project.PipelineStage == "script_manual" || project.PipelineStage == "script_running" || project.AutoGenerate {
		return nil, nil, fmt.Errorf("剧本生产正在运行，请完成或停止后再修复规划")
	}
	bytes, err := json.Marshal([]any{allRows, allScenes, dialogues, project.Generation, project.PipelineStage})
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(bytes)
	preview := &adaptationReflowPreview{Token: hex.EncodeToString(sum[:]), Start: start, UnplannedEpisode: rows[len(rows)-1].EpisodeN}
	for i := 0; i+1 < len(rows); i++ {
		preview.Items = append(preview.Items, adaptationReflowItem{rows[i].EpisodeN, rows[i].Title, rows[i+1].EpisodeN, rows[i+1].Title, rows[i+1].AdaptationGoal, rows[i+1]})
	}
	return preview, rows, nil
}

func (s *AdaptationService) applyReflow(projectID uint, start int, token string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		local := *s
		local.db = tx
		projects := *s.projects
		projects.db = tx
		local.projects = &projects
		preview, rows, err := local.previewReflow(projectID, start)
		if err != nil {
			return err
		}
		if token == "" || token != preview.Token {
			return fmt.Errorf("规划或生产内容已变化，请重新预览")
		}
		if !tx.Migrator().HasTable(&models.ScriptRevision{}) {
			return fmt.Errorf("缺少剧本修订表，拒绝无备份调整")
		}
		// Full mapping backup, including script, boundary and review fields.
		scriptSnapshot, err := captureScriptSnapshot(tx, projectID, start)
		if err != nil {
			return err
		}
		backup, err := json.Marshal(map[string]any{"project": scriptSnapshot.Project, "episode": scriptSnapshot.Episode, "scenes": scriptSnapshot.Scenes, "reason": "before_consumed_episode_reflow", "mappings": rows, "preview": preview})
		if err != nil {
			return err
		}
		for _, ep := range rows {
			if _, err := createScriptRevisionTx(tx, projectID, ep.EpisodeN, "before_script_replace", nil); err != nil {
				return err
			}
		}
		// Existing ScriptRevision schema stores an additional audit-only mapping snapshot.
		if err := tx.Create(&models.ScriptRevision{ProjectID: projectID, EpisodeN: start, Reason: "before_consumed_episode_reflow", SnapshotJSON: string(backup)}).Error; err != nil {
			return err
		}
		for i, target := range rows {
			updates := map[string]any{"status": "draft", "continuity_status": "pending", "review_json": "", "version": target.Version + 1}
			if i+1 < len(rows) {
				source := rows[i+1]
				updates["status"] = "approved"
				updates["title"] = source.Title
				updates["chapter_start"] = source.ChapterStart
				updates["chapter_end"] = source.ChapterEnd
				updates["source_chapter_ids"] = source.SourceChapterIDs
				updates["target_duration"] = source.TargetDuration
				updates["target_scenes"] = source.TargetScenes
				updates["adaptation_goal"] = source.AdaptationGoal
				updates["must_keep_events"] = source.MustKeepEvents
				updates["optional_events"] = source.OptionalEvents
				updates["omitted_events"] = source.OmittedEvents
				updates["opening_state"] = source.OpeningState
				updates["ending_state"] = source.EndingState
				updates["hook"] = source.Hook
				updates["source_digest"] = source.SourceDigest
			} else {
				updates["title"] = "待规划（前移后的空位）"
				updates["source_chapter_ids"] = "[]"
				updates["chapter_start"] = 0
				updates["chapter_end"] = 0
				for _, field := range []string{"adaptation_goal", "must_keep_events", "optional_events", "omitted_events", "opening_state", "ending_state", "hook", "source_digest"} {
					updates[field] = ""
				}
			}
			// Old script/Scenes/media are deliberately retained until successful regeneration.
			updates["source_traces_json"] = ""
			updates["dialogue_coverage_json"] = ""
			if err := tx.Model(&models.EpisodeAdaptation{}).Where("id=? AND project_id=?", target.ID, projectID).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) HandlePreviewAdaptationReflow(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		Start int    `json:"start_episode"`
		Token string `json:"token"`
		Apply bool   `json:"apply"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	if req.Apply {
		if err := s.Adaptations.applyReflow(p.ID, req.Start, req.Token); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"applied": true, "message": "已保留前集并前移未生产规划；旧剧本和媒体保留，新剧本成功后才替换。末尾空位需重新规划。"})
		return
	}
	preview, _, err := s.Adaptations.previewReflow(p.ID, req.Start)
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, preview)
}
