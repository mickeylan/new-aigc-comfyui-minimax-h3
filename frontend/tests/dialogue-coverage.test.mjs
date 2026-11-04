import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { parseDialogueCoverage } from '../src/utils/dialogueCoverage.js'

const view = readFileSync(new URL('../src/views/AdaptationPlan.vue', import.meta.url), 'utf8')

test('空覆盖数据不会被静默解释为无对白', () => {
  assert.equal(parseDialogueCoverage(''), null)
  assert.equal(parseDialogueCoverage(null), null)
  assert.equal(parseDialogueCoverage('{bad json'), null)
})

test('完整覆盖数据解析总数和已安排数', () => {
  assert.deepEqual(parseDialogueCoverage(JSON.stringify({ total: 2, covered: 2, missing: [] })), {
    total: 2,
    covered: 2,
    missing: []
  })
})

test('遗漏覆盖数据保留章节、位置、speaker和原句', () => {
  assert.deepEqual(parseDialogueCoverage({
    total: 3,
    covered: 2,
    missing: [{ chapter_id: 7, chapter_no: 4, offset: 18, speaker: '', text: '你终于来了。' }]
  }), {
    total: 3,
    covered: 2,
    missing: [{ chapter_id: 7, chapter_no: 4, offset: 18, speaker: '', text: '你终于来了。' }]
  })
})

test('覆盖提示是非阻断警告且剧本生成后刷新详情', () => {
  assert.match(view, /原文对白 \{\{dialogueCoverage\(ep\)\.total\}\} · 已安排/)
  assert.match(view, /item\.speaker \|\| '待绑定'/)
  assert.match(view, /不能静默判定本集无对白/)
  assert.match(view, /不会禁用后续操作，也不会自动覆写已有媒体或结构化编辑草稿/)
  assert.match(view, /generateAdaptationScript[\s\S]*?planningBatch\(id,ep\.batch_id\)/)
  assert.doesNotMatch(view, /:disabled="[^"]*dialogueCoverage/)
})
