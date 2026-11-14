// Offline fallbacks for /api/model-capabilities. The backend constants in
// model_capabilities.go are the source of truth; these values only apply when the
// request fails, so every view shows the same numbers either way.
export const MODEL_LIMIT_DEFAULTS = Object.freeze({
  shot_max_seconds: 15,
  shot_typical_min_seconds: 3,
  reaction_shot_min_seconds: 2,
  combat_shot_max_seconds: 6,
  long_shot_seconds: 10,
  scene_min_seconds: 3,
  scene_max_seconds: 60,
  h3_generation_max_seconds: 15,
  h3_high_risk_seconds: 30,
  h3_reference_gpu_memory_gb: 12,
  scene_reference_images: 9,
  h3_reference_images: 8,
})

// mergeModelLimits keeps every known key: a missing or non-positive server value
// falls back to its default instead of leaving the copy blank.
export function mergeModelLimits(data) {
  const out = { ...MODEL_LIMIT_DEFAULTS }
  for (const key of Object.keys(MODEL_LIMIT_DEFAULTS)) {
    const value = Number(data?.[key])
    if (Number.isFinite(value) && value > 0) out[key] = value
  }
  return out
}
