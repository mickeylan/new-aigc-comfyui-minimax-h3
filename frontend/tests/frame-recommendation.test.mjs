import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('衔接帧候选标出清晰度推荐帧，但不替用户自动选中', () => {
  const source = readFileSync(new URL('../src/components/FrameSelector.vue', import.meta.url), 'utf8')
  assert.match(source, /recommended: f\.recommended/)
  assert.match(source, /<em v-if="f\.recommended" class="recommend-badge">推荐<\/em>/)
  assert.match(source, /仅供参考/)
  // Selecting writes to the backend, so extraction must still leave the choice empty.
  assert.match(source, /selectedOutput\.value = null\n    selectedFrameId\.value = 0/)
})
