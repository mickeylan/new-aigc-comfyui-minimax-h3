import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('俯视调度是默认关闭的实验开关', () => {
  assert.match(settings, /v-model="form\['experimental\.topview'\]"/)
  assert.match(settings, /实验：俯视调度/)
  assert.match(settings, /3 个 Scene 的 A\/B 对比/)
  assert.match(detail, /const topviewEnabled = ref\(false\)/)
  assert.match(detail, /topviewEnabled\.value = data\?\.\['experimental\.topview'\] === 'true'/)
})

test('场景资产俯视图按场景图缓存，仅在开关打开时出现', () => {
  assert.match(detail, /v-if="topviewEnabled && a\.kind === 'location'"/)
  assert.match(detail, /function topviewCacheValid\(a\) \{ return !!\(a && a\.image && a\.topview_image && a\.topview_source_image === a\.image\) \}/)
  assert.match(api, /generateTopviewStructure: \(id, aid, force = false\) => http\.post\(`\/projects\/\$\{id\}\/assets\/location\/\$\{aid\}\/topview`, \{ force \}\)/)
})

test('结尾调度先出草稿，确认后才写入人物站位', () => {
  assert.match(detail, /AI推演结尾调度/)
  assert.match(detail, /确认写入人物站位/)
  assert.match(detail, /setBlockingDraft\(data\.blocking\)/)
  assert.match(api, /draftTopviewDispatch: \(id, sid\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/topview\/dispatch`/)
  assert.match(api, /applyTopviewDispatch: \(id, sid, dispatch\) => http\.put\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/topview\/dispatch`, dispatch\)/)
})
