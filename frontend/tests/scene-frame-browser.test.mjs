import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'

const detail = fs.readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const component = fs.readFileSync(new URL('../src/components/SceneFrameBrowser.vue', import.meta.url), 'utf8')
const api = fs.readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('每个已生成视频的分镜卡片可打开逐帧浏览器', () => {
  assert.match(detail, /查看全部帧/)
  assert.match(detail, /<SceneFrameBrowser/)
  assert.match(detail, /frameBrowserScene/)
})

test('逐帧浏览器显示全部帧并将人工选择保存到项目磁盘', () => {
  assert.match(component, /v-for="frame in frames"/)
  assert.match(component, /selectedFrameIndex/)
  assert.match(component, /api\.extractAllSceneFrames/)
  assert.match(component, /api\.saveExtractedSceneFrame/)
  assert.match(component, /保存所选帧到磁盘/)
  assert.match(component, /\?download=1/)
  assert.match(api, /scenes\/\$\{sid\}\/frames\/extract-all/)
  assert.match(api, /scenes\/\$\{sid\}\/frames\/save/)
})
