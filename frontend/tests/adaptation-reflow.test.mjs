import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const page = readFileSync(new URL('../src/views/AdaptationPlan.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
test('分集规划修复先预览再携带token确认，不隐式生成剧本', () => {
  assert.match(page, /保留前集，修复重复分集规划/)
  assert.match(page, /预览自动修复方案/)
  assert.match(page, /确认应用此修复方案/)
  assert.match(page, /unplanned_episode/)
  assert.match(page, /token:reflowPreview.value.token,apply:true/)
  assert.match(page, /旧重复剧本会保留至重新生成成功/)
  const apply = page.slice(page.indexOf('async function applyReflow'), page.indexOf('let timer=null'))
  assert.doesNotMatch(apply, /generateAdaptationScript|generateScript\(/)
  assert.match(api, /adaptations\/reflow/)
})
