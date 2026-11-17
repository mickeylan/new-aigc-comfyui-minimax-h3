// 把 H3 提示词中的 <Subject N> / <Picture N> 协议标记与参考图对应起来，只用于界面显示。
// 提交给 H3 的文本始终原样保留这些标记，这里绝不改写提示词。

const TAG_PATTERN = /<(Subject|Picture)\s+(\d+)>/g

const ROLE_LABELS = {
  character: '人物',
  look: '造型',
  location: '场景',
  prop: '道具',
  storyboard: '分镜图',
  reference: '参考图'
}

export function referenceRoleLabel(role) {
  return ROLE_LABELS[role] || ROLE_LABELS.reference
}

export function referenceDisplayName(binding) {
  if (!binding) return ''
  // 分镜帧没有「名字」，identity 会退化成整行标签，界面上只显示用途。
  if (binding.role === 'storyboard') return ROLE_LABELS.storyboard
  return String(binding.identity || binding.label || `参考图 ${binding.picture}`).trim()
}

// 人物主体插入 <Subject N>，其余参考图只能以 <Picture N> 引用。
export function referenceInsertTag(binding) {
  return binding?.subject ? `<Subject ${binding.subject}>` : `<Picture ${binding.picture}>`
}

function bindingIssue(kind, n, binding, total) {
  if (!binding) return `第 ${n} 张参考图不存在（当前共 ${total} 张）`
  if (kind === 'subject' && !binding.subject) return `第 ${n} 张是${referenceRoleLabel(binding.role)}「${referenceDisplayName(binding)}」，不是人物主体，不能写成 <Subject ${n}>`
  return ''
}

// 返回按原文顺序排列的片段：{ type: 'text', text } 或
// { type: 'ref', kind, n, raw, binding, issue }。所有片段的 text/raw 拼起来等于原文。
export function parsePromptReferences(text, bindings = []) {
  const source = String(text || '')
  const byPicture = new Map((bindings || []).map(b => [Number(b.picture), b]))
  const total = byPicture.size
  const segments = []
  let last = 0
  for (const match of source.matchAll(TAG_PATTERN)) {
    if (match.index > last) segments.push({ type: 'text', text: source.slice(last, match.index) })
    const kind = match[1] === 'Subject' ? 'subject' : 'picture'
    const n = Number(match[2])
    const binding = byPicture.get(n) || null
    segments.push({ type: 'ref', kind, n, raw: match[0], binding, issue: bindingIssue(kind, n, binding, total) })
    last = match.index + match[0].length
  }
  if (last < source.length) segments.push({ type: 'text', text: source.slice(last) })
  return segments
}

// 提示词在同一行里写明“<Picture N> …角色「X」”时，核对 X 与当前第 N 张参考图是否同一人。
// 参考图增删后旧提示词的编号会整体错位，而编号本身仍在范围内，只有对名字才能发现。
// 两种写法都要认：分镜图提示词写“角色「X」”，视频提示词写“中名为「X」的角色”。
const NAMED_PICTURE_PATTERN = /<Picture\s+(\d+)>[^\n<]*?(?:角色(?:造型)?|名为)「([^」]+)」/g

function pictureNameIssues(text, byPicture) {
  const issues = []
  for (const match of String(text || '').matchAll(NAMED_PICTURE_PATTERN)) {
    const n = Number(match[1])
    const written = match[2].trim()
    const binding = byPicture.get(n)
    // 分镜帧没有可对照的名字；场景、道具图被写成某个角色同样是错位。
    if (!binding || binding.role === 'storyboard') continue
    const actual = referenceDisplayName(binding)
    if (written && actual && written !== actual) issues.push({ raw: `<Picture ${n}>「${written}」`, issue: `提示词写的是「${written}」，但当前第 ${n} 张参考图是「${actual}」；参考图可能增删过，请重新生成或手动核对` })
  }
  return issues
}

export function promptReferenceIssues(text, bindings = []) {
  const seen = new Set()
  const issues = []
  for (const segment of parsePromptReferences(text, bindings)) {
    if (segment.type !== 'ref' || !segment.issue || seen.has(segment.raw)) continue
    seen.add(segment.raw)
    issues.push({ raw: segment.raw, issue: segment.issue })
  }
  const byPicture = new Map((bindings || []).map(b => [Number(b.picture), b]))
  for (const item of pictureNameIssues(text, byPicture)) {
    if (seen.has(item.raw)) continue
    seen.add(item.raw)
    issues.push(item)
  }
  return issues
}

// 在光标处插入标记；没有选区信息时追加到末尾。返回新文本和插入后的光标位置。
export function insertAtSelection(value, tag, start, end) {
  const text = String(value || '')
  const from = Number.isInteger(start) ? Math.max(0, Math.min(start, text.length)) : text.length
  const to = Number.isInteger(end) ? Math.max(from, Math.min(end, text.length)) : from
  return { value: text.slice(0, from) + tag + text.slice(to), caret: from + tag.length }
}
