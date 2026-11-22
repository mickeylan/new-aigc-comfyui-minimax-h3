/**
 * 希区柯克变焦（dolly zoom）：相机往目标推进（或后退），同时反向改视角，
 * 让目标在画面里的高度几乎不变，只有背景被拉伸/压扁 —— 眩晕感就从这个反差来。
 *
 * 只算机位（位置 + fov），不碰 three；编辑器把结果追加进 cameraKeys，
 * 于是跟普通机位一样能播放、存档、导出序列。
 */

export type DollyVec3 = [number, number, number];

export interface DollyKey {
  position: DollyVec3;
  target: DollyVec3;
  fov: number;
  /** 走到下一个机位要几秒（最后一格的时长不参与总时长）。 */
  duration: number;
}

/** 推进到起始距离的多少倍：小于 1 是推进，大于 1 是后退。 */
export const DOLLY_RANGE_LIMITS = { min: 0.1, max: 3 };
export const DOLLY_SEGMENTS_LIMITS = { min: 2, max: 16 };
export const DOLLY_SECONDS_LIMITS = { min: 0.5, max: 30 };
export const DEFAULT_DOLLY_RANGE = 0.35;
export const DEFAULT_DOLLY_SEGMENTS = 6;
export const DEFAULT_DOLLY_SECONDS = 3;
/** 视角上下限，跟相机路径面板上的 fov 输入一致。 */
export const DOLLY_FOV_LIMITS = { min: 15, max: 90 };

const clampNumber = (value: unknown, min: number, max: number, fallback: number) => {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed)) return fallback;
  return Math.min(max, Math.max(min, parsed));
};

export function normalizeDollyRange(value: unknown): number {
  return clampNumber(value, DOLLY_RANGE_LIMITS.min, DOLLY_RANGE_LIMITS.max, DEFAULT_DOLLY_RANGE);
}

export function normalizeDollySegments(value: unknown): number {
  const parsed = clampNumber(
    value,
    DOLLY_SEGMENTS_LIMITS.min,
    DOLLY_SEGMENTS_LIMITS.max,
    DEFAULT_DOLLY_SEGMENTS,
  );
  return Math.round(parsed);
}

export function normalizeDollySeconds(value: unknown): number {
  return clampNumber(
    value,
    DOLLY_SECONDS_LIMITS.min,
    DOLLY_SECONDS_LIMITS.max,
    DEFAULT_DOLLY_SECONDS,
  );
}

export interface DollyOptions {
  /** 当前机位。 */
  start: DollyVec3;
  /** 对准的目标（一般就是角色）。 */
  target: DollyVec3;
  /** 当前视角（度）。 */
  fov: number;
  /** 结束时距离是起始距离的多少倍。 */
  range?: number;
  seconds?: number;
  segments?: number;
}

/**
 * 保持目标在画面上的大小不变：距离 d 与视角 θ 满足 d·tan(θ/2) = 常数，
 * 所以 θ(d) = 2·atan(tan(θ0/2)·d0/d)。距离越近视角越广。
 */
export function dollyZoomKeys(options: DollyOptions): DollyKey[] {
  const { start, target } = options;
  const range = normalizeDollyRange(options.range);
  const segments = normalizeDollySegments(options.segments);
  const seconds = normalizeDollySeconds(options.seconds);
  const fov0 = clampNumber(options.fov, DOLLY_FOV_LIMITS.min, DOLLY_FOV_LIMITS.max, 45);
  const dx = start[0] - target[0];
  const dy = start[1] - target[1];
  const dz = start[2] - target[2];
  const distance0 = Math.hypot(dx, dy, dz);
  // 相机贴在目标上：没有距离就没有推进可言。
  if (distance0 < 1e-3) return [];
  const direction: DollyVec3 = [dx / distance0, dy / distance0, dz / distance0];
  const tanHalf = Math.tan((fov0 * Math.PI) / 360);
  // 视角有上下限：先按 fov 能到的范围夹住推进距离，否则最后几格视角被钳住、
  // 「目标大小不变」就破了（画面里人会忽然变小/变大）。
  const distanceForFov = (degrees: number) =>
    (tanHalf * distance0) / Math.tan((degrees * Math.PI) / 360);
  const minDistance = distanceForFov(DOLLY_FOV_LIMITS.max);
  const maxDistance = distanceForFov(DOLLY_FOV_LIMITS.min);
  const distance1 = Math.min(maxDistance, Math.max(minDistance, distance0 * range));
  const duration = Math.max(0.2, seconds / segments);
  const keys: DollyKey[] = [];
  for (let index = 0; index <= segments; index += 1) {
    const ratio = index / segments;
    const distance = distance0 + (distance1 - distance0) * ratio;
    const fovRaw =
      (2 * Math.atan((tanHalf * distance0) / distance) * 180) / Math.PI;
    // 保留一位小数：机位行里显示 49.3130… 这种数字没人看得懂，0.1° 也够用。
    const fov =
      Math.round(
        Math.min(DOLLY_FOV_LIMITS.max, Math.max(DOLLY_FOV_LIMITS.min, fovRaw)) * 10,
      ) / 10;
    keys.push({
      position: [
        target[0] + direction[0] * distance,
        target[1] + direction[1] * distance,
        target[2] + direction[2] * distance,
      ],
      target: [target[0], target[1], target[2]],
      fov,
      duration,
    });
  }
  return keys;
}