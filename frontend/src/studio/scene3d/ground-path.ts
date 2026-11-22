/**
 * 地面路径：视频生成要的「角色从 B 点走到 A 点」得先在地面上落下两个点。
 *
 * 这里只放纯数据与拾取点的钳制逻辑（能被 node --test 直接跑），
 * three 的射线拾取留在编辑器里。
 */

export type GroundPoint = [number, number, number];

/** 途经点上限：再多分支曲线会自交，面板也放不下。 */
export const MAX_GROUND_VIAS = 3;

export interface GroundPath {
  start: GroundPoint;
  end: GroundPoint;
  /**
   * 途经点（0–3 个，必被曲线穿过）：绕桌子、绕柱子、从左侧进画再从右侧出画。
   * 存成数组而不是单个点：真实走位经常是「绕过桌子再避开柱子」这种两三个拐点。
   */
  vias?: GroundPoint[];
}

export interface GroundPoints {
  start: GroundPoint | null;
  end: GroundPoint | null;
  vias?: GroundPoint[] | null;
}

/** 地面是 20×20 的平面，留半米余量，免得点落在网格外悬空。 */
export const GROUND_EXTENT = 9.5;

const isFiniteTriple = (value: unknown): value is GroundPoint =>
  Array.isArray(value) &&
  value.length === 3 &&
  value.every((item) => typeof item === "number" && Number.isFinite(item));

export function normalizeGroundPoint(value: unknown): GroundPoint | null {
  if (!isFiniteTriple(value)) return null;
  return [value[0], value[1], value[2]];
}

/**
 * 两点都在、且没有重合才算一条可走的路径。
 *
 * 途经点逐个校验：非法项丢掉，跟上一点或端点重合的也丢掉（否则出现零长度段，
 * 曲线上会出现尖点）；最后按 MAX_GROUND_VIAS 截断。
 * 老存档里的单数 via 也认，读出来并成数组。
 */
export function normalizeGroundPath(value: unknown): GroundPath | null {
  if (!value || typeof value !== "object") return null;
  const source = value as { start?: unknown; end?: unknown; via?: unknown; vias?: unknown };
  const start = normalizeGroundPoint(source.start);
  const end = normalizeGroundPoint(source.end);
  if (!start || !end) return null;
  if (pointDistance(start, end) < 1e-4) return null;
  const raw = Array.isArray(source.vias) ? source.vias : [source.via];
  const vias: GroundPoint[] = [];
  let previous = start;
  for (const item of raw) {
    if (vias.length >= MAX_GROUND_VIAS) break;
    const point = normalizeGroundPoint(item);
    if (!point) continue;
    if (pointDistance(point, previous) < 1e-3) continue;
    if (pointDistance(point, end) < 1e-3) continue;
    vias.push(point);
    previous = point;
  }
  return vias.length > 0 ? { start, end, vias } : { start, end };
}

function pointDistance(a: GroundPoint, b: GroundPoint): number {
  return Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
}

/**
 * 拖动微调时「动的是哪一格」：起点、终点，或第几个途经点。
 *
 * 用带标签的对象而不是裸字符串 + 下标：路过点越界时能原样返回，不会把终点点到起点上。
 */
export type GroundSlot = { kind: "start" } | { kind: "end" } | { kind: "via"; index: number };

/** 落在网格外太远的点会飘在空地上，直接钳到地面范围内。 */
export function clampGroundPoint(point: GroundPoint, extent = GROUND_EXTENT): GroundPoint {
  const limit = Math.max(0, extent);
  const clamp = (value: number) => Math.min(limit, Math.max(-limit, value));
  return [clamp(point[0]), point[1], clamp(point[2])];
}

/**
 * 拖动微调：把 slot 指的那一格换成新落点，别的点一个都不动。
 *
 * 纯函数、不修改入参：React state 里能安全地拿旧值算新值。
 * 途经点下标越界（比如刚撤销了一个还在拖）时原样返回旧值，不抛错、也不误改端点。
 */
