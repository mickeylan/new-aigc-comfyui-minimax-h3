package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var materializedShotTitlePattern = regexp.MustCompile(`^(.*) · 镜头([0-9]+)$`)

type existingNativeGroup struct {
	Scenes   []models.Scene
	Shots    []models.Shot
	Duration float64
}

func materializedSceneBase(title string) (string, int, bool) {
	match := materializedShotTitlePattern.FindStringSubmatch(strings.TrimSpace(title))
	if len(match) != 3 {
		return "", 0, false
	}
	n, err := strconv.Atoi(match[2])
	if err != nil {
		return "", 0, false
	}
	return match[1], n, true
}

func (s *ShotService) loadMaterializedSceneRun(db *gorm.DB, projectID, selectedID uint) ([]models.Scene, string, error) {
	var selected models.Scene
	if err := db.Where("id = ? AND project_id = ?", selectedID, projectID).First(&selected).Error; err != nil {
		return nil, "", err
	}
	base, _, ok := materializedSceneBase(selected.Title)
	if !ok {
		return nil, "", fmt.Errorf("当前场景不是由导演Shot拆分出的场景")
	}
	var episodeScenes []models.Scene
	if err := db.Where("project_id = ? AND generation = ? AND episode_n = ?", projectID, selected.Generation, selected.EpisodeN).Order("`order`, id").Find(&episodeScenes).Error; err != nil {
		return nil, "", err
	}
	run := []models.Scene{}
	for _, scene := range episodeScenes {
		candidateBase, _, matched := materializedSceneBase(scene.Title)
		if matched && candidateBase == base {
			run = append(run, scene)
		}
	}
	if len(run) < 2 {
		return nil, "", fmt.Errorf("没有可重新合并的同组拆分场景")
	}
	for i := 1; i < len(run); i++ {
		if run[i].Order != run[i-1].Order+1 {
			return nil, "", fmt.Errorf("同组拆分场景已被打散，无法安全自动合并")
		}
	}
	return run, base, nil
}

func packExistingNativeScenes(scenes []models.Scene) []existingNativeGroup {
	groups := []existingNativeGroup{}
	for _, scene := range scenes {
		if len(groups) == 0 || groups[len(groups)-1].Duration+scene.Duration > maxNativeSceneDuration {
			groups = append(groups, existingNativeGroup{})
		}
		group := &groups[len(groups)-1]
		group.Scenes = append(group.Scenes, scene)
		group.Duration += scene.Duration
	}
	return groups
}

func (s *ShotService) PreviewExistingNativeRegroup(projectID, selectedID uint) (*ShotMaterializationPreview, error) {
	run, base, err := s.loadMaterializedSceneRun(s.db, projectID, selectedID)
	if err != nil {
		return nil, err
	}
	groups := packExistingNativeScenes(run)
	items := make([]ShotMaterializationItem, 0, len(groups))
	for i, group := range groups {
		ids := make([]uint, 0, len(group.Scenes))
		content := []string{}
		dialogue := []string{}
		for _, scene := range group.Scenes {
			ids = append(ids, scene.ID)
			content = append(content, scene.Content)
			var ds []models.Dialogue
			_ = s.db.Where("scene_id=?", scene.ID).Order("`order`,id").Find(&ds).Error
			for _, d := range ds {
				dialogue = append(dialogue, d.Text)
			}
		}
		items = append(items, ShotMaterializationItem{ShotID: ids[0], ShotIDs: ids, Order: i + 1, Title: fmt.Sprintf("%s · Native段%d", base, i+1), Duration: group.Duration, Content: strings.Join(content, "\n"), Dialogue: strings.Join(dialogue, "")})
	}
	return &ShotMaterializationPreview{SourceSceneID: selectedID, ShotCount: len(run), Items: items, Warning: "确认后会把已经拆开的短Scene重新合并；每个原导演镜头仍作为内部Shot保留，尚未生成或已有的拆分场景媒体将失效。"}, nil
}

