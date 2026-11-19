// 角色年龄阶段的前端纯逻辑：标签、套装分组与批量指定摘要。页面只负责渲染。

export const ageStagePortraitEngines = [
  { value: 'qwen_edit', label: '以主标准像改年龄（Qwen-Image-2.1 编辑，推荐）' },
  { value: 'krea2', label: '仅按阶段外貌文生图（Krea2）' },
  { value: 'qwen_image_2_1', label: '仅按阶段外貌文生图（Qwen-Image-2.1）' },
]

export function ageStageLabel(stage) {
  if (!stage) return ''
  const age = Number(stage.age)
  return Number.isInteger(age) && age > 0 ? `${stage.name}（${age}岁）` : String(stage.name || '')
}

export const stageApproved = stage => stage?.audit_status === 'approved' || stage?.audit_status === 'published'

// 场景下拉分组：主形象在前，年龄阶段按年龄升序（无年龄的排最后）。
export function groupOutfitsByStage(outfits = []) {
  const main = { key: 'main', label: '主形象', outfits: [] }
  const stages = new Map()
  for (const outfit of outfits) {
    const stage = outfit?.age_stage
    if (!outfit?.age_stage_id || !stage) { main.outfits.push(outfit); continue }
    if (!stages.has(stage.id)) stages.set(stage.id, { key: `stage-${stage.id}`, label: ageStageLabel(stage), age: Number.isInteger(stage.age) ? stage.age : Infinity, outfits: [] })
    stages.get(stage.id).outfits.push(outfit)
  }
  const ordered = [...stages.values()].sort((a, b) => a.age - b.age || a.label.localeCompare(b.label, 'zh'))
  return [main, ...ordered].filter(group => group.outfits.length).map(({ key, label, outfits }) => ({ key, label, outfits }))
}

export const episodeAssignActionLabels = { assign: '将改为本套装', unchanged: '已是本套装', skip_other: '已手动选择其他套装，跳过' }

export function validEpisodeRange(from, to) {
  const a = Number(from), b = Number(to)
  return Number.isInteger(a) && Number.isInteger(b) && a >= 1 && b >= a
}

export function episodeAssignSummary(result) {
  if (!result) return ''
  const parts = [`将修改 ${result.assign || 0} 个 Scene`]
  if (result.unchanged) parts.push(`${result.unchanged} 个已是本套装`)
  if (result.skipped) parts.push(`${result.skipped} 个已手动选择其他套装（未勾选覆盖，跳过）`)
  const media = (result.items || []).filter(item => item.action === 'assign' && item.has_media).length
  if (media) parts.push(`其中 ${media} 个已有图片/视频：保留不删，提示词标记需更新，不会自动重新生成`)
  return parts.join('；')
}
