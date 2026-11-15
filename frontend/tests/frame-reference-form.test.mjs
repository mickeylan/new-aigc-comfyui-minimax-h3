import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/components/FrameSelector.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('续接模式提供 原图/白模/线稿 参考形式，转换结果另存、原尾帧不变', () => {
  assert.match(source, /参考形式：/)
  assert.match(source, /<option value="original">原图<\/option>/)
  assert.match(source, /<option value="clay">白模<\/option>/)
  assert.match(source, /<option value="lineart">线稿<\/option>/)
  // The option only exists for continue mode; bridge mode always uses the original frame.
  assert.match(source, /v-if="mode === 'continue_from_previous' && selectedOutput"/)
  assert.match(source, /payload\.reference_form = mode\.value === 'continue_from_previous' \? referenceForm\.value : 'original'/)
  assert.match(source, /原尾帧保持不变/)
  assert.match(source, /不作为开始画面/)
})

test('转换进行中禁用按钮并轮询，卸载时停止轮询', () => {
  assert.match(source, /:disabled="converting \|\| !!selectedOutput\.convert_task_id"/)
  assert.match(source, /onUnmounted\(stopPolling\)/)
  assert.match(api, /convertFrameCandidate: \(id, sid, frameId, kind\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/continuity\/frames\/\$\{frameId\}\/convert`, \{ kind \}\)/)
})