export function moveGroundPoint(
  points: GroundPoints,
  slot: GroundSlot,
  point: GroundPoint,
): GroundPoints {
  const next = clampGroundPoint(point);
  if (slot.kind === "start") return { ...points, start: next };
  if (slot.kind === "end") return { ...points, end: next };
  const vias = points.vias ?? [];
  if (!Number.isInteger(slot.index) || slot.index < 0 || slot.index >= vias.length) return points;
  return { ...points, vias: vias.map((item, index) => (index === slot.index ? next : item)) };
}

/** 路径的骨架点：起点、途经点、终点。 */
function groundPathKnots(path: GroundPath): GroundPoint[] {
  return [path.start, ...(path.vias ?? []), path.end];
}

export function groundPathLength(path: GroundPath): number {
  const knots = groundPathKnots(path);
  if (knots.length === 2) return pointDistance(knots[0], knots[1]);
  // 曲线按每段 24 折线量长度：面板上显示「7.3 m」这种长度，直线距离会把弯路说短。
  const samples = 24 * (knots.length - 1);
  let total = 0;
  let previous = groundPathPointAt(path, 0);
  for (let index = 1; index <= samples; index += 1) {
    const next = groundPathPointAt(path, index / samples);
    total += pointDistance(previous, next);
    previous = next;
  }
  return total;
}

/**
 * 向心 Catmull-Rom（alpha = 0.5）：曲线严格穿过每个骨架点，拐角自动抹圆。
 *
 * 为什么不是「每个途经点一段二次贝塞尔」：那样两段在途经点处切线不连续，
 * 角色走到拐点会突然转头；也不直接把途经点当贝塞尔控制点 —— 控制点只把曲线
 * 拉过去一半，角色会从障碍物里侧擦过去，与「绕过去」的用途正好相反。
 * 向心参数化（按 距离^0.5 分段）比均匀参数化过冲小得多，急转弯不会甩出去。
 */
function catmullRom(
  p0: GroundPoint,
  p1: GroundPoint,
  p2: GroundPoint,
  p3: GroundPoint,
  local: number,
): GroundPoint {
  const alpha = 0.5;
  const d0 = Math.pow(Math.max(pointDistance(p0, p1), 1e-6), alpha);
  const d1 = Math.pow(Math.max(pointDistance(p1, p2), 1e-6), alpha);
  const d2 = Math.pow(Math.max(pointDistance(p2, p3), 1e-6), alpha);
  const t0 = 0;
  const t1 = t0 + d0;
  const t2 = t1 + d1;
  const t3 = t2 + d2;
  const u = t1 + (t2 - t1) * local;
  const mix = (a: GroundPoint, b: GroundPoint, ta: number, tb: number): GroundPoint => {
    const weight = (u - ta) / (tb - ta);
    return [
      a[0] + (b[0] - a[0]) * weight,
      a[1] + (b[1] - a[1]) * weight,
      a[2] + (b[2] - a[2]) * weight,
    ];
  };
  const a1 = mix(p0, p1, t0, t1);
  const a2 = mix(p1, p2, t1, t2);
  const a3 = mix(p2, p3, t2, t3);
  const b1 = mix(a1, a2, t0, t2);
  const b2 = mix(a2, a3, t1, t3);
  return mix(b1, b2, t1, t2);
}

/** 每段在 0..1 里占多长：按 距离^0.5 分配，跟向心参数化的分段一致。 */
function groundPathSegmentWeights(knots: GroundPoint[]): number[] {
  return knots.slice(0, -1).map((knot, index) => {
    return Math.pow(Math.max(pointDistance(knot, knots[index + 1]), 1e-6), 0.5);
  });
}

