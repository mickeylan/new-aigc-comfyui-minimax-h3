import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  DEFAULT_ORBIT_SECONDS,
  ORBIT_PITCH_LIMITS,
  ORBIT_SEGMENTS_LIMITS,
  ORBIT_TURNS_LIMITS,
  normalizeOrbitPitch,
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
  assert.match(stage, /const \[orbitPitch, setOrbitPitch\] = useState\(DEFAULT_ORBIT_PITCH\);/)
  assert.match(stage, /const keys = orbitCameraKeys\(\{/)
  assert.match(stage, /segments: Math\.max\(ORBIT_SEGMENTS_LIMITS\.min, Math\.round\(orbitTurns \* 8\)\),/)
  assert.match(stage, /pitch: orbitPitch,/)
  // 追加而不是替换：环绕可以接在一段推拉后面
  assert.match(stage, /setCameraKeys\(\(current\) => \[\s*\n\s*\.\.\.current,/)
  assert.match(stage, /duration: normalizeCameraDuration\(key\.duration\),/)
  assert.match(stage, /easing: DEFAULT_CAMERA_EASING,/)
  // UI
  assert.match(stage, /title="绕几圈"/)
  assert.match(stage, /title="绕完用几秒"/)
  assert.match(stage, /title="收尾时机位抬高多少度（0=保持起幅高度；负数压低）。转着圈升空就是俯拍环绕"/)
  assert.match(stage, /绕左栏选中的对象转圈/)
})

// 2026-11-22 新增：环绕俯仰。以前只能平视绕圈，抬升/压低得手动机位；
// 现在 pitch 决定收尾仰角，机位沿圆弧走（到中心的距离不变，画面里主体大小不变）。
test('环绕俯仰：pitch=0 时与老的平视环绕逐点一致', () => {
  const flat = orbitCameraKeys({ center, start: [3, 2, 0], turns: 1, seconds: 4, segments: 8 })
  const withZero = orbitCameraKeys({ center, start: [3, 2, 0], turns: 1, seconds: 4, segments: 8, pitch: 0 })
  assert.deepEqual(withZero, flat)
})

test('环绕俯仰：抬升 30° 时收尾机位升高、到中心的距离不变、目标仍对着中心', () => {
  const start = [3, 2, 0]
  const keys = orbitCameraKeys({ center, start, turns: 0.25, seconds: 2, segments: 4, pitch: 30 })
  const distance = (p) => Math.hypot(p[0] - center[0], p[1] - center[1], p[2] - center[2])
  const startDistance = distance(start)
  assert.equal(keys.length, 5)
  // 起幅还是原机位
  assert.ok(Math.abs(keys[0].position[1] - 2) < 1e-9, '起幅高度不能变')
  // 收尾更高，且三维距离守恒（弧线走，不是直着拉高）
  assert.ok(keys[4].position[1] > keys[0].position[1] + 0.5, '收尾应明显更高')
  for (const key of keys) {
    assert.ok(Math.abs(distance(key.position) - startDistance) < 1e-6, '到中心的距离要守恒')
    assert.deepEqual(key.target, [0, 1, 0], '目标始终是中心')
  }
  // 单调上升：中间机位高度逐段变高
  for (let index = 1; index < keys.length; index += 1) {
    assert.ok(keys[index].position[1] > keys[index - 1].position[1], '高度应逐段上升')
  }
})

test('环绕俯仰：压低 50° 时收尾更低，水平半径变小（俯角越大越贴中心轴）', () => {
  const keys = orbitCameraKeys({ center, start: [3, 2, 0], turns: 0.25, seconds: 2, segments: 4, pitch: -50 })
  assert.ok(keys[4].position[1] < keys[0].position[1], '收尾应更低')
  const planar = (p) => Math.hypot(p[0] - center[0], p[2] - center[2])
  assert.ok(planar(keys[4].position) < planar(keys[0].position), '俯角更大 → 水平半径更小')
})

test('环绕俯仰：俯角减小时水平半径变大但不超过三维距离', () => {
  // 起幅仰角 18.4°，压低 20° 后仰角变到 ≈ -1.6°：|仰角| 变小 → 水平半径变大。
  const keys = orbitCameraKeys({ center, start: [3, 2, 0], turns: 0.25, seconds: 2, segments: 4, pitch: -20 })
  const planar = (p) => Math.hypot(p[0] - center[0], p[2] - center[2])
  const distance = Math.hypot(3, 1)
  assert.ok(keys[4].position[1] < keys[0].position[1], '收尾应更低')
  assert.ok(planar(keys[4].position) > planar(keys[0].position), '仰角趋零 → 水平半径趋近三维距离')
  assert.ok(planar(keys[4].position) <= distance + 1e-9)
})

test('环绕俯仰参数：上下限夹紧，非法值回 0；仰角本身夹在 ±85°', () => {
  assert.equal(normalizeOrbitPitch(999), ORBIT_PITCH_LIMITS.max)
  assert.equal(normalizeOrbitPitch(-999), ORBIT_PITCH_LIMITS.min)
  assert.equal(normalizeOrbitPitch('x'), 0)
  // 起幅仰角已经 18.4°，再加 60° 不会超过 85°
  const keys = orbitCameraKeys({ center, start: [3, 2, 0], turns: 0.25, segments: 4, pitch: 60 })
  const elevation = (Math.asin((keys[4].position[1] - center[1]) / Math.hypot(3, 1)) * 180) / Math.PI
  assert.ok(elevation <= 85 + 1e-6, `仰角不能越过 85°（实际 ${elevation}）`)
})