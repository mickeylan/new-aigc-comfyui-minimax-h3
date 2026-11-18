package service

import (
	"encoding/json"
	"fmt"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// Binds a preview to saved authority and byte-level reference versions, not the
// editable prompt text. Using the same function at adoption detects concurrent edits.
func (s *Service) blockingDraftSource(db *gorm.DB, scene *models.Scene) (string, error) {
	var shots []models.Shot
	if err := db.Where("scene_id = ?", scene.ID).Order("id").Find(&shots).Error; err != nil {
		return "", err
	}
	var dialogues []models.Dialogue
	if err := db.Where("scene_id = ?", scene.ID).Order("id").Find(&dialogues).Error; err != nil {
		return "", err
	}
	var project models.Project
	if err := db.First(&project, scene.ProjectID).Error; err != nil {
		return "", err
	}
	references := []string{}
	projects := *s.Projects
	projects.db = db
	for _, c := range projects.sceneReferenceCandidates(scene) {
		ref := candidateFileMeta(scene.ProjectID, c)
		if c.CoveredImage != "" {
			ref = FileMeta{TaskID: fmt.Sprint(scene.ProjectID), Name: c.CoveredImage}
		}
		references = append(references, c.Key, ref.TaskID+"/"+ref.Name, s.Projects.fileSHA256(ref))
	}
	return preparationJSONVersion(scene, shots, dialogues, project, references), nil
}

func normalizedBlockingDraft(scene *models.Scene, raw json.RawMessage) (SceneBlocking, error) {
	value, err := parseSceneBlocking(string(raw))
	if err != nil {
		return value, err
	}
	current := sceneDialogueContentHash(scene.Content)
	if value.SourceContentHash != "" && value.SourceContentHash != current {
		return value, fmt.Errorf("场景正文已变化，请重新核对预演")
	}
	value.SourceContentHash = current
	return value, nil
}

func (s *ProjectService) generateVideoActionFromBlockingDraft(scene *models.Scene) (string, error) {
	source := normalizeUserH3ShotMarkers(sceneDialogueGenerationContent(scene))
	source, timedCount := normalizeSceneTimedBeats(source, normalizeSceneDuration(scene.Duration))
	if timedCount == 0 && len(h3ShotMarkerPattern.FindAllStringIndex(source, -1)) > 1 {
		_, refs := s.sceneVideoReferenceFiles(scene, fmt.Sprint(scene.ProjectID))
		return s.compileAuthoredH3ShotActionWithSpatialDraft(scene, source, refs, sceneBlockingPromptContext(scene))
	}
	return s.GenerateSceneVideoAction(scene)
}
