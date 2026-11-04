package service

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrRenameConflict = errors.New("rename name conflict")

// RenameCharacter changes only identity labels and their structured references. It deliberately
// preserves the character ID, profile/review state, selected media, tasks, variants and audio.
func (s *ProjectService) RenameCharacter(projectID, characterID uint, requestedName string) (*models.Character, error) {
	name := strings.TrimSpace(requestedName)
	if name == "" {
		return nil, fmt.Errorf("角色名不能为空")
	}
	if utf8.RuneCountInString(name) > 100 {
		return nil, fmt.Errorf("角色名不能超过100个字符")
	}
	var renamed models.Character
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var ch models.Character
		if err := tx.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
			return err
		}
		if ch.Name == name {
			renamed = ch
			return nil
		}
		var peers []models.Character
		if err := tx.Where("project_id = ? AND id != ?", projectID, characterID).Find(&peers).Error; err != nil {
			return err
		}
		for _, peer := range peers {
			if normalizeCanonName(peer.Name) == normalizeCanonName(name) {
				return fmt.Errorf("%w: 项目内已存在角色「%s」", ErrRenameConflict, peer.Name)
			}
		}
		old := ch.Name
		if err := rewriteSceneNameReferences(tx, projectID, "character", old, name, true); err != nil {
			return err
		}
		if reconciliationHasTable(tx, "character_alias_candidates") {
			if err := tx.Model(&models.CharacterAliasCandidate{}).Where("project_id = ? AND canonical_name = ?", projectID, old).Update("canonical_name", name).Error; err != nil {
				return err
			}
			alias := models.CharacterAliasCandidate{ProjectID: projectID, CanonicalName: name, Alias: old, Status: "confirmed"}
			if err := tx.Where("project_id = ? AND alias = ?", projectID, old).Assign(map[string]any{"canonical_name": name, "status": "confirmed"}).FirstOrCreate(&alias).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&models.Character{}).Where("id = ? AND project_id = ?", characterID, projectID).Update("name", name).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND project_id = ?", characterID, projectID).First(&renamed).Error
	})
	if err != nil {
		return nil, err
	}
	s.pushProject(nil)
	return &renamed, nil
}

// RenameAsset preserves the asset row and all generated media while updating structured scene references.
func (s *ProjectService) RenameAsset(projectID, assetID uint, kind, requestedName string) (*models.Asset, error) {
	kind, err := normalizeAssetKind(kind)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		return nil, fmt.Errorf("%s名不能为空", AssetKindLabel(kind))
	}
	if utf8.RuneCountInString(name) > 100 {
		return nil, fmt.Errorf("名称不能超过100个字符")
	}
	var renamed models.Asset
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var asset models.Asset
		if err := tx.Where("id = ? AND project_id = ? AND kind = ?", assetID, projectID, kind).First(&asset).Error; err != nil {
			return err
		}
		if asset.Name == name {
			renamed = asset
			return nil
		}
		var peers []models.Asset
		if err := tx.Where("project_id = ? AND kind = ? AND id != ?", projectID, kind, assetID).Find(&peers).Error; err != nil {
			return err
		}
		for _, peer := range peers {
			if normalizeCanonName(peer.Name) == normalizeCanonName(name) {
				return fmt.Errorf("%w: 项目内已存在%s「%s」", ErrRenameConflict, AssetKindLabel(kind), peer.Name)
			}
		}
		if err := rewriteSceneNameReferences(tx, projectID, kind, asset.Name, name, true); err != nil {
			return err
		}
		if err := tx.Model(&models.Asset{}).Where("id = ? AND project_id = ? AND kind = ?", assetID, projectID, kind).Update("name", name).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND project_id = ? AND kind = ?", assetID, projectID, kind).First(&renamed).Error
	})
	if err != nil {
		return nil, err
	}
	s.pushProject(nil)
	return &renamed, nil
}
