import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'

const detail = fs.readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const component = fs.readFileSync(new URL('../src/components/SceneFrameBrowser.vue', import.meta.url), 'utf8')
const api = fs.readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('每个已有视频的分镜卡片都可打开逐帧浏览器', () => {
  assert.match(detail, /查看全部帧/)
  assert.match(detail, /<SceneFrameBrowser/)
  assert.match(detail, /sc\.video_input_file \|\| \(sc\.video_file && sc\.video_gpu/)
  assert.match(detail, /frameBrowserScene/)
  assert.match(detail, /width:calc\(100vw - 16px\)/)
  assert.match(detail, /height:calc\(100vh - 16px\)/)
})

test('逐帧浏览器显示全部帧并将人工选择以PNG保存到项目磁盘', () => {
  assert.match(component, /v-for="frame in visibleFrames"/)
  assert.match(component, /selectedFrameIndex/)
  assert.match(component, /selected-frame-preview/)
  assert.match(component, /单独打开原图/)
  assert.match(component, /pageSize = 48/)
  assert.match(component, /grid-template-columns:repeat\(auto-fill,420px\)/)
  assert.match(component, /width:420px/)
  assert.match(component, /overflow-y:scroll/)
  assert.match(component, /scrollbar-gutter:stable/)
  assert.doesNotMatch(component, /aspect-ratio:16\/9/)
  assert.match(component, /api\.extractAllSceneFrames/)
  assert.match(component, /api\.saveExtractedSceneFrame/)
  assert.match(component, /保存所选帧到磁盘/)
  assert.match(component, /设为当前分镜图/)
  assert.match(component, /\?download=1/)
  assert.match(api, /scenes\/\$\{sid\}\/frames\/extract-all/)
  assert.match(api, /scenes\/\$\{sid\}\/frames\/save/)
  assert.match(api, /setAsSceneImage/)
})
