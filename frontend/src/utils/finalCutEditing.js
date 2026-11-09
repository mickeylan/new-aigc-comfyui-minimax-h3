const MIN_TRIM = 0.05
let nextOccurrence = 0

function finiteNumber(value, name) {
  if (value === null || value === '' || typeof value === 'boolean') throw new TypeError(`${name} must be finite`)
  const number = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(number)) throw new TypeError(`${name} must be finite`)
  return number
}

export function uniqueClipId(prefix = 'clip') {
  nextOccurrence += 1
  return `${prefix}-${Date.now().toString(36)}-${nextOccurrence.toString(36)}`
}

export function normalizeClip(clip, physicalDuration = clip.duration) {
  const duration = finiteNumber(physicalDuration, 'duration')
  if (duration < MIN_TRIM) throw new RangeError('duration is too short')
  const start = clip.source_start == null ? 0 : finiteNumber(clip.source_start, 'source_start')
  const end = clip.source_end == null ? duration : finiteNumber(clip.source_end, 'source_end')
  if (start < 0 || end > duration || end - start < MIN_TRIM) throw new RangeError('invalid trim bounds')
  return { ...clip, id: clip.id || uniqueClipId(clip.scene_id ? 'scene' : 'material'), duration, source_start: start, source_end: end }
}

export function splitClip(clip, playhead) {
  const normalized = normalizeClip(clip, clip.duration)
  const at = finiteNumber(playhead, 'playhead')
  if (at - normalized.source_start < MIN_TRIM || normalized.source_end - at < MIN_TRIM) throw new RangeError('split must leave at least 0.05s on each side')
  return [
    { ...normalized, id: uniqueClipId('clip'), source_end: at },
    { ...normalized, id: uniqueClipId('clip'), source_start: at },
  ]
}

export function trimmedDuration(clip) {
  const normalized = normalizeClip(clip, clip.duration)
  return normalized.source_end - normalized.source_start
}

export function buildEditSequence(clips) {
  return clips.map(clip => {
    const normalized = normalizeClip(clip, clip.duration)
    const item = { id: normalized.id, label: String(normalized.label || ''), source_start: normalized.source_start, source_end: normalized.source_end >= normalized.duration ? null : normalized.source_end }
    if (normalized.scene_id != null) item.scene_id = normalized.scene_id
    if (normalized.material_id != null) item.material_id = normalized.material_id
    return item
  })
}

export { MIN_TRIM }
