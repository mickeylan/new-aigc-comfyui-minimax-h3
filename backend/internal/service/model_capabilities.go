package service

import "gorm.io/gorm"

// System-wide video limits shared by planning, validation and the UI. Per-template
// capabilities (reference slots, first/last frame, duration inputs) stay derived from
// Template.InputsJSON in model_catalog.go; the values here apply whatever the template.
const (
	// One H3 generation lasts at most 15 s, so an internal Shot or Native segment does too.
	h3SegmentMaxSeconds = 15.0
	// Shortest Scene or Native segment submitted to a video template.
	minSceneVideoDuration = 3.0
	// A long Scene may reach 60 s; it is materialized as Native segments of at most 15 s.
	maxLongSceneVideoDuration = 60.0
	// Scene reference images per generation; H3 reserves one slot for the start frame.
	maxSceneReferenceImages = 9

	maxSceneVideoDuration  = h3SegmentMaxSeconds
	maxNativeSceneDuration = h3SegmentMaxSeconds
	maxShotDuration        = h3SegmentMaxSeconds
)

// Director pacing rules for internal Shots. They are editorial advice, not model limits:
// a typical cut lasts at least 3 s, a reaction/detail cut may be 2 s, and two
// consecutive cuts under 3 s read as jitter.
const (
	minTypicalShotSeconds  = 3.0
	minReactionShotSeconds = 2.0
)

// ModelCapabilityLimits is the read model the UI uses for duration inputs and reference pickers.
type ModelCapabilityLimits struct {
	ShotMaxSeconds         float64 `json:"shot_max_seconds"`
	SceneMinSeconds        float64 `json:"scene_min_seconds"`
	SceneMaxSeconds        float64 `json:"scene_max_seconds"`
	H3GenerationMaxSeconds float64 `json:"h3_generation_max_seconds"`
	SceneReferenceImages   int     `json:"scene_reference_images"`
	H3ReferenceImages      int     `json:"h3_reference_images"`
}

func modelCapabilityLimits(db *gorm.DB) ModelCapabilityLimits {
	return ModelCapabilityLimits{
		ShotMaxSeconds: maxShotDuration, SceneMinSeconds: minSceneVideoDuration, SceneMaxSeconds: maxLongSceneVideoDuration,
		H3GenerationMaxSeconds: configuredH3GenerationDuration(db),
		SceneReferenceImages:   maxSceneReferenceImages, H3ReferenceImages: maxSceneReferenceImages - 1,
	}
}
