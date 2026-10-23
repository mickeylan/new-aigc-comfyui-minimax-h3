package service

import (
	"fmt"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type ProductionCheckFinding struct {
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	EntityType string `json:"entity_type"`
	EntityID   uint   `json:"entity_id"`
	Message    string `json:"message"`
}

type ProductionCheckResult struct {
	ProjectID uint                     `json:"project_id"`
	EpisodeN  int                      `json:"episode_n,omitempty"`
	Ready     bool                     `json:"ready"`
	Findings  []ProductionCheckFinding `json:"findings"`
}

type ProductionCheckService struct{ db *gorm.DB }

func NewProductionCheckService(db *gorm.DB) *ProductionCheckService {
	return &ProductionCheckService{db: db}
}

// Check returns bounded production-state diagnostics and never exposes prompts, story text or file paths.
func (s *ProductionCheckService) Check(projectID uint, episodeN int) (ProductionCheckResult, error) {
	result := ProductionCheckResult{ProjectID: projectID, EpisodeN: episodeN, Ready: true, Findings: []ProductionCheckFinding{}}
	if projectID == 0 {
		return result, fmt.Errorf("project_id is required")
	}
	if episodeN < 0 {
		return result, fmt.Errorf("episode_n cannot be negative")
	}
	var projectCount int64
	if err := s.db.Model(&models.Project{}).Where("id = ?", projectID).Count(&projectCount).Error; err != nil {
		return result, err
	}
	if projectCount != 1 {
		return result, gorm.ErrRecordNotFound
	}
	sceneQuery := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		sceneQuery = sceneQuery.Where("episode_n = ?", episodeN)
	}
	var scenes []models.Scene
	if err := sceneQuery.Order("episode_n, generation, `order`, id").Find(&scenes).Error; err != nil {
		return result, err
	}
	if len(scenes) == 0 {
		result.Findings = append(result.Findings, ProductionCheckFinding{"error", "episode_has_no_scenes", "episode", 0, "当前范围没有可生产的场景"})
	}
	for _, scene := range scenes {
		if strings.TrimSpace(scene.VideoFile) == "" && strings.TrimSpace(scene.VideoInputFile) == "" {
			result.Findings = append(result.Findings, ProductionCheckFinding{"error", "scene_video_missing", "scene", scene.ID, "场景尚无已完成视频"})
		}
		if strings.TrimSpace(scene.ImageFile) == "" {
			result.Findings = append(result.Findings, ProductionCheckFinding{"warning", "scene_storyboard_missing", "scene", scene.ID, "场景尚无分镜图；仅T2V流程可忽略"})
		}
		if scene.PromptStale {
			result.Findings = append(result.Findings, ProductionCheckFinding{"error", "scene_prompt_stale", "scene", scene.ID, "视频提示词已过期，正式生成前需重新生成或确认"})
		}
		if strings.TrimSpace(scene.Error) != "" {
			result.Findings = append(result.Findings, ProductionCheckFinding{"error", "scene_error", "scene", scene.ID, "场景存在未处理的生产错误"})
		}
		if scene.Duration < 3 || scene.Duration > 15 {
			result.Findings = append(result.Findings, ProductionCheckFinding{"warning", "scene_duration_out_of_range", "scene", scene.ID, "场景时长不在 MiniMax H3 的 3–15 秒范围"})
		}
		var continuity models.SceneContinuity
		if q := s.db.Where("scene_id = ?", scene.ID).Limit(1).Find(&continuity); q.Error != nil {
			return result, q.Error
		} else if q.RowsAffected > 0 && continuity.Mode != models.ContinuityModeIndependent && (continuity.SelectedFrameID == nil || continuity.Status != "ready") {
			result.Findings = append(result.Findings, ProductionCheckFinding{"error", "continuity_not_ready", "scene", scene.ID, "连续性模式尚未绑定有效的人工确认帧"})
		}
	}
	dialogueQuery := s.db.Model(&models.Dialogue{}).Joins("JOIN scenes ON scenes.id = dialogues.scene_id").Where("dialogues.project_id = ?", projectID)
	if episodeN > 0 {
		dialogueQuery = dialogueQuery.Where("scenes.episode_n = ?", episodeN)
	}
	var dialogues []models.Dialogue
	if err := dialogueQuery.Select("dialogues.*").Find(&dialogues).Error; err != nil {
		return result, err
	}
	for _, dialogue := range dialogues {
		if dialogue.AudioStale {
			result.Findings = append(result.Findings, ProductionCheckFinding{"warning", "dialogue_audio_stale", "dialogue", dialogue.ID, "对白音频已过期，需要重新生成并审核候选"})
		}
	}
	cueQuery := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		cueQuery = cueQuery.Where("episode_n = ?", episodeN)
	}
	var cues []models.MusicCueSheet
	if err := cueQuery.Find(&cues).Error; err == nil {
		for _, cue := range cues {
			if cue.Stale || cue.ReviewStatus != "approved" {
				result.Findings = append(result.Findings, ProductionCheckFinding{"warning", "music_cue_not_approved", "music_cue", cue.ID, "配乐 Cue 未处于当前已审核状态"})
			}
		}
	} else if !s.db.Migrator().HasTable(&models.MusicCueSheet{}) { /* optional subsystem */
	} else {
		return result, err
	}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		if result.Findings[i].EntityType == result.Findings[j].EntityType {
			return result.Findings[i].EntityID < result.Findings[j].EntityID
		}
		return result.Findings[i].EntityType < result.Findings[j].EntityType
	})
	for _, finding := range result.Findings {
		if finding.Severity == "error" {
			result.Ready = false
			break
		}
	}
	return result, nil
}
