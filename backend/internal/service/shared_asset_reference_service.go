package service

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrSharedAssetReferenceNotFound = errors.New("shared asset reference not found")

type SharedAssetReferenceService struct {
	db     *gorm.DB
	upload *UploadManager
}

func NewSharedAssetReferenceService(db *gorm.DB, uploads ...*UploadManager) *SharedAssetReferenceService {
	service := &SharedAssetReferenceService{db: db}
	if len(uploads) > 0 {
		service.upload = uploads[0]
	}
	return service
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

type ResolvedSharedAsset struct {
	Material  models.Material              `json:"material"`
	Reference *models.SharedAssetReference `json:"reference,omitempty"`
	Mode      string                       `json:"mode"`
	Path      string                       `json:"path"`
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

func (s *SharedAssetReferenceService) resolveCopyPath(ref *models.SharedAssetReference) error {
	if ref.Mode != "copy" {
		return nil
	}
	if s.upload == nil || s.upload.remote == nil {
		return fmt.Errorf("copy mode storage is unavailable")
	}
	var material models.Material
	if err := s.db.First(&material, ref.MaterialID).Error; err != nil {
		return err
	}
	clean := filepath.Clean(strings.TrimSpace(material.Path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("material has no valid file to copy")
	}
	source, err := s.upload.remote.Open(filepath.Join(s.upload.InputDir(), clean))
	if err != nil {
		return fmt.Errorf("open material copy source: %w", err)
	}
	data, readErr := io.ReadAll(source)
	closeErr := source.Close()
	if readErr != nil {
		return fmt.Errorf("read material copy source: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close material copy source: %w", closeErr)
	}
	ext := filepath.Ext(clean)
	name := fmt.Sprintf("shared_asset_%d_%d%s", ref.MaterialID, time.Now().UnixNano(), ext)
	copied, _, err := s.upload.SaveFile(fmt.Sprint(ref.ProjectID), material.Type, name, data)
	if err != nil {
		return fmt.Errorf("save material copy: %w", err)
	}
	ref.LocalFile = filepath.ToSlash(copied)
	return nil
}

func (s *SharedAssetReferenceService) Create(projectID uint, in SharedAssetReferenceInput) (*models.SharedAssetReference, error) {
	ref, err := normalizeSharedAssetReference(projectID, in)
	if err != nil {
		return nil, err
	}
	if err := s.validateOwnership(&ref); err != nil {
		return nil, err
	}
	if err := s.resolveCopyPath(&ref); err != nil {
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
	if err := s.invalidateDependents(ref, "共享素材引用已添加"); err != nil {
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
	if err := s.resolveCopyPath(&ref); err != nil {
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
	if err := s.invalidateDependents(existing, "共享素材引用已修改"); err != nil {
		return nil, err
	}
	if err := s.invalidateDependents(ref, "共享素材引用已修改"); err != nil {
		return nil, err
	}
	if err := s.db.First(&existing, id).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *SharedAssetReferenceService) Delete(projectID, id uint) error {
	var existing models.SharedAssetReference
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSharedAssetReferenceNotFound
		}
		return err
	}
	result := s.db.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.SharedAssetReference{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSharedAssetReferenceNotFound
	}
	return s.invalidateDependents(existing, "共享素材引用已移除")
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
func (s *SharedAssetReferenceService) invalidateDependents(ref models.SharedAssetReference, reason string) error {
	query := s.db.Model(&models.Scene{}).Where("project_id = ?", ref.ProjectID)
	if ref.SceneID != nil {
		query = query.Where("id = ?", *ref.SceneID)
	} else if ref.ShotID != nil {
		query = query.Where("id = (SELECT scene_id FROM shots WHERE id = ?)", *ref.ShotID)
	}
	var sceneIDs []uint
	if err := query.Pluck("id", &sceneIDs).Error; err != nil {
		return err
	}
	for _, sceneID := range sceneIDs {
		if err := s.db.Model(&models.Scene{}).Where("id = ?", sceneID).Update("prompt_stale", true).Error; err != nil {
			return err
		}
		if s.db.Migrator().HasTable(&models.GenerationCandidate{}) {
			if err := MarkSceneCandidatesStale(s.db, ref.ProjectID, sceneID, "", reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListResolvedForScene returns image assets applicable to a scene. Global assets are
// available live by default; explicit project/scene/shot references add scoped live
// or copy variants. Copy paths remain fixed when the source Material later changes.
func (s *SharedAssetReferenceService) ListResolvedForScene(projectID, sceneID uint) ([]ResolvedSharedAsset, error) {
	effective, err := s.ListEffective(projectID)
	if err != nil {
		return nil, err
	}
	var shotIDs []uint
	if err := s.db.Model(&models.Shot{}).Where("scene_id = ?", sceneID).Pluck("id", &shotIDs).Error; err != nil {
		return nil, err
	}
	shots := make(map[uint]bool, len(shotIDs))
	for _, id := range shotIDs {
		shots[id] = true
	}
	out := make([]ResolvedSharedAsset, 0)
	for _, item := range effective {
		if item.Material.Type != "image" || strings.TrimSpace(item.Material.Path) == "" {
			continue
		}
		if item.Material.ProjectID == nil {
			out = append(out, ResolvedSharedAsset{Material: item.Material, Mode: "live", Path: item.Material.Path})
		}
		for i := range item.References {
			ref := item.References[i]
			applicable := ref.SceneID == nil && ref.ShotID == nil
			if ref.SceneID != nil && *ref.SceneID == sceneID {
				applicable = ref.ShotID == nil || shots[*ref.ShotID]
			} else if ref.SceneID == nil && ref.ShotID != nil {
				applicable = shots[*ref.ShotID]
			}
			if !applicable {
				continue
			}
			path := item.Material.Path
			if ref.Mode == "copy" {
				path = ref.LocalFile
			}
			copyRef := ref
			out = append(out, ResolvedSharedAsset{Material: item.Material, Reference: &copyRef, Mode: ref.Mode, Path: path})
		}
	}
	return out, nil
}

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
