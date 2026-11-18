import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/components/FrameSelector.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('续接模式提供 原图/白模/线稿 参考形式，转换结果另存、原尾帧不变', () => {
  assert.match(source, /参考形式：/)
  assert.match(source, /<option value="original">原图<\/option>/)
  assert.match(source, /<option v-if="mode === 'continue_from_previous'" value="clay">白模<\/option>/)
  assert.match(source, /<option v-if="mode === 'continue_from_previous'" value="lineart">线稿<\/option>/)
  // Clay/lineart remain continue-only; HD is available in every generation mode.
  assert.match(source, /<section v-if="sourceSceneId" class="block">/)
  assert.match(source, /<div v-if="selectedOutput" class="form-block">/)
  assert.match(source, /payload\.reference_form = referenceForm\.value/)
  assert.match(source, /watch\(mode/)
  assert.match(source, /\['clay', 'lineart'\]\.includes\(referenceForm\.value\)/)
  assert.match(source, /原尾帧保持不变/)
  assert.match(source, /不作为开始画面/)
})

test('转换进行中禁用按钮并轮询，卸载时停止轮询', () => {
  assert.match(source, /:disabled="converting \|\| !!selectedOutput\.convert_task_id"/)
  assert.match(source, /onUnmounted\(stopPolling\)/)
  assert.match(api, /convertFrameCandidate: \(id, sid, frameId, kind\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/continuity\/frames\/\$\{frameId\}\/convert`, \{ kind \}\)/)
})
