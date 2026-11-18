// 资产交互设计的纯函数：两稿差异与来源标注分段。不依赖 Vue，便于测试。

const MAX_CHAR_CELLS = 2_500_000

function lcsOps(a, b) {
  const n = a.length
  const m = b.length
  const dp = new Uint32Array((n + 1) * (m + 1))
  const w = m + 1
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i * w + j] = a[i] === b[j] ? dp[(i + 1) * w + j + 1] + 1 : Math.max(dp[(i + 1) * w + j], dp[i * w + j + 1])
    }
  }
  const ops = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) { ops.push(['same', a[i]]); i++; j++ }
    else if (dp[(i + 1) * w + j] >= dp[i * w + j + 1]) { ops.push(['del', a[i]]); i++ }
    else { ops.push(['add', b[j]]); j++ }
  }
  while (i < n) ops.push(['del', a[i++]])
  while (j < m) ops.push(['add', b[j++]])
  return ops
}

function splitClauses(text) {
  return text.match(/[^，。；、！？,.;!?\n]*[，。；、！？,.;!?\n]?/g).filter(Boolean)
}

function pushSegment(out, type, text) {
  if (!text) return
  const last = out[out.length - 1]
  if (last && last.type === type) last.text += text
  else out.push({ type, text })
}

// diffText 返回 [{type:'same'|'add'|'del', text}]：前缀后缀先剥掉，中间按字比对，太长时退到按分句比对。
export function diffText(prev = '', next = '') {
  prev = String(prev)
  next = String(next)
  let start = 0
  while (start < prev.length && start < next.length && prev[start] === next[start]) start++
  let endPrev = prev.length
  let endNext = next.length
  while (endPrev > start && endNext > start && prev[endPrev - 1] === next[endNext - 1]) { endPrev--; endNext-- }
  const out = []
  pushSegment(out, 'same', prev.slice(0, start))
  const a = prev.slice(start, endPrev)
  const b = next.slice(start, endNext)
  let ops
  if (a.length * b.length <= MAX_CHAR_CELLS) ops = lcsOps([...a], [...b])
  else {
    const ca = splitClauses(a)
    const cb = splitClauses(b)
    ops = ca.length * cb.length <= MAX_CHAR_CELLS ? lcsOps(ca, cb) : [['del', a], ['add', b]]
  }
  for (const [type, text] of ops) pushSegment(out, type, text)
  pushSegment(out, 'same', prev.slice(endPrev))
  return out
}

export const ASSET_SOURCE_TAGS = ['沿用', '按你的要求', 'AI补充', '与原文不同']

// markSources 把来源摘录定位回正文，返回 [{text, tag}]（tag 为空表示未标注）；重叠的摘录只取先出现的。
export function markSources(text = '', sources = []) {
  const spans = []
  for (const src of sources || []) {
    if (!src?.text || !ASSET_SOURCE_TAGS.includes(src.tag)) continue
    let from = 0
    let at = -1
    while ((at = text.indexOf(src.text, from)) >= 0) {
      const end = at + src.text.length
      if (!spans.some(s => at < s.end && end > s.start)) { spans.push({ start: at, end, tag: src.tag }); break }
      from = at + 1
    }
  }
  spans.sort((x, y) => x.start - y.start)
  const out = []
  let pos = 0
  for (const s of spans) {
    if (s.start > pos) out.push({ text: text.slice(pos, s.start), tag: '' })
    out.push({ text: text.slice(s.start, s.end), tag: s.tag })
    pos = s.end
  }
  if (pos < text.length) out.push({ text: text.slice(pos), tag: '' })
  return out
}

// 多行文本 ⇄ 去空去重的列表；与后端 ParseAssetDesignDirections 一致。
export function splitDesignLines(text = '') {
  return [...new Set(String(text || '').split('\n').map(s => s.trim()).filter(Boolean))]
}
