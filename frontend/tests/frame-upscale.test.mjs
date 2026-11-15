import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/components/FrameSelector.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const settings = readFileSync(new URL('../src/views/Settings.vue', import.meta.url), 'utf8')

test('续接参考形式提供 高清（本地超分），结果另存且仍是开始画面', () => {
  assert.match(source, /<option value="hd">高清（本地超分）<\/option>/)
  assert.match(source, /v-if="referenceForm === 'hd'"[^>]*:disabled="upscaling"[^>]*@click="upscale"/)
  assert.match(source, /仍是本镜 0\.00 秒的开始画面/)
  assert.match(source, /原尾帧保持不变/)
  assert.match(source, /api\.upscaleFrameCandidate\(props\.projectId, props\.sourceSceneId, selectedOutput\.value\.id\)/)
})

test('超分接口与设置里的引擎路径', () => {
  assert.match(api, /upscaleFrameCandidate: \(id, sid, frameId\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/continuity\/frames\/\$\{frameId\}\/upscale`, \{\}, \{ timeout: 330000 \}\)/)
  assert.match(settings, /v-model="form\.realesrgan_path"/)
  assert.match(settings, /REALESRGAN_PATH/)
})
