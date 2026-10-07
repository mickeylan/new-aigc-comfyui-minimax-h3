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
  assert.match(api, /screenTextPreflight:/)
  assert.match(editor, /许可证与SHA-256校验/)
  assert.match(editor, /最终渲染检查/)
  assert.match(editor, /正式导出将被阻止/)
  assert.match(editor, /api\.screenTextPreflight/)
  assert.match(editor, /v-model="mergeScreenText" \/>烧录功能文字/)
  assert.match(editor, /screen_text: mergeScreenText\.value/)
  assert.match(editor, /value="custom">自定义精确位置/)
  assert.match(editor, /@click="setScreenTextPosition"/)
  assert.match(editor, /screen-text-safe-zone/)
  assert.match(editor, /position_x/)
  assert.match(editor, /position_y/)
  assert.match(editor, /墨迹显字（当前以慢淡入渲染）/)
  assert.match(editor, /style-\$\{screenTextForm\.style_code\}/)
})

test('功能文字首版覆盖人物地点时间过场和本集完', () => {
  for (const value of ['character_intro', 'location', 'time_card', 'transition', 'end_card']) assert.match(editor, new RegExp(`value="${value}"`))
  assert.match(editor, /v-if="screenTextForm\.kind==='character_intro'"/)
  assert.match(editor, /v-model="screenTextForm\.character_id"/)
})
