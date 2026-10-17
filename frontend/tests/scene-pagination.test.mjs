import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('分镜分页支持页码首页末页和输入跳转', () => {
  assert.match(detail, /aria-label="分镜分页"/)
  assert.match(detail, /@click="goToScenePage\(1\)"[^>]*>« 首页/)
  assert.match(detail, /v-for="item in scenePageItems"/)
  assert.match(detail, /aria-current="item\.page===scenePage\?'page':undefined"/)
  assert.match(detail, /@click="goToScenePage\(scenePageCount\)"[^>]*>末页 »/)
  assert.match(detail, /v-model\.number="scenePageJump"/)
  assert.match(detail, /@submit\.prevent="jumpToScenePage"/)
  assert.match(detail, /current - 2/)
  assert.match(detail, /current \+ 2/)
  assert.match(detail, /Math\.max\(1, Math\.min\(scenePageCount\.value, Math\.trunc\(parsed\)\)\)/)
})
