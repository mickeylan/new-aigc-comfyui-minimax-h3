package service

import (
	"errors"
	"fmt"
	"path/filepath"
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
	if v.File == "" || filepath.IsAbs(v.File) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(v.File)), "../") {
		return nil, fmt.Errorf("invalid variant file")
	}
	if err := validateVariantOwner(s.db, v.ProjectID, v.EntityType, v.EntityID); err != nil {
		return nil, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if v.Selected {
			if err := setVariantEntityFile(tx, v.ProjectID, v.EntityType, v.EntityID, v.File); err != nil {
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
		if err := setVariantEntityFile(tx, projectID, row.EntityType, row.EntityID, row.File); err != nil {
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
