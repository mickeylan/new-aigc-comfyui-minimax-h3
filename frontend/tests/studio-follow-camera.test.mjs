import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const { groundPathFollowDelta, groundPathPointAt } = await import(
  '../src/studio/scene3d/ground-path.ts'
)

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')
const path = { start: [1, 0.02, 2], end: [4, 0.02, 6] }

test('跟拍位移：起点为零、终点等于水平位移，y 恒为 0', () => {
  assert.deepEqual(groundPathFollowDelta(path, 0), [0, 0, 0])
  const end = groundPathFollowDelta(path, 1)
  assert.equal(end[0], 3)
  assert.equal(end[1], 0)
  assert.equal(end[2], 4)
  // 中间按比例；越界钳到端点（相机不能跟着飞出地面）
  assert.deepEqual(groundPathFollowDelta(path, 0.5), [1.5, 0, 2])
  assert.deepEqual(groundPathFollowDelta(path, 5), [3, 0, 4])
  assert.deepEqual(groundPathFollowDelta(path, -2), [0, 0, 0])
  // 位移就是「当前位置 − 起点」，跟 groundPathPointAt 保持一致
  const now = groundPathPointAt(path, 0.25)
  const start = groundPathPointAt(path, 0)
  assert.deepEqual(groundPathFollowDelta(path, 0.25), [now[0] - start[0], 0, now[2] - start[2]])
})

test('跟拍接进播放与序列导出：相机位置和目标一起平移', () => {
  // 状态与存档字段
  assert.match(stage, /const \[followMover, setFollowMover\] = useState\(false\);/)
  assert.match(stage, /follow: followMover/)
  assert.match(stage, /setFollowMover\(Boolean\(savedMove\?\.follow\)\)/)
  // 播放：角色在播放期间自己走 A→B，相机加同一个位移
  assert.match(stage, /const followPath = followMover \? toGroundPath\(groundPoints\) : null;/)
  assert.match(stage, /delta = groundPathFollowDelta\(followPath, progress\);/)
  assert.match(stage, /pose\.position\[0\] \+ delta\[0\],/)
  assert.match(stage, /pose\.target\[2\] \+ delta\[2\],/)
  // 播完把角色放回原处
  assert.match(stage, /followMoverEntry\.object\.position\.copy\(savedMover\.position\);/)
  // 序列导出：只在勾了跟拍时平移，并且平移完要刷新相机
  assert.match(stage, /if \(followMover\) \{/)
  assert.match(stage, /const delta = groundPathFollowDelta\(path, progress\);/)
  assert.match(stage, /camera\.position\.x \+ delta\[0\],/)
  // UI：勾选框
  assert.match(stage, /跟拍（相机跟着角色走）/)
})

test('载入场景不再踩暂时性死区：savedMove 先声明后使用', () => {
  const declare = stage.indexOf('const savedMove = doc.move')
  const use = stage.indexOf('const loadedMover = savedMove')
  assert.ok(declare > 0, '找不到 savedMove 声明')
  assert.ok(use > 0, '找不到 loadedMover 使用')
  assert.ok(declare < use, 'savedMove 必须在 loadedMover 之前声明，否则载入必抛 ReferenceError')
})