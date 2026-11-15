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
	// Shortest Scene or Native segment submitted to a video template; H3 output starts at 4 s.
	// Internal Shots are cuts inside one generation and may be shorter.
	minSceneVideoDuration = 4.0
	// Scenes saved before the 4 s minimum may still last 3 s; their existing videos and
	// timelines keep that length instead of being stretched.
	minLegacySceneDuration = 3.0
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
	// Advisory speech capacity (director rules SND-01). Exceeding these counts only
	// raises a warning: dialogue is never dropped, reordered, or spoken faster.
	// A Shot that keeps more speaking characters strains face and voice attribution;
	// a whole H3 segment is read as one continuous scene, so it allows one more.
	maxShotSpeakers    = 2
	maxShotTalkUnits   = 3
	maxSegmentSpeakers = 3
	// Above 10 s a Shot or segment counts as long: it carries an action timeline, a Scene of
	// that length usually takes 2-4 cuts, and shorter merged segments generate more reliably.
	longShotSeconds = 10.0
	// Usual cut counts: a short Scene (typical minimum to typical maximum) takes 1-2 Shots,
	// a long Scene (longShotSeconds to the segment cap) takes 2-4.
	shortSceneShotsMin = 1
	shortSceneShotsMax = 2
	longSceneShotsMin  = 2
	longSceneShotsMax  = 4
	// Only the final combat Shot may settle into a held stance, for about its last 1.5 s.
	finalShotSettleSeconds = 1.5
	// A Scene without authored [Shot N] gets suggested internal cuts only from 9 s; a solo sword
	// practice with three or more distinct techniques gets four cuts instead of three from 12 s.
	minSuggestedCutSceneSeconds     = 9.0
	minFourCutSoloSwordSceneSeconds = 12.0
)

// Scene density advice. A Scene at least sparseSceneMinSeconds long with one static action
// reads as empty; the prompt illustrates overload with a Scene of densityExampleSeconds
// carrying as many split-second actions.
const (
	sparseSceneMinSeconds = 7.0
	densityExampleSeconds = 6
)

// H3 duration risk guidance for the settings page: the 15 s default was validated on a GPU
// with about 12 GB of VRAM, and generations of 30 s or more are usually high risk.
const (
	h3ReferenceGPUMemoryGB = 12
	h3HighRiskSeconds      = 30.0
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
	"{long_shot}", pacingNumber(longShotSeconds),
	"{short_scene_shots_min}", strconv.Itoa(shortSceneShotsMin),
	"{short_scene_shots_max}", strconv.Itoa(shortSceneShotsMax),
	"{long_scene_shots_min}", strconv.Itoa(longSceneShotsMin),
	"{long_scene_shots_max}", strconv.Itoa(longSceneShotsMax),
	"{final_settle}", pacingNumber(finalShotSettleSeconds),
	"{sparse_scene_min}", pacingNumber(sparseSceneMinSeconds),
	"{density_example}", strconv.Itoa(densityExampleSeconds),
	"{dialogue_margin}", pacingNumber(dialogueOpeningClosingMargin),
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
	LongShotSeconds        float64 `json:"long_shot_seconds"`
	H3HighRiskSeconds      float64 `json:"h3_high_risk_seconds"`
	H3ReferenceGPUMemoryGB int     `json:"h3_reference_gpu_memory_gb"`
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
		LongShotSeconds: longShotSeconds, H3HighRiskSeconds: h3HighRiskSeconds, H3ReferenceGPUMemoryGB: h3ReferenceGPUMemoryGB,
		H3GenerationMaxSeconds: configuredH3GenerationDuration(db),
		SceneReferenceImages:   maxSceneReferenceImages, H3ReferenceImages: maxSceneReferenceImages - 1,
	}
}
