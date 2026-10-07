import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const editor = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')

test('功能文字使用独立项目级CRUD而非Dialogue接口', () => {
  for (const name of ['screenTextCues:', 'createScreenTextCue:', 'updateScreenTextCue:', 'deleteScreenTextCue:']) assert.match(api, new RegExp(name))
  assert.match(api, /screen-text-cues/)
  assert.match(editor, /功能文字（非对白字幕）/)
  assert.match(editor, /不会进入Dialogue、配音或H3提示词/)
  assert.match(editor, /api\.createScreenTextCue/)
  assert.match(editor, /api\.updateScreenTextCue/)
  assert.match(editor, /api\.deleteScreenTextCue/)
})

test('功能文字轨支持中文横排和古装竖排且不使用Canvas', () => {
  assert.match(editor, /aria-label="功能文字时间轴"/)
  assert.match(editor, /value="horizontal-ltr">中文横排/)
  assert.match(editor, /value="vertical-rl">从上到下、列从右到左/)
  assert.match(editor, /writing-mode:vertical-rl/)
  assert.match(editor, /selectedScreenTextCues/)
  assert.doesNotMatch(editor, /<canvas/i)
  assert.match(api, /approvedFonts:/)
  assert.match(editor, /许可证与SHA-256校验/)
})

test('功能文字首版覆盖人物地点时间过场和本集完', () => {
  for (const value of ['character_intro', 'location', 'time_card', 'transition', 'end_card']) assert.match(editor, new RegExp(`value="${value}"`))
  assert.match(editor, /v-if="screenTextForm\.kind==='character_intro'"/)
  assert.match(editor, /v-model="screenTextForm\.character_id"/)
})
