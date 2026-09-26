import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'

const source = fs.readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')

test('剪辑台提供Scene生产看板和只读双层时间轴', () => {
  assert.match(source, /PRODUCTION BOARD/)
  assert.match(source, /Scene是唯一生产单元/)
  assert.match(source, /场景\{\{selected\.order\}\}内部Shot/)
  assert.match(source, /continues_from_previous/)
  assert.match(source, /continues_to_next/)
  assert.doesNotMatch(source, /class="shot-clip"[^>]*draggable/)
  assert.match(source, /预览并重新配时/)
  assert.match(source, /shot\._start\.toFixed\(1\)/)
  assert.match(source, /本集正式生成前检查/)
  assert.match(source, /mismatchedScenes/)
  assert.match(source, /shot_duration_total/)
  assert.match(source, /未指定说话人/)
})

test('剪辑台展示连续性三图对比和正式Picture Subject顺序', () => {
  assert.match(source, /CONTINUITY COMPARE/)
  assert.match(source, /上一Scene确认尾帧/)
  assert.match(source, /当前Scene分镜图/)
  assert.match(source, /当前视频真实首帧/)
  assert.match(source, /不是成片实测首帧/)
  assert.match(source, /reference_bindings/)
  assert.match(source, /Picture \{\{ref\.picture\}\}/)
  assert.match(source, /Subject \{\{ref\.subject\}\}/)
  assert.match(source, /ref\.identity/)
  assert.match(source, /ref\.usage/)
  assert.match(source, /referenceShotScope/)
})

test('连续性选择器接收真实上一Scene上下文', () => {
  assert.match(source, /:source-scene-id="previousScene\?\.id \|\| 0"/)
  assert.match(source, /:source-scene-order="previousScene\?\.order \|\| 0"/)
  assert.match(source, /:source-video-ready="Boolean\(previousScene\?\.video_url \|\| previousScene\?\.video_file\)"/)
  assert.match(source, /:project-id="Number\(id\(\)\)"/)
})
