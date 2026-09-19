package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// ScriptRevisionSnapshot is the normalized, ordered script hierarchy stored in each revision.
type ScriptRevisionSnapshot struct {
	Project models.Project  `json:"project"`
	Episode *models.Episode `json:"episode,omitempty"`
	Scenes  []RevisionScene `json:"scenes"`
}

type RevisionScene struct {
	Scene     models.Scene      `json:"scene"`
	Shots     []models.Shot     `json:"shots"`
	Dialogues []models.Dialogue `json:"dialogues"`
}

type ScriptRevisionDetail struct {
	models.ScriptRevision
	Snapshot ScriptRevisionSnapshot `json:"snapshot"`
}

type ScriptRevisionService struct{ db *gorm.DB }

func NewScriptRevisionService(db *gorm.DB) *ScriptRevisionService {
	return &ScriptRevisionService{db: db}
}

func normalizeRevisionEpisode(episodeN int) int {
	if episodeN <= 0 {
		return 1
	}
	return episodeN
}

func captureScriptSnapshot(tx *gorm.DB, projectID uint, episodeN int) (ScriptRevisionSnapshot, error) {
	episodeN = normalizeRevisionEpisode(episodeN)
	var snapshot ScriptRevisionSnapshot
	if err := tx.First(&snapshot.Project, projectID).Error; err != nil {
		return snapshot, err
	}
	var episode models.Episode
	err := tx.Where("project_id = ? AND episode_number = ?", projectID, episodeN).First(&episode).Error
	if err == nil {
		snapshot.Episode = &episode
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return snapshot, err
	}

	var scenes []models.Scene
	if err := tx.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("`order`, id").Find(&scenes).Error; err != nil {
		return snapshot, err
	}
	snapshot.Scenes = make([]RevisionScene, 0, len(scenes))
	for _, scene := range scenes {
		item := RevisionScene{Scene: scene, Shots: []models.Shot{}, Dialogues: []models.Dialogue{}}
		if err := tx.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&item.Shots).Error; err != nil {
			return snapshot, err
		}
		if err := tx.Where("project_id = ? AND scene_id = ?", projectID, scene.ID).Order("`order`, id").Find(&item.Dialogues).Error; err != nil {
			return snapshot, err
		}
		snapshot.Scenes = append(snapshot.Scenes, item)
	}
	return snapshot, nil
}

func createScriptRevisionTx(tx *gorm.DB, projectID uint, episodeN int, reason string, sourceRevisionID *uint) (*models.ScriptRevision, error) {
	snapshot, err := captureScriptSnapshot(tx, projectID, episodeN)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode script revision: %w", err)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "manual"
	}
	revision := &models.ScriptRevision{
		ProjectID: projectID, EpisodeN: normalizeRevisionEpisode(episodeN), Reason: reason,
		SourceRevisionID: sourceRevisionID, SnapshotJSON: string(payload),
	}
	if err := tx.Create(revision).Error; err != nil {
		return nil, err
	}
	return revision, nil
}

func (s *ScriptRevisionService) Create(projectID uint, episodeN int, reason string) (*models.ScriptRevision, error) {
	return createScriptRevisionTx(s.db, projectID, episodeN, reason, nil)
}

func (s *ScriptRevisionService) List(projectID uint, episodeN int) ([]models.ScriptRevision, error) {
	var revisions []models.ScriptRevision
	query := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		query = query.Where("episode_n = ?", episodeN)
	}
	err := query.Order("created_at DESC, id DESC").Find(&revisions).Error
	return revisions, err
}

