package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestValidateAdaptationTreatsSceneCountAsPlanningHint(t *testing.T) {
	for _, scenes := range []int{8, 25, 40} {
		v := models.EpisodeAdaptation{EpisodeN: 1, ChapterStart: 1, ChapterEnd: 2, SourceChapterIDs: "[1,2]", TargetDuration: 180, TargetScenes: scenes}
		if err := validateAdaptation(&v); err != nil {
			t.Fatalf("target_scenes=%d should be accepted: %v", scenes, err)
		}
	}
	for _, scenes := range []int{-1, 101} {
		v := models.EpisodeAdaptation{EpisodeN: 1, ChapterStart: 1, ChapterEnd: 2, SourceChapterIDs: "[1,2]", TargetDuration: 180, TargetScenes: scenes}
		if err := validateAdaptation(&v); err == nil {
			t.Fatalf("target_scenes=%d should be rejected", scenes)
		}
	}
}
