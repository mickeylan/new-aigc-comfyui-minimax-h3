/**
 * 地面路径：视频生成要的「角色从 B 点走到 A 点」得先在地面上落下两个点。
 *
 * 这里只放纯数据与拾取点的钳制逻辑（能被 node --test 直接跑），
 * three 的射线拾取留在编辑器里。
 *
 * 同一文件还存放**场景环境**（时间 / 天气 / 地形）的纯计算：地形一启用，
 * 「地面」就不再是固定的 20×20 平面，落点、走位可行性与渲染光照都必须读同一份
 * 高度场与同一套太阳参数，否则面板说的和画面看到的会不一致。因此环境采样与
 * 地面采样放在一起，并由编辑器把它同时喂给视口、截图、序列与参考图取样。
 */

import type { SceneTerrain } from "./toonflow/sceneDocument";
import { validateTerrainPaint } from "./toonflow/terrainPaint.ts";

/**
 * 地表编号上限（Toonflow 的 terrainSurfaceMaxId = terrainSurfaceOptions.length − 1，当前 58）。
 *
 * 不直接 import 那个模块：本文件必须能被 `node --test` 直接跑（见文件头），
 * 而 toonflow/terrainModel 是一条带 three/fflate 的依赖链。这里硬编码上限，
 * 由 environment.spec.ts 对照真实模块断言两者相等——调色板一扩就会红。
 */
const TERRAIN_SURFACE_MAX_ID = 58;

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
  /** 可选诊断名，不用于推断碰撞语义。 */
  name?: string;
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
  return routeAroundObstaclesDetailed(path, obstacles, options).path;
}

export interface RouteResult {
  path: GroundPath;
  /** 这次真的插了自动绕行点。 */
  detoured: boolean;
  /** 绕行预算（maxDetours）用完，还有一段会直穿障碍——调用方该提示用户。 */
  exhausted: boolean;
}

/**
 * 同 routeAroundObstacles，但把「绕行预算是否用完」也带出来。
 * 之前用尽预算时只 break，路就这样直穿模型过去，界面上看不出任何异常。
 */
