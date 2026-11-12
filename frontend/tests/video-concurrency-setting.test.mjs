import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')

test('视频生成并发数上限跟随实例数而非写死8', () => {
  assert.doesNotMatch(settings, /id="video_concurrency"[^>]*max="8"/)
  assert.match(settings, /id="video_concurrency"[^>]*:max="instanceCount \|\| undefined"/)
  assert.match(settings, /const instanceCount = computed\(\(\) => store\.instances\.length\)/)
  assert.match(settings, /api\.instances\(\)/)
  assert.match(settings, /视频生成并发数不能超过实例数/)
})
