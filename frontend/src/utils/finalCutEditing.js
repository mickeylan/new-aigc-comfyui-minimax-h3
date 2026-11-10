import { normalizeTransform, splitTransform } from './clipTransform.js'
import { normalizeClipTracks, splitClipTracks } from './clipTracks.js'
const MIN_TRIM = 0.05
import {transitionOptions,normalizePackaging} from './visualPackaging.js'
const TRANSITIONS = new Set(transitionOptions.map(([key])=>key))
let nextOccurrence = 0

function finiteNumber(value, name) {
  if (value === null || value === '' || typeof value === 'boolean') throw new TypeError(`${name} must be finite`)
  const number = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(number)) throw new TypeError(`${name} must be finite`)
  return number
}

function ranged(value, fallback, min, max, name) {
  const number = value == null ? fallback : finiteNumber(value, name)
  if (number < min || number > max) throw new RangeError(`${name} must be between ${min} and ${max}`)
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
  if (start < 0 || end > duration || end - start < MIN_TRIM - 1e-9) throw new RangeError('invalid trim bounds')
  const speed = ranged(clip.speed, 1, .25, 4, 'speed')
  const volume = ranged(clip.volume, 1, 0, 4, 'volume')
  const outputDuration = (end - start) / speed
  const fade_in = ranged(clip.fade_in, 0, 0, outputDuration, 'fade_in')
  const fade_out = ranged(clip.fade_out, 0, 0, outputDuration, 'fade_out')
  const transition = clip.transition == null ? 'cut' : String(clip.transition)
  if (!TRANSITIONS.has(transition)) throw new RangeError('unknown transition')
  const transition_duration = ranged(clip.transition_duration, .3, .05, 2, 'transition_duration')
  const brightness = ranged(clip.brightness, 0, -1, 1, 'brightness')
  const contrast = ranged(clip.contrast, 1, .1, 3, 'contrast')
  const saturation = ranged(clip.saturation, 1, 0, 3, 'saturation')
  const title = String(clip.title || '')
  if (Array.from(title).length > 200) throw new RangeError('title must be at most 200 characters')
  const title_position = clip.title_position == null ? 'bottom' : String(clip.title_position)
  if (!['top', 'bottom'].includes(title_position)) throw new RangeError('unknown title_position')
  const title_size = ranged(clip.title_size, 48, 16, 96, 'title_size')
  const overlay_material_id = clip.overlay_material_id == null || clip.overlay_material_id === '' ? null : finiteNumber(clip.overlay_material_id, 'overlay_material_id')
  if (overlay_material_id != null && (!Number.isInteger(overlay_material_id) || overlay_material_id <= 0)) throw new RangeError('overlay_material_id must be a positive integer')
  const overlay_x = ranged(clip.overlay_x, 0, 0, 1, 'overlay_x')
  const overlay_y = ranged(clip.overlay_y, 0, 0, 1, 'overlay_y')
  const overlay_width = ranged(clip.overlay_width, .25, .05, 1, 'overlay_width')
  const overlay_opacity = ranged(clip.overlay_opacity, 1, .05, 1, 'overlay_opacity')
  const transform=normalizeTransform(clip.transform,outputDuration)
  return { ...clip, packaging:normalizePackaging(clip.packaging), ...normalizeClipTracks(clip,outputDuration), transform, id: clip.id || uniqueClipId(clip.scene_id ? 'scene' : 'material'), duration, source_start: start, source_end: end, speed, volume, fade_in, fade_out, transition, transition_duration, brightness, contrast, saturation, title, title_position, title_size, overlay_material_id, overlay_x, overlay_y, overlay_width, overlay_opacity }
}

export function clipOutputDuration(clip) {
  const normalized = normalizeClip(clip, clip.duration)
  return (normalized.source_end - normalized.source_start) / normalized.speed
}

