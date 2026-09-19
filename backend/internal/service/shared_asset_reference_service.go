package service

import (
	"errors"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrSharedAssetReferenceNotFound = errors.New("shared asset reference not found")

type SharedAssetReferenceService struct {
	db *gorm.DB
}

func NewSharedAssetReferenceService(db *gorm.DB) *SharedAssetReferenceService {
	return &SharedAssetReferenceService{db: db}
}

type SharedAssetReferenceInput struct {
	MaterialID uint   `json:"material_id"`
	SceneID    *uint  `json:"scene_id"`
	ShotID     *uint  `json:"shot_id"`
	Mode       string `json:"mode"`
	LocalFile  string `json:"local_file"`
}

type SharedAssetReferenceView struct {
	models.SharedAssetReference
	Material models.Material `json:"material"`
}

type EffectiveSharedAsset struct {
	Material   models.Material               `json:"material"`
	References []models.SharedAssetReference `json:"references"`
}

func normalizeSharedAssetReference(projectID uint, in SharedAssetReferenceInput) (models.SharedAssetReference, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "live"
	}
	if mode != "live" && mode != "copy" {
		return models.SharedAssetReference{}, fmt.Errorf("mode must be live or copy")
	}
	localFile := strings.TrimSpace(in.LocalFile)
	if mode == "copy" && localFile == "" {
		return models.SharedAssetReference{}, fmt.Errorf("local_file is required for copy mode")
	}
	if mode == "live" {
		localFile = ""
	}
	if in.MaterialID == 0 {
		return models.SharedAssetReference{}, fmt.Errorf("material_id is required")
	}
	return models.SharedAssetReference{
		MaterialID: in.MaterialID,
		ProjectID:  projectID,
		SceneID:    in.SceneID,
		ShotID:     in.ShotID,
		Mode:       mode,
		LocalFile:  localFile,
	}, nil
}

func (s *SharedAssetReferenceService) validateOwnership(ref *models.SharedAssetReference) error {
	var projectCount int64
	if err := s.db.Model(&models.Project{}).Where("id = ?", ref.ProjectID).Count(&projectCount).Error; err != nil {
		return err
	}
	if projectCount == 0 {
		return fmt.Errorf("project not found")
	}

	var material models.Material
	if err := s.db.First(&material, ref.MaterialID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("material not found")
		}
		return err
	}
	if material.ProjectID != nil && *material.ProjectID != ref.ProjectID {
		return fmt.Errorf("material does not belong to this project")
	}

	if ref.SceneID != nil {
		var count int64
		if err := s.db.Model(&models.Scene{}).Where("id = ? AND project_id = ?", *ref.SceneID, ref.ProjectID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("scene does not belong to this project")
		}
	}
	if ref.ShotID != nil {
		var shot models.Shot
		if err := s.db.Joins("JOIN scenes ON scenes.id = shots.scene_id").
			Where("shots.id = ? AND scenes.project_id = ?", *ref.ShotID, ref.ProjectID).
			Select("shots.*").First(&shot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("shot does not belong to this project")
			}
			return err
		}
		if ref.SceneID != nil && shot.SceneID != *ref.SceneID {
			return fmt.Errorf("shot does not belong to the selected scene")
		}
	}
	return nil
}

func sameNullableUint(column string, value *uint) (string, []any) {
	if value == nil {
		return column + " IS NULL", nil
	}
	return column + " = ?", []any{*value}
}

func (s *SharedAssetReferenceService) duplicateExists(ref *models.SharedAssetReference, exceptID uint) (bool, error) {
	q := s.db.Model(&models.SharedAssetReference{}).
		Where("material_id = ? AND project_id = ?", ref.MaterialID, ref.ProjectID)
	for column, value := range map[string]*uint{"scene_id": ref.SceneID, "shot_id": ref.ShotID} {
		clause, args := sameNullableUint(column, value)
		q = q.Where(clause, args...)
	}
	if exceptID != 0 {
		q = q.Where("id != ?", exceptID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SharedAssetReferenceService) Create(projectID uint, in SharedAssetReferenceInput) (*models.SharedAssetReference, error) {
	ref, err := normalizeSharedAssetReference(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateOwnership(&ref); err != nil {
		return nil, err
	}
	duplicate, err := s.duplicateExists(&ref, 0)
	if err != nil {
		return nil, err
	}
	if duplicate {
		return nil, fmt.Errorf("shared asset reference already exists")
	}
	if err := s.db.Create(&ref).Error; err != nil {
		return nil, err
	}
	return &ref, nil
}

func (s *SharedAssetReferenceService) Update(projectID, id uint, in SharedAssetReferenceInput) (*models.SharedAssetReference, error) {
	var existing models.SharedAssetReference
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSharedAssetReferenceNotFound
		}
		return nil, err
	}
	ref, err := normalizeSharedAssetReference(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateOwnership(&ref); err != nil {
		return nil, err
	}
	duplicate, err := s.duplicateExists(&ref, id)
	if err != nil {
		return nil, err
	}
	if duplicate {
		return nil, fmt.Errorf("shared asset reference already exists")
	}
	updates := map[string]any{
		"material_id": ref.MaterialID, "scene_id": ref.SceneID, "shot_id": ref.ShotID,
		"mode": ref.Mode, "local_file": ref.LocalFile,
	}
	if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.db.First(&existing, id).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *SharedAssetReferenceService) Delete(projectID, id uint) error {
	result := s.db.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.SharedAssetReference{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSharedAssetReferenceNotFound
	}
	return nil
}

// List returns only references explicitly created by a user for this project.
func (s *SharedAssetReferenceService) List(projectID uint) ([]SharedAssetReferenceView, error) {
	var refs []models.SharedAssetReference
	if err := s.db.Where("project_id = ?", projectID).Order("id").Find(&refs).Error; err != nil {
		return nil, err
	}
	views := make([]SharedAssetReferenceView, 0, len(refs))
	for _, ref := range refs {
		var material models.Material
		if err := s.db.First(&material, ref.MaterialID).Error; err != nil {
			return nil, err
		}
		views = append(views, SharedAssetReferenceView{SharedAssetReference: ref, Material: material})
	}
	return views, nil
}

// ListEffective exposes global materials and project-owned materials that have an explicit
// project reference. Generated/project materials are never enrolled automatically.
func (s *SharedAssetReferenceService) ListEffective(projectID uint) ([]EffectiveSharedAsset, error) {
	var refs []models.SharedAssetReference
	if err := s.db.Where("project_id = ?", projectID).Order("id").Find(&refs).Error; err != nil {
		return nil, err
	}
	refIDs := make([]uint, 0, len(refs))
	refsByMaterial := make(map[uint][]models.SharedAssetReference)
	for _, ref := range refs {
		if _, seen := refsByMaterial[ref.MaterialID]; !seen {
			refIDs = append(refIDs, ref.MaterialID)
		}
		refsByMaterial[ref.MaterialID] = append(refsByMaterial[ref.MaterialID], ref)
	}
	var materials []models.Material
	q := s.db.Where("project_id IS NULL")
	if len(refIDs) > 0 {
		q = s.db.Where("project_id IS NULL OR id IN ?", refIDs)
	}
	if err := q.Order("id").Find(&materials).Error; err != nil {
		return nil, err
	}
	out := make([]EffectiveSharedAsset, 0, len(materials))
	for _, material := range materials {
		references := refsByMaterial[material.ID]
		if references == nil {
			references = []models.SharedAssetReference{}
		}
		out = append(out, EffectiveSharedAsset{Material: material, References: references})
	}
	return out, nil
}
