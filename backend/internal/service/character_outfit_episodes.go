package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// maxOutfitEpisodeRange 防止误填集数一次触达整部长剧；更大范围请分批确认。
const maxOutfitEpisodeRange = 200

type OutfitEpisodeAssignInput struct {
	FromEpisode int  `json:"from_episode"`
	ToEpisode   int  `json:"to_episode"`
	Overwrite   bool `json:"overwrite"`
	Preview     bool `json:"preview"`
}

// 批量指定动作：assign=将改为本套装；unchanged=已是本套装；skip_other=已手动选择其他套装且未勾选覆盖。
const (
	outfitEpisodeAssign    = "assign"
	outfitEpisodeUnchanged = "unchanged"
	outfitEpisodeSkip      = "skip_other"
)

type OutfitEpisodeAssignItem struct {
	SceneID           uint   `json:"scene_id"`
	EpisodeN          int    `json:"episode_n"`
	Order             int    `json:"order"`
	Title             string `json:"title"`
	CurrentOutfitID   uint   `json:"current_outfit_id"`
	CurrentOutfitName string `json:"current_outfit_name"`
	Action            string `json:"action"`
	HasMedia          bool   `json:"has_media"`
}

type OutfitEpisodeAssignResult struct {
	Items     []OutfitEpisodeAssignItem `json:"items"`
	Assign    int                       `json:"assign"`
	Unchanged int                       `json:"unchanged"`
	Skipped   int                       `json:"skipped"`
	Applied   bool                      `json:"applied"`
}

// characterSceneNames 返回角色名及已确认别名，用于判断角色是否在 Scene 中可见。
func characterSceneNames(db *gorm.DB, ch *models.Character) map[string]bool {
	names := map[string]bool{strings.TrimSpace(ch.Name): true}
	if !db.Migrator().HasTable(&models.CharacterAliasCandidate{}) {
		return names
	}
	var aliases []models.CharacterAliasCandidate
	if db.Where("project_id = ? AND canonical_name = ? AND status = ?", ch.ProjectID, ch.Name, "confirmed").Find(&aliases).Error == nil {
		for _, alias := range aliases {
			if value := strings.TrimSpace(alias.Alias); value != "" {
				names[value] = true
			}
		}
	}
	return names
}

// AssignOutfitToEpisodes 预览或应用“把一套已审核套装指定给某集数范围内该角色可见的所有 Scene”。
// 只作用于每集最新版本、未归档的 Scene；默认跳过已手动选择其他套装的 Scene；已生成媒体保留，不自动重新生成。
func (s *CharacterLookService) AssignOutfitToEpisodes(projectID, characterID, outfitID uint, req OutfitEpisodeAssignInput) (*OutfitEpisodeAssignResult, error) {
	if req.FromEpisode < 1 || req.ToEpisode < req.FromEpisode {
		return nil, fmt.Errorf("集数范围无效")
	}
	if req.ToEpisode-req.FromEpisode+1 > maxOutfitEpisodeRange {
		return nil, fmt.Errorf("单次最多指定%d集，请分批操作", maxOutfitEpisodeRange)
	}
	var outfit models.CharacterOutfit
	if err := s.db.Where("id = ? AND project_id = ? AND character_id = ?", outfitID, projectID, characterID).First(&outfit).Error; err != nil {
		return nil, fmt.Errorf("套装不属于当前项目或角色")
	}
	if outfit.AuditStatus != models.LookStatusApproved && outfit.AuditStatus != models.LookStatusPublished {
		return nil, fmt.Errorf("套装「%s」尚未审核通过", outfit.Name)
	}
	if outfit.AgeStageID != 0 {
		stage, err := s.loadAgeStage(s.db, projectID, characterID, outfit.AgeStageID)
		if err != nil {
			return nil, err
		}
		if !ageStageApproved(stage) {
			return nil, fmt.Errorf("年龄阶段「%s」尚未审核通过", ageStageLabel(stage))
		}
	}
	var ch models.Character
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return nil, err
	}
	scenes, err := s.latestEpisodeScenes(projectID, req.FromEpisode, req.ToEpisode)
	if err != nil {
		return nil, err
	}
	names := characterSceneNames(s.db, &ch)
	visible := scenes[:0]
	for _, sc := range scenes {
		for _, name := range visibleSceneCharacterNames(&sc) {
			if names[strings.TrimSpace(name)] {
				visible = append(visible, sc)
				break
			}
		}
	}
	current := map[uint]models.CharacterOutfit{}
	if len(visible) > 0 {
		ids := make([]uint, 0, len(visible))
		for _, sc := range visible {
			ids = append(ids, sc.ID)
		}
		var rows []models.SceneCharacterOutfit
		if err := s.db.Preload("Outfit").Where("scene_id IN ? AND character_id = ?", ids, characterID).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			selected := models.CharacterOutfit{ID: row.OutfitID}
			if row.Outfit != nil {
				selected = *row.Outfit
			}
			current[row.SceneID] = selected
		}
	}
	result := &OutfitEpisodeAssignResult{Items: make([]OutfitEpisodeAssignItem, 0, len(visible))}
	var targets []models.Scene
	for _, sc := range visible {
		item := OutfitEpisodeAssignItem{SceneID: sc.ID, EpisodeN: sc.EpisodeN, Order: sc.Order, Title: sc.Title, Action: outfitEpisodeAssign,
			HasMedia: strings.TrimSpace(sc.ImageFile) != "" || strings.TrimSpace(sc.VideoFile) != ""}
		if selected, ok := current[sc.ID]; ok {
			item.CurrentOutfitID, item.CurrentOutfitName = selected.ID, selected.Name
			switch {
			case selected.ID == outfit.ID:
				item.Action = outfitEpisodeUnchanged
			case !req.Overwrite:
				item.Action = outfitEpisodeSkip
			}
		}
		switch item.Action {
		case outfitEpisodeAssign:
			result.Assign++
			targets = append(targets, sc)
		case outfitEpisodeUnchanged:
			result.Unchanged++
		default:
			result.Skipped++
		}
		result.Items = append(result.Items, item)
	}
	if req.Preview || len(targets) == 0 {
		return result, nil
	}
	var cancel []string
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		for i := range targets {
			sc := &targets[i]
			if err := tx.Where("scene_id = ? AND character_id = ?", sc.ID, characterID).Delete(&models.SceneCharacterOutfit{}).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.SceneCharacterOutfit{SceneID: sc.ID, CharacterID: characterID, OutfitID: outfit.ID}).Error; err != nil {
				return err
			}
			updates := map[string]any{"prompt_stale": true}
			cancel = append(cancel, staleSceneTaskUpdates(tx, sc, updates)...)
			if err := tx.Model(&models.Scene{}).Where("id = ?", sc.ID).Updates(updates).Error; err != nil {
				return err
			}
			if err := markSceneMediaSourceChanged(tx, projectID, sc.ID, "", "按集批量指定角色新形象"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	cancelSceneTasks(s.tasks, cancel)
	result.Applied = true
	return result, nil
}

func (s *CharacterLookService) latestEpisodeScenes(projectID uint, from, to int) ([]models.Scene, error) {
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n BETWEEN ? AND ? AND (archived = ? OR archived IS NULL)", projectID, from, to, false).
		Order("episode_n, `order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	latest := map[int]uint{}
	for _, sc := range scenes {
		if sc.Generation > latest[sc.EpisodeN] {
			latest[sc.EpisodeN] = sc.Generation
		}
	}
	out := scenes[:0]
	for _, sc := range scenes {
		if sc.Generation == latest[sc.EpisodeN] {
			out = append(out, sc)
		}
	}
	return out, nil
}