export function splitClip(clip, playhead) {
  const normalized = normalizeClip(clip, clip.duration)
  const at = finiteNumber(playhead, 'playhead')
  if (at - normalized.source_start < MIN_TRIM || normalized.source_end - at < MIN_TRIM) throw new RangeError('split must leave at least 0.05s on each side')
  const leftOutput = (at - normalized.source_start) / normalized.speed
  const rightOutput = (normalized.source_end - at) / normalized.speed
  const [leftTransform,rightTransform]=splitTransform(normalized.transform,leftOutput,leftOutput+rightOutput)
  const [leftTracks,rightTracks]=splitClipTracks(normalized,leftOutput,leftOutput+rightOutput)
  return [
    { ...normalized, ...leftTracks, transform:leftTransform, id: uniqueClipId('clip'), source_end: at, fade_in: Math.min(normalized.fade_in, leftOutput), fade_out: 0, transition: 'cut', transition_duration: .3 },
    { ...normalized, ...rightTracks, transform:rightTransform, id: uniqueClipId('clip'), source_start: at, fade_in: 0, fade_out: Math.min(normalized.fade_out, rightOutput) },
  ]
}

export function trimmedDuration(clip) {
  const normalized = normalizeClip(clip, clip.duration)
  return normalized.source_end - normalized.source_start
}

function putNonDefault(item, clip, name, fallback) {
  if (clip[name] !== fallback && clip[name] !== '' && clip[name] != null) item[name] = clip[name]
}

export function buildEditSequence(clips) {
  const normalized = clips.map(clip => normalizeClip(clip, clip.duration))
  normalized.forEach((clip, index) => {
    if (index === normalized.length - 1 && clip.transition !== 'cut') throw new RangeError('final clip transition must be cut')
    if (clip.transition !== 'cut') {
      const limit = Math.min(clipOutputDuration(clip), clipOutputDuration(normalized[index + 1])) / 2
      if (clip.transition_duration > limit) throw new RangeError('transition_duration exceeds adjacent clip limit')
    }
  })
  return normalized.map(clip => {
    const item = { id: clip.id, label: String(clip.label || ''), source_start: clip.source_start, source_end: clip.source_end >= clip.duration ? null : clip.source_end }
    if(clip.packaging)item.packaging=clip.packaging
    if(clip.transform) item.transform=clip.transform
    for(const key of ['visual_tracks','volume_envelope','markers'])if(clip[key]?.length)item[key]=clip[key]
    if (clip.scene_id != null) item.scene_id = clip.scene_id
    if (clip.material_id != null) item.material_id = clip.material_id
    putNonDefault(item, clip, 'speed', 1); putNonDefault(item, clip, 'volume', 1)
    putNonDefault(item, clip, 'fade_in', 0); putNonDefault(item, clip, 'fade_out', 0)
    putNonDefault(item, clip, 'transition', 'cut')
    if (clip.transition !== 'cut') item.transition_duration = clip.transition_duration
    putNonDefault(item, clip, 'brightness', 0); putNonDefault(item, clip, 'contrast', 1); putNonDefault(item, clip, 'saturation', 1)
    putNonDefault(item, clip, 'title', '')
    if (clip.title) { putNonDefault(item, clip, 'title_position', 'bottom'); putNonDefault(item, clip, 'title_size', 48) }
    if (clip.overlay_material_id != null) {
      item.overlay_material_id = clip.overlay_material_id
      putNonDefault(item, clip, 'overlay_x', 0); putNonDefault(item, clip, 'overlay_y', 0); putNonDefault(item, clip, 'overlay_width', .25); putNonDefault(item, clip, 'overlay_opacity', 1)
    }
    return item
  })
}

export function sequenceDuration(clips) {
  if (!clips.length) return 0
  const normalized = clips.map(clip => normalizeClip(clip, clip.duration))
  buildEditSequence(normalized)
  return normalized.reduce((total, clip, index) => total + clipOutputDuration(clip) - (index < normalized.length - 1 && clip.transition !== 'cut' ? clip.transition_duration : 0), 0)
}

export function serializeDraft(clips) {
  return JSON.stringify({ version: 1, clips: clips.map(clip => { const item = buildEditSequence([{ ...clip, transition: 'cut' }])[0]; if (clip.transition && clip.transition !== 'cut') { item.transition = clip.transition; item.transition_duration = clip.transition_duration ?? .3 } return item }) })
}

export { MIN_TRIM }