/** 沿路径取位置：t 为 0..1 的比例，超出范围钳到端点。有途经点时走样条并穿过每一个。 */
export function groundPathPointAt(path: GroundPath, t: number): GroundPoint {
  const ratio = Math.min(1, Math.max(0, Number.isFinite(t) ? t : 0));
  const knots = groundPathKnots(path);
  if (knots.length === 2) {
    const [start, end] = knots;
    return [
      start[0] + (end[0] - start[0]) * ratio,
      start[1] + (end[1] - start[1]) * ratio,
      start[2] + (end[2] - start[2]) * ratio,
    ];
  }
  const weights = groundPathSegmentWeights(knots);
  const total = weights.reduce((sum, weight) => sum + weight, 0);
  let cursor = 0;
  let segment = 0;
  let localRatio = 0;
  for (let index = 0; index < weights.length; index += 1) {
    const span = weights[index] / total;
    segment = index;
    if (ratio <= cursor + span || index === weights.length - 1) {
      localRatio = span > 0 ? Math.min(1, (ratio - cursor) / span) : 0;
      break;
    }
    cursor += span;
  }
  // 端点处用「镜像虚拟邻点」（P0 = 2·P1 - P2）而不是复制端点：
  // 这样起步切线正好指向第一个途经点（复制端点会让起点朝向偏几度）。
  const p0 =
    segment > 0
      ? knots[segment - 1]
      : ([
          2 * knots[0][0] - knots[1][0],
          2 * knots[0][1] - knots[1][1],
          2 * knots[0][2] - knots[1][2],
        ] as GroundPoint);
  const p1 = knots[segment];
  const p2 = knots[segment + 1];
  const p3 =
    segment + 2 <= knots.length - 1
      ? knots[segment + 2]
      : ([
          2 * knots[knots.length - 1][0] - knots[knots.length - 2][0],
          2 * knots[knots.length - 1][1] - knots[knots.length - 2][1],
          2 * knots[knots.length - 1][2] - knots[knots.length - 2][2],
        ] as GroundPoint);
  return catmullRom(p0, p1, p2, p3, Math.min(1, Math.max(0, localRatio)));
}

/**
 * 行进朝向：模型默认面朝 +Z，yaw = atan2(dx, dz)。
 * 有途经点时按曲线切线算（用前后各取一步的差分数值求导），拐弯处不会瞬间转头。
 */
export function groundPathYaw(path: GroundPath, t = 1): number {
  if (!path.vias || path.vias.length === 0) {
    return Math.atan2(path.end[0] - path.start[0], path.end[2] - path.start[2]);
  }
  const ratio = Math.min(1, Math.max(0, Number.isFinite(t) ? t : 0));
  const step = 1e-3;
  const before = groundPathPointAt(path, Math.max(0, ratio - step));
  const after = groundPathPointAt(path, Math.min(1, ratio + step));
  const dx = after[0] - before[0];
  const dz = after[2] - before[2];
  // 两端差分可能都落在同一个点上（极短段），退回 A→B 的方向，别返回 NaN。
  if (Math.hypot(dx, dz) < 1e-9) {
    return Math.atan2(path.end[0] - path.start[0], path.end[2] - path.start[2]);
  }
  return Math.atan2(dx, dz);
}

/**
 * 曲线避障：盒子是模型在 XZ 平面上的包围盒（A 世界坐标的 min/max 两角）。
 * 路径撞上就自动插两个点从盒角外侧绕过去。
 *
 * 为什么不让用户手点：模型一挪，手工绕的点就废了；自动绕只需要知道「谁挡路」，
 * 而且自动插入的点不进存档（存档里只留用户点的 A/B/途经点）。
 */
export interface GroundObstacle {
  /** [x, z] 的最小角 */
  min: [number, number];
  /** [x, z] 的最大角 */
  max: [number, number];
}

/** 障碍盒外扩多少米：贴着模型边缘走会蹭到裙摆/披风，留半米更稳。 */
export const GROUND_OBSTACLE_MARGIN = 0.35;
/** 最多插多少个自动点：再多说明场景本身挤成一团，硬绕也不好看了。 */
const MAX_ROUTE_DETOURS = 12;

interface RouteOptions {
  margin?: number;
  maxDetours?: number;
}

