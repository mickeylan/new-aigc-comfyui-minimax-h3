// Field-level Shot refine patches: labels and selection helpers for the review panel.
export const refineFieldLabels = {
  shot_type: '景别', camera_angle: '机位角度', camera_movement: '运镜', blocking: '人物调度', eye_line: '视线',
  performance: '演员表演', reaction: '可见反应', prompt_action: '姿态与动作', prompt_camera: '摄影机与构图', prompt_lighting: '光线与氛围'
}

export const refineFieldLabel = field => refineFieldLabels[field] || field

export function refineRationaleText(rationale) {
  const r = rationale || {}
  return [['问题', r.issue], ['不足', r.problem], ['改法', r.change_reason], ['效果', r.result_reason]]
    .filter(([, value]) => String(value || '').trim())
    .map(([label, value]) => `${label}：${String(value).trim()}`)
    .join('；')
}

// Only checked edits are sent; the server re-validates each one against the source hash.
export function selectedRefineEdits(edits, selected) {
  const chosen = new Set(selected || [])
  return (edits || []).filter((_, index) => chosen.has(index))
}

export const refineStateLabel = state => ({ applied: '已应用', rolled_back: '已撤销' })[state] || state
