import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  imageMaterialOptions,
  materialUrl,
  imagePlaneName,
  skyboxLabel,
  findImage,
  looksLikeImage,
} = await import('../src/studio/scene3d/background-library.ts')

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

const rows = [
  { id: 3, name: 'loc-22.png', path: 'materials/loc-22.png', type: 'image' },
  { id: 9, name: 'clay-seq.mp4', path: 'materials/clay-seq.mp4', type: 'video' },
  { id: 7, name: 'scene-1.png', path: 'materials/scene-1.png', type: 'image' },
  { id: 8, name: 'model.glb', path: 'materials/model.glb', type: '3d' },
]

test('只留图片：视频/3D 素材不会被当成纹理', () => {
  const options = imageMaterialOptions(rows)
  assert.deepEqual(options.map((item) => item.name), ['scene-1.png', 'loc-22.png'])
})

test('新存的排在前面，同名只留一张', () => {
  const options = imageMaterialOptions([
    { id: 1, name: 'a.png', path: 'materials/a.png', type: 'image' },
    { id: 5, name: 'b.png', path: 'materials/b.png', type: 'image' },
    { id: 2, name: 'a.png', path: 'materials/a-copy.png', type: 'image' },
  ])
  assert.deepEqual(options.map((item) => item.id), [5, 1])
})

test('包一层 items 也认；limit 截断保留最新的一批', () => {
  const payload = { items: rows }
  assert.equal(imageMaterialOptions(payload).length, 2)
  assert.deepEqual(imageMaterialOptions(payload, 1).map((item) => item.id), [7])
})

test('缺 name/path 的行被丢掉，不会生成空选项', () => {
  const options = imageMaterialOptions([
    { id: 1, name: '', path: 'materials/a.png', type: 'image' },
    { id: 2, name: 'b.png', path: '', type: 'image' },
    null,
    { id: 3, name: 'c.png', path: 'materials/c.png', type: 'image' },
  ])
  assert.deepEqual(options.map((item) => item.name), ['c.png'])
})

test('老后端不带 type 时按扩展名兜底', () => {
  assert.equal(looksLikeImage('materials/a.PNG'), true)
  assert.equal(looksLikeImage('materials/a.webp'), true)
  assert.equal(looksLikeImage('materials/a.glb'), false)
  const options = imageMaterialOptions([
    { id: 1, name: 'a.png', path: 'materials/a.png' },
    { id: 2, name: 'b.glb', path: 'materials/b.glb' },
  ])
  assert.deepEqual(options.map((item) => item.name), ['a.png'])
})

test('路径拼成 /api/input：只切第一个斜杠', () => {
  assert.equal(materialUrl('materials/a.png'), '/api/input/materials/a.png')
  assert.equal(materialUrl('/materials/a.png'), '/api/input/materials/a.png')
  assert.equal(materialUrl('a.png'), '/api/input/a.png')
  assert.equal(materialUrl('materials/sub/a.png'), '/api/input/materials/sub/a.png')
  assert.equal(materialUrl(''), '')
})

test('平面名带前缀、天空盒名保持原样，找不到选中值返回 null', () => {
  const image = { id: 4, name: 'loc-22.png', path: 'materials/loc-22.png' }
  assert.equal(imagePlaneName(image), 'planar-loc-22.png')
  assert.equal(skyboxLabel(image), 'loc-22.png')
  assert.equal(findImage([image], 4)?.name, 'loc-22.png')
  assert.equal(findImage([image], 0), null)
  assert.equal(findImage([image], 'nope'), null)
})

test('编辑器接线：素材库下拉 + 铺成图片平面 / 设为天空盒', () => {
  assert.match(stage, /imageMaterialOptions/)
  assert.match(stage, /\/api\/materials\?type=image/)
  assert.match(stage, /铺成图片平面/)
  assert.match(stage, /设为天空盒/)
  // 两条路都复用既有的加载函数，不另起一套纹理管线
  assert.match(stage, /addPlane\(url, imagePlaneName\(image\), "image"\)/)
  assert.match(stage, /applySkybox\(url, skyboxLabel\(image\)\)/)
})