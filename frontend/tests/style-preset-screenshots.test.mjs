import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const editor = readFileSync(new URL('../src/components/ShotDirectorEditor.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')

test('screenshot style extraction sits next to the preset select and only saves on explicit click', () => {
  assert.match(editor, /@click="applyStyle\(shot\)">应用风格<\/button>\s*<button[^>]*@click="pickStyleScreenshots">从截图提取风格</)
  assert.match(api, /stylePresetFromScreenshots: \(projectId, files\) => \{/)
  assert.match(api, /createPreset: \(preset\) => http\.post\('\/presets', preset\)/)
  const extract = editor.slice(editor.indexOf('async function extractStyleFromScreenshots'), editor.indexOf('async function saveStyleShotDraft'))
  // The analysis only fills a draft; it never creates a preset or touches a Shot.
  assert.doesNotMatch(extract, /createPreset|applyStyle|applyPreset/)
  const save = editor.slice(editor.indexOf('async function saveStyleShotDraft'), editor.indexOf('function syncAuthoritativeSceneContent'))
  assert.match(save, /api\.createPreset\(styleShotDraft\.value\)/)
  assert.doesNotMatch(save, /applyStyle|applyPreset/)
})

test('the vision switch is an explicit settings checkbox that defaults off', () => {
  assert.match(settings, /v-model="form\.text_provider_vision" type="checkbox" true-value="true" false-value="false"/)
  assert.match(settings, /当前文本模型支持看图/)
})
