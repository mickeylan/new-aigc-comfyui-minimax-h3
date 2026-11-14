// Plot-driven Shot splitting for a whole episode runs as a durable backend batch that
// reuses the per-Scene director draft, so every Scene passes the same validation and
// dialogue snapshot as a manual draft. Saved Shots are an editable reference for H3;
// Scenes the user already directed, produced, or is generating are never touched.
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

export function shotSplitBatchActive(batch) {
  return ['queued', 'running'].includes(batch?.status)
}

export function shotSplitBatchOpen(batch) {
  return shotSplitBatchActive(batch) || batch?.status === 'paused'
}

const statusLabels = { queued: '排队中', running: '拆镜中', paused: '已暂停', completed: '已完成', failed: '部分失败', cancelled: '已取消' }

export function shotSplitStatusLabel(status) {
  return statusLabels[status] || status || ''
}

// Groups batch items by outcome and attaches the Scene for display; Scenes that were
// regenerated or deleted since the batch started still show by id.
export function shotSplitReport(detail, scenes = []) {
  const byId = new Map((scenes || []).map(scene => [scene.id, scene]))
  const report = { saved: [], skipped: [], failed: [], pending: [] }
  for (const item of detail?.items || []) {
    const row = { item, scene: byId.get(item.scene_id) || { id: item.scene_id, order: item.order, title: '' } }
    if (item.status === 'saved') report.saved.push(row)
    else if (item.status === 'skipped') report.skipped.push(row)
    else if (item.status === 'failed') report.failed.push(row)
    else if (item.status === 'queued' || item.status === 'running') report.pending.push(row)
  }
  return report
}