func (s *ScriptRevisionService) Get(projectID, revisionID uint) (*ScriptRevisionDetail, error) {
	var revision models.ScriptRevision
	if err := s.db.Where("id = ? AND project_id = ?", revisionID, projectID).First(&revision).Error; err != nil {
		return nil, err
	}
	var snapshot ScriptRevisionSnapshot
	if err := json.Unmarshal([]byte(revision.SnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("decode script revision: %w", err)
	}
	if snapshot.Project.ID != projectID {
		return nil, fmt.Errorf("script revision project mismatch")
	}
	return &ScriptRevisionDetail{ScriptRevision: revision, Snapshot: snapshot}, nil
}

// Restore atomically saves the current state and replaces the revision's episode hierarchy.
func (s *ScriptRevisionService) Restore(projectID, revisionID uint) (*models.ScriptRevision, error) {
	detail, err := s.Get(projectID, revisionID)
	if err != nil {
		return nil, err
	}
	episodeN := detail.EpisodeN
	var safetyRevision *models.ScriptRevision
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		safetyRevision, err = createScriptRevisionTx(tx, projectID, episodeN, "before_restore", &revisionID)
		if err != nil {
			return err
		}
		var current models.Project
		if err := tx.First(&current, projectID).Error; err != nil {
			return err
		}

		var currentScenes []models.Scene
		if err := tx.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Find(&currentScenes).Error; err != nil {
			return err
		}
		currentIDs := make([]uint, 0, len(currentScenes))
		for _, scene := range currentScenes {
			currentIDs = append(currentIDs, scene.ID)
		}
		if err := deleteSceneDependents(tx, projectID, currentIDs); err != nil {
			return err
		}
		if err := tx.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Delete(&models.Scene{}).Error; err != nil {
			return err
		}

		if detail.Snapshot.Episode == nil {
			if err := tx.Where("project_id = ? AND episode_number = ?", projectID, episodeN).Delete(&models.Episode{}).Error; err != nil {
				return err
			}
		} else {
			episode := *detail.Snapshot.Episode
			if episode.ProjectID != projectID || episode.Number != episodeN {
				return fmt.Errorf("script revision episode mismatch")
			}
			if err := tx.Where("project_id = ? AND episode_number = ?", projectID, episodeN).Delete(&models.Episode{}).Error; err != nil {
				return err
			}
			if err := tx.Create(&episode).Error; err != nil {
				return err
			}
		}

		for _, item := range detail.Snapshot.Scenes {
			if item.Scene.ProjectID != projectID || item.Scene.EpisodeN != episodeN {
				return fmt.Errorf("script revision scene mismatch")
			}
			snapshotSceneID := item.Scene.ID
			scene := item.Scene
			// Revisions restore editorial content, never old generated assets or in-flight task state.
			scene.ID = 0
			scene.Generation = current.Generation
			scene.ImageFile, scene.ImageToken, scene.ImageTaskID = "", "", ""
			scene.VideoTaskID, scene.VideoFile, scene.VideoInputFile, scene.VideoFullPrompt = "", "", "", ""
			scene.VideoGPU = nil
			scene.VideoFirstFrameImg, scene.VideoLastFrameImg = "", ""
			scene.ImageCandidateParentID, scene.VideoCandidateParentID = nil, nil
			scene.Status, scene.Error, scene.PromptStale = "pending", "", true
			scene.ImageRetries, scene.VideoRetries = 0, 0
			scene.ShotCount = len(item.Shots)
			scene.CreatedAt, scene.UpdatedAt = time.Time{}, time.Time{}
			if err := tx.Create(&scene).Error; err != nil {
				return err
			}
			for _, storedShot := range item.Shots {
				if storedShot.SceneID != snapshotSceneID {
					return fmt.Errorf("script revision shot mismatch")
				}
				shot := storedShot
				shot.ID, shot.SceneID = 0, scene.ID
				shot.CreatedAt, shot.UpdatedAt = time.Time{}, time.Time{}
				if err := tx.Create(&shot).Error; err != nil {
					return err
				}
			}
			for _, storedDialogue := range item.Dialogues {
				if storedDialogue.ProjectID != projectID || storedDialogue.SceneID != snapshotSceneID {
					return fmt.Errorf("script revision dialogue mismatch")
				}
				dialogue := storedDialogue
				dialogue.ID, dialogue.SceneID = 0, scene.ID
				dialogue.AudioFile, dialogue.PreviousAudioFile = "", ""
				dialogue.AudioHash, dialogue.AudioToken = "", ""
				dialogue.AudioRevision = 0
				dialogue.AudioStale, dialogue.AudioStaleReason = true, "剧本版本已恢复，需重新合成"
				dialogue.Status, dialogue.Error = "pending", ""
				dialogue.CreatedAt, dialogue.UpdatedAt = time.Time{}, time.Time{}
				if err := tx.Create(&dialogue).Error; err != nil {
					return err
				}
			}
		}

		// A revision is episode-scoped. Merge only this episode's script text and never
		// rewind project generation, pipeline state, other episodes, or global visual bible.
		var currentScripts, snapshotScripts map[string]string
		_ = json.Unmarshal([]byte(current.Scripts), &currentScripts)
		_ = json.Unmarshal([]byte(detail.Snapshot.Project.Scripts), &snapshotScripts)
		if currentScripts == nil {
			currentScripts = map[string]string{}
		}
		key := fmt.Sprint(episodeN)
		if value, ok := snapshotScripts[key]; ok {
			currentScripts[key] = value
		} else {
			delete(currentScripts, key)
		}
		merged, err := json.Marshal(currentScripts)
		if err != nil {
			return err
		}
		updates := map[string]any{"scripts": string(merged)}
		if episodeN == 1 {
			updates["script"] = detail.Snapshot.Project.Script
		}
		return tx.Model(&models.Project{}).Where("id = ?", projectID).Updates(updates).Error
	})
	return safetyRevision, err
}
