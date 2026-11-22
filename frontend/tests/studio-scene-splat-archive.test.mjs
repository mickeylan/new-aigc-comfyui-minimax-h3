import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

test('存档升到 v2，泼溅单独用 splats 字段内嵌字节', () => {
  assert.match(stage, /interface SplatDocument \{/)
  assert.match(stage, /data: string;/, '泼溅字节要 base64 内嵌')
  assert.match(stage, /version: 1 \| 2;/)
  assert.match(stage, /splats\?: SplatDocument\[\];/)
  assert.match(stage, /version: 2,/, '新存档写 v2')
})

test('保存时先把 .spz 字节取回来，取不到就整体失败', () => {
  assert.match(stage, /if \(!entry\.splatUrl\) throw new Error\(/, '没有来源地址要报错')
  assert.match(stage, /const response = await fetch\(entry\.splatUrl\);/)
  assert.match(stage, /data: arrayBufferToBase64\(await response\.arrayBuffer\(\)\)/)
  // 泼溅的变换记在 splats 里，不能混进 GLB 节点名单
  assert.match(stage, /\.filter\(\(entry\) => !entry\.splat\)/)
})

test('载入 v1 与 v2，泼溅按 blob URL 交给 Spark 重建并回填变换', () => {
  assert.match(stage, /if \(\(doc\.version !== 1 && doc\.version !== 2\)/, 'v1 老存档仍要能读')
  assert.match(stage, /for \(const saved of doc\.splats \?\? \[\]\)/)
  assert.match(stage, /new Blob\(\[base64ToArrayBuffer\(saved\.data\)\]/)
  assert.match(stage, /await loadSplatMesh\(url, \{ placeOnGround: false \}\)/)
  assert.match(stage, /group\.position\.fromArray\(saved\.position\);/)
  assert.match(stage, /splatUrl: url,/)
})

test('泼溅不进 GLB 导出，纯泼溅场景给出明确提示', () => {
  assert.match(stage, /if \(entry\.splat\) return;/, 'exportGeometry 要跳过泼溅')
  assert.match(stage, /场景里只有泼溅，导出不了 GLB；请先存成场景存档/)
  assert.match(stage, /场景里只有泼溅，造不了 GLB 素材；请在左侧对象列表里逐层「存库」，或先存成场景存档/)
})

test('载入新场景时释放旧泼溅的 GPU 缓冲', () => {
  assert.match(stage, /if \(entry\.splat\) disposeSplat\(entry\.splat\);/)
})