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
