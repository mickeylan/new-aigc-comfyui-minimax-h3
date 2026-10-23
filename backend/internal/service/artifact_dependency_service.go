package service

import (
	"fmt"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	ArtifactKindMusicCue               = "music_cue"
	ArtifactKindMusicCandidate         = "music_candidate"
	ArtifactKindAudioLayer             = "audio_layer"
	ArtifactKindDialogue               = "dialogue"
	ArtifactKindDialogueAudioCandidate = "dialogue_audio_candidate"
	ArtifactKindContinuityFrame        = "continuity_frame"
	ArtifactKindSceneImageCandidate    = "scene_image_candidate"
	ArtifactKindSceneVideoCandidate    = "scene_video_candidate"
)

var artifactKinds = map[string]bool{
	ArtifactKindMusicCue: true, ArtifactKindMusicCandidate: true, ArtifactKindAudioLayer: true,
	ArtifactKindDialogue: true, ArtifactKindDialogueAudioCandidate: true,
	ArtifactKindContinuityFrame: true, ArtifactKindSceneImageCandidate: true, ArtifactKindSceneVideoCandidate: true,
}

type ArtifactRef struct {
	Kind string `json:"kind"`
	ID   uint   `json:"id"`
}

type ArtifactDependencyService struct {
	db      *gorm.DB
	enabled bool
}

func NewArtifactDependencyService(db *gorm.DB, enabled bool) *ArtifactDependencyService {
	return &ArtifactDependencyService{db: db, enabled: enabled}
}

func validArtifactRef(ref ArtifactRef) bool { return artifactKinds[ref.Kind] && ref.ID > 0 }

func (s *ArtifactDependencyService) LinkMusicCandidate(projectID, cueID, candidateID uint, sourceVersion string) error {
	return s.LinkMusicCandidateTx(s.db, projectID, cueID, candidateID, sourceVersion)
}

func (s *ArtifactDependencyService) LinkMusicCandidateTx(tx *gorm.DB, projectID, cueID, candidateID uint, sourceVersion string) error {
	if s == nil || !s.enabled {
		return nil
	}
	if tx == nil {
		tx = s.db
	}
	return s.LinkTx(tx, projectID, ArtifactRef{Kind: ArtifactKindMusicCue, ID: cueID}, sourceVersion, ArtifactRef{Kind: ArtifactKindMusicCandidate, ID: candidateID})
}

func (s *ArtifactDependencyService) DeleteArtifactTx(tx *gorm.DB, projectID uint, ref ArtifactRef) error {
	if s == nil || !s.enabled {
		return nil
	}
	if tx == nil {
		tx = s.db
	}
	ref.Kind = strings.TrimSpace(ref.Kind)
	if projectID == 0 || !validArtifactRef(ref) {
		return fmt.Errorf("project and artifact reference are required")
	}
	return tx.Where("project_id = ? AND ((upstream_kind = ? AND upstream_id = ?) OR (downstream_kind = ? AND downstream_id = ?))", projectID, ref.Kind, ref.ID, ref.Kind, ref.ID).Delete(&models.ArtifactDependency{}).Error
}

// Link records a dependency without changing domain state. Existing invalidation paths remain authoritative.
func (s *ArtifactDependencyService) Link(projectID uint, upstream ArtifactRef, upstreamVersion string, downstream ArtifactRef) error {
	return s.LinkTx(s.db, projectID, upstream, upstreamVersion, downstream)
}

func (s *ArtifactDependencyService) LinkTx(tx *gorm.DB, projectID uint, upstream ArtifactRef, upstreamVersion string, downstream ArtifactRef) error {
	if s == nil || !s.enabled {
		return nil
	}
	if tx == nil {
		tx = s.db
	}
	upstream.Kind, downstream.Kind = strings.TrimSpace(upstream.Kind), strings.TrimSpace(downstream.Kind)
	if projectID == 0 || !validArtifactRef(upstream) || !validArtifactRef(downstream) {
		return fmt.Errorf("project and artifact references are required")
	}
	if upstream == downstream {
		return fmt.Errorf("artifact cannot depend on itself")
	}
	affected, err := s.affectedTx(tx, projectID, downstream)
	if err != nil {
		return err
	}
	for _, item := range affected {
		if item == upstream {
			return fmt.Errorf("artifact dependency cycle detected")
		}
	}
	row := models.ArtifactDependency{ProjectID: projectID, UpstreamKind: upstream.Kind, UpstreamID: upstream.ID, UpstreamVersion: strings.TrimSpace(upstreamVersion), DownstreamKind: downstream.Kind, DownstreamID: downstream.ID}
	return tx.Where(models.ArtifactDependency{ProjectID: projectID, UpstreamKind: upstream.Kind, UpstreamID: upstream.ID, DownstreamKind: downstream.Kind, DownstreamID: downstream.ID}).Assign(map[string]any{"upstream_version": row.UpstreamVersion}).FirstOrCreate(&row).Error
}

func (s *ArtifactDependencyService) Affected(projectID uint, source ArtifactRef) ([]ArtifactRef, error) {
	return s.affectedTx(s.db, projectID, source)
}

func (s *ArtifactDependencyService) affectedTx(tx *gorm.DB, projectID uint, source ArtifactRef) ([]ArtifactRef, error) {
	if s == nil || !s.enabled {
		return []ArtifactRef{}, nil
	}
	if tx == nil {
		tx = s.db
	}
	source.Kind = strings.TrimSpace(source.Kind)
	if projectID == 0 || !validArtifactRef(source) {
		return nil, fmt.Errorf("artifact reference is required")
	}
	queue := []ArtifactRef{source}
	seen := map[ArtifactRef]bool{source: true}
	result := []ArtifactRef{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		var rows []models.ArtifactDependency
		if err := tx.Where("project_id = ? AND upstream_kind = ? AND upstream_id = ?", projectID, current.Kind, current.ID).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			next := ArtifactRef{Kind: row.DownstreamKind, ID: row.DownstreamID}
			if seen[next] {
				continue
			}
			seen[next] = true
			result = append(result, next)
			queue = append(queue, next)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].ID < result[j].ID
		}
		return result[i].Kind < result[j].Kind
	})
	return result, nil
}

// RecordInvalidation writes audit events only; callers still perform domain-specific stale updates transactionally.
func (s *ArtifactDependencyService) RecordInvalidation(tx *gorm.DB, projectID uint, source ArtifactRef, reason string) ([]ArtifactRef, error) {
	if s == nil || !s.enabled {
		return []ArtifactRef{}, nil
	}
	if tx == nil {
		tx = s.db
	}
	affected, err := s.affectedTx(tx, projectID, source)
	if err != nil {
		return nil, err
	}
	for _, item := range affected {
		event := models.ArtifactInvalidationEvent{ProjectID: projectID, ArtifactKind: item.Kind, ArtifactID: item.ID, Reason: strings.TrimSpace(reason), SourceKind: source.Kind, SourceID: source.ID}
		if err := tx.Create(&event).Error; err != nil {
			return nil, err
		}
	}
	return affected, nil
}
