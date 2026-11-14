// Generation snapshots record what a Scene image/video submission actually used.
// The comparison is a plain field diff; it explains why two takes differ without
// judging which one is better.

export function parseSnapshotRefs(snapshot) {
  try {
    const refs = JSON.parse(snapshot?.refs_json || '[]')
    return Array.isArray(refs) ? refs : []
  } catch {
    return []
  }
}

function shortHash(hash) {
  return hash ? String(hash).slice(0, 12) : '未记录'
}

export function compareGenerationSnapshots(current, previous) {
  if (!current || !previous) return []
  const changes = []
  if (current.template_code !== previous.template_code) changes.push(`模板：${previous.template_code} → ${current.template_code}`)
  else if (current.template_sha256 !== previous.template_sha256) changes.push(`模板 ${current.template_code} 的工作流已更新：${shortHash(previous.template_sha256)} → ${shortHash(current.template_sha256)}`)
  if (current.prompt_sha256 !== previous.prompt_sha256) changes.push('提示词已修改')
  if ((current.params_json || '') !== (previous.params_json || '')) changes.push('生成参数已修改')
  const key = ref => `${ref.role}|${ref.file}`
  const before = new Map(parseSnapshotRefs(previous).map(ref => [key(ref), ref]))
  const after = new Map(parseSnapshotRefs(current).map(ref => [key(ref), ref]))
  for (const [k, ref] of after) {
    const old = before.get(k)
    if (!old) changes.push(`新增参考：${ref.file}`)
    else if (old.sha256 !== ref.sha256) changes.push(`参考图内容已变：${ref.file}（${shortHash(old.sha256)} → ${shortHash(ref.sha256)}）`)
  }
  for (const [k, ref] of before) {
    if (!after.has(k)) changes.push(`移除参考：${ref.file}`)
  }
  return changes
}
