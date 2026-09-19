package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EpisodeHierarchy struct {
	Episode models.Episode   `json:"episode"`
	Scenes  []SceneWithShots `json:"scenes"`
}

type SceneWithShots struct {
	Scene models.Scene  `json:"scene"`
	Shots []models.Shot `json:"shots"`
}

type episodePlanMeta struct {
	Episodes []struct {
		N              int     `json:"n"`
		Title          string  `json:"title"`
		TargetDuration float64 `json:"target_duration"`
		TargetScenes   int     `json:"target_scenes"`
	} `json:"episodes"`
}

// EnsureProjectEpisodes backfills the durable episode aggregate from existing Scene.EpisodeN
// and optional project plan metadata. It is idempotent and never rewrites legacy Scene rows.
func EnsureProjectEpisodes(db *gorm.DB, projectID uint) error {
	var project models.Project
	if err := db.First(&project, projectID).Error; err != nil {
		return err
	}
	metaByNumber := map[int]models.Episode{}
	if strings.TrimSpace(project.Plan) != "" {
		var plan episodePlanMeta
		if json.Unmarshal([]byte(project.Plan), &plan) == nil {
			for _, item := range plan.Episodes {
				if item.N > 0 {
					metaByNumber[item.N] = models.Episode{ProjectID: projectID, Number: item.N, Title: item.Title, TargetDuration: item.TargetDuration, TargetScenes: item.TargetScenes, Status: "draft", Version: 1}
				}
			}
		}
	}
	var numbers []int
	if err := db.Model(&models.Scene{}).Where("project_id = ?", projectID).Distinct("episode_n").Pluck("episode_n", &numbers).Error; err != nil {
		return err
	}
	if len(numbers) == 0 && project.Episodes > 0 {
		for n := 1; n <= project.Episodes; n++ {
			numbers = append(numbers, n)
		}
	}
	for n := range metaByNumber {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	seen := map[int]bool{}
	for _, n := range numbers {
		if n < 1 || seen[n] {
			continue
		}
		seen[n] = true
		episode := metaByNumber[n]
		if episode.ProjectID == 0 {
			episode = models.Episode{ProjectID: projectID, Number: n, Title: fmt.Sprintf("第%d集", n), TargetDuration: 180, TargetScenes: 25, Status: "draft", Version: 1}
		}
		if episode.Title == "" {
			episode.Title = fmt.Sprintf("第%d集", n)
		}
		if episode.TargetDuration <= 0 {
			episode.TargetDuration = 180
		}
		if episode.TargetScenes <= 0 {
			episode.TargetScenes = 25
		}
		if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "project_id"}, {Name: "episode_number"}}, DoNothing: true}).Create(&episode).Error; err != nil {
			return err
		}
	}
	return nil
}

func CreateProjectEpisode(db *gorm.DB, projectID uint, episode models.Episode) (*models.Episode, error) {
	if episode.Number < 1 {
		return nil, fmt.Errorf("集数必须大于0")
	}
	if episode.TargetDuration <= 0 {
		episode.TargetDuration = 180
	}
	if episode.TargetScenes <= 0 {
		episode.TargetScenes = 25
	}
	if strings.TrimSpace(episode.Title) == "" {
		episode.Title = fmt.Sprintf("第%d集", episode.Number)
	}
	episode.ID, episode.ProjectID, episode.Version = 0, projectID, 1
	if episode.Status == "" {
		episode.Status = "draft"
	}
	if err := db.Create(&episode).Error; err != nil {
		return nil, err
	}
	return &episode, nil
}

func UpdateProjectEpisode(db *gorm.DB, projectID uint, number int, updates map[string]any) (*models.Episode, error) {
	var episode models.Episode
	if err := db.Where("project_id = ? AND episode_number = ?", projectID, number).First(&episode).Error; err != nil {
		return nil, err
	}
	allowed := map[string]any{}
	if value, ok := updates["title"].(string); ok && strings.TrimSpace(value) != "" {
		allowed["title"] = strings.TrimSpace(value)
	}
	if value, ok := updates["target_duration"].(float64); ok {
		if value <= 0 {
			return nil, fmt.Errorf("目标时长必须大于0")
		}
		allowed["target_duration"] = value
	}
	if value, ok := updates["target_scenes"].(float64); ok {
		if value < 1 {
			return nil, fmt.Errorf("目标场景数必须大于0")
		}
		allowed["target_scenes"] = int(value)
	}
	if value, ok := updates["status"].(string); ok {
		switch value {
		case "draft", "planned", "producing", "ready", "approved", "completed":
			allowed["status"] = value
		default:
			return nil, fmt.Errorf("无效的Episode状态")
		}
	}
	for _, field := range []string{"summary", "next_hook", "character_appearances"} {
		if value, ok := updates[field].(string); ok {
			allowed[field] = strings.TrimSpace(value)
		}
	}
	allowed["version"] = gorm.Expr("version + 1")
	if err := db.Model(&episode).Updates(allowed).Error; err != nil {
		return nil, err
	}
	if err := db.First(&episode, episode.ID).Error; err != nil {
		return nil, err
	}
	return &episode, nil
}

func DeleteProjectEpisode(db *gorm.DB, projectID uint, number int) error {
	var count int64
	if err := db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ?", projectID, number).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("Episode仍包含场景，不能删除")
	}
	result := db.Where("project_id = ? AND episode_number = ?", projectID, number).Delete(&models.Episode{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func ProjectEpisodeHierarchy(db *gorm.DB, projectID uint) ([]EpisodeHierarchy, error) {
	if err := EnsureProjectEpisodes(db, projectID); err != nil {
		return nil, err
	}
	var episodes []models.Episode
	if err := db.Where("project_id = ?", projectID).Order("episode_number, id").Find(&episodes).Error; err != nil {
		return nil, err
	}
	result := make([]EpisodeHierarchy, 0, len(episodes))
	for _, episode := range episodes {
		var scenes []models.Scene
		if err := db.Where("project_id = ? AND episode_n = ?", projectID, episode.Number).Order("generation DESC, `order`, id").Find(&scenes).Error; err != nil {
			return nil, err
		}
		row := EpisodeHierarchy{Episode: episode, Scenes: make([]SceneWithShots, 0, len(scenes))}
		for _, scene := range scenes {
			var shots []models.Shot
			if err := db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
				return nil, err
			}
			row.Scenes = append(row.Scenes, SceneWithShots{Scene: scene, Shots: shots})
		}
		result = append(result, row)
	}
	return result, nil
}
