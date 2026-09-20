package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var ErrCharacterMotionReferenceNotFound = errors.New("character motion reference not found")

type CharacterMotionReferenceService struct {
	db     *gorm.DB
	upload *UploadManager
}

func NewCharacterMotionReferenceService(db *gorm.DB, upload *UploadManager) *CharacterMotionReferenceService {
	return &CharacterMotionReferenceService{db: db, upload: upload}
}

func (s *CharacterMotionReferenceService) validateOwner(projectID, characterID uint) error {
	var count int64
	if err := s.db.Model(&models.Character{}).Where("id = ? AND project_id = ?", characterID, projectID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("character does not belong to project")
	}
	return nil
}

func (s *CharacterMotionReferenceService) Upload(projectID, characterID uint, name, videoName string, video []byte, audioName string, audio []byte) (*models.CharacterMotionReference, error) {
	if err := s.validateOwner(projectID, characterID); err != nil {
		return nil, err
	}
	if s.upload == nil {
		return nil, fmt.Errorf("upload service unavailable")
	}
	if err := validateUploadContent("video", videoName, video); err != nil {
		return nil, err
	}
	if len(audio) > 0 {
		if err := validateUploadContent("audio", audioName, audio); err != nil {
			return nil, err
		}
	}
	taskID := fmt.Sprintf("motion_p%d_c%d", projectID, characterID)
	unique := time.Now().UnixNano()
	videoStored := fmt.Sprintf("motion_%d%s", unique, strings.ToLower(filepath.Ext(videoName)))
	videoPath, _, err := s.upload.SaveFile(taskID, "video", videoStored, video)
	if err != nil {
		return nil, err
	}
	audioPath := ""
	if len(audio) > 0 {
		audioStored := fmt.Sprintf("motion_%d%s", unique, strings.ToLower(filepath.Ext(audioName)))
		audioPath, _, err = s.upload.SaveFile(taskID, "audio", audioStored, audio)
		if err != nil {
			return nil, err
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(videoName), filepath.Ext(videoName))
	}
	ref := models.CharacterMotionReference{ProjectID: projectID, CharacterID: characterID, Name: name, VideoPath: videoPath, AudioPath: audioPath}
	if err := s.db.Create(&ref).Error; err != nil {
		return nil, err
	}
	return &ref, nil
}

func (s *CharacterMotionReferenceService) List(projectID, characterID uint) ([]models.CharacterMotionReference, error) {
	if err := s.validateOwner(projectID, characterID); err != nil {
		return nil, err
	}
	var refs []models.CharacterMotionReference
	err := s.db.Where("project_id = ? AND character_id = ?", projectID, characterID).Order("selected DESC, id DESC").Find(&refs).Error
	return refs, err
}

func (s *CharacterMotionReferenceService) Select(projectID, characterID, id uint) (*models.CharacterMotionReference, error) {
	if err := s.validateOwner(projectID, characterID); err != nil {
		return nil, err
	}
	var ref models.CharacterMotionReference
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND project_id = ? AND character_id = ?", id, projectID, characterID).First(&ref).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.CharacterMotionReference{}).Where("project_id = ? AND character_id = ?", projectID, characterID).Update("selected", false).Error; err != nil {
			return err
		}
		return tx.Model(&ref).Update("selected", true).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCharacterMotionReferenceNotFound
	}
	if err != nil {
		return nil, err
	}
	ref.Selected = true
	return &ref, nil
}

func (s *CharacterMotionReferenceService) Delete(projectID, characterID, id uint) error {
	result := s.db.Where("id = ? AND project_id = ? AND character_id = ?", id, projectID, characterID).Delete(&models.CharacterMotionReference{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrCharacterMotionReferenceNotFound
	}
	return nil
}
