import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/AdaptationPlan.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('长篇未规划分集从项目详情跳转到目标滚动规划', () => {
  assert.match(detail, /project\.episodes > 20 && !episodeHasScript\(episodeProductionTarget\)/)
  assert.match(detail, /\/adaptation\?episode=\$\{episodeProductionTarget\}/)
  assert.match(detail, /规划并生成第\{\{episodeProductionTarget\}\}集/)
})

test('滚动规划页读取目标集并展开所属批次', () => {
  assert.match(view, /const targetEpisode=computed/)
  assert.match(view, /规划并生成第\{\{targetEpisode\}\}集/)
  assert.match(view, /const targetBatch=computed/)
  assert.match(view, /api\.planningBatch\(id,batch\.id\)/)
  assert.match(view, /生成方案 → 审核方案并应用Episode → 生成本集剧本/)
})
