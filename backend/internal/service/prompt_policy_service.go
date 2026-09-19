package service

import (
	"fmt"
	"regexp"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	PromptPolicyImagePolish = "image_prompt_polish"
	PromptPolicyVideoPolish = "video_prompt_polish"
)

var promptPolicyKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,99}$`)

type PromptPolicyContext struct {
	ProjectID uint
	EpisodeID *uint
	SceneID   *uint
	ShotID    *uint
}

type EffectivePromptPolicy struct {
	PolicyKey string `json:"policy_key"`
	Content   string `json:"content"`
	Version   int    `json:"version"`
	Source    string `json:"source"`
	SourceID  *uint  `json:"source_id,omitempty"`
}

type PromptPolicyService struct{ db *gorm.DB }

func NewPromptPolicyService(db *gorm.DB) *PromptPolicyService { return &PromptPolicyService{db: db} }

func validatePromptPolicyKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if !promptPolicyKeyPattern.MatchString(key) {
		return "", fmt.Errorf("policy_key must be lowercase letters, numbers, dot, underscore, or hyphen")
	}
	return key, nil
}

// normalizeContext validates ownership and derives missing ancestors from a shot or scene.
func (s *PromptPolicyService) normalizeContext(ctx PromptPolicyContext) (PromptPolicyContext, error) {
	if ctx.ProjectID == 0 {
		return ctx, fmt.Errorf("project is required")
	}
	var project models.Project
	if err := s.db.Select("id").First(&project, ctx.ProjectID).Error; err != nil {
		return ctx, err
	}
	if ctx.ShotID != nil {
		var shot models.Shot
		if err := s.db.Joins("JOIN scenes ON scenes.id = shots.scene_id").Where("shots.id = ? AND scenes.project_id = ?", *ctx.ShotID, ctx.ProjectID).First(&shot).Error; err != nil {
			return ctx, fmt.Errorf("shot does not belong to project: %w", err)
		}
		if ctx.SceneID != nil && *ctx.SceneID != shot.SceneID {
			return ctx, fmt.Errorf("shot does not belong to scene")
		}
		ctx.SceneID = &shot.SceneID
	}
	if ctx.SceneID != nil {
		var scene models.Scene
		if err := s.db.Where("id = ? AND project_id = ?", *ctx.SceneID, ctx.ProjectID).First(&scene).Error; err != nil {
			return ctx, fmt.Errorf("scene does not belong to project: %w", err)
		}
		var episode models.Episode
		err := s.db.Where("project_id = ? AND episode_number = ?", ctx.ProjectID, scene.EpisodeN).First(&episode).Error
		if err == nil {
			if ctx.EpisodeID != nil && *ctx.EpisodeID != episode.ID {
				return ctx, fmt.Errorf("scene does not belong to episode")
			}
			ctx.EpisodeID = &episode.ID
		} else if err != gorm.ErrRecordNotFound {
			return ctx, err
		} else if ctx.EpisodeID != nil {
			return ctx, fmt.Errorf("scene episode does not exist")
		}
	}
	if ctx.EpisodeID != nil {
		var episode models.Episode
		if err := s.db.Where("id = ? AND project_id = ?", *ctx.EpisodeID, ctx.ProjectID).First(&episode).Error; err != nil {
			return ctx, fmt.Errorf("episode does not belong to project: %w", err)
		}
	}
	return ctx, nil
}

func policyScope(row *models.PromptPolicyOverride) (string, *uint) {
	if row.ShotID != nil {
		return "shot", row.ShotID
	}
	if row.SceneID != nil {
		return "scene", row.SceneID
	}
	if row.EpisodeID != nil {
		return "episode", row.EpisodeID
	}
	return "project", &row.ProjectID
}

func (s *PromptPolicyService) Resolve(ctx PromptPolicyContext, policyKey, systemDefault string) (*EffectivePromptPolicy, error) {
	key, err := validatePromptPolicyKey(policyKey)
	if err != nil {
		return nil, err
	}
	ctx, err = s.normalizeContext(ctx)
	if err != nil {
		return nil, err
	}
	lookups := []struct {
		column string
		id     *uint
	}{
		{"shot_id", ctx.ShotID}, {"scene_id", ctx.SceneID}, {"episode_id", ctx.EpisodeID}, {"project", &ctx.ProjectID},
	}
	for _, lookup := range lookups {
		if lookup.id == nil {
			continue
		}
		var row models.PromptPolicyOverride
		q := s.db.Where("project_id = ? AND policy_key = ?", ctx.ProjectID, key)
		switch lookup.column {
		case "shot_id":
			q = q.Where("shot_id = ?", *lookup.id)
		case "scene_id":
			q = q.Where("shot_id IS NULL AND scene_id = ?", *lookup.id)
		case "episode_id":
			q = q.Where("shot_id IS NULL AND scene_id IS NULL AND episode_id = ?", *lookup.id)
		default:
			q = q.Where("shot_id IS NULL AND scene_id IS NULL AND episode_id IS NULL")
		}
		err := q.Order("id DESC").First(&row).Error
		if err == nil {
			source, sourceID := policyScope(&row)
			return &EffectivePromptPolicy{PolicyKey: key, Content: row.Content, Version: row.Version, Source: source, SourceID: sourceID}, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}
	return &EffectivePromptPolicy{PolicyKey: key, Content: systemDefault, Version: 0, Source: "system_default"}, nil
}

func (s *PromptPolicyService) Create(row *models.PromptPolicyOverride) (*models.PromptPolicyOverride, error) {
	key, err := validatePromptPolicyKey(row.PolicyKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	scopeCount := 0
	for _, id := range []*uint{row.EpisodeID, row.SceneID, row.ShotID} {
		if id != nil {
			scopeCount++
		}
	}
	if scopeCount > 1 {
		return nil, fmt.Errorf("choose only one of episode_id, scene_id, or shot_id")
	}
	ctx, err := s.normalizeContext(PromptPolicyContext{ProjectID: row.ProjectID, EpisodeID: row.EpisodeID, SceneID: row.SceneID, ShotID: row.ShotID})
	if err != nil {
		return nil, err
	}
	// Preserve the requested scope; normalizeContext is only used for ownership validation.
	row.PolicyKey, row.Content, row.Version = key, strings.TrimSpace(row.Content), 1
	q := s.db.Model(&models.PromptPolicyOverride{}).Where("project_id = ? AND policy_key = ?", row.ProjectID, key)
	if row.ShotID != nil {
		q = q.Where("shot_id = ?", *ctx.ShotID)
	} else if row.SceneID != nil {
		q = q.Where("shot_id IS NULL AND scene_id = ?", *ctx.SceneID)
	} else if row.EpisodeID != nil {
		q = q.Where("shot_id IS NULL AND scene_id IS NULL AND episode_id = ?", *ctx.EpisodeID)
	} else {
		q = q.Where("shot_id IS NULL AND scene_id IS NULL AND episode_id IS NULL")
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, fmt.Errorf("policy override already exists at this scope")
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func (s *PromptPolicyService) List(projectID uint, policyKey string) ([]models.PromptPolicyOverride, error) {
	if _, err := s.normalizeContext(PromptPolicyContext{ProjectID: projectID}); err != nil {
		return nil, err
	}
	var rows []models.PromptPolicyOverride
	q := s.db.Where("project_id = ?", projectID)
	if strings.TrimSpace(policyKey) != "" {
		key, err := validatePromptPolicyKey(policyKey)
		if err != nil {
			return nil, err
		}
		q = q.Where("policy_key = ?", key)
	}
	return rows, q.Order("policy_key, id").Find(&rows).Error
}

func (s *PromptPolicyService) Update(projectID, id uint, content string) (*models.PromptPolicyOverride, error) {
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	var row models.PromptPolicyOverride
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&row).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&row).Updates(map[string]any{"content": strings.TrimSpace(content), "version": gorm.Expr("version + 1")}).Error; err != nil {
		return nil, err
	}
	return &row, s.db.First(&row, row.ID).Error
}

func (s *PromptPolicyService) Delete(projectID, id uint) error {
	result := s.db.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.PromptPolicyOverride{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
