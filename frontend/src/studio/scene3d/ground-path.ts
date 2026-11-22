/**
 * 地面路径：视频生成要的「角色从 B 点走到 A 点」得先在地面上落下两个点。
 *
 * 这里只放纯数据与拾取点的钳制逻辑（能被 node --test 直接跑），
 * three 的射线拾取留在编辑器里。
 */

export type GroundPoint = [number, number, number];

export interface GroundPath {
  start: GroundPoint;
  end: GroundPoint;
  /**
   * 途经点（可选）：走位从直线变成经过它的二次贝塞尔曲线。
   * 为什么要它：绕桌子、绕柱子、从左侧进画再从右侧出画，直线走位做不到，
   * 而「角色沿路径移动」正是 3D 参考片里最需要被复刻的东西。
   */
  via?: GroundPoint;
}

export interface GroundPoints {
  start: GroundPoint | null;
  end: GroundPoint | null;
  via?: GroundPoint | null;
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

/** 两点都在、且没有重合才算一条可走的路径。 */
export function normalizeGroundPath(value: unknown): GroundPath | null {
  if (!value || typeof value !== "object") return null;
  const source = value as { start?: unknown; end?: unknown; via?: unknown };
  const start = normalizeGroundPoint(source.start);
  const end = normalizeGroundPoint(source.end);
  if (!start || !end) return null;
  if (Math.hypot(end[0] - start[0], end[1] - start[1], end[2] - start[2]) < 1e-4) return null;
  // 途经点跟端点重合（或没存）时仍按直线走：曲线有没有都说得通，不必报错。
  const via = normalizeGroundPoint(source.via);
  if (via && pointDistance(via, start) > 1e-3 && pointDistance(via, end) > 1e-3) {
    return { start, end, via };
  }
  return { start, end };
}

function pointDistance(a: GroundPoint, b: GroundPoint): number {
  return Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
}

/** 落在网格外太远的点会飘在空地上，直接钳到地面范围内。 */
export function clampGroundPoint(point: GroundPoint, extent = GROUND_EXTENT): GroundPoint {
  const limit = Math.max(0, extent);
  const clamp = (value: number) => Math.min(limit, Math.max(-limit, value));
  return [clamp(point[0]), point[1], clamp(point[2])];
}

export function groundPathLength(path: GroundPath): number {
  if (!path.via) {
    return Math.hypot(
      path.end[0] - path.start[0],
      path.end[1] - path.start[1],
      path.end[2] - path.start[2],
    );
  }
  // 曲线按 24 段折线量长度：面板上显示「7.3 m」这种长度，直线距离会把弯路说短。
  const segments = 24;
  let total = 0;
  let previous = groundPathPointAt(path, 0);
  for (let index = 1; index <= segments; index += 1) {
    const next = groundPathPointAt(path, index / segments);
    total += Math.hypot(next[0] - previous[0], next[1] - previous[1], next[2] - previous[2]);
    previous = next;
  }
  return total;
}

/**
 * 途经点当「必须经过的点」而不是贝塞尔控制点：控制点取 2V-(A+B)/2，
 * 这样 B(0.5) 恰好落在 V 上。若直接拿 V 当控制点，曲线只会走到 V 的一半，
 * 角色会从障碍物里侧擦过去 —— 与「绕桌子/绕柱子」的用途正好相反。
 */
function viaControlPoint(path: GroundPath & { via: GroundPoint }): GroundPoint {
  return [
    2 * path.via[0] - (path.start[0] + path.end[0]) / 2,
    2 * path.via[1] - (path.start[1] + path.end[1]) / 2,
    2 * path.via[2] - (path.start[2] + path.end[2]) / 2,
  ];
}

/** 沿路径取位置：t 为 0..1 的比例，超出范围钳到端点。有途经点时走二次贝塞尔并穿过它。 */
export function groundPathPointAt(path: GroundPath, t: number): GroundPoint {
  const ratio = Math.min(1, Math.max(0, Number.isFinite(t) ? t : 0));
  if (path.via) {
    const control = viaControlPoint({ start: path.start, end: path.end, via: path.via });
    const inverse = 1 - ratio;
    const w0 = inverse * inverse;
    const w1 = 2 * inverse * ratio;
    const w2 = ratio * ratio;
    return [
      w0 * path.start[0] + w1 * control[0] + w2 * path.end[0],
      w0 * path.start[1] + w1 * control[1] + w2 * path.end[1],
      w0 * path.start[2] + w1 * control[2] + w2 * path.end[2],
    ];
  }
  return [
    path.start[0] + (path.end[0] - path.start[0]) * ratio,
    path.start[1] + (path.end[1] - path.start[1]) * ratio,
    path.start[2] + (path.end[2] - path.start[2]) * ratio,
  ];
}

/**
 * 行进朝向：模型默认面朝 +Z，yaw = atan2(dx, dz)。
 * 有途经点时按曲线切线算，起步/收尾都沿切线，不会在拐弯处瞬间转头。
 */
export function groundPathYaw(path: GroundPath, t = 1): number {
  if (!path.via) {
    return Math.atan2(path.end[0] - path.start[0], path.end[2] - path.start[2]);
  }
  const control = viaControlPoint({ start: path.start, end: path.end, via: path.via });
  const ratio = Math.min(1, Math.max(0, Number.isFinite(t) ? t : 0));
  // B'(t) = 2(1-t)(C-A) + 2t(B-C)；C≠A、C≠B 已由 normalizeGroundPath 保证，不会出现零切线。
  const dx =
    2 * (1 - ratio) * (control[0] - path.start[0]) +
    2 * ratio * (path.end[0] - control[0]);
  const dz =
    2 * (1 - ratio) * (control[2] - path.start[2]) +
    2 * ratio * (path.end[2] - control[2]);
  return Math.atan2(dx, dz);
}

/** 从零散落点收成一条路径：只落了一个点时返回 null。 */
export function toGroundPath(points: GroundPoints): GroundPath | null {
  if (!points.start || !points.end) return null;
  return normalizeGroundPath({ start: points.start, end: points.end, via: points.via ?? null });
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