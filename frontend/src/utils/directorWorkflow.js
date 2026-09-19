export function skillOperationOf(skill) {
  return String(skill?.operation || skill?.code || '').trim()
}

export function compatibleSkills(skills, stage, operation) {
  return (skills || []).filter(skill => skill.enabled && skill.stage === stage && skillOperationOf(skill) === operation)
}

export function projectSkillConfig(configs, stage, operation) {
  return (configs || []).find(row => row.stage === stage && row.operation === operation) || null
}

export function promptHistoryLabel(version) {
  const actions = { build: '构建', optimize: '优化', translate: '翻译', manual: '手工保存', rollback: '回滚草稿' }
  return `${actions[version?.action] || version?.action || ''} · ${version?.state === 'applied' ? '已应用' : '草稿'}`
}