func (s *ShotService) RegroupExistingNativeScenes(projectID, selectedID uint) ([]models.Scene, error) {
	if _, err := s.PreviewExistingNativeRegroup(projectID, selectedID); err != nil {
		return nil, err
	}
	created := []models.Scene{}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		run, base, err := s.loadMaterializedSceneRun(tx, projectID, selectedID)
		if err != nil {
			return err
		}
		groups := packExistingNativeScenes(run)
		runIDs := make([]uint, len(run))
		for i := range run {
			runIDs[i] = run[i].ID
		}
		var allScenes []models.Scene
		if err := tx.Where("project_id=? AND generation=? AND episode_n=?", projectID, run[0].Generation, run[0].EpisodeN).Order("`order`,id").Find(&allScenes).Error; err != nil {
			return err
		}
		if err := invalidateContinuityDependentsTx(tx, projectID, runIDs, "相邻短Scene已重新合并为Native场景"); err != nil {
			return err
		}
		if err := tx.Model(&models.Scene{}).Where("project_id=? AND generation=? AND episode_n=?", projectID, run[0].Generation, run[0].EpisodeN).Update("order", gorm.Expr("-id")).Error; err != nil {
			return err
		}
		firstOrder := run[0].Order
		for gi, group := range groups {
			keeper := group.Scenes[0]
			keeper.Order = firstOrder + gi
			keeper.Title = fmt.Sprintf("%s · Native段%d", base, gi+1)
			keeper.Duration = group.Duration
			contents, images, videos, negatives := []string{}, []string{}, []string{}, []string{}
			shotOrder, dialogueOrder := 0, 0
			for _, scene := range group.Scenes {
				contents = append(contents, scene.Content)
				images = append(images, scene.ImagePrompt)
				videos = append(videos, scene.VideoPrompt)
				negatives = append(negatives, scene.NegativePrompt)
				var shots []models.Shot
				if err := tx.Where("scene_id=?", scene.ID).Order("order_num,id").Find(&shots).Error; err != nil {
					return err
				}
				for _, shot := range shots {
					shotOrder++
					if err := tx.Model(&models.Shot{}).Where("id=?", shot.ID).Updates(map[string]any{"scene_id": keeper.ID, "order_num": shotOrder}).Error; err != nil {
						return err
					}
				}
				var dialogues []models.Dialogue
				if err := tx.Where("scene_id=?", scene.ID).Order("`order`,id").Find(&dialogues).Error; err != nil {
					return err
				}
				for _, d := range dialogues {
					dialogueOrder++
					if err := tx.Model(&models.Dialogue{}).Where("id=?", d.ID).Updates(map[string]any{"scene_id": keeper.ID, "order": dialogueOrder}).Error; err != nil {
						return err
					}
				}
			}
			keeper.Content, keeper.ImagePrompt, keeper.VideoPrompt = strings.Join(contents, "\n"), strings.Join(images, "\n"), strings.Join(videos, "\n")
			if engine, _ := normalizeSceneImageEngine(keeper.ImageEngine); engine == ImageEngineQwen21 {
				keeper.ImagePrompt = ""
			}
			keeper.NegativePrompt, keeper.ShotCount = strings.Join(nonEmptyStrings(negatives), ", "), shotOrder
			resetMaterializedScene(&keeper, keeper)
			if err := tx.Model(&models.Scene{}).Where("id=?", keeper.ID).Updates(map[string]any{"order": keeper.Order, "title": keeper.Title, "duration": keeper.Duration, "content": keeper.Content, "image_prompt": keeper.ImagePrompt, "video_prompt": keeper.VideoPrompt, "negative_prompt": keeper.NegativePrompt, "shot_count": keeper.ShotCount, "image_file": "", "image_task_id": "", "video_task_id": "", "video_file": "", "video_input_file": "", "video_full_prompt": "", "status": "pending", "prompt_stale": true}).Error; err != nil {
				return err
			}
			created = append(created, keeper)
			for _, scene := range group.Scenes[1:] {
				if err := deleteSceneDependents(tx, projectID, []uint{scene.ID}); err != nil {
					return err
				}
				if err := tx.Delete(&scene).Error; err != nil {
					return err
				}
			}
		}
		removed := len(run) - len(groups)
		for _, scene := range allScenes {
			inRun := false
			for _, id := range runIDs {
				if scene.ID == id {
					inRun = true
					break
				}
			}
			if inRun {
				continue
			}
			order := scene.Order
			if order > run[len(run)-1].Order {
				order -= removed
			}
			if err := tx.Model(&models.Scene{}).Where("id=?", scene.ID).Update("order", order).Error; err != nil {
				return err
			}
		}
		if err := renumberSceneTitles(tx, projectID, run[0].Generation, run[0].EpisodeN); err != nil {
			return err
		}
		return markEpisodeEditorialStale(tx, projectID, run[0].EpisodeN)
	})
	return created, err
}