function normalizeObstacle(value: GroundObstacle, margin: number): GroundObstacle | null {
  const min = value?.min;
  const max = value?.max;
  if (!Array.isArray(min) || !Array.isArray(max)) return null;
  if (min.length !== 2 || max.length !== 2) return null;
  if (![...min, ...max].every((item) => typeof item === "number" && Number.isFinite(item))) return null;
  const lo: [number, number] = [Math.min(min[0], max[0]) - margin, Math.min(min[1], max[1]) - margin];
  const hi: [number, number] = [Math.max(min[0], max[0]) + margin, Math.max(min[1], max[1]) + margin];
  // 退化成一个点/一条线的盒子绕不开，直接丢掉。
  if (hi[0] - lo[0] < 1e-6 || hi[1] - lo[1] < 1e-6) return null;
  return { min: lo, max: hi };
}

function boxContainsPoint(box: GroundObstacle, point: GroundPoint): boolean {
  return (
    point[0] >= box.min[0] &&
    point[0] <= box.max[0] &&
    point[2] >= box.min[1] &&
    point[2] <= box.max[1]
  );
}

/**
 * 2D 线段与盒子内部是否相交（slab 法）。盒子先内缩 1e-6：
 * 贴着盒角走到拐点是允许的（那是绕行本身），只有穿进内部才算撞上。
 */
function segmentHitsBox(a: GroundPoint, b: GroundPoint, box: GroundObstacle): boolean {
  const shrink = 1e-6;
  let t0 = 0;
  let t1 = 1;
  const origin = [a[0], a[2]];
  const delta = [b[0] - a[0], b[2] - a[2]];
  const bounds: Array<[number, number]> = [
    [box.min[0] + shrink, box.max[0] - shrink],
    [box.min[1] + shrink, box.max[1] - shrink],
  ];
  for (let axis = 0; axis < 2; axis += 1) {
    const o = origin[axis];
    const d = delta[axis];
    const [lo, hi] = bounds[axis];
    if (Math.abs(d) < 1e-9) {
      if (o < lo || o > hi) return false;
      continue;
    }
    let near = (lo - o) / d;
    let far = (hi - o) / d;
    if (near > far) [near, far] = [far, near];
    t0 = Math.max(t0, near);
    t1 = Math.min(t1, far);
    if (t0 > t1) return false;
  }
  return true;
}

function boxCorners(box: GroundObstacle): GroundPoint[] {
  return [
    [box.min[0], 0.02, box.min[1]],
    [box.max[0], 0.02, box.min[1]],
    [box.min[0], 0.02, box.max[1]],
    [box.max[0], 0.02, box.max[1]],
  ];
}

function flatDistance(a: GroundPoint, b: GroundPoint): number {
  return Math.hypot(a[0] - b[0], a[2] - b[2]);
}

/**
 * 绕一个盒子：四条边各是一对拐点（同一条边上的两个角），挑总长最短且三段都不穿盒子的那组。
 *
 * 用「整条边」而不是单个角：只走一个角时，角到终点那段常常又切回盒子里，
 * 走完整条边（贴着盒边外侧）才真正绕得过去。
 */
function detourAroundBox(a: GroundPoint, b: GroundPoint, box: GroundObstacle): GroundPoint[] | null {
  const [c00, c10, c01, c11] = boxCorners(box);
  const sides: Array<[GroundPoint, GroundPoint]> = [
    [c00, c10],
    [c01, c11],
    [c00, c01],
    [c10, c11],
  ];
  let best: { points: GroundPoint[]; cost: number } | null = null;
  for (const [first, second] of sides) {
    for (const pair of [
      [first, second],
      [second, first],
    ] as Array<[GroundPoint, GroundPoint]>) {
      const [p1, p2] = pair;
      if (segmentHitsBox(a, p1, box)) continue;
      if (segmentHitsBox(p1, p2, box)) continue;
      if (segmentHitsBox(p2, b, box)) continue;
      const cost = flatDistance(a, p1) + flatDistance(p1, p2) + flatDistance(p2, b);
      // 同长时按坐标定序，保证同输入永远给出同一条路线。
      if (
        !best ||
        cost < best.cost - 1e-9 ||
        (Math.abs(cost - best.cost) <= 1e-9 && comparePoints([p1, p2], best.points) < 0)
      ) {
        best = { points: [p1, p2], cost };
      }
    }
  }
  return best ? best.points : null;
}

