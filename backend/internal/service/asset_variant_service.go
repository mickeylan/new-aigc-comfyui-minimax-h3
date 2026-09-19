package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const MaxNonFavoriteAssetVariants = 8

const (
	VariantCharacterPortrait = "character_portrait"
	VariantCharacterSheet    = "character_sheet"
	VariantCharacterLook     = "character_look"
	VariantAssetImage        = "asset_image"
	VariantAssetSheet        = "asset_sheet"
)

type AssetVariantService struct{ db *gorm.DB }

func NewAssetVariantService(db *gorm.DB) *AssetVariantService { return &AssetVariantService{db: db} }

func (s *AssetVariantService) List(projectID uint, entityType string, entityID uint) ([]models.AssetVariant, error) {
	if err := validateVariantOwner(s.db, projectID, entityType, entityID); err != nil {
		return nil, err
	}
	var rows []models.AssetVariant
	err := s.db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, entityType, entityID).
		Order("selected DESC, favorite DESC, created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (s *AssetVariantService) Register(v models.AssetVariant) (*models.AssetVariant, error) {
	v.EntityType = strings.TrimSpace(strings.ToLower(v.EntityType))
	v.File = strings.TrimSpace(v.File)
	if _, err := safeFileSegment(v.File, "variant file"); err != nil {
		return nil, fmt.Errorf("invalid variant file")
	}
	if err := validateVariantOwner(s.db, v.ProjectID, v.EntityType, v.EntityID); err != nil {
		return nil, err
	}
	if err := validateProjectImageUpload(s.db, v.ProjectID, v.File); err != nil {
		return nil, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if v.Selected {
			if err := validateProjectImageUpload(tx, v.ProjectID, v.File); err != nil {
				return err
			}
			if err := setVariantEntityFile(tx, v.ProjectID, v.EntityType, v.EntityID, v.File); err != nil {
				return err
			}
			if err := invalidateVariantConsumers(tx, v.ProjectID, v.EntityType, v.EntityID); err != nil {
				return err
			}
			if err := tx.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", v.ProjectID, v.EntityType, v.EntityID).Update("selected", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&v).Error; err != nil {
			return err
		}
		return cleanupAssetVariants(tx, v.ProjectID, v.EntityType, v.EntityID)
	})
	return &v, err
}

func (s *AssetVariantService) Select(projectID, variantID uint) (*models.AssetVariant, error) {
	var row models.AssetVariant
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND project_id = ?", variantID, projectID).First(&row).Error; err != nil {
			return err
		}
		if err := validateVariantOwner(tx, projectID, row.EntityType, row.EntityID); err != nil {
			return err
		}
		if err := validateProjectImageUpload(tx, projectID, row.File); err != nil {
			return err
		}
		if err := setVariantEntityFile(tx, projectID, row.EntityType, row.EntityID, row.File); err != nil {
			return err
		}
		if err := invalidateVariantConsumers(tx, projectID, row.EntityType, row.EntityID); err != nil {
			return err
		}
		if err := tx.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, row.EntityType, row.EntityID).Update("selected", false).Error; err != nil {
			return err
		}
		return tx.Model(&row).Update("selected", true).Error
	})
	return &row, err
}

func (s *AssetVariantService) SetFavorite(projectID, variantID uint, favorite bool) (*models.AssetVariant, error) {
	var row models.AssetVariant
	if err := s.db.Where("id = ? AND project_id = ?", variantID, projectID).First(&row).Error; err != nil {
		return nil, err
	}
	if err := validateVariantOwner(s.db, projectID, row.EntityType, row.EntityID); err != nil {
		return nil, err
	}
	if err := s.db.Model(&row).Update("favorite", favorite).Error; err != nil {
		return nil, err
	}
	row.Favorite = favorite
	return &row, cleanupAssetVariants(s.db, projectID, row.EntityType, row.EntityID)
}

func (s *AssetVariantService) Delete(projectID, variantID uint) error {
	var row models.AssetVariant
	if err := s.db.Where("id = ? AND project_id = ?", variantID, projectID).First(&row).Error; err != nil {
		return err
	}
	if row.Selected {
		return fmt.Errorf("selected variant cannot be deleted")
	}
	if err := validateVariantOwner(s.db, projectID, row.EntityType, row.EntityID); err != nil {
		return err
	}
	return s.db.Delete(&row).Error
}

func cleanupAssetVariants(tx *gorm.DB, projectID uint, entityType string, entityID uint) error {
	var stale []models.AssetVariant
	if err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND favorite = ? AND selected = ?", projectID, entityType, entityID, false, false).
		Order("created_at DESC, id DESC").Offset(MaxNonFavoriteAssetVariants).Find(&stale).Error; err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(stale))
	for _, row := range stale {
		ids = append(ids, row.ID)
	}
	return tx.Where("id IN ?", ids).Delete(&models.AssetVariant{}).Error
}

