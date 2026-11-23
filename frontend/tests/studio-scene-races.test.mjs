import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')
const section = (from, to) => { const start = stage.indexOf(from); const end = stage.indexOf(to, start + from.length); assert.ok(start >= 0 && end > start); return stage.slice(start, end) }

test('PNG编码前同步还原临时材质、辅助物和相机，不跨await保留编辑器状态', () => {
  for (const [from, to, restore] of [
    ['const captureReference =', 'const captureViewportAt =', /item\.mesh\.material = item\.material/],
    ['const captureViewportAt =', 'const exportFirstLastFrames =', /camera\.position\.copy\(previous\.position\)/],
  ]) {
    const body = section(from, to)
    assert.match(body, /return new Promise<Blob \| null>/)
    assert.doesNotMatch(body, /await new Promise/)
    assert.match(body, /finally \{/); assert.match(body, restore)
  }
})
test('序列在每帧及异步编码后检查场景代次，切场景不回滚新相机', () => {
  const body = section('const exportSequence =', 'const saveReferenceToLibrary =')
  assert.match(body, /const epoch = sceneEpochRef\.current/)
  assert.match(body, /for \(let index[\s\S]*?assertCurrent\(\)/)
  assert.match(body, /await captureReference\(pass\);\s*assertCurrent\(\)/)
  assert.match(body, /await shot\.blob\.arrayBuffer\(\)[\s\S]*?assertCurrent\(\)/)
  assert.match(body, /finally \{[\s\S]*?if \(epoch === sceneEpochRef\.current\)/)
  assert.match(body, /resizeFrameRef\.current\(\)/)
  assert.match(body, /!canMoveEntry\(mover\) \|\| !objectsRef\.current\.includes\(mover\)/)
})
test('首尾帧在编码和上传返回后检查代次，单参考图拒绝迟到结果', () => {
  const pair = section('const exportFirstLastFrames =', 'const copyFirstLastNote =')
  assert.match(pair, /await captureViewportAt\(pose\);\s*assertCurrent\(\)/)
  assert.match(pair, /await response\.json\(\);\s*assertCurrent\(\)/)
  assert.match(section('const exportReference =', 'const exportSequence ='), /!shot \|\| epoch !== sceneEpochRef\.current/)
})
test('播放及移动预览拒绝旧场景与已删除目标，锁定隐藏后不再还原变换', () => {
  const playback = section('const playCameraPath =', 'const agentUndoRef =')
  assert.match(playback, /if \(epoch !== sceneEpochRef\.current\) \{ finish\(\); return; \}/)
  assert.match(playback, /savedMover && canMoveEntry\(followMoverEntry\) && objectsRef\.current\.includes/)
  assert.match(section('const previewMove =', 'const closeAiMenu ='), /!objectsRef\.current\.includes\(mover\) \|\| !canMoveEntry\(mover\)/)
})
test('泼溅复制及存档保留解码归一化的内层TRS，旧档仍可省略', () => {
  assert.match(stage, /contentTransform\?: PoseSnapshot/)
  assert.match(stage, /mesh\.position\.copy\(entry\.splat\.position\)/)
  assert.match(stage, /mesh\.quaternion\.copy\(entry\.splat\.quaternion\)/)
  assert.match(stage, /mesh\.scale\.copy\(entry\.splat\.scale\)/)
  assert.match(stage, /contentTransform: \{ position: entry\.splat\.position\.toArray\(\)/)
  assert.match(stage, /if \(saved\.contentTransform\) \{[\s\S]*?mesh\.scale\.fromArray\(saved\.contentTransform\.scale\)/)
})
test('成功恢复场景清除旧参考图及序列缓存，迟到复制不能复活已删除对象', () => {
  const restore = section('const importSceneText =', 'const loadScene =')
  assert.match(restore, /committed = true;\s*sequenceFramesRef\.current = null/)
  assert.match(restore, /referenceUrlRef\.current = null; setReferenceShot\(null\)/)
  assert.match(section('const duplicateSelected =', 'const captureCurrentView ='), /!objectsRef\.current\.includes\(entry\)/)
})
