import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')

test('IndexTTS可选择ComfyUI集群或Rust本地且默认集群', () => {
  assert.match(settings, /IndexTTS 2\.5 · 执行方式/)
  assert.match(settings, /ComfyUI集群（默认）/)
  assert.match(settings, /Python IndexTTS ComfyUI工作流/)
  assert.match(settings, /Rust本地/)
  assert.match(settings, /form\.index_tts_execution_mode='comfyui'/)
  assert.match(settings, /form\.index_tts_execution_mode='native_local'/)
  assert.match(settings, /form\.index_tts_execution_mode='disabled'/)
  assert.match(settings, /不会在集群失败后静默回退/)
  assert.match(settings, /includes\(form\.value\.index_tts_execution_mode\)\) form\.value\.index_tts_execution_mode = 'comfyui'/)
  assert.match(settings, /保存IndexTTS执行方式/)
})
