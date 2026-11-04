const nonNegativeInteger = value => {
  const number = Number(value)
  return Number.isFinite(number) ? Math.max(0, Math.trunc(number)) : 0
}

export const validateSceneOrders = orders => {
  if (!Array.isArray(orders)) return []
  return [...new Set(orders.filter(order => Number.isInteger(order) && order > 0))]
}

export function parseDialogueCoverage(raw) {
  if (raw == null || (typeof raw === 'string' && !raw.trim())) return null

  let value = raw
  if (typeof raw === 'string') {
    try { value = JSON.parse(raw) } catch { return null }
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null

  const missing = Array.isArray(value.missing)
    ? value.missing.map(item => ({
        chapter_id: item?.chapter_id ?? null,
        chapter_no: item?.chapter_no ?? null,
        offset: item?.offset ?? null,
        speaker: String(item?.speaker ?? '').trim(),
        text: String(item?.text ?? '').trim()
      }))
    : []
  const total = nonNegativeInteger(value.total)
  const covered = Math.min(total, nonNegativeInteger(value.covered))

  const story_pending_scene_orders = validateSceneOrders(value.story_pending_scene_orders)

  return { total, covered, missing, story_pending_scene_orders }
}