export function routeAroundObstaclesDetailed(
  path: GroundPath,
  obstacles: GroundObstacle[] | null | undefined,
  options: RouteOptions = {},
): RouteResult {
  const margin = Math.max(0, Number(options.margin ?? GROUND_OBSTACLE_MARGIN) || 0);
  const maxDetours = Math.max(0, Math.floor(Number(options.maxDetours ?? MAX_ROUTE_DETOURS) || 0));
  const boxes = (obstacles ?? [])
    .map((item) => normalizeObstacle(item, margin))
    .filter((item): item is GroundObstacle => !!item)
    .filter((box) => !boxContainsPoint(box, path.start) && !boxContainsPoint(box, path.end));
  if (boxes.length === 0) return { path, detoured: false, exhausted: false };

  const knots = [path.start, ...(path.vias ?? []), path.end];
  const route: GroundPoint[] = [path.start];
  let detours = 0;
  let exhausted = false;
  for (let index = 0; index < knots.length - 1; index += 1) {
    const target = knots[index + 1];
    // 一段可能连穿几个盒子：绕着走直到这一段不再撞任何盒子为止。
    for (let guard = 0; guard < MAX_ROUTE_DETOURS; guard += 1) {
      const from = route[route.length - 1];
      const box = boxes.find((item) => segmentHitsBox(from, target, item));
      if (!box) break;
      // 预算用完但还有盒子挡住：这一段只能直穿，交给调用方提醒用户。
      if (detours >= maxDetours) {
        exhausted = true;
        break;
      }
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
  const result = inner.length > 0 ? { start: route[0], end: route[route.length - 1], vias: inner } : path;
  return { path: result, detoured: detours > 0, exhausted };
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

// ---------------------------------------------------------------------------
// 场景环境：时间 / 天气 / 地形
//
// 版本化文档成员（version 1）。旧存档没有这个字段，读出为 null，行为与以前完全一致
// （20×20 平地、灯光预设）；一旦启用，太阳方向、雾、天空底色与地面高度都以这里为准。
// 所有函数都是纯函数：同一份环境在视口、截图、序列与参考图取样里得到同一结果，
// 不使用 Math.random、不读系统时间。
// ---------------------------------------------------------------------------

export const WEATHER_CONDITIONS = ["clear", "cloudy", "rain", "snow", "fog"] as const;
export type WeatherCondition = (typeof WEATHER_CONDITIONS)[number];

export interface SceneWeatherState {
  condition: WeatherCondition;
  /** 0–1，同一 preset 的强弱（雨量、雾浓度、云量）。 */
  intensity: number;
  /** 0–1，控制雨雪水平漂移；不改变光照。 */
  wind: number;
}

export interface SceneEnvironment {
  version: 1;
  /** 一天中的小时（0–24，允许小数）。不做自动走时：同一个值永远画同一张图。 */
  hour: number;
  /** 纬度（-66–66）。春分近似下太阳轨迹只由纬度与小时决定。 */
  latitude: number;
  /** 场景北向相对 -Z 的偏移（度），只旋转太阳的水平方位。 */
  northOffset: number;
  weather: SceneWeatherState;
  /** 高度场地形；null = 平地，沿用原有的 20×20 地面与 GROUND_EXTENT。 */
  terrain: SceneTerrain | null;
}

export const TERRAIN_LIMITS = {
  minSize: 20,
  maxSize: 1024,
  minSegments: 64,
  maxSegments: 256,
  minHeight: -12,
  maxHeight: 40,
  maxSeed: 2147483647,
} as const;

/** 默认环境：正午、晴天、平地。地形单独由 `createEnvironmentTerrain` 打开。 */
export function defaultSceneEnvironment(): SceneEnvironment {
  return {
    version: 1,
    hour: 12,
    latitude: 30,
    northOffset: 0,
    weather: { condition: "clear", intensity: 0.5, wind: 0.2 },
    terrain: null,
  };
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

function rejectUnknownKeys(value: Record<string, unknown>, allowed: readonly string[], label: string) {
  for (const key of Object.keys(value)) {
    if (!allowed.includes(key)) throw new Error(`${label}包含未支持的字段「${key}」`);
  }
}

function finiteIn(value: unknown, min: number, max: number, label: string): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value < min || value > max) {
    throw new Error(`${label}须为 ${min}–${max} 的有限数`);
  }
  return value;
}

function integerIn(value: unknown, min: number, max: number, label: string): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < min || value > max) {
    throw new Error(`${label}须为 ${min}–${max} 的整数`);
  }
  return value;
}

const TERRAIN_KEYS = ["size", "segments", "heights", "surfaces", "seed", "waterLevels", "surfaceBlend", "surfaceColors", "waterColors", "surfacePaint"] as const;

