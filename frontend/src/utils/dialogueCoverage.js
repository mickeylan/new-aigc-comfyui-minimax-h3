const nonNegativeInteger = value => {
  const number = Number(value)
  return Number.isFinite(number) ? Math.max(0, Math.trunc(number)) : 0
}

export const validateSceneOrders = orders => {
  if (!Array.isArray(orders)) return []
  return [...new Set(orders.filter(order => Number.isInteger(order) && order > 0))]
}

// Where a missing source line most likely belongs, from the scenes of its nearest covered
// neighbours. Advisory text only; nothing is moved or inserted.
export function missingDialoguePlacement(item) {
  const prev = nonNegativeInteger(item?.prev_scene_order)
  const next = nonNegativeInteger(item?.next_scene_order)
  if (prev && next) return prev === next ? `应放在第${prev}场内` : `应放在第${prev}场与第${next}场之间`
  if (prev) return `应放在第${prev}场之后`
  if (next) return `应放在第${next}场之前`
  return ''
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
        text: String(item?.text ?? '').trim(),
        placement: missingDialoguePlacement(item)
      }))
    : []
  const total = nonNegativeInteger(value.total)
  const covered = Math.min(total, nonNegativeInteger(value.covered))

  const story_pending_scene_orders = validateSceneOrders(value.story_pending_scene_orders)

  return { total, covered, missing, story_pending_scene_orders }
}

// Non-blocking scene density / content-vs-dialogue notes written by script generation.
export function parseSceneAdvisories(raw) {
  let value = raw
  if (typeof raw === 'string') {
    try { value = JSON.parse(raw) } catch { return [] }
  }
  if (!value || typeof value !== 'object' || !Array.isArray(value.scene_advisories)) return []
  return value.scene_advisories.map(item => String(item ?? '').trim()).filter(Boolean)
}
