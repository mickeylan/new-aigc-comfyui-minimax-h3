import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/components/FrameSelector.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')

test('续接参考形式提供 高清（VOSR2 超分），结果另存且仍是开始画面', () => {
  assert.match(source, /<option value="hd">高清（VOSR2 超分）<\/option>/)
  assert.match(source, /仍是本镜 0\.00 秒的开始画面/)
  assert.match(source, /请核对人脸和服装/)
  assert.match(source, /原尾帧保持不变/)
})

test('高清与白模线稿一样提交 ComfyUI 任务后轮询，不再有本地超分接口和引擎设置', () => {
  assert.match(source, /<button v-if="referenceForm !== 'original'"[^>]*:disabled="converting \|\| !!selectedOutput\.convert_task_id"[^>]*@click="convert"/)
  assert.match(source, /api\.convertFrameCandidate\(props\.projectId, props\.sourceSceneId, selectedOutput\.value\.id, referenceForm\.value\)/)
  assert.doesNotMatch(source, /upscale|Real-ESRGAN/)
  assert.doesNotMatch(api, /upscaleFrameCandidate/)
  assert.doesNotMatch(settings, /realesrgan|REALESRGAN/i)
})
