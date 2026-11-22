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
/**
 * 俯仰：结束时机位的仰角相对起幅抬高/压低多少度。
 * 0 = 保持起幅高度（老的平视环绕）；正值是转着圈上升（俯拍环绕），
 * 负值是转着圈下降。仰角本身夹在 ±85° 以内，避免相机落到中心正上/正下方。
 */
export const ORBIT_PITCH_LIMITS = { min: -60, max: 60 };
export const DEFAULT_ORBIT_PITCH = 0;
export const ORBIT_ELEVATION_LIMIT_DEG = 85;

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

export function normalizeOrbitPitch(value: unknown): number {
  return clampNumber(value, ORBIT_PITCH_LIMITS.min, ORBIT_PITCH_LIMITS.max, DEFAULT_ORBIT_PITCH);
}

export interface OrbitOptions {
  /** 环绕中心（一般是角色的位置）。 */
  center: Vec3;
  /** 相机现在的机位：半径与高度都从它量出来，所以按「看到的画面」转圈。 */
  start: Vec3;
  turns?: number;
  seconds?: number;
  segments?: number;
  /** 结束时机位抬高/压低多少度（0 = 保持起幅高度，转成平视环绕）。 */
  pitch?: number;
}

/**
 * 生成环绕机位：绕中心转，距离保持起始机位的，按 pitch 决定收尾时抬高还是压低。
 *
 * pitch 为 0 时就是老行为：高度、水平半径都不变，只是绕圈。
 * pitch 非 0 时，机位沿一段圆弧抬升/下降（三维距离不变），
 * 相机仍对着中心 —— 于是画面里主体大小不变、背景竖向滚动，
 * 就是「一边转一边升空俯拍」的环绕。
 * 中心与机位重合时返回空数组（没法绕一个点转）。
 */
export function orbitCameraKeys(options: OrbitOptions): OrbitKey[] {
  const { center, start } = options;
  const turns = normalizeOrbitTurns(options.turns);
  const segments = normalizeOrbitSegments(options.segments);
  const seconds = normalizeOrbitSeconds(options.seconds);
  const pitch = normalizeOrbitPitch(options.pitch);
  const offsetX = start[0] - center[0];
  const offsetY = start[1] - center[1];
  const offsetZ = start[2] - center[2];
  const radius = Math.hypot(offsetX, offsetZ);
  const distance = Math.hypot(radius, offsetY);
  if (radius < 1e-4 || distance < 1e-4) return [];
  const startAngle = Math.atan2(offsetX, offsetZ);
  // 起幅仰角：从当前机位量出来，pitch 是相对它的增量，0 就落回原高度。
  const startElevation = Math.asin(Math.min(1, Math.max(-1, offsetY / distance)));
  const limit = (ORBIT_ELEVATION_LIMIT_DEG * Math.PI) / 180;
  const endElevation = Math.min(
    limit,
    Math.max(-limit, startElevation + (pitch * Math.PI) / 180),
  );
  const duration = Math.max(0.2, seconds / segments);
  const keys: OrbitKey[] = [];
  for (let index = 0; index <= segments; index += 1) {
    const progress = index / segments;
    const angle = startAngle + Math.PI * 2 * turns * progress;
    const elevation = startElevation + (endElevation - startElevation) * progress;
    const planar = distance * Math.cos(elevation);
    keys.push({
      position: [
        center[0] + Math.sin(angle) * planar,
        center[1] + distance * Math.sin(elevation),
        center[2] + Math.cos(angle) * planar,
      ],
      target: [center[0], center[1], center[2]],
      duration,
    });
  }
  return keys;
}