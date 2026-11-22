import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  GROUND_EXTENT,
  MAX_GROUND_VIAS,
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
  // 存档字段仍是 v3 的可选字段（vias 是有途经点时才写的可选子字段），老存档没有就清空
  assert.match(stage, /groundPath\?: \{ start: GroundPoint; end: GroundPoint; vias\?: GroundPoint\[\] \} \| null;/)
  assert.match(stage, /groundPath: toGroundPath\(groundPoints\),/)
  assert.match(stage, /const savedPath = normalizeGroundPath\(doc\.groundPath\);/)
  assert.match(stage, /vias: savedPath\?\.vias \?\? null,/)
  // 只点一端时不算路径，UI 上也不显示距离
  assert.match(stage, /const path = toGroundPath\(groundPoints\);/)
  assert.match(stage, /path \? `\$\{groundPathLength\(path\)\.toFixed\(1\)\} m` : "起点 \/ 终点"/)
  // 两端标记 + 中间虚线
  assert.match(stage, /if \(groundPoints\.start\) ring\(groundPoints\.start, 0x22c55e\);/)
  assert.match(stage, /if \(groundPoints\.end\) ring\(groundPoints\.end, 0xf59e0b\);/)
  assert.match(stage, /new THREE\.LineDashedMaterial\(\{ color: 0x94a3b8, dashSize: 0\.18, gapSize: 0\.12 \}\)/)
})

// 2026-11-22 新增：途经点（0–3 个）。直线走位绕不过桌子/柱子，而「角色沿路径移动」
// 恰好是 3D 参考片里最需要被复刻的东西。曲线用向心 Catmull-Rom，穿过每一个骨架点。
test('单个途经点：曲线穿过它、端点不动、长度变长、两侧同向偏', () => {
  const path = { start: [0, 0.02, 0], end: [4, 0.02, 0], vias: [[2, 0.02, 3]] }
  assert.deepEqual(groundPathPointAt(path, 0), [0, 0.02, 0])
  assert.deepEqual(groundPathPointAt(path, 1), [4, 0.02, 0])
  // 对称放置时途经点正好在 t=0.5 的参数位置（向心参数化按 距离^0.5 分段）
  const mid = groundPathPointAt(path, 0.5)
  assert.ok(Math.abs(mid[0] - 2) < 1e-9, `中点 x 应等于途经点，实际 ${mid[0]}`)
  assert.ok(Math.abs(mid[2] - 3) < 1e-9, `中点 z 应等于途经点，实际 ${mid[2]}`)
  // 前后两半都朝同一侧偏，说明整条线是弧不是折线
  assert.ok(groundPathPointAt(path, 0.25)[2] > 1)
  assert.ok(groundPathPointAt(path, 0.75)[2] > 1)
  // 弧长大于直线距离 4（UI 上显示的长度不能把弯路说短）
  assert.ok(groundPathLength(path) > 5, `弧长应明显大于直线，实际 ${groundPathLength(path)}`)
})

test('曲线穿过每一个途经点（最多 3 个），骨架点都落在曲线上', () => {
  const path = {
    start: [-4, 0.02, -4],
    end: [4, 0.02, 4],
    vias: [
      [-2, 0.02, 2],
      [0, 0.02, -2],
      [2, 0.02, 2],
    ],
  }
  const knots = [path.start, ...path.vias, path.end]
  const nearest = (knot) => {
    let best = Infinity
    for (let index = 0; index <= 4000; index += 1) {
      const at = groundPathPointAt(path, index / 4000)
      best = Math.min(best, Math.hypot(at[0] - knot[0], at[1] - knot[1], at[2] - knot[2]))
    }
    return best
  }
  // 采样步长 1/4000，最近距离只应剩采样误差（< 5e-3）
  for (const knot of knots) assert.ok(nearest(knot) < 5e-3, `骨架点 ${knot} 没落在曲线上`)
  // 弯曲的路径比 11.3 的直线长，但也不会绕成两三倍
  const length = groundPathLength(path)
  assert.ok(length > 11.3 && length < 26, `曲线长度不合理：${length}`)
  // 过冲受控：不超出骨架点包络 0.5 m（向心参数化的意义就在这，均匀参数化会甩出去）
  for (let index = 0; index <= 2000; index += 1) {
    const at = groundPathPointAt(path, index / 2000)
    assert.ok(Math.abs(at[0]) <= 4.5 && Math.abs(at[2]) <= 4.5, `曲线过冲太远：${at}`)
  }
})

