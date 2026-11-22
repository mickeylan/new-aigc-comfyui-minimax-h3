import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  GROUND_EXTENT,
  clampGroundPoint,
  groundPathLength,
  groundPathPointAt,
  groundPathYaw,
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
  // 地面网格只有 20×20，点远处会打空：退回无限平面，否则用户会觉得点了没反应
  assert.match(stage, /: raycaster\.ray\.intersectPlane\(/)
  assert.match(stage, /if \(!onPlane\) return;/)
  assert.match(stage, /if \(moved > 4\) return;/)
  assert.match(stage, /const point = clampGroundPoint\(\[onPlane\.x, 0\.02, onPlane\.z\]\);/)
  // 落点标记不进参考图
  assert.match(stage, /hide\(groundPathGroupRef\.current\);/)
})

test('沿路径取点：两端精确、中间线性、越界钳到端点', () => {
  const path = { start: [0, 0.02, 0], end: [4, 0.02, 2] }
  assert.deepEqual(groundPathPointAt(path, 0), [0, 0.02, 0])
  assert.deepEqual(groundPathPointAt(path, 1), [4, 0.02, 2])
  assert.deepEqual(groundPathPointAt(path, 0.5), [2, 0.02, 1])
  // 进度是帧号除以时长算出来的，可能略越界或出现 NaN
  assert.deepEqual(groundPathPointAt(path, 1.7), [4, 0.02, 2])
  assert.deepEqual(groundPathPointAt(path, -0.2), [0, 0.02, 0])
  assert.deepEqual(groundPathPointAt(path, Number.NaN), [0, 0.02, 0])
})

test('行进朝向按 +Z 为 0 计算：走 +X 转 90°、走 -Z 转 180°', () => {
  assert.equal(groundPathYaw({ start: [0, 0, 0], end: [0, 0, 5] }), 0)
  assert.ok(Math.abs(groundPathYaw({ start: [0, 0, 0], end: [5, 0, 0] }) - Math.PI / 2) < 1e-9)
  assert.ok(Math.abs(Math.abs(groundPathYaw({ start: [0, 0, 0], end: [0, 0, -5] })) - Math.PI) < 1e-9)
})

test('序列总时长取相机路径与角色移动里较长的一条，帧数随之变', () => {
  // 相机 2 段各 1.6s = 3.2s；角色 5s → 按 5s 出帧
  assert.match(stage, /const seconds = Math\.max\(cameraPathDuration\(cameraKeys\), moverSeconds \?\? 0\);/)
  assert.match(stage, /frames: Math\.min\(SEQUENCE_MAX_FRAMES, Math\.max\(1, Math\.round\(seconds \* fps\)\)\),/)
  assert.match(stage, /const plan = sequencePlan\(cameraKeys, moverSeconds, fps\);/)
  // 移动时长有上下限，别让 0.01 秒或 1 小时把帧数算爆
  assert.match(stage, /const MOVE_SECONDS_LIMITS = \{ min: 0\.5, max: 30 \};/)
  assert.match(stage, /Math\.min\(MOVE_SECONDS_LIMITS\.max, Math\.max\(MOVE_SECONDS_LIMITS\.min, seconds\)\)/)
})

test('导出序列时逐帧推角色、结束还原，预览走同一个取样函数', () => {
  // 进度 = 帧时间 / 移动时长，超出停在 B
  assert.match(stage, /const progress = Math\.min\(1, index \/ fps \/ moverSeconds\);/)
  assert.match(stage, /mover\.object\.position\.set\(\.\.\.groundPathPointAt\(path, progress\)\);/)
  assert.match(stage, /mover\.object\.rotation\.y = groundPathYaw\(path\);/)
  // 导出后对象要回到原位，不能停在 B 点
  assert.match(stage, /mover\.object\.position\.copy\(savedMover\.position\);/)
  assert.match(stage, /mover\.object\.quaternion\.copy\(savedMover\.quaternion\);/)
  // 编辑器里的预览与导出共用取样，不另写一份插值
  assert.match(stage, /mover\.object\.position\.set\(\.\.\.groundPathPointAt\(path, progress\)\);/)
  assert.match(stage, /const \[playingMove, setPlayingMove\] = useState\(false\);/)
  assert.match(stage, /\{playingMove \? "移动中…" : "预览"\}/)
  // 无头环境改不动 <select>，所以另给一个「用左栏选中对象」的按钮，顺手也更好用
  assert.match(stage, /onClick=\{\(\) => setMoverId\(selectedId\)\}/)
  assert.match(stage, /用左栏选中的对象当移动目标/)
})

test('移动计划进出存档：按名字找对象（载入后 id 会变）', () => {
  assert.match(stage, /move\?: \{ targetName: string; seconds: number \} \| null;/)
  assert.match(stage, /const movePlan =[\s\S]{0,120}targetName: moverEntry\.name, seconds: normalizeMoveSeconds\(moveSeconds\)/)
  assert.match(stage, /const loadedMover = savedMove/)
  assert.match(stage, /objectsRef\.current\.find\(\(entry\) => entry\.name === savedMove\.targetName\) \?\? null/)
  assert.match(stage, /setMoverId\(loadedMover\?\.id \?\? null\);/)
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