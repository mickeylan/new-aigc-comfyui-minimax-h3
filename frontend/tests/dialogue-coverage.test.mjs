import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { parseDialogueCoverage, validateSceneOrders } from '../src/utils/dialogueCoverage.js'

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
    missing: [],
    story_pending_scene_orders: []
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
    missing: [{ chapter_id: 7, chapter_no: 4, offset: 18, speaker: '', text: '你终于来了。' }],
    story_pending_scene_orders: []
  })
})

test('待整合场次只保留唯一正整数且不改变覆盖统计', () => {
  assert.deepEqual(validateSceneOrders([1, 2, 2, 0, -1, 1.5, '3', null]), [1, 2])
  assert.deepEqual(parseDialogueCoverage({
    total: 29,
    covered: 29,
    missing: [],
    story_pending_scene_orders: [1, 2, 2, 0, '3']
  }), {
    total: 29,
    covered: 29,
    missing: [],
    story_pending_scene_orders: [1, 2]
  })
})

test('旧数据中的畸形待整合场次字段按空列表兼容', () => {
  assert.deepEqual(parseDialogueCoverage({ total: 2, covered: 2, missing: [], story_pending_scene_orders: '1,2' }), {
    total: 2,
    covered: 2,
    missing: [],
    story_pending_scene_orders: []
  })
})

test('覆盖提示是非阻断警告且剧本生成后刷新详情', () => {
  assert.match(view, /原文对白 \{\{dialogueCoverage\(ep\)\.total\}\} · 已安排/)
  assert.match(view, /item\.speaker \|\| '待绑定'/)
  assert.match(view, /不能静默判定本集无对白/)
  assert.match(view, /第\{\{order\}\}场对白尚未自然融入故事内容，请到编辑场景→按原文整合对白预览审核/)
  assert.match(view, /story_pending_scene_orders\.length/)
  assert.match(view, /不会禁用后续操作，也不会自动覆写已有媒体或结构化编辑草稿/)
  assert.match(view, /generateAdaptationScript[\s\S]*?planningBatch\(id,ep\.batch_id\)/)
  assert.doesNotMatch(view, /:disabled="[^"]*dialogueCoverage/)
})
