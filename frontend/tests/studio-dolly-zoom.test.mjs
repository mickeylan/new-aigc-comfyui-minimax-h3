import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  DEFAULT_DOLLY_SECONDS,
  DOLLY_FOV_LIMITS,
  DOLLY_RANGE_LIMITS,
  dollyZoomKeys,
  normalizeDollyRange,
  normalizeDollySeconds,
  normalizeDollySegments,
} = await import('../src/studio/scene3d/dolly-zoom.ts')

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')
const target = [0, 1, 0]
const start = [0, 1, 10]

const distance = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2])
// 目标在画面上的相对高度 ∝ 距离 × tan(fov/2)；dolly zoom 要让它保持不变。
const subjectScale = (key) => distance(key.position, key.target) * Math.tan((key.fov * Math.PI) / 360)

test('希区柯克：推进时视角变广，目标大小保持不变', () => {
  const keys = dollyZoomKeys({ start, target, fov: 45, range: 0.35, seconds: 3, segments: 6 })
  assert.equal(keys.length, 7, 'segments + 1 个机位')
  // 距离从 10 收敛到 3.5 —— 但 45° 推到 3.5 需要 99.6° 视角，超过 90° 上限，
  // 所以距离被夹到「90° 对应的 4.14」而不是硬推。
  const minPush = (10 * Math.tan((45 * Math.PI) / 360)) / Math.tan((90 * Math.PI) / 360)
  assert.ok(Math.abs(distance(keys[0].position, target) - 10) < 1e-9)
  assert.ok(Math.abs(distance(keys[6].position, target) - minPush) < 1e-9)
  assert.ok(Math.abs(minPush - 4.142) < 1e-3)
  // 相机沿同一条直线推进（x 不变），target 不动
  for (const key of keys) {
    assert.ok(Math.abs(key.position[0]) < 1e-9)
    assert.deepEqual(key.target, [0, 1, 0])
  }
  // 目标大小恒定（fov 保留一位小数，允许 0.5% 误差）
  const base = subjectScale(keys[0])
  for (const key of keys) {
    assert.ok(
      Math.abs(subjectScale(key) / base - 1) < 0.005,
      `第 ${key.fov}° 时目标大小变了`,
    )
  }
  // 视角一位小数，机位行里好看
  for (const key of keys) assert.equal(key.fov, Math.round(key.fov * 10) / 10)
  // 视角变广（45° → 约 88°，但不超过 90° 上限）
  assert.ok(keys[6].fov > keys[0].fov)
  assert.ok(keys[6].fov <= DOLLY_FOV_LIMITS.max)
  // 每段时长 = 总时长 / 段数
  assert.equal(keys[0].duration, 0.5)
})

test('希区柯克参数：范围/段数/秒数上下限，退化情形返回空', () => {
  assert.equal(normalizeDollyRange(5), DOLLY_RANGE_LIMITS.max)
  assert.equal(normalizeDollyRange(0), DOLLY_RANGE_LIMITS.min)
  assert.equal(normalizeDollyRange('x'), 0.35)
  assert.equal(normalizeDollySegments(99), 16)
  assert.equal(normalizeDollySegments(1), 2)
  assert.equal(normalizeDollySeconds(0), 0.5)
  assert.equal(normalizeDollySeconds('x'), DEFAULT_DOLLY_SECONDS)
  // 后退（range > 1）时视角变窄
  const back = dollyZoomKeys({ start, target, fov: 45, range: 2, seconds: 3, segments: 4 })
  assert.ok(back[4].fov < back[0].fov)
  assert.ok(back[4].fov >= DOLLY_FOV_LIMITS.min)
  // 相机贴在目标上：没有推进可言
  assert.deepEqual(dollyZoomKeys({ start: [0, 1, 0], target, fov: 45 }), [])
})

test('希区柯克接进编辑器：追加机位并带上 fov', () => {
  assert.match(stage, /const \[dollyRange, setDollyRange\] = useState\(0\.35\);/)
  assert.match(stage, /const \[dollySeconds, setDollySeconds\] = useState\(DEFAULT_DOLLY_SECONDS\);/)
  assert.match(stage, /const keys = dollyZoomKeys\(\{/)
  assert.match(stage, /fov: normalizeCameraFov\(key\.fov\),/)
  assert.match(stage, /已追加希区柯克 \$\{keys\.length\} 个机位/)
  // UI
  assert.match(stage, /推进到/)
  assert.match(stage, /对着左栏选中的对象推进并反向变焦/)
})