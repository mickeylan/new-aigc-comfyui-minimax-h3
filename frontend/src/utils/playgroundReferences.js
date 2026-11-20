// 生成试验场的参考图是「有序列表」：数组下标决定提示词里的 <imageN>。
// 后端 RenderWorkflow 按 ref_images 的数组顺序绑定 images.image_1..N（H3 编辑模板是 <Picture N>），
// 所以顺序必须由用户显式控制，不能依赖文件对话框返回的顺序。
export const MAX_PLAYGROUND_REFERENCES = 16

export function referenceLabel(index) {
  return `<image${index + 1}>`
}

// appendReferences 追加而不是覆盖：已上传的参考图保持原有顺序，超出上限的部分丢弃。
export function appendReferences(current, incoming, limit = MAX_PLAYGROUND_REFERENCES) {
  const rows = Array.isArray(current) ? [...current] : []
  const next = (Array.isArray(incoming) ? incoming : []).filter(Boolean)
  const room = Math.max(0, limit - rows.length)
  const accepted = next.slice(0, room)
  return { rows: [...rows, ...accepted], skipped: next.length - accepted.length }
}

export function moveReference(rows, index, delta) {
  const list = Array.isArray(rows) ? [...rows] : []
  const target = index + delta
  if (index < 0 || index >= list.length || target < 0 || target >= list.length) return list
  const [item] = list.splice(index, 1)
  list.splice(target, 0, item)
  return list
}

export function removeReference(rows, index) {
  const list = Array.isArray(rows) ? [...rows] : []
  if (index < 0 || index >= list.length) return list
  list.splice(index, 1)
  return list
}

// referenceSlots 给界面用：序号标签、文件名与可预览的输入文件地址。
export function referenceSlots(rows) {
  return (Array.isArray(rows) ? rows : []).map((row, index) => ({
    label: referenceLabel(index),
    key: `${row?.task_id || ''}:${row?.name || index}`,
    name: row?.name || '',
    url: row?.name ? `/api/input/${row.task_id || ''}/${row.name}` : '',
  }))
}