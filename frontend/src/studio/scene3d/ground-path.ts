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
}

export interface GroundPoints {
  start: GroundPoint | null;
  end: GroundPoint | null;
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
  const source = value as { start?: unknown; end?: unknown };
  const start = normalizeGroundPoint(source.start);
  const end = normalizeGroundPoint(source.end);
  if (!start || !end) return null;
  if (Math.hypot(end[0] - start[0], end[1] - start[1], end[2] - start[2]) < 1e-4) return null;
  return { start, end };
}

/** 落在网格外太远的点会飘在空地上，直接钳到地面范围内。 */
export function clampGroundPoint(point: GroundPoint, extent = GROUND_EXTENT): GroundPoint {
  const limit = Math.max(0, extent);
  const clamp = (value: number) => Math.min(limit, Math.max(-limit, value));
  return [clamp(point[0]), point[1], clamp(point[2])];
}

export function groundPathLength(path: GroundPath): number {
  return Math.hypot(
    path.end[0] - path.start[0],
    path.end[1] - path.start[1],
    path.end[2] - path.start[2],
  );
}

/** 沿路径取位置：t 为 0..1 的比例，超出范围钳到端点。 */
export function groundPathPointAt(path: GroundPath, t: number): GroundPoint {
  const ratio = Math.min(1, Math.max(0, Number.isFinite(t) ? t : 0));
  return [
    path.start[0] + (path.end[0] - path.start[0]) * ratio,
    path.start[1] + (path.end[1] - path.start[1]) * ratio,
    path.start[2] + (path.end[2] - path.start[2]) * ratio,
  ];
}

/** 行进朝向：模型默认面朝 +Z，yaw = atan2(dx, dz)。 */
export function groundPathYaw(path: GroundPath): number {
  return Math.atan2(path.end[0] - path.start[0], path.end[2] - path.start[2]);
}

/** 从零散落点收成一条路径：只落了一个点时返回 null。 */
export function toGroundPath(points: GroundPoints): GroundPath | null {
  if (!points.start || !points.end) return null;
  return normalizeGroundPath({ start: points.start, end: points.end });
}