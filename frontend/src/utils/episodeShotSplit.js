// Plot-driven Shot splitting for a whole episode reuses the per-Scene director draft
// endpoint, so every Scene passes the same validation and dialogue snapshot as a manual
// draft. Saved Shots are an editable reference for H3; Scenes the user already directed,
// produced, or is generating are never touched.
const workingStatuses = ['image_pending', 'video_pending', 'video_creating', 'video_running']

export function episodeShotSplitSkipReason(scene) {
  if (Number(scene?.shot_count || 0) > 0) return '已有导演Shot'
  if (scene?.status === 'video_ready') return '视频已就绪'
  if (workingStatuses.includes(scene?.status)) return '正在生成'
  if (!String(scene?.content || '').trim()) return '场景正文为空'
  return ''
}

export function directorDraftShotPayload(draft) {
  return (draft?.shots || []).map(({ checks, ...shot }) => ({ ...shot, speech_presentation: shot.speech_presentation || '', listener_character: shot.listener_character || '' }))
}

// Scenes run one at a time: each draft is one or two model calls, and a failure is
// recorded per Scene instead of aborting the episode.
export async function splitEpisodeShots({ scenes, generate, save, onProgress = () => {}, cancelled = () => false }) {
  const result = { saved: [], skipped: [], failed: [], cancelled: false }
  const queue = []
  for (const scene of scenes || []) {
    const reason = episodeShotSplitSkipReason(scene)
    if (reason) result.skipped.push({ scene, reason })
    else queue.push(scene)
  }
  for (let i = 0; i < queue.length; i++) {
    if (cancelled()) { result.cancelled = true; break }
    const scene = queue[i]
    onProgress({ done: i, total: queue.length, scene })
    try {
      const { data } = await generate(scene)
      const shots = directorDraftShotPayload(data?.draft)
      if (!shots.length) throw new Error('导演草稿没有镜头')
      await save(scene, shots, data?.dialogue_snapshot || '')
      result.saved.push({ scene, count: shots.length })
    } catch (error) {
      result.failed.push({ scene, error })
    }
  }
  onProgress({ done: result.saved.length + result.failed.length, total: queue.length, scene: null })
  return result
}
