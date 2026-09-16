package service

import (
	"strings"

	"comfyui-console/internal/models"
)

func (s *ProjectService) sceneHasCharacter(sc *models.Scene, name string) bool {
	name = strings.TrimSpace(name)
	var shots []models.Shot
	_ = s.db.Where("scene_id = ?", sc.ID).Order("order_num").Find(&shots).Error
	structured := ""
	for _, shot := range shots {
		structured += "\n" + shot.PromptSubject
	}
	// Structured shot subjects are the narrowest authority for who is visible.
	if strings.TrimSpace(structured) != "" {
		return strings.Contains(structured, name)
	}
	visible := parseSceneCharacters(sc.Characters)
	if len(visible) == 0 {
		return true
	} // preserve explicit manual selection when cast metadata is absent
	for _, item := range visible {
		if strings.TrimSpace(item) == name {
			return true
		}
	}
	return false
}

// sceneReferenceIsRelevant prevents references for off-screen characters or unrelated assets
// from being submitted merely because they were previously selected at scene level.
func (s *ProjectService) sceneReferenceIsRelevant(sc *models.Scene, ref SceneReferenceSelection) bool {
	switch ref.SourceType {
	case "character":
		var ch models.Character
		return s.db.First(&ch, ref.SourceID).Error == nil && ch.ProjectID == sc.ProjectID && s.sceneHasCharacter(sc, ch.Name)
	case "look":
		var look models.CharacterLook
		if s.db.First(&look, ref.SourceID).Error != nil || look.ProjectID != sc.ProjectID {
			return false
		}
		var ch models.Character
		return s.db.First(&ch, look.CharacterID).Error == nil && s.sceneHasCharacter(sc, ch.Name)
	case "asset":
		var asset models.Asset
		return s.db.First(&asset, ref.SourceID).Error == nil && asset.ProjectID == sc.ProjectID
	default:
		return false
	}
}
