import { MIN_TRIM, normalizeClip } from './finalCutEditing.js'

export function layoutClips(clips) {
  let cursor = 0
  return clips.map((clip, index) => {
    const duration = Math.max(0, (Number(clip.source_end ?? clip.duration) - Number(clip.source_start || 0)) / Number(clip.speed || 1))
    const start = cursor
    const overlap = index < clips.length - 1 && clip.transition && clip.transition !== 'cut' ? Number(clip.transition_duration || 0) : 0
    cursor += duration - overlap
    return { index, start, end: start + duration, duration, overlap }
  })
}
export function snapTime(value, targets, pixelsPerSecond, enabled = true) {
  if (!enabled) return value
  const closest = targets.reduce((best, target) => Math.abs(target-value) < Math.abs(best-value) ? target : best, Infinity)
  return Math.abs(closest-value) * pixelsPerSecond <= 8 ? closest : value
}
export function trimTimelineClip(clip, edge, deltaSeconds) {
  const speed = Number(clip.speed || 1)
  const start = Number(clip.source_start || 0), end = Number(clip.source_end ?? clip.duration)
  const changes = edge === 'start'
    ? { source_start: Math.max(0, Math.min(end-MIN_TRIM, start+deltaSeconds*speed)) }
    : { source_end: Math.min(Number(clip.duration), Math.max(start+MIN_TRIM, end+deltaSeconds*speed)) }
  const duration = ((changes.source_end ?? end)-(changes.source_start ?? start))/speed
  return normalizeClip({ ...clip, ...changes, fade_in: Math.min(Number(clip.fade_in||0),duration), fade_out: Math.min(Number(clip.fade_out||0),duration) },clip.duration)
}
// Bounded immutable history; records all clip fields, including effects and source identity.
export function createEditHistory(limit = 80) {
  let entries = [], index = -1
  const clone = value => JSON.parse(JSON.stringify(value))
  return {
    record(value) {
      const next = JSON.stringify(value)
      if (entries[index] === next) return
      entries = entries.slice(0,index+1); entries.push(next)
      if (entries.length > limit) entries.shift()
      index = entries.length-1
    },
    undo() { if(index<=0)return null; return clone(JSON.parse(entries[--index])) },
    redo() { if(index>=entries.length-1)return null;return clone(JSON.parse(entries[++index])) },
    get canUndo(){return index>0}, get canRedo(){return index<entries.length-1},
  }
}
