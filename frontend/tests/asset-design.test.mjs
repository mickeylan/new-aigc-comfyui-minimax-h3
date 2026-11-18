import test from 'node:test'
import assert from 'node:assert/strict'
import { diffText, markSources, splitDesignLines } from '../src/utils/assetDesign.js'

const rebuild = (segments, keep) => segments.filter(s => keep.includes(s.type)).map(s => s.text).join('')

test('diff segments rebuild both drafts exactly', () => {
  const prev = '旷野地面有几道裂缝，远山轮廓不变，夜色浓重。'
  const next = '旷野地面布满纵横交错的深裂缝，远山轮廓不变，夜色浓重，月光冷白。'
  const segments = diffText(prev, next)
  assert.equal(rebuild(segments, ['same', 'del']), prev)
  assert.equal(rebuild(segments, ['same', 'add']), next)
  assert.ok(segments.some(s => s.type === 'add' && s.text.includes('月光冷白')))
  assert.ok(segments.some(s => s.type === 'same' && s.text.includes('远山轮廓不变')))
})

test('identical and empty drafts produce no changes', () => {
  assert.deepEqual(diffText('同一稿', '同一稿'), [{ type: 'same', text: '同一稿' }])
  assert.deepEqual(diffText('', '新稿'), [{ type: 'add', text: '新稿' }])
  assert.deepEqual(diffText('', ''), [])
})

test('very long drafts fall back to clause diff and still rebuild', () => {
  const prev = Array.from({ length: 400 }, (_, i) => `第${i}句旧描述，`).join('')
  const next = prev.replace('第200句旧描述，', '第200句改过的描述，')
  const segments = diffText(prev + 'x'.repeat(2000), next + 'y'.repeat(2000))
  assert.equal(rebuild(segments, ['same', 'del']), prev + 'x'.repeat(2000))
  assert.equal(rebuild(segments, ['same', 'add']), next + 'y'.repeat(2000))
})

test('source marks locate excerpts, skip overlaps and unknown tags', () => {
  const text = '巨石碎成散落的碎块，远山沿用原景，天色转为深夜。'
  const parts = markSources(text, [
    { tag: '按你的要求', text: '天色转为深夜' },
    { tag: '沿用', text: '远山沿用原景' },
    { tag: 'AI补充', text: '沿用原景' },
    { tag: '胡写', text: '巨石' },
    { tag: '沿用', text: '不在正文里' }
  ])
  assert.equal(parts.map(p => p.text).join(''), text)
  assert.deepEqual(parts.filter(p => p.tag).map(p => [p.tag, p.text]), [['沿用', '远山沿用原景'], ['按你的要求', '天色转为深夜']])
})

test('design lines are trimmed and de-duplicated', () => {
  assert.deepEqual(splitDesignLines(' 深夜\n\n深夜\n地面焦痕 '), ['深夜', '地面焦痕'])
  assert.deepEqual(splitDesignLines(null), [])
})
