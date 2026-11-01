import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('项目详情提供常驻分集快速切换而不依赖展开创作方案', () => {
  assert.match(detail, /class="episode-quick-nav" aria-label="快速切换分集"/)
  assert.match(detail, /@click="selectEp\(activeEpN-1\)"[^>]*>← 上一集/)
  assert.match(detail, /v-for="episode in episodeQuickOptions"/)
  assert.match(detail, /@change="selectEp\(Number\(\$event\.target\.value\)\)"/)
  assert.match(detail, /@click="selectEp\(activeEpN\+1\)"[^>]*>下一集 →/)
  assert.match(detail, /v-model\.number="episodeJump"/)
  assert.match(detail, /@submit\.prevent="jumpToEpisode"/)
  assert.match(detail, /episodeNeighbourOptions/)
  assert.match(detail, /position: sticky/)
  assert.match(detail, /episodeJump\.value = episodeN/)
  assert.match(detail, /Math\.max\(1, Math\.min\(episodeUpperBound\.value/)
})

test('当前集完成后常驻显示下一集剧本生成入口且保留原有入口', () => {
  assert.match(detail, /class="btn btn-sm episode-next-script"/)
  assert.match(detail, /@click="generateNextEpisodeScript"/)
  assert.match(detail, /生成.*第\$\{nextEpisodeScriptN\}集剧本/)
  assert.match(detail, /await api\.generateScript\(id\(\), target\)/)
  assert.match(detail, /await selectEp\(target\)/)
  assert.match(detail, /系统会先保存安全版本/)
  assert.match(detail, /@click="regenerateScript"/)
  assert.match(detail, /@click="generatePlan"/)
  assert.match(detail, /@click="startPipeline"/)
  assert.match(detail, /进入5–20集滚动规划/)
})
