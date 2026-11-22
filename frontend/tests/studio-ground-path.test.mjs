import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  GROUND_EXTENT,
  clampGroundPoint,
  groundPathLength,
  normalizeGroundPath,
  normalizeGroundPoint,
  toGroundPath,
} = await import('../src/studio/scene3d/ground-path.ts')

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

test('落点钳在地面范围内，y 保持不变', () => {
  assert.deepEqual(clampGroundPoint([12, 0.02, -13]), [GROUND_EXTENT, 0.02, -GROUND_EXTENT])
  assert.deepEqual(clampGroundPoint([1.5, 0.02, -2.5]), [1.5, 0.02, -2.5])
  // 地面尺寸变化时可以由调用方传更小的范围
  assert.deepEqual(clampGroundPoint([5, 0, 5], 2), [2, 0, 2])
})

test('存档里的落点逐个校验：非数字/长度不对/NaN 都不收', () => {
  assert.deepEqual(normalizeGroundPoint([1, 2, 3]), [1, 2, 3])
  assert.equal(normalizeGroundPoint([1, 2]), null)
  assert.equal(normalizeGroundPoint([1, 2, 3, 4]), null)
  assert.equal(normalizeGroundPoint(['1', 2, 3]), null)
  assert.equal(normalizeGroundPoint([Number.NaN, 2, 3]), null)
  assert.equal(normalizeGroundPoint(null), null)
})

test('两端都有才算路径；两端重合（双击同一个点）不算', () => {
  const start = [0, 0.02, 0]
  const end = [3, 0.02, 4]
  assert.deepEqual(normalizeGroundPath({ start, end }), { start, end })
  assert.equal(normalizeGroundPath({ start }), null)
  assert.equal(normalizeGroundPath({ start, end: [0, 0.02, 0] }), null)
  assert.equal(normalizeGroundPath('start,end'), null)
  assert.equal(normalizeGroundPath(null), null)
})

test('两点间距按三维距离算，只有一端时没有路径', () => {
  // 3-4-0 直角三角形的斜边
  assert.equal(groundPathLength({ start: [0, 0, 0], end: [3, 4, 0] }), 5)
  assert.equal(toGroundPath({ start: [0, 0, 0], end: null }), null)
  assert.deepEqual(toGroundPath({ start: [0, 0, 0], end: [0, 0, 2] }), {
    start: [0, 0, 0],
    end: [0, 0, 2],
  })
})

test('编辑器：只对地面拾取、拖拽超过阈值不落点、参考图导出时藏起标记', () => {
  // 选点期间点击走射线；只对地面做 intersectObject，点模型不该变成落点
  assert.match(stage, /const hit = raycaster\.intersectObject\(ground, false\)\[0\];/)
  assert.match(stage, /if \(moved > 4\) return;/)
  assert.match(stage, /const point = clampGroundPoint\(\[hit\.point\.x, 0\.02, hit\.point\.z\]\);/)
  // 落点标记不进参考图
  assert.match(stage, /hide\(groundPathGroupRef\.current\);/)
})

test('地面路径进出存档：写入可选字段、载入时校验、UI 能点选/清除', () => {
  // 存档字段仍是 v3 的可选字段，老存档没有就清空
  assert.match(stage, /groundPath\?: \{ start: GroundPoint; end: GroundPoint \} \| null;/)
  assert.match(stage, /groundPath: toGroundPath\(groundPoints\),/)
  assert.match(stage, /const savedPath = normalizeGroundPath\(doc\.groundPath\);/)
  assert.match(stage, /setGroundPoints\(\{ start: savedPath\?\.start \?\? null, end: savedPath\?\.end \?\? null \}\);/)
  // 只点一端时不算路径，UI 上也不显示距离
  assert.match(stage, /const path = toGroundPath\(groundPoints\);/)
  assert.match(stage, /path \? `\$\{groundPathLength\(path\)\.toFixed\(1\)\} m` : "起点 \/ 终点"/)
  // 两端标记 + 中间虚线
  assert.match(stage, /if \(groundPoints\.start\) ring\(groundPoints\.start, 0x22c55e\);/)
  assert.match(stage, /if \(groundPoints\.end\) ring\(groundPoints\.end, 0xf59e0b\);/)
  assert.match(stage, /new THREE\.LineDashedMaterial\(\{ color: 0x94a3b8, dashSize: 0\.18, gapSize: 0\.12 \}\)/)
})