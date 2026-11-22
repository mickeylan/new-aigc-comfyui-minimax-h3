/**
 * 环绕运镜：把相机绕着某个中心转一圈，采样成一串机位。
 *
 * 只算位置/朝向，不碰 three；编辑器拿到结果直接往 cameraKeys 里追加，
 * 于是环绕跟其他机位一样能被播放、存档、导出序列。
 */

export type Vec3 = [number, number, number];

export interface OrbitKey {
  position: Vec3;
  target: Vec3;
  /** 走到下一个机位要几秒（最后一个机位的时长不参与总时长）。 */
  duration: number;
}

/** 圈数：0.25 圈（四分之一圈）到 3 圈。 */
export const ORBIT_TURNS_LIMITS = { min: 0.25, max: 3 };
/** 采样段数：4 段能看出是圆的，再多只是更顺。 */
export const ORBIT_SEGMENTS_LIMITS = { min: 4, max: 24 };
export const DEFAULT_ORBIT_TURNS = 1;
export const DEFAULT_ORBIT_SEGMENTS = 8;
export const DEFAULT_ORBIT_SECONDS = 4;
export const ORBIT_SECONDS_LIMITS = { min: 0.5, max: 60 };

const clampNumber = (value: unknown, min: number, max: number, fallback: number) => {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed)) return fallback;
  return Math.min(max, Math.max(min, parsed));
};

export function normalizeOrbitTurns(value: unknown): number {
  return clampNumber(value, ORBIT_TURNS_LIMITS.min, ORBIT_TURNS_LIMITS.max, DEFAULT_ORBIT_TURNS);
}

export function normalizeOrbitSegments(value: unknown): number {
  const parsed = clampNumber(
    value,
    ORBIT_SEGMENTS_LIMITS.min,
    ORBIT_SEGMENTS_LIMITS.max,
    DEFAULT_ORBIT_SEGMENTS,
  );
  return Math.round(parsed);
}

export function normalizeOrbitSeconds(value: unknown): number {
  return clampNumber(
    value,
    ORBIT_SECONDS_LIMITS.min,
    ORBIT_SECONDS_LIMITS.max,
    DEFAULT_ORBIT_SECONDS,
  );
}

export interface OrbitOptions {
  /** 环绕中心（一般是角色的位置）。 */
  center: Vec3;
  /** 相机现在的机位：半径与高度都从它量出来，所以按「看到的画面」转圈。 */
  start: Vec3;
  turns?: number;
  seconds?: number;
  segments?: number;
}

/**
 * 生成环绕机位：半径、高度都保持起始机位的，只是绕中心转。
 * 中心与机位重合时返回空数组（没法绕一个点转）。
 */
export function orbitCameraKeys(options: OrbitOptions): OrbitKey[] {
  const { center, start } = options;
  const turns = normalizeOrbitTurns(options.turns);
  const segments = normalizeOrbitSegments(options.segments);
  const seconds = normalizeOrbitSeconds(options.seconds);
  const offsetX = start[0] - center[0];
  const offsetZ = start[2] - center[2];
  const radius = Math.hypot(offsetX, offsetZ);
  if (radius < 1e-4) return [];
  const startAngle = Math.atan2(offsetX, offsetZ);
  const height = start[1] - center[1];
  const duration = Math.max(0.2, seconds / segments);
  const keys: OrbitKey[] = [];
  for (let index = 0; index <= segments; index += 1) {
    const angle = startAngle + Math.PI * 2 * turns * (index / segments);
    keys.push({
      position: [
        center[0] + Math.sin(angle) * radius,
        center[1] + height,
        center[2] + Math.cos(angle) * radius,
      ],
      target: [center[0], center[1], center[2]],
      duration,
    });
  }
  return keys;
}