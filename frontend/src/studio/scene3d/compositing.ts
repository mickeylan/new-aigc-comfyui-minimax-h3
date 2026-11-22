// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
// 3D 合成的纯逻辑：判断素材是图片/视频/全景，按宽高比算平面尺寸。
// 不碰 DOM 与 three，便于单元测试；场景接线在 Scene3DStage.tsx。

const IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "webp", "avif", "bmp", "gif"];
const VIDEO_EXTENSIONS = ["mp4", "webm", "mov", "m4v", "ogv"];

export type MediaKind = "image" | "video" | "unsupported";

export function detectMediaKind(nameOrPath: string): MediaKind {
  const extension = nameOrPath.split("?")[0].split(".").pop()?.toLowerCase() ?? "";
  if (IMAGE_EXTENSIONS.includes(extension)) return "image";
  if (VIDEO_EXTENSIONS.includes(extension)) return "video";
  return "unsupported";
}

export const IMAGE_PLANE_ACCEPT = IMAGE_EXTENSIONS.map((ext) => `.${ext}`).join(",");
export const VIDEO_PLANE_ACCEPT = VIDEO_EXTENSIONS.map((ext) => `.${ext}`).join(",");

/**
 * 平面按「基准高度 + 素材宽高比」定尺寸。
 * 用高度做基准而不是最长边：竖构图（9:16 角色参考）不会因为宽度被压到看不见。
 */
export function planeSize(aspect: number, baseHeight = 2) {
  const safeAspect = Number.isFinite(aspect) && aspect > 0 ? aspect : 1;
  return { width: baseHeight * safeAspect, height: baseHeight };
}

/** 视频/图片还没加载出元数据时先按 16:9 占位，加载完再按真实比例校正。 */
export const DEFAULT_PLANE_ASPECT = 16 / 9;

export interface SkyboxSettings {
  enabled: boolean;
  /** 背景亮度系数：全景图当背景时常常过亮，压一下才看得清模型。 */
  intensity: number;
  /** 同时把全景图当环境光照用（让金属/粗糙材质有反射与补光）。 */
  asEnvironment: boolean;
}

export const DEFAULT_SKYBOX: SkyboxSettings = {
  enabled: false,
  intensity: 1,
  asEnvironment: true,
};

/** 天空盒强度只作用在背景色上，不影响 environment 的采样。 */
export function skyboxBackgroundColor(intensity: number) {
  const clamped = Math.min(2, Math.max(0, intensity));
  return clamped;
}

export interface PlaneLayerSettings {
  opacity: number;
  /** 双面：从背面看也要能看见，参考板常常要从两侧看。 */
  doubleSided: boolean;
  /** 透明贴图用 alphaTest 裁掉透明像素，避免半透明排序把模型画在平面后面。 */
  alphaTest: number;
}

export const DEFAULT_PLANE_LAYER: PlaneLayerSettings = {
  opacity: 1,
  doubleSided: true,
  alphaTest: 0.02,
};

export function clampOpacity(value: number) {
  if (!Number.isFinite(value)) return 1;
  return Math.min(1, Math.max(0, value));
}

/** 视频纹理只用于预览，不进 GLB（glTF 无法内嵌正在播放的视频帧）。 */
export function isExportablePlane(kind: MediaKind) {
  return kind === "image";
}