test('途经点的朝向按曲线切线算：起步朝第一个途经点，不是固定的 A→B 方向', () => {
  const path = { start: [0, 0.02, 0], end: [4, 0.02, 0], vias: [[0, 0.02, 3]] }
  // 起点切线 = 指向第一个途经点 (0, 3) → yaw 0（差分数值求导，容差留 1e-2 ≈ 0.6°）
  assert.ok(Math.abs(groundPathYaw(path, 0)) < 1e-2, `起步应朝 +Z，实际 ${groundPathYaw(path, 0)}`)
  // 收尾切线 = 从途经点指向终点 (4, -3)
  assert.ok(Math.abs(groundPathYaw(path, 1) - Math.atan2(4, -3)) < 1e-2)
  // 对称单途经点：过途经点那一刻朝向正好是 +X
  const symmetric = { start: [0, 0.02, 0], end: [4, 0.02, 0], vias: [[2, 0.02, 3]] }
  assert.ok(Math.abs(groundPathYaw(symmetric, 0.5) - Math.PI / 2) < 1e-2)
})

test('老存档的单数 via 仍能读出来；跟端点重合的途经点被丢掉', () => {
  const base = { start: [0, 0.02, 0], end: [4, 0.02, 0] }
  // 老存档：单个 via → 归一成一个元素的 vias
  assert.deepEqual(normalizeGroundPath({ ...base, via: [2, 0.02, 3] }), {
    ...base,
    vias: [[2, 0.02, 3]],
  })
  // 与端点/前一个途经点重合的丢掉，非法项丢掉
  assert.deepEqual(normalizeGroundPath({ ...base, vias: [[0, 0.02, 0], [4, 0.02, 0]] }), base)
  assert.deepEqual(normalizeGroundPath({ ...base, vias: [['x', 0, 0]] }), base)
  assert.deepEqual(normalizeGroundPath({ ...base, vias: [] }), base)
  // 超过上限截断到 3 个
  const many = normalizeGroundPath({
    ...base,
    vias: [
      [0.5, 0.02, 1],
      [1.5, 0.02, 1],
      [2.5, 0.02, 1],
      [3.5, 0.02, 1],
    ],
  })
  assert.equal(many?.vias?.length, MAX_GROUND_VIAS)
  assert.equal(MAX_GROUND_VIAS, 3)
  // 跟拍位移仍只取水平分量：镜头不会跟着角色上下飘
  const path = toGroundPath({ start: [0, 0.5, 0], end: [4, 0.5, 0], vias: [[2, 0.5, 2]] })
  assert.ok(path?.vias?.length, 'vias 应该被保留')
  const delta = groundPathFollowDelta(path, 1)
  assert.equal(delta[1], 0)
  assert.ok(Math.abs(delta[0] - 4) < 1e-9)
  assert.ok(Math.abs(delta[2]) < 1e-9)
})

test('编辑器接上多途经点：追加落点、上限提示、撤销/清除、蓝色标记', () => {
  assert.match(stage, /\{\(\["start", "via", "end"\] as const\)\.map\(\(which\) => \(/)
  assert.match(stage, /which === "start" \? "点选 A" : which === "via" \? "途经" : "点选 B"/)
  // 途经点是追加，并且挡住第 4 个
  assert.match(stage, /if \(placing === "via"\) \{/)
  assert.match(stage, /if \(taken >= MAX_GROUND_VIAS\) \{/)
  assert.match(stage, /vias: \[\.\.\.\(current\.vias \?\? \[\]\), point\]\.slice\(0, MAX_GROUND_VIAS\),/)
  // 标记逐个画，路径按途经点数量加密采样
  assert.match(stage, /for \(const point of groundPoints\.vias \?\? \[\]\) ring\(point, 0x38bdf8\);/)
  assert.match(stage, /const samples = path\.vias\?\.length \? 32 \* path\.vias\.length : 1;/)
  // 撤销 = 去掉最后一个（并更新状态条）；清除途经 = 清空数组
  assert.match(stage, /vias: \(current\.vias \?\? \[\]\)\.slice\(0, -1\),/)
  assert.match(stage, /setStatus\(left > 0 \? `已撤掉最后一个途经点，还剩 \$\{left\} 个` : "已撤掉全部途经点，走直线 A→B"\);/)
  assert.match(stage, /onClick=\{\(\) => \{\n\s+setGroundPoints\(\(current\) => \(\{ \.\.\.current, vias: \[\] \}\)\);\n\s+setStatus\("已去掉全部途经点，走直线 A→B"\);/)
  // 落点 toast 与提示语区分 A / B / 第 N 个途经点
  assert.match(stage, /`途经点 \$\{Math\.min\(taken \+ 1, MAX_GROUND_VIAS\)\} 已记录/)
  assert.match(stage, /`第 \$\{\(groundPoints\.vias\?\.length \?\? 0\) \+ 1\} 个途经点（蓝，路径会穿过它）`/)
})