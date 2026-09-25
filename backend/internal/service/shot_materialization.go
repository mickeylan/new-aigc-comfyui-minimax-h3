package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type ShotMaterializationItem struct {
	ShotID   uint    `json:"shot_id"`
	Order    int     `json:"order"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Content  string  `json:"content"`
	Dialogue string  `json:"dialogue"`
}

type ShotMaterializationPreview struct {
	SourceSceneID uint                      `json:"source_scene_id"`
	Items         []ShotMaterializationItem `json:"items"`
	Warning       string                    `json:"warning"`
}

type dialogueFragment struct {
	Source models.Dialogue
	Text   string
}

func canonicalDialogueRunes(value string) []rune { return []rune(canonicalDialogueText(value)) }

func planDialogueFragments(shots []models.Shot, dialogues []models.Dialogue) ([][]dialogueFragment, error) {
	var authoritative []rune
	ranges := make([][2]int, len(dialogues))
	for i, d := range dialogues {
		start := len(authoritative)
		authoritative = append(authoritative, canonicalDialogueRunes(d.Text)...)
		ranges[i] = [2]int{start, len(authoritative)}
	}
	var planned []rune
	for _, shot := range shots {
		planned = append(planned, canonicalDialogueRunes(shot.Dialogue)...)
	}
	if string(planned) != string(authoritative) {
		return nil, fmt.Errorf("已保存镜头台词与当前结构化Dialogue不一致，请重新生成对白节奏拆镜")
	}
	out := make([][]dialogueFragment, len(shots))
	cursor := 0
	for i, shot := range shots {
		count := len(canonicalDialogueRunes(shot.Dialogue))
		end := cursor + count
		for j, span := range ranges {
			from, to := cursor, end
			if from < span[0] {
				from = span[0]
			}
			if to > span[1] {
				to = span[1]
			}
			if from >= to {
				continue
			}
			out[i] = append(out[i], dialogueFragment{Source: dialogues[j], Text: string(authoritative[from:to])})
		}
		cursor = end
	}
	return out, nil
}

func buildShotMaterializationPreview(scene models.Scene, shots []models.Shot) ShotMaterializationPreview {
	items := make([]ShotMaterializationItem, 0, len(shots))
	for i, shot := range shots {
		items = append(items, ShotMaterializationItem{ShotID: shot.ID, Order: i + 1, Title: fmt.Sprintf("%s · 镜头%d", strings.TrimSpace(scene.Title), i+1), Duration: shot.Duration, Content: shot.Description, Dialogue: shot.Dialogue})
	}
	return ShotMaterializationPreview{SourceSceneID: scene.ID, Items: items, Warning: "确认后将用这些Native场景替换原场景；原场景图片、视频、配音及连续性结果会失效。"}
}

func (s *ShotService) PreviewMaterialization(projectID, sceneID uint) (*ShotMaterializationPreview, error) {
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, err
	}
	shots, err := s.GetSceneShots(sceneID)
	if err != nil {
		return nil, err
	}
	if len(shots) < 2 {
		return nil, fmt.Errorf("至少需要两个已保存导演镜头才能拆成可制作场景")
	}
	for i, shot := range shots {
		if shot.Duration < 3 || shot.Duration > 15 {
			return nil, fmt.Errorf("镜头%d时长必须为3至15秒", i+1)
		}
		if strings.TrimSpace(shot.Description) == "" {
			return nil, fmt.Errorf("镜头%d缺少镜头说明", i+1)
		}
	}
	var dialogues []models.Dialogue
	if err := s.db.Where("scene_id = ? AND project_id = ?", sceneID, projectID).Order("`order` ASC, `id` ASC").Find(&dialogues).Error; err != nil {
		return nil, err
	}
	if _, err := planDialogueFragments(shots, validSceneDialogues(dialogues)); err != nil {
		return nil, err
	}
	preview := buildShotMaterializationPreview(scene, shots)
	return &preview, nil
}

func resetDialogueForMaterialization(d *models.Dialogue, sceneID uint, order int, text string) {
	d.ID, d.SceneID, d.Order, d.Text = 0, sceneID, order, text
	d.Position, d.Offset = 0, 0
	d.AudioFile, d.PreviousAudioFile, d.PreviousAudioHash, d.AudioHash, d.AudioToken = "", "", "", "", ""
	d.AudioRevision, d.AudioStale, d.AudioStaleReason = 0, true, "对白拆成Native场景，需重新生成配音"
	d.Status, d.Error = "pending", ""
}

func (s *ShotService) Materialize(projectID, sceneID uint) ([]models.Scene, error) {
	preview, err := s.PreviewMaterialization(projectID, sceneID)
	if err != nil {
		return nil, err
	}
	_ = preview
	var created []models.Scene
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var source models.Scene
		if err := tx.Where("id = ? AND project_id = ?", sceneID, projectID).First(&source).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num ASC, id ASC").Find(&shots).Error; err != nil {
			return err
		}
		var dialogues []models.Dialogue
		if err := tx.Where("scene_id = ? AND project_id = ?", sceneID, projectID).Order("`order` ASC, `id` ASC").Find(&dialogues).Error; err != nil {
			return err
		}
		fragments, err := planDialogueFragments(shots, validSceneDialogues(dialogues))
		if err != nil {
			return err
		}
		for i, shot := range shots {
			if shot.Duration < 3 || shot.Duration > 15 {
				return fmt.Errorf("镜头%d时长必须为3至15秒", i+1)
			}
		}

		var sceneLooks []models.SceneCharacterLook
		var sceneOutfits []models.SceneCharacterOutfit
		_ = tx.Where("scene_id = ?", sceneID).Find(&sceneLooks).Error
		_ = tx.Where("scene_id = ?", sceneID).Find(&sceneOutfits).Error
		shotLooks := map[uint][]models.ShotCharacterLook{}
		shotOutfits := map[uint][]models.ShotCharacterOutfit{}
		for _, shot := range shots {
			var a []models.ShotCharacterLook
			var b []models.ShotCharacterOutfit
			_ = tx.Where("shot_id = ?", shot.ID).Find(&a).Error
			_ = tx.Where("shot_id = ?", shot.ID).Find(&b).Error
			shotLooks[shot.ID], shotOutfits[shot.ID] = a, b
		}

		if err := invalidateContinuityDependentsTx(tx, projectID, []uint{sceneID}, "上游场景已按对白节奏拆成Native场景"); err != nil {
			return err
		}
		var episodeScenes []models.Scene
		if err := tx.Where("project_id = ? AND generation = ? AND episode_n = ?", projectID, source.Generation, source.EpisodeN).Order("`order` ASC, id ASC").Find(&episodeScenes).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Scene{}).Where("project_id = ? AND generation = ? AND episode_n = ?", projectID, source.Generation, source.EpisodeN).Update("order", gorm.Expr("-id")).Error; err != nil {
			return err
		}
		if err := deleteSceneDependents(tx, projectID, []uint{sceneID}); err != nil {
			return err
		}
		if err := tx.Delete(&source).Error; err != nil {
			return err
		}

		for i, shot := range shots {
			child := source
			child.ID = 0
			child.Order = source.Order + i
			child.Title = fmt.Sprintf("%s · 镜头%d", strings.TrimSpace(source.Title), i+1)
			child.Content, child.Duration = strings.TrimSpace(shot.Description), shot.Duration
			child.ImagePrompt = canonicalShotPrompt(shot)
			if strings.TrimSpace(child.ImagePrompt) == "" {
				child.ImagePrompt = child.Content
			}
			child.VideoPrompt = strings.TrimSpace(strings.Join(nonEmptyStrings([]string{shot.Description, shot.PromptAction, shot.PromptCamera, "起始状态：" + shot.StartState, "结束状态：" + shot.EndState}), "；"))
			child.NegativePrompt = strings.TrimSpace(strings.Join(nonEmptyStrings([]string{source.NegativePrompt, shot.NegativePrompt}), ", "))
			child.ImageFile, child.ImageToken, child.ImageTaskID, child.VideoTaskID, child.VideoFile, child.VideoInputFile = "", "", "", "", "", ""
			child.VideoGPU, child.ImageCandidateParentID, child.VideoCandidateParentID = nil, nil, nil
			child.VideoFullPrompt, child.VideoTemplate, child.VideoFirstFrameImg, child.VideoLastFrameImg = "", "", "", ""
			child.ImageLocked, child.VideoLocked, child.PromptStale, child.Status, child.Error = false, false, true, "pending", ""
			child.ImageRetries, child.VideoRetries, child.ShotCount = 0, 0, 1
			if err := tx.Create(&child).Error; err != nil {
				return err
			}
			clone := shot
			clone.ID, clone.SceneID, clone.Order = 0, child.ID, 1
			if err := tx.Create(&clone).Error; err != nil {
				return err
			}
			for _, row := range sceneLooks {
				row.ID, row.SceneID = 0, child.ID
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
			for _, row := range sceneOutfits {
				row.ID, row.SceneID = 0, child.ID
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
			for _, row := range shotLooks[shot.ID] {
				row.ID, row.ShotID = 0, clone.ID
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
			for _, row := range shotOutfits[shot.ID] {
				row.ID, row.ShotID = 0, clone.ID
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
			for j, fragment := range fragments[i] {
				d := fragment.Source
				resetDialogueForMaterialization(&d, child.ID, j+1, fragment.Text)
				if err := tx.Create(&d).Error; err != nil {
					return err
				}
			}
			created = append(created, child)
		}
		shift := len(shots) - 1
		for _, sibling := range episodeScenes {
			if sibling.ID == sceneID {
				continue
			}
			order := sibling.Order
			if order > source.Order {
				order += shift
			}
			if err := tx.Model(&models.Scene{}).Where("id = ?", sibling.ID).Update("order", order).Error; err != nil {
				return err
			}
		}
		return markEpisodeEditorialStale(tx, projectID, source.EpisodeN)
	})
	return created, err
}