func validateVariantOwner(db *gorm.DB, projectID uint, entityType string, entityID uint) error {
	if projectID == 0 || entityID == 0 {
		return fmt.Errorf("project_id and entity_id are required")
	}
	var count int64
	switch entityType {
	case VariantCharacterPortrait, VariantCharacterSheet:
		db.Model(&models.Character{}).Where("id = ? AND project_id = ?", entityID, projectID).Count(&count)
	case VariantCharacterLook:
		db.Model(&models.CharacterLook{}).Where("id = ? AND project_id = ?", entityID, projectID).Count(&count)
	case VariantAssetImage, VariantAssetSheet:
		db.Model(&models.Asset{}).Where("id = ? AND project_id = ?", entityID, projectID).Count(&count)
	default:
		return fmt.Errorf("invalid entity_type")
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func validateProjectImageUpload(db *gorm.DB, projectID uint, file string) error {
	var count int64
	err := db.Model(&models.UploadFile{}).
		Where("task_id = ? AND type = ? AND name = ?", strconv.FormatUint(uint64(projectID), 10), "image", file).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("variant file is not a project-owned image upload")
	}
	return nil
}

func variantConsumerSceneIDs(tx *gorm.DB, projectID uint, entityType string, entityID uint) ([]uint, error) {
	ids := map[uint]struct{}{}
	add := func(rows []uint) {
		for _, id := range rows {
			ids[id] = struct{}{}
		}
	}
	var rows []uint
	switch entityType {
	case VariantCharacterPortrait, VariantCharacterSheet:
		var character models.Character
		if err := tx.Where("id = ? AND project_id = ?", entityID, projectID).First(&character).Error; err != nil {
			return nil, err
		}
		var scenes []models.Scene
		if err := tx.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil {
			return nil, err
		}
		name := normalizeCanonName(character.Name)
		for _, scene := range scenes {
			for _, field := range []string{scene.VisibleCharacters, scene.Characters} {
				for _, token := range parseSceneCharacters(field) {
					if normalizeCanonName(token) == name {
						ids[scene.ID] = struct{}{}
					}
				}
			}
		}
		if err := tx.Table("scene_character_looks AS scl").Select("DISTINCT scl.scene_id").
			Joins("JOIN character_looks AS cl ON cl.id = scl.look_id").
			Joins("JOIN scenes AS s ON s.id = scl.scene_id").
			Where("cl.character_id = ? AND s.project_id = ?", entityID, projectID).Pluck("scl.scene_id", &rows).Error; err != nil {
			return nil, err
		}
		add(rows)
		rows = nil
		if err := tx.Table("shot_character_looks AS shcl").Select("DISTINCT shots.scene_id").
			Joins("JOIN character_looks AS cl ON cl.id = shcl.look_id").Joins("JOIN shots ON shots.id = shcl.shot_id").
			Joins("JOIN scenes AS s ON s.id = shots.scene_id").Where("cl.character_id = ? AND s.project_id = ?", entityID, projectID).
			Pluck("shots.scene_id", &rows).Error; err != nil {
			return nil, err
		}
		add(rows)
		rows = nil
		if err := tx.Table("scene_character_outfits AS sco").Select("DISTINCT sco.scene_id").
			Joins("JOIN scenes AS s ON s.id = sco.scene_id").Where("sco.character_id = ? AND s.project_id = ?", entityID, projectID).
			Pluck("sco.scene_id", &rows).Error; err != nil {
			return nil, err
		}
		add(rows)
		rows = nil
		if err := tx.Table("shot_character_outfits AS shco").Select("DISTINCT shots.scene_id").
			Joins("JOIN shots ON shots.id = shco.shot_id").Joins("JOIN scenes AS s ON s.id = shots.scene_id").
			Where("shco.character_id = ? AND s.project_id = ?", entityID, projectID).Pluck("shots.scene_id", &rows).Error; err != nil {
			return nil, err
		}
		add(rows)
	case VariantCharacterLook:
		if err := tx.Table("scene_character_looks AS scl").Select("DISTINCT scl.scene_id").
			Joins("JOIN scenes AS s ON s.id = scl.scene_id").Where("scl.look_id = ? AND s.project_id = ?", entityID, projectID).
			Pluck("scl.scene_id", &rows).Error; err != nil {
			return nil, err
		}
		add(rows)
		rows = nil
		if err := tx.Table("shot_character_looks AS shcl").Select("DISTINCT shots.scene_id").
			Joins("JOIN shots ON shots.id = shcl.shot_id").Joins("JOIN scenes AS s ON s.id = shots.scene_id").
			Where("shcl.look_id = ? AND s.project_id = ?", entityID, projectID).Pluck("shots.scene_id", &rows).Error; err != nil {
			return nil, err
		}
		add(rows)
	case VariantAssetImage, VariantAssetSheet:
		var asset models.Asset
		if err := tx.Where("id = ? AND project_id = ?", entityID, projectID).First(&asset).Error; err != nil {
			return nil, err
		}
		var scenes []models.Scene
		if err := tx.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil {
			return nil, err
		}
		name := normalizeCanonName(asset.Name)
		for _, scene := range scenes {
			matched := asset.Kind == AssetKindLocation && normalizeCanonName(scene.LocationName) == name
			if asset.Kind == AssetKindProp {
				for _, token := range parseSceneCharacters(scene.Props) {
					matched = matched || normalizeCanonName(token) == name
				}
			}
			if matched {
				ids[scene.ID] = struct{}{}
			}
		}
	}
	out := make([]uint, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

func invalidateVariantConsumers(tx *gorm.DB, projectID uint, entityType string, entityID uint) error {
	direct, err := variantConsumerSceneIDs(tx, projectID, entityType, entityID)
	if err != nil {
		return err
	}
	reason := "视觉资产候选已切换"
	seen := map[uint]struct{}{}
	queue := make([]uint, 0, len(direct))
	for _, sceneID := range direct {
		seen[sceneID] = struct{}{}
		queue = append(queue, sceneID)
		if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", sceneID, projectID).Updates(map[string]any{
			"image_file": "", "image_token": "", "image_task_id": "", "image_candidate_parent_id": nil,
			"video_task_id": "", "video_file": "", "video_input_file": "", "video_gpu": nil,
			"video_full_prompt": "", "video_template": "", "video_first_frame_img": "", "video_last_frame_img": "",
			"video_candidate_parent_id": nil, "image_retries": 0, "video_retries": 0,
			"status": "pending", "error": "", "prompt_stale": true,
		}).Error; err != nil {
			return err
		}
		if err := MarkSceneCandidatesStale(tx, projectID, sceneID, "", reason); err != nil {
			return err
		}
	}
	for len(queue) > 0 {
		sourceID := queue[0]
		queue = queue[1:]
		var configs []models.SceneContinuity
		if err := tx.Where("source_scene_id = ? AND mode != ?", sourceID, models.ContinuityModeIndependent).Find(&configs).Error; err != nil {
			return err
		}
		for _, cfg := range configs {
			var dependent models.Scene
			if err := tx.Where("id = ? AND project_id = ?", cfg.SceneID, projectID).First(&dependent).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			if err := tx.Model(&models.SceneContinuity{}).Where("id = ?", cfg.ID).Updates(map[string]any{
				"status": "source_invalidated", "error": reason, "selected_frame_id": nil, "version": gorm.Expr("version + 1"),
			}).Error; err != nil {
				return err
			}
			if _, exists := seen[dependent.ID]; exists {
				continue
			}
			seen[dependent.ID] = struct{}{}
			queue = append(queue, dependent.ID)
			if err := invalidateSceneVideoTx(tx, projectID, dependent.ID, reason); err != nil {
				return err
			}
			if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", dependent.ID, projectID).Update("prompt_stale", true).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func setVariantEntityFile(tx *gorm.DB, projectID uint, entityType string, entityID uint, file string) error {
	var model any
	field := ""
	switch entityType {
	case VariantCharacterPortrait:
		model, field = &models.Character{}, "portrait"
	case VariantCharacterSheet:
		model, field = &models.Character{}, "sheet"
	case VariantCharacterLook:
		model, field = &models.CharacterLook{}, "image"
	case VariantAssetImage:
		model, field = &models.Asset{}, "image"
	case VariantAssetSheet:
		model, field = &models.Asset{}, "sheet"
	default:
		return fmt.Errorf("invalid entity_type")
	}
	res := tx.Model(model).Where("id = ? AND project_id = ?", entityID, projectID).Update(field, file)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// registerSelectedAssetVariant is intentionally best-effort for generation/upload hooks:
// the canonical image has already been saved and must not be rolled back by history bookkeeping.
func registerSelectedAssetVariant(db *gorm.DB, projectID uint, entityType string, entityID uint, file, prompt, provenance string) {
	if db == nil || strings.TrimSpace(file) == "" {
		return
	}
	var existing models.AssetVariant
	err := db.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file = ?", projectID, entityType, entityID, file).First(&existing).Error
	if err == nil {
		_ = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, entityType, entityID).Update("selected", false).Error; err != nil {
				return err
			}
			return tx.Model(&existing).Updates(map[string]any{"selected": true, "prompt": prompt, "provenance": provenance}).Error
		})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	_, _ = NewAssetVariantService(db).Register(models.AssetVariant{ProjectID: projectID, EntityType: entityType, EntityID: entityID, File: file, Prompt: prompt, Provenance: provenance, Selected: true})
}
