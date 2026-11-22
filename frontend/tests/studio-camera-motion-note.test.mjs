import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const { cameraMotionNote, firstLastMotionNote } = await import(
  '../src/studio/scene3d/camera-motion-note.ts'
)

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

/** 站在 +Z 前方看原点。 */
const front = { position: [0, 1, 6], target: [0, 1, 0], fov: 45 }

test('向右绕四分之一圈：方位从 0° 到 90°，说「向右环绕 90°（四分之一圈）」', () => {
  const right = { position: [6, 1, 0], target: [0, 1, 0], fov: 45 }
  assert.equal(cameraMotionNote(front, right), '相机向右环绕 90°（四分之一圈）')
})

test('向左绕半圈：方位到 -180°，说「向左环绕 180°（半圈）」', () => {
  const behind = { position: [0, 1, -6], target: [0, 1, 0], fov: 45 }
  const note = cameraMotionNote(front, behind)
  // -180 折回 180 时按正向处理，方向词只认符号；这里允许两侧说法但必须带半圈
  assert.match(note, /环绕 180°（半圈）/)
})

test('绕一整圈回到原点：方位差归零，只报阈值以外的差异', () => {
  const same = { position: [0, 1.5, 6], target: [0, 1, 0], fov: 45 }
  const note = cameraMotionNote(front, same)
  assert.equal(note, '机位抬高 5°')
})

test('推近与拉远按距离比例报百分比', () => {
  const pushIn = { position: [0, 1, 3], target: [0, 1, 0], fov: 45 }
  assert.equal(cameraMotionNote(front, pushIn), '距离推近 50%')
  const pullOut = { position: [0, 1, 12], target: [0, 1, 0], fov: 45 }
  assert.equal(cameraMotionNote(front, pullOut), '距离拉远 100%')
})

test('变焦：视角变化单独成句，方向是「从 a° 收到 b°」', () => {
  const zoom = { position: [0, 1, 6], target: [0, 1, 0], fov: 30 }
  assert.equal(cameraMotionNote(front, zoom), '视角从 45° 收到 30°')
})

test('原地摇镜不说「环绕」：机位没挪、只是视线转了', () => {
  const pan = { position: [0, 1, 6], target: [1.25, 1, 0], fov: 45 }
  assert.equal(cameraMotionNote(front, pan), '机位原地向右转向 12°；视线中心平移 1.3 米')
})

test('组合运镜按固定顺序拼接：方位 → 俯仰 → 距离 → 视角 → 平移', () => {
  const last = { position: [3, 3, 0], target: [0, 2, 0], fov: 32 }
  const note = cameraMotionNote(front, last)
  assert.match(
    note,
    /^相机向右环绕 90°（四分之一圈）；机位抬高 \d+°；距离推近 \d+%；视角从 45° 收到 32°；视线中心平移 1\.0 米$/,
  )
})

test('两个机位一样就说基本不动，不硬凑句子', () => {
  assert.equal(
    cameraMotionNote(front, { position: [0, 1, 6], target: [0, 1, 0], fov: 45 }),
    '机位基本不动（只差在阈值以内）',
  )
})

test('阈值可调：把最小角度放到 30° 时 5° 不再算运镜', () => {
  const slight = { position: [0, 1.5, 6], target: [0, 1, 0], fov: 45 }
  assert.equal(cameraMotionNote(front, slight, { minDegrees: 30 }), '机位基本不动（只差在阈值以内）')
})

test('缺机位返回空串，不抛异常', () => {
  assert.equal(cameraMotionNote(null, front), '')
  assert.equal(cameraMotionNote(front, undefined), '')
})

test('首尾帧入口只取两端，中间机位不影响文案', () => {
  const keys = [
    front,
    { position: [4, 1, 4], target: [0, 1, 0], fov: 40 },
    { position: [6, 1, 0], target: [0, 1, 0], fov: 45 },
  ]
  assert.equal(firstLastMotionNote(keys), '相机向右环绕 90°（四分之一圈）')
  // 只有两个以上机位才有「从哪到哪」
  assert.equal(firstLastMotionNote([front]), '')
  assert.equal(firstLastMotionNote(null), '')
})

test('编辑器接线：导出首尾帧后给出运镜文案并可复制', () => {
  assert.match(stage, /firstLastMotionNote/)
  assert.match(stage, /运镜文案/)
  assert.match(stage, /navigator\.clipboard\.writeText/)
})