function comparePoints(a: GroundPoint[], b: GroundPoint[]): number {
  for (let index = 0; index < Math.min(a.length, b.length); index += 1) {
    const diff = a[index][0] - b[index][0] || a[index][2] - b[index][2];
    if (Math.abs(diff) > 1e-9) return diff;
  }
  return a.length - b.length;
}

/**
 * 让路径绕开障碍：原来的骨架点不动，撞到盒子的那一段换成「过盒边两个拐点」。
 *
 * 起点/终点落在盒子里的障碍直接忽略 —— 那通常就是角色自己站的位置，
 * 绕开它等于把角色从自己身上挪走。
 */
export function routeAroundObstacles(
  path: GroundPath,
  obstacles: GroundObstacle[] | null | undefined,
  options: RouteOptions = {},
): GroundPath {
  const margin = Math.max(0, Number(options.margin ?? GROUND_OBSTACLE_MARGIN) || 0);
  const maxDetours = Math.max(0, Math.floor(Number(options.maxDetours ?? MAX_ROUTE_DETOURS) || 0));
  const boxes = (obstacles ?? [])
    .map((item) => normalizeObstacle(item, margin))
    .filter((item): item is GroundObstacle => !!item)
    .filter((box) => !boxContainsPoint(box, path.start) && !boxContainsPoint(box, path.end));
  if (boxes.length === 0) return path;

  const knots = [path.start, ...(path.vias ?? []), path.end];
  const route: GroundPoint[] = [path.start];
  let detours = 0;
  for (let index = 0; index < knots.length - 1; index += 1) {
    const target = knots[index + 1];
    // 一段可能连穿几个盒子：绕着走直到这一段不再撞任何盒子为止。
    for (let guard = 0; guard < MAX_ROUTE_DETOURS; guard += 1) {
      const from = route[route.length - 1];
      if (detours >= maxDetours) break;
      const box = boxes.find((item) => segmentHitsBox(from, target, item));
      if (!box) break;
      const detour = detourAroundBox(from, target, box);
      if (!detour) break;
      for (const point of detour) {
        const clamped = clampGroundPoint(point);
        const last = route[route.length - 1];
        if (pointDistance(clamped, last) < 1e-3) continue;
        if (pointDistance(clamped, target) < 1e-3) continue;
        route.push(clamped);
        detours += 1;
      }
    }
    const clampedTarget = clampGroundPoint(target);
    const last = route[route.length - 1];
    if (pointDistance(clampedTarget, last) >= 1e-4) route.push(clampedTarget);
  }
  const inner = route.slice(1, -1);
  return inner.length > 0 ? { start: route[0], end: route[route.length - 1], vias: inner } : path;
}

/** 从零散落点收成一条路径：只落了一个点时返回 null。 */
export function toGroundPath(points: GroundPoints): GroundPath | null {
  if (!points.start || !points.end) return null;
  return normalizeGroundPath({
    start: points.start,
    end: points.end,
    vias: points.vias ?? [],
  });
}

/**
 * 跟拍位移：角色走到 t（0..1）时相对起点的水平位移。
 *
 * 相机把自己的位置与目标整体加上这个位移，构图不变、只是跟着角色走；
 * 只取水平分量，免得角色起点比地面高时镜头跟着往上飘。
 */
export function groundPathFollowDelta(path: GroundPath, t: number): GroundPoint {
  const now = groundPathPointAt(path, t);
  const start = groundPathPointAt(path, 0);
  return [now[0] - start[0], 0, now[2] - start[2]];
}