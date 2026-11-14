package service

import (
	"strconv"
	"strings"

	"gorm.io/gorm"
)

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
	// A typical internal Shot lasts up to 8 s; longer cuts carry an action timeline.
	maxTypicalShotSeconds = 8.0
	// Combat Shots stay short so actions cannot be stretched into slow motion.
	maxCombatShotSeconds = 6.0
	// Cut density cap inside one H3 generation.
	maxShotsPerGeneration = 5
	// A plot-driven Scene usually lasts at least 5 s.
	minTypicalSceneSeconds = 5.0
)

// Episode planning defaults: soft references for pacing, never padding or compression targets.
const (
	defaultEpisodeTargetSeconds = 180.0
	defaultEpisodeTargetScenes  = 25
)

func pacingNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// pacingText fills named pacing tokens so prompts and copy follow the constants above instead
// of repeating literals. Every token renders the same digits the prompts used before.
var pacingText = strings.NewReplacer(
	"{shot_max}", pacingNumber(maxShotDuration),
	"{scene_min}", pacingNumber(minSceneVideoDuration),
	"{scene_typical_min}", pacingNumber(minTypicalSceneSeconds),
	"{shot_typical_min}", pacingNumber(minTypicalShotSeconds),
	"{shot_typical_max}", pacingNumber(maxTypicalShotSeconds),
	"{reaction_min}", pacingNumber(minReactionShotSeconds),
	"{combat_shot_max}", pacingNumber(maxCombatShotSeconds),
	"{shots_per_generation}", strconv.Itoa(maxShotsPerGeneration),
	"{action_beat}", pacingNumber(actionBeatSeconds),
	"{speech_rate}", pacingNumber(defaultChineseCharsPerSecond),
	"{speech_rate_min}", pacingNumber(minChineseCharsPerSecond),
	"{speech_rate_max}", pacingNumber(maxChineseCharsPerSecond),
	"{episode_seconds}", pacingNumber(defaultEpisodeTargetSeconds),
	"{episode_scenes}", strconv.Itoa(defaultEpisodeTargetScenes),
)

func pacing(text string) string {
	return pacingText.Replace(text)
}

// ModelCapabilityLimits is the read model the UI uses for duration inputs and reference pickers.
type ModelCapabilityLimits struct {
	ShotMaxSeconds         float64 `json:"shot_max_seconds"`
	ShotTypicalMinSeconds  float64 `json:"shot_typical_min_seconds"`
	ReactionShotMinSeconds float64 `json:"reaction_shot_min_seconds"`
	CombatShotMaxSeconds   float64 `json:"combat_shot_max_seconds"`
	SceneMinSeconds        float64 `json:"scene_min_seconds"`
	SceneMaxSeconds        float64 `json:"scene_max_seconds"`
	H3GenerationMaxSeconds float64 `json:"h3_generation_max_seconds"`
	SceneReferenceImages   int     `json:"scene_reference_images"`
	H3ReferenceImages      int     `json:"h3_reference_images"`
}

func modelCapabilityLimits(db *gorm.DB) ModelCapabilityLimits {
	return ModelCapabilityLimits{
		ShotMaxSeconds: maxShotDuration, SceneMinSeconds: minSceneVideoDuration, SceneMaxSeconds: maxLongSceneVideoDuration,
		ShotTypicalMinSeconds: minTypicalShotSeconds, ReactionShotMinSeconds: minReactionShotSeconds, CombatShotMaxSeconds: maxCombatShotSeconds,
		H3GenerationMaxSeconds: configuredH3GenerationDuration(db),
		SceneReferenceImages:   maxSceneReferenceImages, H3ReferenceImages: maxSceneReferenceImages - 1,
	}
}
