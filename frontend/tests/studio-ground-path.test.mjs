import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  GROUND_EXTENT,
  clampGroundPoint,
  groundPathFollowDelta,
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
  // 朝向也按当前进度取切线（有途经点时是一条曲线，不能用固定朝向）
  assert.match(stage, /mover\.object\.rotation\.y = groundPathYaw\(path, progress\);/)
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
  assert.match(stage, /move\?: \{ targetName: string; seconds: number; follow\?: boolean \} \| null;/)
  assert.match(stage, /const movePlan =[\s\S]{0,160}targetName: moverEntry\.name, seconds: normalizeMoveSeconds\(moveSeconds\), follow: followMover/)
  assert.match(stage, /const loadedMover = savedMove/)
  assert.match(stage, /objectsRef\.current\.find\(\(entry\) => entry\.name === savedMove\.targetName\) \?\? null/)
  assert.match(stage, /setMoverId\(loadedMover\?\.id \?\? null\);/)
})

test('地面路径进出存档：写入可选字段、载入时校验、UI 能点选/清除', () => {
  // 存档字段仍是 v3 的可选字段（via 是有途经点时才写的可选子字段），老存档没有就清空
  assert.match(stage, /groundPath\?: \{ start: GroundPoint; end: GroundPoint; via\?: GroundPoint \} \| null;/)
  assert.match(stage, /groundPath: toGroundPath\(groundPoints\),/)
  assert.match(stage, /const savedPath = normalizeGroundPath\(doc\.groundPath\);/)
  assert.match(stage, /via: savedPath\?\.via \?\? null,/)
  // 只点一端时不算路径，UI 上也不显示距离
  assert.match(stage, /const path = toGroundPath\(groundPoints\);/)
  assert.match(stage, /path \? `\$\{groundPathLength\(path\)\.toFixed\(1\)\} m` : "起点 \/ 终点"/)
  // 两端标记 + 中间虚线
  assert.match(stage, /if \(groundPoints\.start\) ring\(groundPoints\.start, 0x22c55e\);/)
  assert.match(stage, /if \(groundPoints\.end\) ring\(groundPoints\.end, 0xf59e0b\);/)
  assert.match(stage, /new THREE\.LineDashedMaterial\(\{ color: 0x94a3b8, dashSize: 0\.18, gapSize: 0\.12 \}\)/)
})

// 2026-11-22 新增：途经点。直线走位绕不过桌子/柱子，而「角色沿路径移动」
// 恰好是 3D 参考片里最需要被复刻的东西，所以给 A→B 加一个可选途经点，走二次贝塞尔。
test('有途经点时走二次贝塞尔并穿过途经点：端点不动、中点就是途经点、长度变长', () => {
  const path = { start: [0, 0.02, 0], end: [4, 0.02, 0], via: [2, 0.02, 3] }
  assert.deepEqual(groundPathPointAt(path, 0), [0, 0.02, 0])
  assert.deepEqual(groundPathPointAt(path, 1), [4, 0.02, 0])
  // 控制点取 2V-(A+B)/2，所以 t=0.5 正好踩在途经点上（角色真的绕过障碍物，不是擦里侧）
  const mid = groundPathPointAt(path, 0.5)
  assert.ok(Math.abs(mid[0] - 2) < 1e-9, `中点 x 应等于途经点，实际 ${mid[0]}`)
  assert.ok(Math.abs(mid[2] - 3) < 1e-9, `中点 z 应等于途经点，实际 ${mid[2]}`)
  // 前后两半都朝同一侧偏，说明整条线是弧不是折线
  assert.ok(groundPathPointAt(path, 0.25)[2] > 1)
  assert.ok(groundPathPointAt(path, 0.75)[2] > 1)
  // 弧长大于直线距离 4（UI 上显示的长度不能把弯路说短）
  assert.ok(groundPathLength(path) > 5, `弧长应明显大于直线，实际 ${groundPathLength(path)}`)
})

test('途经点的朝向按曲线切线算，不是固定的 A→B 方向', () => {
  const path = { start: [0, 0.02, 0], end: [4, 0.02, 0], via: [0, 0.02, 3] }
  // 控制点 C = 2V-(A+B)/2 = (-2, 0, 6)；t=0 切线 = 2(C-A) = (-4, 0, 12)
  assert.ok(Math.abs(groundPathYaw(path, 0) - Math.atan2(-4, 12)) < 1e-9)
  // t=1 切线 = 2(B-C) = (12, 0, -12)
  assert.ok(Math.abs(groundPathYaw(path, 1) - Math.atan2(12, -12)) < 1e-9)
  // t=0.5 切线 = (C-A)+(B-C) = B-A → 正 +X
  assert.ok(Math.abs(groundPathYaw(path, 0.5) - Math.PI / 2) < 1e-9)
})

test('途经点与端点重合、或老存档没有 via，都退回直线（不产生零长度曲线）', () => {
  const base = { start: [0, 0.02, 0], end: [4, 0.02, 0] }
  assert.deepEqual(normalizeGroundPath({ ...base, via: [0, 0.02, 0] }), base)
  assert.deepEqual(normalizeGroundPath({ ...base, via: [4, 0.02, 0] }), base)
  assert.deepEqual(normalizeGroundPath({ ...base, via: null }), base)
  assert.deepEqual(normalizeGroundPath(base), base)
  // 跟拍位移仍只取水平分量：镜头不会跟着角色上下飘
  const path = toGroundPath({ start: [0, 0.5, 0], end: [4, 0.5, 0], via: [2, 0.5, 2] })
  assert.ok(path?.via, 'via 应该被保留')
  const delta = groundPathFollowDelta(path, 1)
  assert.equal(delta[1], 0)
  assert.ok(Math.abs(delta[0] - 4) < 1e-9)
  assert.ok(Math.abs(delta[2]) < 1e-9)
})

test('编辑器接上途经点：第三个落点按钮、蓝色标记、曲线采样、存档字段', () => {
  assert.match(stage, /\{\(\["start", "via", "end"\] as const\)\.map\(\(which\) => \(/)
  assert.match(stage, /which === "start" \? "点选 A" : which === "via" \? "途经" : "点选 B"/)
  assert.match(stage, /if \(groundPoints\.via\) ring\(groundPoints\.via, 0x38bdf8\);/)
  // 有途经点才按 32 段采样，直线仍是两点
  assert.match(stage, /const samples = path\.via \? 32 : 1;/)
  // 「走直线」把途经点清掉
  assert.match(stage, /onClick=\{\(\) => setGroundPoints\(\(current\) => \(\{ \.\.\.current, via: null \}\)\)\}/)
  // 落点 toast 区分 A / B / 途经点
  assert.match(stage, /\$\{placing === "start" \? "A" : placing === "end" \? "B" : "途经点"\} 已记录/)
  // 落点提示语同样区分三者（选了途经却提示「B」会让人以为点错按钮）
  assert.match(stage, /: "途经点（蓝，路径会穿过它）"\n/)
})