/** 只接受编辑器实际会渲染的高度场字段：多带字段就拒绝，避免存了看不见的语义。 */
function validateSceneTerrain(value: unknown): SceneTerrain {
  if (!isRecord(value)) throw new Error("地形不是对象");
  rejectUnknownKeys(value, TERRAIN_KEYS, "地形");
  const size = finiteIn(value.size, TERRAIN_LIMITS.minSize, TERRAIN_LIMITS.maxSize, "地形边长");
  const segments = integerIn(value.segments, TERRAIN_LIMITS.minSegments, TERRAIN_LIMITS.maxSegments, "地形分段数");
  const count = (segments + 1) ** 2;
  const heights = value.heights;
  const surfaces = value.surfaces;
  if (!Array.isArray(heights) || heights.length !== count) throw new Error(`地形高度数组长度须为 ${count}`);
  if (!Array.isArray(surfaces) || surfaces.length !== count) throw new Error(`地形地表数组长度须为 ${count}`);
  const checkedHeights = heights.map((height, index) =>
    finiteIn(height, TERRAIN_LIMITS.minHeight, TERRAIN_LIMITS.maxHeight, `地形高度[${index}]`));
  const checkedSurfaces = surfaces.map((surface, index) =>
    integerIn(surface, 0, TERRAIN_SURFACE_MAX_ID, `地形地表[${index}]`));
  const seed = integerIn(value.seed, 0, TERRAIN_LIMITS.maxSeed, "地形种子");
  let waterLevels: (number | null)[] | undefined;
  if (value.waterLevels !== undefined) {
    if (!Array.isArray(value.waterLevels) || value.waterLevels.length !== count) throw new Error(`水面数组长度须为 ${count}`);
    waterLevels = value.waterLevels.map((level, index) => {
      if (level === null) return null;
      const checked = finiteIn(level, TERRAIN_LIMITS.minHeight, TERRAIN_LIMITS.maxHeight, `水面高度[${index}]`);
      if (checkedSurfaces[index] !== 2 || checked <= checkedHeights[index] + 0.01) throw new Error(`水面[${index}]须在河床上方且地表为水体`);
      return checked;
    });
  }
  let surfaceBlend: SceneTerrain['surfaceBlend'];
  if (value.surfaceBlend !== undefined) {
    const blend = value.surfaceBlend;
    if (!isRecord(blend)) throw new Error('地表混合不是对象');
    rejectUnknownKeys(blend, ['surfaces', 'weights'], '地表混合');
    if (!Array.isArray(blend.surfaces) || blend.surfaces.length !== count || !Array.isArray(blend.weights) || blend.weights.length !== count) throw new Error(`地表混合数组长度须为 ${count}`);
    surfaceBlend = {
      surfaces: blend.surfaces.map((id, index) => integerIn(id, 0, TERRAIN_SURFACE_MAX_ID, `混合地表[${index}]`)),
      weights: blend.weights.map((weight, index) => finiteIn(weight, 0, 1, `混合权重[${index}]`)),
    };
  }
  const colors = (raw: unknown, water: boolean): Record<string, string> | undefined => {
    if (raw === undefined) return undefined;
    const label = water ? '水色' : '地表色';
    if (!isRecord(raw)) throw new Error(`${label}不是对象`);
    const result: Record<string, string> = {};
    for (const [key, color] of Object.entries(raw)) {
      if (!/^(0|[1-9][0-9]*)$/.test(key) || !Number.isSafeInteger(Number(key)) || Number(key) > (water ? count - 1 : TERRAIN_SURFACE_MAX_ID) || (!water && Number(key) === 38)) throw new Error(`${label}索引无效`);
      if (typeof color !== 'string' || !/^#[0-9a-f]{6}$/i.test(color)) throw new Error(`${label}须为六位十六进制颜色`);
      if (water && typeof waterLevels?.[Number(key)] !== 'number') throw new Error('水色只能覆盖有水顶点');
      result[key] = color;
    }
    return result;
  };
  let surfacePaint: SceneTerrain['surfacePaint'];
  if (value.surfacePaint !== undefined) {
    const paint = value.surfacePaint;
    if (!isRecord(paint)) throw new Error('地表绘图不是对象');
    rejectUnknownKeys(paint, ['segments', 'data', 'styles'], '地表绘图');
    if (![256, 512, 1024].includes(paint.segments as number) || typeof paint.data !== 'string') throw new Error('地表绘图分辨率或数据无效');
    if (paint.styles !== undefined) {
      if (!Array.isArray(paint.styles) || paint.styles.length < 1 || paint.styles.length > 256) throw new Error('地表样式数量无效');
      for (const style of paint.styles) {
        if (!isRecord(style)) throw new Error('地表样式不是对象');
        rejectUnknownKeys(style, ['surface', 'color'], '地表样式');
        integerIn(style.surface, 0, TERRAIN_SURFACE_MAX_ID, '样式材质');
        if (typeof style.color !== 'string' || !/^#[0-9a-f]{6}$/i.test(style.color)) throw new Error('样式颜色无效');
      }
    }
    surfacePaint = { segments: paint.segments as number, data: paint.data, ...(paint.styles ? { styles: (paint.styles as NonNullable<SceneTerrain['surfacePaint']>['styles'])!.map(style => ({ ...style })) } : {}) };
    const error = validateTerrainPaint(surfacePaint);
    if (error) throw new Error(error);
  }
  const surfaceColors = colors(value.surfaceColors, false), waterColors = colors(value.waterColors, true);
  return { size, segments, heights: checkedHeights, surfaces: checkedSurfaces, seed, ...(waterLevels ? { waterLevels } : {}), ...(surfaceBlend ? { surfaceBlend } : {}), ...(surfaceColors ? { surfaceColors } : {}), ...(waterColors ? { waterColors } : {}), ...(surfacePaint ? { surfacePaint } : {}) };
}

/**
 * 校验一份环境文档。未知版本、未知字段、越界数值一律显式报错，
 * 不做默认补齐；返回值是深拷贝，调用方之后改对象不会污染已校验的数据。
 */
export function validateSceneEnvironment(value: unknown): SceneEnvironment {
  if (!isRecord(value)) throw new Error("环境不是对象");
  rejectUnknownKeys(value, ["version", "hour", "latitude", "northOffset", "weather", "terrain"], "环境");
  if (value.version !== 1) throw new Error("不支持的环境版本");
  const hour = finiteIn(value.hour, 0, 24, "环境小时");
  const latitude = finiteIn(value.latitude, -66, 66, "环境纬度");
  const northOffset = finiteIn(value.northOffset, -180, 180, "环境北向偏移");
  if (!isRecord(value.weather)) throw new Error("天气不是对象");
  rejectUnknownKeys(value.weather, ["condition", "intensity", "wind"], "天气");
  const condition = value.weather.condition;
  if (typeof condition !== "string" || !(WEATHER_CONDITIONS as readonly string[]).includes(condition)) {
    throw new Error("天气类型无效");
  }
  const weather: SceneWeatherState = {
    condition: condition as WeatherCondition,
    intensity: finiteIn(value.weather.intensity, 0, 1, "天气强度"),
    wind: finiteIn(value.weather.wind, 0, 1, "风力"),
  };
  const terrain = value.terrain === null || value.terrain === undefined ? null : validateSceneTerrain(value.terrain);
  return { version: 1, hour, latitude, northOffset, weather, terrain };
}

const DEG = Math.PI / 180;
const clampUnit = (value: number) => Math.min(1, Math.max(0, value));
const mix = (from: number, to: number, ratio: number) => Math.round(from + (to - from) * clampUnit(ratio));
const hex = (value: number) => `#${value.toString(16).padStart(6, "0")}`;

function mixHex(from: string, to: string, ratio: number): string {
  const a = parseInt(from.slice(1), 16);
  const b = parseInt(to.slice(1), 16);
  const channel = (shift: number) => mix((a >> shift) & 0xff, (b >> shift) & 0xff, ratio) << shift;
  return hex(channel(16) | channel(8) | channel(0));
}

export interface EnvironmentLight {
  /** 单位向量，方向是「从场景指向太阳」；three 的平行光位置直接乘距离即可。 */
  sunDirection: [number, number, number];
  sunElevation: number;
  sunAzimuth: number;
  sunColor: string;
  sunIntensity: number;
  ambientColor: string;
  ambientIntensity: number;
  skyColor: string;
  /** 半球光地面色：晴天是土色，夜里趋近黑。 */
  groundColor: string;
  background: string;
  fogColor: string;
  /** THREE.FogExp2 的密度；0 表示无雾。 */
  fogDensity: number;
  /** 0–1：太阳在地平线以上的程度，供界面显示「白天/黄昏/夜」。 */
  daylight: number;
}

const WEATHER_SUN_DIM: Record<WeatherCondition, number> = {
  clear: 1, cloudy: 0.62, rain: 0.4, snow: 0.5, fog: 0.3,
};
const WEATHER_FOG: Record<WeatherCondition, number> = {
  clear: 0.0006, cloudy: 0.004, rain: 0.011, snow: 0.008, fog: 0.03,
};

/**
 * 由时间/纬度/天气推出确定性光照与天空色。
 *
 * 天文口径：春分近似（太阳赤纬 0），只由纬度与小时决定高度与方位；
 * 不含黄赤交角、均时差与大气折射。用于画面氛围与可复现取样，不作天文数据。
 */
export function environmentLight(env: SceneEnvironment): EnvironmentLight {
  const latitude = env.latitude * DEG;
  const hourAngle = (env.hour - 12) * 15 * DEG;
  const sinElevation = Math.cos(latitude) * Math.cos(hourAngle);
  const elevation = Math.asin(Math.min(1, Math.max(-1, sinElevation)));
  // 世界坐标：+X 东、+Y 上、-Z 北（相机默认朝 -Z）。
  const east = -Math.sin(hourAngle);
  const north = -Math.sin(latitude) * Math.cos(hourAngle);
  const up = Math.cos(latitude) * Math.cos(hourAngle);
  const rotation = env.northOffset * DEG;
  const x = east * Math.cos(rotation) + north * Math.sin(rotation);
  const z = -east * Math.sin(rotation) + north * Math.cos(rotation);
  const length = Math.hypot(x, up, z) || 1;
  const sunDirection: [number, number, number] = [x / length, up / length, z / length];

  const elevationDeg = elevation / DEG;
  const daylight = clampUnit((elevationDeg + 2) / 10);
  const dim = WEATHER_SUN_DIM[env.weather.condition] * (1 - 0.35 * env.weather.intensity);
  const sunIntensity = Math.max(0, 3.2 * daylight * dim);
  const sunColor = mixHex("#ff9d5c", "#fff6e8", elevationDeg / 25);
  const skyDay = mixHex("#ffb27a", "#b8d4ff", clampUnit(elevationDeg / 30));
  const sky = mixHex("#0a0f1c", skyDay, daylight);
  const weatherGray = env.weather.condition === "clear" ? 0 : 0.55 * env.weather.intensity;
  const skyColor = mixHex(sky, "#8d949e", weatherGray);
  const fogDensity = WEATHER_FOG[env.weather.condition] * (0.25 + env.weather.intensity);
  return {
    sunDirection,
    sunElevation: elevationDeg,
    sunAzimuth: Math.atan2(east, north) / DEG,
    sunColor,
    sunIntensity,
    ambientColor: skyColor,
    ambientIntensity: 0.18 + 0.62 * daylight,
    skyColor,
    groundColor: mixHex("#05070c", "#4a4034", daylight),
    background: skyColor,
    fogColor: mixHex(skyColor, "#7d838c", weatherGray),
    fogDensity,
    daylight,
  };
}

/** 面板/诊断用摘要：不含高度数组，避免把几十 KB 塞进状态文本。 */
export function summarizeEnvironment(env: SceneEnvironment): string {
  const light = environmentLight(env);
  const phase = light.daylight <= 0 ? "夜" : light.daylight < 0.6 ? "黄昏/清晨" : "白天";
  const terrain = env.terrain
    ? `地形 ${env.terrain.size}×${env.terrain.size} 米 / ${env.terrain.segments} 段 / 种子 ${env.terrain.seed}`
    : "平地（无地形）";
  return `${phase} ${env.hour.toFixed(2)} 点 · 太阳高度 ${light.sunElevation.toFixed(1)}° · ${env.weather.condition} ${env.weather.intensity.toFixed(2)} · ${terrain}`;
}

function smoothstep(edge0: number, edge1: number, value: number): number {
  const ratio = clampUnit((value - edge0) / (edge1 - edge0 || 1));
  return ratio * ratio * (3 - 2 * ratio);
}

/** 确定性整数哈希：不用 Math.random，也不用三角函数哈希（跨引擎尾数不同）。 */
function hashGrid(x: number, z: number, seed: number): number {
  let hash = Math.imul(x, 0x27d4eb2d) ^ Math.imul(z, 0x165667b1) ^ Math.imul(seed, 0x9e3779b1);
  hash = Math.imul(hash ^ (hash >>> 15), 0x85ebca6b);
  hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35);
  hash ^= hash >>> 16;
  return (hash >>> 0) / 4294967296;
}

function valueNoise(x: number, z: number, seed: number): number {
  const column = Math.floor(x);
  const row = Math.floor(z);
  const u = smoothstep(0, 1, x - column);
  const v = smoothstep(0, 1, z - row);
  const a = hashGrid(column, row, seed);
  const b = hashGrid(column + 1, row, seed);
  const c = hashGrid(column, row + 1, seed);
  const d = hashGrid(column + 1, row + 1, seed);
  return (a * (1 - u) + b * u) * (1 - v) + (c * (1 - u) + d * u) * v;
}

export interface TerrainOptions {
  size?: number;
  segments?: number;
  seed?: number;
  /** 起伏幅度（米）；0 = 完全平地。 */
  amplitude?: number;
}

/**
 * 生成一块确定性高度场地形：同一组参数永远得到同一份 heights。
 *
 * 中心留一块平垫（半径随世界边长增长），让原点附近的角色/道具仍站在平地上；
 * 此函数只生成高度场；人工抬升/整平/平滑/地表编辑由 environmentEdit 的不可变笔刷处理。
 */
export function createEnvironmentTerrain(options: TerrainOptions = {}): SceneTerrain {
  const size = options.size === undefined ? 80 : finiteIn(options.size, TERRAIN_LIMITS.minSize, TERRAIN_LIMITS.maxSize, "地形边长");
  const segments = options.segments === undefined
    ? 64
    : integerIn(options.segments, TERRAIN_LIMITS.minSegments, TERRAIN_LIMITS.maxSegments, "地形分段数");
  const seed = options.seed === undefined ? 41317 : integerIn(options.seed, 0, TERRAIN_LIMITS.maxSeed, "地形种子");
  const amplitude = options.amplitude === undefined ? 6 : finiteIn(options.amplitude, 0, 16, "地形起伏");
  const width = segments + 1;
  const spacing = size / segments;
  const pad = Math.max(4, size * 0.06);
  const heights: number[] = [];
  for (let row = 0; row < width; row += 1) {
    for (let column = 0; column < width; column += 1) {
      const x = column * spacing - size / 2;
      const z = row * spacing - size / 2;
      const fade = smoothstep(pad, pad * 1.8, Math.hypot(x, z));
      const broad = valueNoise(x / Math.max(6, size * 0.12), z / Math.max(6, size * 0.12), seed);
      const detail = valueNoise(x / Math.max(2, size * 0.035), z / Math.max(2, size * 0.035), seed + 1);
      const height = (broad * 0.65 + detail * 0.35 - 0.5) * 2 * amplitude * fade;
      const clamped = Math.min(TERRAIN_LIMITS.maxHeight, Math.max(TERRAIN_LIMITS.minHeight, height));
      heights.push(Math.round(clamped * 10000) / 10000 || 0); // Canonical JSON zero, not -0.
    }
  }
  return { size, segments, heights, surfaces: Array(width * width).fill(38), seed };
}

export interface TerrainBounds {
  minX: number;
  maxX: number;
  minZ: number;
  maxZ: number;
  minHeight: number;
  maxHeight: number;
}

export function terrainBounds(terrain: SceneTerrain): TerrainBounds {
  let minHeight = Infinity;
  let maxHeight = -Infinity;
  for (const height of terrain.heights) {
    minHeight = Math.min(minHeight, height);
    maxHeight = Math.max(maxHeight, height);
  }
  const half = terrain.size / 2;
  return {
    minX: -half, maxX: half, minZ: -half, maxZ: half,
    minHeight: Number.isFinite(minHeight) ? minHeight : 0,
    maxHeight: Number.isFinite(maxHeight) ? maxHeight : 0,
  };
}

/**
 * 地形高度：与 toonflow/terrainModel.getTerrainHeight 逐行同构（a/b/d、b/c/d 三角划分，
 * 与渲染网格的索引一致，山坡上不会一处悬空一处穿透）。
 *
 * 为什么在这里再写一遍：ground-path.ts 要能被 `node --test` 直接跑，而 terrainModel
 * 是一条带 three/fflate 的依赖链。两边的等价性由 environment.spec.ts 的对照测试守住，
 * 谁改了插值另一边就会红。
 */
function terrainHeightOf(terrain: SceneTerrain, x: number, z: number): number {
  if (!Number.isFinite(x) || !Number.isFinite(z)) return 0;
  if (Math.abs(x) > terrain.size / 2 || Math.abs(z) > terrain.size / 2) return 0;
  const gx = (x / terrain.size + 0.5) * terrain.segments;
  const gz = (z / terrain.size + 0.5) * terrain.segments;
  const column = Math.min(terrain.segments - 1, Math.floor(gx));
  const row = Math.min(terrain.segments - 1, Math.floor(gz));
  const u = gx - column;
  const v = gz - row;
  const a = row * (terrain.segments + 1) + column;
  const b = a + terrain.segments + 1;
  const d = a + 1;
  const c = b + 1;
  return u + v <= 1
    ? terrain.heights[a]! + (terrain.heights[d]! - terrain.heights[a]!) * u + (terrain.heights[b]! - terrain.heights[a]!) * v
    : terrain.heights[c]! + (terrain.heights[b]! - terrain.heights[c]!) * (1 - u) + (terrain.heights[d]! - terrain.heights[c]!) * (1 - v);
}

/** 地面高度：没有地形就是 0（与旧行为一致），有地形则按渲染网格同一套三角插值取值。 */
export function groundHeightAt(terrain: SceneTerrain | null | undefined, x: number, z: number): number {
  if (!terrain) return 0;
  return terrainHeightOf(terrain, x, z);
}

/** 落点钳制：有地形时钳到地形内圈，Y 落在地面上（+2cm 防 z-fighting），否则沿用旧的 20×20 平地。 */
export function clampGroundPointToTerrain(terrain: SceneTerrain | null | undefined, point: GroundPoint): GroundPoint {
  if (!terrain) return clampGroundPoint(point);
  const limit = Math.max(0, terrain.size / 2 - 0.5);
  const clamp = (value: number) => Math.min(limit, Math.max(-limit, value));
  const x = clamp(Number.isFinite(point[0]) ? point[0] : 0);
  const z = clamp(Number.isFinite(point[2]) ? point[2] : 0);
  return [x, groundHeightAt(terrain, x, z) + 0.02, z];
}

/** 坡度（无量纲，竖直变化/水平距离）。地形外或非有限坐标按 0 处理。 */
export function terrainSlopeAt(terrain: SceneTerrain, x: number, z: number): number {
  if (!Number.isFinite(x) || !Number.isFinite(z)) return 0;
  if (Math.abs(x) > terrain.size / 2 || Math.abs(z) > terrain.size / 2) return 0;
  const step = Math.max(0.05, terrain.size / terrain.segments * 0.5);
  const dx = (groundHeightAt(terrain, x + step, z) - groundHeightAt(terrain, x - step, z)) / (2 * step);
  const dz = (groundHeightAt(terrain, x, z + step) - groundHeightAt(terrain, x, z - step)) / (2 * step);
  return Math.hypot(dx, dz);
}

/** 默认可行走坡度上限：0.6 ≈ 31°，再陡就不是走位而是攀爬。 */
export const WALK_MAX_SLOPE = 0.6;

export interface WalkProof {
  ok: boolean;
  /** 不可走时的原因；可走时说明采样口径。 */
  reason: string;
  samples: number;
  maxSlope: number;
  /** 终点高度 − 起点高度（米）。 */
  climb: number;
  startY: number;
  endY: number;
}

/**
 * 沿地面路径取样时把 Y 压到当前地形上：路径存的是「地面上的点」，
 * 地形一换，同一个 XZ 的地面高度就变了。没有地形时返回原来的插值结果，
 * 因此旧存档（y≈0.02 的平地落点）行为不变。
 */
export function groundPathPointOnTerrain(
  terrain: SceneTerrain | null | undefined,
  path: GroundPath,
  t: number,
): GroundPoint {
  const point = groundPathPointAt(path, t);
  if (!terrain) return point;
  return [point[0], groundHeightAt(terrain, point[0], point[2]) + 0.02, point[2]];
}

/**
 * 走位可行性证明：沿两点之间的 XZ 直线等距取样，逐点检查是否在地形范围内、
 * 坡度是否超过上限，并对调用方提供的静态XZ障碍盒作连续相交检查。
 * 不隐式绕行；未提供障碍时保留旧行为。不证明动态碰撞、脚底贴合或寻路。
 */
export function proveWalkableLine(
  terrain: SceneTerrain | null | undefined,
  start: { x: number; z: number },
  end: { x: number; z: number },
  options: { maxSlope?: number; step?: number; obstacles?: readonly GroundObstacle[]; obstacleMargin?: number } = {},
): WalkProof {
  const endpoint = (point: { x: number; z: number }, label: string) => {
    if (typeof point?.x !== "number" || typeof point?.z !== "number" || !Number.isFinite(point.x) || !Number.isFinite(point.z)) {
      throw new Error(`${label}不是有限 XZ 坐标`);
    }
  };
  endpoint(start, "走位起点");
  endpoint(end, "走位终点");
  const maxSlope = options.maxSlope === undefined
    ? WALK_MAX_SLOPE
    : finiteIn(options.maxSlope, 0.01, 10, "坡度上限");
  const step = options.step === undefined ? 0.5 : finiteIn(options.step, 0.05, 10, "取样间距");
  const distance = Math.hypot(end.x - start.x, end.z - start.z);
  const margin = options.obstacleMargin === undefined ? GROUND_OBSTACLE_MARGIN : finiteIn(options.obstacleMargin, 0, 10, '障碍安全边距');
  for (const obstacle of options.obstacles ?? []) {
    const box = normalizeObstacle(obstacle, margin);
    if (!box) throw Error('障碍包围盒无效，无法证明可走');
    if (segmentHitsBox([start.x, 0, start.z], [end.x, 0, end.z], box)) {
      return { ok: false, reason: `直线走位与障碍「${obstacle.name || '未命名对象'}」相交（安全边距 ${margin} 米），请调整走位或布景；不会自动绕行`, samples: 0, maxSlope: 0, climb: 0,
        startY: groundHeightAt(terrain, start.x, start.z), endY: groundHeightAt(terrain, end.x, end.z) };
    }
  }
  if (!terrain) {
    return { ok: true, reason: "平地（未启用地形）：沿用原有平地直线", samples: 2, maxSlope: 0, climb: 0, startY: 0, endY: 0 };
  }
  const half = terrain.size / 2;
  for (const [point, label] of [[start, "起点"], [end, "终点"]] as const) {
    if (Math.abs(point.x) > half || Math.abs(point.z) > half) {
      return {
        ok: false,
        reason: `${label}在地形范围外（X/Z 须在 ±${half} 米内），无法证明可走`,
        samples: 0, maxSlope: 0, climb: 0,
        startY: groundHeightAt(terrain, start.x, start.z), endY: groundHeightAt(terrain, end.x, end.z),
      };
    }
  }
  const samples = Math.min(400, Math.max(2, Math.ceil(distance / step) + 1));
  let worstSlope = 0;
  for (let index = 0; index < samples; index += 1) {
    const ratio = samples === 1 ? 0 : index / (samples - 1);
    const x = start.x + (end.x - start.x) * ratio;
    const z = start.z + (end.z - start.z) * ratio;
    const cell = Math.round((z / terrain.size + 0.5) * terrain.segments) * (terrain.segments + 1) + Math.round((x / terrain.size + 0.5) * terrain.segments);
    if (typeof terrain.waterLevels?.[cell] === 'number') return { ok: false, reason: `第 ${index + 1}/${samples} 个采样点为水域，不支持涉水，请调整走位`, samples, maxSlope: worstSlope, climb: 0, startY: groundHeightAt(terrain, start.x, start.z), endY: groundHeightAt(terrain, end.x, end.z) };
    const slope = terrainSlopeAt(terrain, x, z);
    worstSlope = Math.max(worstSlope, slope);
    if (slope > maxSlope) {
      return {
        ok: false,
        reason: `第 ${index + 1}/${samples} 个采样点坡度 ${slope.toFixed(2)} 超过上限 ${maxSlope.toFixed(2)}，无法证明可走`,
        samples, maxSlope: worstSlope,
        climb: groundHeightAt(terrain, end.x, end.z) - groundHeightAt(terrain, start.x, start.z),
        startY: groundHeightAt(terrain, start.x, start.z), endY: groundHeightAt(terrain, end.x, end.z),
      };
    }
  }
  const startY = groundHeightAt(terrain, start.x, start.z);
  const endY = groundHeightAt(terrain, end.x, end.z);
  return {
    ok: true,
    reason: `${samples} 点取样，最大坡度 ${worstSlope.toFixed(2)}（上限 ${maxSlope.toFixed(2)}）`,
    samples, maxSlope: worstSlope, climb: endY - startY, startY, endY,
  };
}