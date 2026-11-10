// Bounded, source-keyed preview metadata cache. Only measured media facts are
// stored; editing decisions remain in the project draft and server snapshot.
const KEY = 'project-editor-preview-cache-v1'
const LIMIT = 150

export function previewSourceKey(clip) {
  if (!clip) return ''
  if (clip.scene_id != null) return `scene:${clip.scene_id}`
  if (clip.material_id != null) return `material:${clip.material_id}`
  return ''
}

export function readPreviewCache(storage = globalThis.localStorage) {
  try {
    const parsed = JSON.parse(storage?.getItem(KEY) || '{}')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch { return {} }
}

export function cachedMedia(clip, storage = globalThis.localStorage) {
  const key = previewSourceKey(clip)
  if (!key) return null
  const entry = readPreviewCache(storage)[key]
  if (!entry || !Number.isFinite(entry.duration) || entry.duration <= 0) return null
  return entry
}

export function rememberMedia(clip, meta, storage = globalThis.localStorage) {
  const key = previewSourceKey(clip)
  if (!key || !meta || !Number.isFinite(Number(meta.duration)) || Number(meta.duration) <= 0) return null
  const cache = readPreviewCache(storage)
  cache[key] = {
    duration: Number(meta.duration),
    fps: Number(meta.fps) > 0 ? Number(meta.fps) : 0,
    width: Number(meta.width) || 0,
    height: Number(meta.height) || 0,
    proxy_ready: Boolean(meta.proxy_ready),
    updated_at: Date.now(),
  }
  const keys = Object.keys(cache)
  if (keys.length > LIMIT) {
    keys.sort((a, b) => (cache[a].updated_at || 0) - (cache[b].updated_at || 0))
    for (const stale of keys.slice(0, keys.length - LIMIT)) delete cache[stale]
  }
  try { storage?.setItem(KEY, JSON.stringify(cache)) } catch { /* quota/private mode */ }
  return cache[key]
}

// Frame stepping uses the measured source rate; 24 is only an explicit fallback
// for sources whose fps could not be probed.
export function previewFPS(clip, storage = globalThis.localStorage) {
  const fps = Number(cachedMedia(clip, storage)?.fps)
  return Number.isFinite(fps) && fps > 0 && fps <= 240 ? fps : 24
}

export function timecodeAt(seconds, fps = 24) {
  const rate = Number(fps) > 0 ? Number(fps) : 24
  const safe = Math.max(0, Number(seconds) || 0)
  const totalFrames = Math.round(safe * rate)
  const frames = totalFrames % Math.round(rate)
  const totalSeconds = Math.floor(totalFrames / rate)
  const pad = value => String(value).padStart(2, '0')
  return `${pad(Math.floor(totalSeconds / 3600))}:${pad(Math.floor(totalSeconds / 60) % 60)}:${pad(totalSeconds % 60)}:${pad(frames)}`
}