import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const { firstLastCameraFrames, cameraFrameFileName } = await import(
  '../src/studio/scene3d/camera-frames.ts'
)

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

const keys = [
  { id: 'a', position: [0, 2, 6], target: [0, 1, 0], duration: 1.6, easing: 'easeInOut', fov: 45 },
  { id: 'b', position: [4, 2, 4], target: [0, 1, 0], duration: 1.6, easing: 'easeInOut', fov: 45 },
  { id: 'c', position: [6, 3, 0], target: [0, 1, 0], duration: 1.6, easing: 'easeInOut', fov: 38 },
]

test('首尾帧取两端机位：起幅 = 第一个，落幅 = 最后一个，中间机位不参与', () => {
  const pair = firstLastCameraFrames(keys)
  assert.equal(pair?.first.id, 'a')
  assert.equal(pair?.last.id, 'c')
  // 只读不复制：调用方拿到的就是用户记下的那份机位
  assert.equal(pair?.first, keys[0])
  assert.equal(pair?.last, keys[2])
})

test('首尾帧：少于两个机位就没有「从哪到哪」，返回 null', () => {
  assert.equal(firstLastCameraFrames([]), null)
  assert.equal(firstLastCameraFrames([keys[0]]), null)
  assert.equal(firstLastCameraFrames(null), null)
  assert.equal(firstLastCameraFrames(undefined), null)
})

test('两个机位时起幅与落幅正好是它们自己', () => {
  const pair = firstLastCameraFrames([keys[0], keys[1]])
  assert.equal(pair?.first.id, 'a')
  assert.equal(pair?.last.id, 'b')
})

test('首尾帧文件名可区分起幅/落幅，空时间戳有兜底', () => {
  assert.equal(cameraFrameFileName('first', '20261122-2145'), 'camera-first-20261122-2145.png')
  assert.equal(cameraFrameFileName('last', '20261122-2145'), 'camera-last-20261122-2145.png')
  assert.equal(cameraFrameFileName('first', '  '), 'camera-first-frame.png')
})

test('编辑器接上首尾帧：按机位渲视口原样图、存素材库、给了模板提示', () => {
  // 按钮：不记录两个机位就点不动
  assert.match(stage, /onClick=\{exportFirstLastFrames\}/)
  assert.match(stage, /disabled=\{cameraKeys\.length < 2\}/)
  assert.match(stage, /导出首尾帧（存素材库）/)
  // 渲的是视口原样：不换材质、不拉黑背景，只藏编辑器痕迹
  assert.match(stage, /const captureViewportAt = useCallback\(async \(pose: MovableCamera\)/)
  assert.match(stage, /hide\(gridRef\.current\);/)
  assert.match(stage, /hide\(transformRef\.current\?\.getHelper\?\.\(\) \?\? null\);/)
  assert.equal(/captureViewportAt[\s\S]{0,2500}overrideMaterial/.test(stage), false)
  // 渲完把视角还回去，不会把用户眼前的相机留在落幅
  assert.match(stage, /camera\.position\.copy\(previous\.position\);/)
  assert.match(stage, /controls\.target\.copy\(previous\.target\);/)
  // 两帧都走 /api/materials（type=image），名字能分出起幅/落幅
  assert.match(stage, /form\.append\("file", shot\.blob, cameraFrameFileName\(edge, stamp\)\);/)
  assert.match(stage, /for \(const edge of \["first", "last"\] as const\)/)
  // 状态条要把「去视频页挂首尾帧 + 选 minimax_h3_first_last」说清楚
  assert.match(stage, /模板选 minimax_h3_first_last/)
  assert.match(stage, /先记录两个机位（起幅和落幅），再导首尾帧/)
})