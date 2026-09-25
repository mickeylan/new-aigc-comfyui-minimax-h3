package service

import (
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const maxNativeSceneDuration = 15.0

type ShotMaterializationItem struct {
	ShotID   uint    `json:"shot_id,omitempty"` // 兼容旧前端；多镜组时为首镜 ID
	ShotIDs  []uint  `json:"shot_ids"`
	Order    int     `json:"order"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Content  string  `json:"content"`
	Dialogue string  `json:"dialogue"`
}

type ShotMaterializationPreview struct {
	SourceSceneID uint                      `json:"source_scene_id"`
	ShotCount     int                       `json:"shot_count"`
	Items         []ShotMaterializationItem `json:"items"`
	Warning       string                    `json:"warning"`
}

type dialogueFragment struct {
	Source models.Dialogue
	Text   string
}

type nativeShotGroup struct {
	Shots        []models.Shot
	FragmentSets [][]dialogueFragment
	Duration     float64
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

// groupNativeShots keeps every reviewed camera setup as a Shot, but packs adjacent
// short Shots into the fewest sequential Native scenes whose total duration is <=15s.
func groupNativeShots(shots []models.Shot, fragments [][]dialogueFragment) []nativeShotGroup {
	groups := make([]nativeShotGroup, 0, len(shots))
	for i, shot := range shots {
		if len(groups) == 0 || groups[len(groups)-1].Duration+shot.Duration > maxNativeSceneDuration {
			groups = append(groups, nativeShotGroup{})
		}
		group := &groups[len(groups)-1]
		group.Shots = append(group.Shots, shot)
		group.FragmentSets = append(group.FragmentSets, fragments[i])
		group.Duration += shot.Duration
	}
	return groups
}

func groupedDescriptions(shots []models.Shot) string {
	parts := make([]string, 0, len(shots))
	for i, shot := range shots {
		parts = append(parts, fmt.Sprintf("[Shot %d] %s", i+1, strings.TrimSpace(shot.Description)))
	}
	return strings.Join(parts, "\n")
}

func groupedDialogue(shots []models.Shot) string {
	parts := make([]string, 0, len(shots))
	for _, shot := range shots {
		if text := strings.TrimSpace(shot.Dialogue); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

func buildShotMaterializationPreview(scene models.Scene, groups []nativeShotGroup, shotCount int) ShotMaterializationPreview {
	items := make([]ShotMaterializationItem, 0, len(groups))
	for i, group := range groups {
		ids := make([]uint, 0, len(group.Shots))
		for _, shot := range group.Shots {
			ids = append(ids, shot.ID)
		}
		items = append(items, ShotMaterializationItem{ShotID: ids[0], ShotIDs: ids, Order: i + 1, Title: fmt.Sprintf("%s · Native段%d", strings.TrimSpace(scene.Title), i+1), Duration: group.Duration, Content: groupedDescriptions(group.Shots), Dialogue: groupedDialogue(group.Shots)})
	}
	return ShotMaterializationPreview{SourceSceneID: scene.ID, ShotCount: shotCount, Items: items, Warning: "确认后将按15秒上限把相邻导演Shot合并为Native场景并替换原场景；每个场景内部仍保留不同Shot。原场景图片、视频、配音及连续性结果会失效。"}
}

func validateMaterializationShots(shots []models.Shot) error {
	if len(shots) < 2 {
		return fmt.Errorf("至少需要两个已保存导演镜头才能拆成可制作场景")
	}
	for i, shot := range shots {
		if shot.Duration < 3 || shot.Duration > maxNativeSceneDuration {
			return fmt.Errorf("镜头%d时长必须为3至15秒", i+1)
		}
		if strings.TrimSpace(shot.Description) == "" {
			return fmt.Errorf("镜头%d缺少镜头说明", i+1)
		}
	}
	return nil
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
	if err := validateMaterializationShots(shots); err != nil {
		return nil, err
	}
	var dialogues []models.Dialogue
	if err := s.db.Where("scene_id = ? AND project_id = ?", sceneID, projectID).Order("`order` ASC, `id` ASC").Find(&dialogues).Error; err != nil {
		return nil, err
	}
	fragments, err := planDialogueFragments(shots, validSceneDialogues(dialogues))
	if err != nil {
		return nil, err
	}
	preview := buildShotMaterializationPreview(scene, groupNativeShots(shots, fragments), len(shots))
	return &preview, nil
}

func resetDialogueForMaterialization(d *models.Dialogue, sceneID uint, order int, text string) {
	d.ID, d.SceneID, d.Order, d.Text = 0, sceneID, order, text
	d.Position, d.Offset = 0, 0
	d.AudioFile, d.PreviousAudioFile, d.PreviousAudioHash, d.AudioHash, d.AudioToken = "", "", "", "", ""
	d.AudioRevision, d.AudioStale, d.AudioStaleReason = 0, true, "对白拆成Native场景，需重新生成配音"
	d.Status, d.Error = "pending", ""
}

func resetMaterializedScene(child *models.Scene, source models.Scene) {
	child.ImageFile, child.ImageToken, child.ImageTaskID, child.VideoTaskID, child.VideoFile, child.VideoInputFile = "", "", "", "", "", ""
	child.VideoGPU, child.ImageCandidateParentID, child.VideoCandidateParentID = nil, nil, nil
	child.VideoFullPrompt, child.VideoTemplate, child.VideoFirstFrameImg, child.VideoLastFrameImg = "", "", "", ""
	child.ImageLocked, child.VideoLocked, child.PromptStale, child.Status, child.Error = false, false, true, "pending", ""
	child.ImageRetries, child.VideoRetries = 0, 0
	_ = source
}

func (s *ShotService) Materialize(projectID, sceneID uint) ([]models.Scene, error) {
	if _, err := s.PreviewMaterialization(projectID, sceneID); err != nil {
		return nil, err
	}
	var created []models.Scene
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var source models.Scene
		if err := tx.Where("id = ? AND project_id = ?", sceneID, projectID).First(&source).Error; err != nil {
			return err
		}
		var shots []models.Shot
		if err := tx.Where("scene_id = ?", sceneID).Order("order_num ASC, id ASC").Find(&shots).Error; err != nil {
			return err
		}
		if err := validateMaterializationShots(shots); err != nil {
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
		groups := groupNativeShots(shots, fragments)

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

		for i, group := range groups {
			child := source
			child.ID, child.Order = 0, source.Order+i
			child.Title = fmt.Sprintf("%s · Native段%d", strings.TrimSpace(source.Title), i+1)
			child.Content, child.Duration, child.ShotCount = groupedDescriptions(group.Shots), group.Duration, len(group.Shots)
			promptParts, videoParts, negativeParts := []string{}, []string{}, []string{source.NegativePrompt}
			for j, shot := range group.Shots {
				promptParts = append(promptParts, fmt.Sprintf("[Shot %d]\n%s", j+1, canonicalShotPrompt(shot)))
				videoParts = append(videoParts, fmt.Sprintf("[Shot %d] %s", j+1, strings.Join(nonEmptyStrings([]string{shot.Description, shot.PromptAction, shot.PromptCamera, "起始状态：" + shot.StartState, "结束状态：" + shot.EndState}), "；")))
				negativeParts = append(negativeParts, shot.NegativePrompt)
			}
			child.ImagePrompt = strings.TrimSpace(strings.Join(promptParts, "\n"))
			if child.ImagePrompt == "" {
				child.ImagePrompt = child.Content
			}
			if engine, _ := normalizeSceneImageEngine(child.ImageEngine); engine == ImageEngineQwen21 {
				// Grouped Shot fields are not a valid Qwen prompt. Require explicit redesign
				// through the Qwen T2I/edit Skill instead of silently submitting generic prose.
				child.ImagePrompt = ""
			}
			child.VideoPrompt = strings.TrimSpace(strings.Join(videoParts, "\n"))
			child.NegativePrompt = strings.TrimSpace(strings.Join(nonEmptyStrings(negativeParts), ", "))
			resetMaterializedScene(&child, source)
			if err := tx.Create(&child).Error; err != nil {
				return err
			}
			dialogueOrder := 0
			for j, shot := range group.Shots {
				clone := shot
				clone.ID, clone.SceneID, clone.Order = 0, child.ID, j+1
				if err := tx.Create(&clone).Error; err != nil {
					return err
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
				for _, fragment := range group.FragmentSets[j] {
					dialogueOrder++
					d := fragment.Source
					resetDialogueForMaterialization(&d, child.ID, dialogueOrder, fragment.Text)
					if err := tx.Create(&d).Error; err != nil {
						return err
					}
				}
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
			created = append(created, child)
		}
		shift := len(groups) - 1
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
