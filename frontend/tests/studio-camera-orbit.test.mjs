import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  DEFAULT_ORBIT_SECONDS,
  ORBIT_SEGMENTS_LIMITS,
  ORBIT_TURNS_LIMITS,
  normalizeOrbitSeconds,
  normalizeOrbitSegments,
  normalizeOrbitTurns,
  orbitCameraKeys,
} = await import('../src/studio/scene3d/camera-orbit.ts')

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')
const center = [0, 1, 0]

test('环绕机位：半径与高度保持，目标始终对着中心', () => {
  const keys = orbitCameraKeys({ center, start: [3, 2, 0], turns: 1, seconds: 4, segments: 8 })
  // 8 段 → 9 个机位（首尾各一个，最后一段时长不计）
  assert.equal(keys.length, 9)
  for (const key of keys) {
    assert.ok(Math.abs(Math.hypot(key.position[0], key.position[2]) - 3) < 1e-9, '半径要保住')
    assert.ok(Math.abs(key.position[1] - 2) < 1e-9, '高度要保住')
    assert.deepEqual(key.target, [0, 1, 0])
  }
  // 第一段起点就是原机位
  assert.ok(Math.abs(keys[0].position[0] - 3) < 1e-9)
  assert.ok(Math.abs(keys[0].position[2]) < 1e-9)
  // 一圈回到原点，但中间确实转过去了（X 走到 -3）
  assert.ok(Math.abs(keys[4].position[0] + 3) < 1e-9, '半圈应到对面')
  assert.ok(Math.abs(keys[8].position[0] - 3) < 1e-9)
  // 每段时长 = 总时长 / 段数
  assert.equal(keys[0].duration, 0.5)
})

test('环绕参数：圈数/段数/秒数都有上下限，非法值回默认', () => {
  assert.equal(normalizeOrbitTurns(99), ORBIT_TURNS_LIMITS.max)
  assert.equal(normalizeOrbitTurns(0), ORBIT_TURNS_LIMITS.min)
  assert.equal(normalizeOrbitTurns('x'), 1)
  assert.equal(normalizeOrbitSegments(1000), ORBIT_SEGMENTS_LIMITS.max)
  assert.equal(normalizeOrbitSegments(1), ORBIT_SEGMENTS_LIMITS.min)
  assert.equal(normalizeOrbitSegments(8.4), 8)
  assert.equal(normalizeOrbitSeconds(0), 0.5)
  assert.equal(normalizeOrbitSeconds('x'), DEFAULT_ORBIT_SECONDS)
  // 相机正好站在中心上：绕不了，返回空
  assert.deepEqual(orbitCameraKeys({ center, start: [0, 2, 0] }), [])
})

test('环绕接进编辑器：追加到机位序列，不改已有存档结构', () => {
  assert.match(stage, /const \[orbitTurns, setOrbitTurns\] = useState\(1\);/)
  assert.match(stage, /const \[orbitSeconds, setOrbitSeconds\] = useState\(DEFAULT_ORBIT_SECONDS\);/)
  assert.match(stage, /const keys = orbitCameraKeys\(\{/)
  assert.match(stage, /segments: Math\.max\(ORBIT_SEGMENTS_LIMITS\.min, Math\.round\(orbitTurns \* 8\)\),/)
  // 追加而不是替换：环绕可以接在一段推拉后面
  assert.match(stage, /setCameraKeys\(\(current\) => \[\s*\n\s*\.\.\.current,/)
  assert.match(stage, /duration: normalizeCameraDuration\(key\.duration\),/)
  assert.match(stage, /easing: DEFAULT_CAMERA_EASING,/)
  // UI
  assert.match(stage, /title="绕几圈"/)
  assert.match(stage, /title="绕完用几秒"/)
  assert.match(stage, /绕左栏选中的对象转圈/)
})