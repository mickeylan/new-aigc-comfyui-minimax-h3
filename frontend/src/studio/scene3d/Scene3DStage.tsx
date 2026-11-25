// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type DragEvent,
} from "react";
import * as THREE from "three";
import { SceneOperations, SceneSaveGuard, buildSceneFingerprint, canMoveEntry, materialRevision, type SceneFingerprint } from './sceneSafety';
import { sequenceTiming, sequenceSampleTime } from './sequenceTiming';
import { referencePlaneMaterial } from './referenceAlpha';
import { ObjectHistory, duplicateMesh, surfaceDrop } from './kitbashEditor';
import { FRAME_ASPECTS, FRAME_VIDEO_SIZES, fitFrame, focalFromFov, fovFromFocal, type FrameAspect } from './sceneFrame';
import { DEFAULT_PLANE_SETTINGS, normalizePlaneSettings, planeMaterial, mediaDataURL, validateSceneSize, validateCompositionDocument, type PlaneSettings } from "./planeDocument";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { TransformControls } from "three/examples/jsm/controls/TransformControls.js";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { GLTFExporter } from "three/examples/jsm/exporters/GLTFExporter.js";
import { OBJLoader } from "three/examples/jsm/loaders/OBJLoader.js";
import { findTemplateId } from "../host/template-cache.js";
import { FKHelper } from "./posing/FKHelper.js";
import { PreviewPlayhead, PREVIEW_FPS, previewFrameCount, previewFrameTime } from './previewPlayhead';
import { AnimationPreview, skeletalClips, saveAnimationClips, restoreAnimationClips, markAnimationArchivePaths, attachAnimationClip, saveMoveAnimation, restoreMoveAnimation, type SavedMoveAnimation, type SavedAnimationClip } from './animationPreview';
import { findCapsuleRigs, gaitCapsuleRig, resetCapsuleRig } from './toonflow/capsuleRig';
import { createStoreZip } from "./zip.js";
import {
  DEFAULT_ORBIT_PITCH,
  DEFAULT_ORBIT_SECONDS,
  ORBIT_SEGMENTS_LIMITS,
  normalizeOrbitPitch,
  normalizeOrbitSeconds,
  normalizeOrbitTurns,
  orbitCameraKeys,
} from "./camera-orbit.js";
import {
  DEFAULT_DOLLY_SECONDS,
  dollyZoomKeys,
  normalizeDollyRange,
  normalizeDollySeconds,
} from "./dolly-zoom.js";
import {
  fileNameStamp,
  referenceFileName,
  sequenceFrameFileName,
  sequenceLibraryBlocker,
} from "./reference-library.js";
import {
  archiveSizeLabel,
  parseSceneArchiveList,
  sceneArchiveBlocker,
  sceneArchiveFileName,
  sceneArchiveFormBody,
  type SceneArchiveItem,
} from "./scene-archive.js";
import {
  GROUND_EXTENT,
  WEATHER_CONDITIONS,
  clampGroundPoint,
  clampGroundPointToTerrain,
  createEnvironmentTerrain,
  defaultSceneEnvironment,
  environmentLight,
  groundHeightAt,
  groundPathFollowDelta,
  groundPathLength,
  groundPathPointOnTerrain,
  groundPathYaw,
  MAX_GROUND_VIAS,
  moveGroundPoint,
  normalizeGroundPath,
  proveWalkableLine,
  routeAroundObstacles,
  routeAroundObstaclesDetailed,
  summarizeEnvironment,
  toGroundPath,
  validateSceneEnvironment,
  type GroundObstacle,
  type GroundPath,
  type GroundPoint,
  type GroundPoints,
  type GroundSlot,
  type SceneEnvironment,
  type WeatherCondition,
} from "./ground-path.js";
import type { SceneTerrain } from './toonflow/sceneDocument';
import type { CharacterAnimationBinding, CharacterAnimationCatalogEntry, CharacterAnimationCatalogSummary } from './toonflow/characterAnimations';
import { releaseMeshGeometry } from './toonflow/sceneResources';
import { createEnvironmentTerrainMesh, disposeEnvironmentTerrainMesh } from './environmentTerrain';
import { createEnvironmentEffects } from './environmentEffects';
import EnvironmentBrushPanel from './EnvironmentBrushPanel';
import ScriptVideoPanel, { type ScriptVideoArtifact, type ScriptVideoMode } from './ScriptVideoPanel';
import ShotEnhancementPanel from './ShotEnhancementPanel';
import CharacterMotionPanel from './CharacterMotionPanel';
import {
  cameraFrameFileName,
  firstLastCameraFrames,
  type MovableCamera,
} from "./camera-frames.js";
import { firstLastMotionNote } from "./camera-motion-note.js";
import {
  appendMotionToPrompt,
  motionPromptLine,
  projectChoices,
  scenePromptChoices,
  type ProjectChoice,
  type PromptSceneChoice,
} from "./motion-prompt.js";
import {
  answerCameraQuestion,
  describeMove,
  parseCameraBrief,
  planCameraMoves,
  type CameraIntent,
  type CameraPlan,
} from "./camera-agent.js";
import AssetLibraryPanel from "./AssetLibraryPanel";
import ProceduralAssetPanel from './ProceduralAssetPanel';
import SetTemplatePanel from './SetTemplatePanel';
import LocationGenerationPanel from "./LocationGenerationPanel";
import { clonePosedContent, hidePoseEditorHelpers, isPoseEditorHelper } from "./posing/poseExport";
import { ASSET_DRAG_MIME, assetKind, assetUrl, type LibraryAsset } from "./assetLibrary.js";
import {
  DEFAULT_PLANE_LAYER,
  DEFAULT_PLANE_ASPECT,
  DEFAULT_SKYBOX,
  IMAGE_PLANE_ACCEPT,
  VIDEO_PLANE_ACCEPT,
  clampOpacity,
  detectMediaKind,
  isExportablePlane,
  planeSize,
  type MediaKind,
  type SkyboxSettings,
} from "./compositing.js";
import {
  findImage,
  imageMaterialOptions,
  imagePlaneName,
  materialUrl,
  skyboxLabel,
  type LibraryImage,
} from "./background-library.js";
import {
  applyBonePose,
  findSkeletonRoot,
  hasSkeleton,
  serializeBonePose,
  type BonePoseNode,
} from "./posing/bonePose.js";
import {
  createSplatRenderer,
  disposeSplat,
  loadSplatMesh,
  stepSplatDetail,
  type SplatDetailGuardState,
} from "./splat.js";

interface Scene3DStageProps {
  exitTo?: string;
}

/**
 * 身份迁移的默认提示词（已由用户逐字确认）。
 * <image1> = 当前 3D 画面（定机位、构图、景别、光影）
 * <image2> = 角色主形象（定脸型、五官、发型、服装）
 */
const DEFAULT_IDENTITY_PROMPT = `以<image2>的人物身份替换<image1>中的主体，保持<image1>的机位、构图、
景别与光影不变；脸型、五官、发型与服装以<image2>为准；
输出写实摄影风格，不要 3D 渲染感、不要塑料皮肤。`;

interface SceneObjectEntry {
  locked?: boolean;
  id: string;
  name: string;
  object: THREE.Object3D;
  /** 骨骼摆姿快照；有骨骼的角色才有，随场景存档一起保存。 */
  bonePose?: BonePoseNode | null;
  /** 模型刚载入时的绑定姿势，用于「恢复初始姿势」。 */
  initialBonePose?: BonePoseNode | null;
  /** 图片/视频平面层：记录媒体类型，导出时跳过视频（glTF 内嵌不了播放中的视频帧）。 */
  mediaKind?: MediaKind;
  plane?: PlaneSettings;
  mediaSource?: string;
  /** 视频平面自己的 <video> 与纹理，删除对象和卸载时要停掉并释放。 */
  video?: { element: HTMLVideoElement; texture: THREE.VideoTexture } | null;
  /** 高斯泼溅对象：Spark 有独立缓冲，要调它自己的 dispose，不走几何释放。 */
  splat?: THREE.Object3D | null;
  /** 泼溅的来源地址（/api/output 或 /api/input）：场景存档要把字节内嵌，得先取回来。 */
  splatUrl?: string;
}

function disposeSceneEntry(entry: SceneObjectEntry) {
  if (entry.splat) { disposeSplat(entry.splat); if (entry.splatUrl?.startsWith("blob:")) URL.revokeObjectURL(entry.splatUrl); return; }
  if (entry.video) { const src = entry.video.element.currentSrc || entry.video.element.src; entry.video.element.pause(); entry.video.element.removeAttribute("src"); entry.video.element.load(); entry.video.texture.dispose(); if (src.startsWith('blob:')) URL.revokeObjectURL(src); }
  entry.object.traverse((child) => {
    const mesh = child as THREE.Mesh;
    if (mesh.isMesh) {
      releaseMeshGeometry(mesh).forEach(geometry => geometry.dispose());
      if ((mesh as THREE.SkinnedMesh).isSkinnedMesh) (mesh as THREE.SkinnedMesh).skeleton.dispose();
    } else mesh.geometry?.dispose();
    const materials = Array.isArray(mesh.material) ? mesh.material : mesh.material ? [mesh.material] : [];
    materials.forEach((material) => {
      Object.values(material).forEach((value) => { if (value instanceof THREE.Texture) value.dispose(); });
      material.dispose();
    });
  });
}

type TransformMode = "translate" | "rotate" | "scale";

type LightPresetName = "studio" | "sunset" | "night" | "rim";

interface MaterialSettings {
  color: string;
  roughness: number;
  metalness: number;
  opacity: number;
}

type CameraEasing = "linear" | "easeInOut" | "easeIn" | "easeOut";

interface CameraKey {
  id: string;
  position: [number, number, number];
  target: [number, number, number];
  /** 从这个机位走到下一个机位的秒数（最后一个机位用不到）。 */
  duration: number;
  /** 这一段按哪种缓动走。 */
  easing: CameraEasing;
  /** 这个机位的视角（度）：越小越「推」，越大越「拉」。 */
  fov: number;
}

/** 老存档 / 新记录的默认段时长，与改动前的硬编码 1.6 秒一致。 */
const DEFAULT_CAMERA_SEGMENT_SECONDS = 1.6;
const DEFAULT_CAMERA_EASING: CameraEasing = "easeInOut";
const CAMERA_SEGMENT_LIMITS = { min: 0.2, max: 20 };
// 15° 已经很「长焦」，90° 接近广角极限；再外扩就不是摄影而是鱼眼了。
const DEFAULT_CAMERA_FOV = 45;
const CAMERA_FOV_LIMITS = { min: 15, max: 90 };

const CAMERA_EASINGS: Record<CameraEasing, { label: string; fn: (t: number) => number }> = {
  // 匀速看起来最"塑料"，但需要机械感时得能选。
  linear: { label: "匀速", fn: (t) => t },
  easeInOut: { label: "缓入缓出", fn: (t) => t * t * (3 - 2 * t) },
  easeIn: { label: "缓入", fn: (t) => t * t },
  easeOut: { label: "缓出", fn: (t) => 1 - (1 - t) * (1 - t) },
};

function normalizeCameraFov(value: unknown) {
  const fov = typeof value === "number" && Number.isFinite(value) ? value : DEFAULT_CAMERA_FOV;
  return Math.min(CAMERA_FOV_LIMITS.max, Math.max(CAMERA_FOV_LIMITS.min, fov));
}

function normalizeCameraDuration(value: unknown) {
  const seconds = Number(value);
  if (!Number.isFinite(seconds) || seconds <= 0) return DEFAULT_CAMERA_SEGMENT_SECONDS;
  return Math.min(CAMERA_SEGMENT_LIMITS.max, Math.max(CAMERA_SEGMENT_LIMITS.min, seconds));
}

/** 参考图三种 pass：素模、法线、深度。给视频生成当控制图用。 */
type ReferencePass = "clay" | "normal" | "depth";

const REFERENCE_PASS_LABELS: Record<ReferencePass, string> = {
  clay: "白模",
  normal: "法线",
  depth: "深度",
};

const REFERENCE_BUSY_PREFIX = "正在渲染参考图";
// 序列帧上限：一按就渲上千帧会把页面卡死，240 帧 ≈ 30 秒 @8fps 已经够当参考。
const SEQUENCE_MAX_FRAMES = 240;
const SEQUENCE_FPS_OPTIONS = [6, 8, 12];
const DEFAULT_SEQUENCE_FPS = 8;
/** 序列导出的宽度：0 = 跟随视口原尺寸；其余按宽度等比缩。 */
const SEQUENCE_WIDTH_OPTIONS = [0, 1280, 960, 720];
const SEQUENCE_WIDTH_LABELS: Record<number, string> = {
  0: "视口原尺寸",
  1280: "1280 宽",
  960: "960 宽",
  720: "720 宽",
};

function cameraPathSegments(keys: CameraKey[]) {
  return keys.slice(0, -1).map((from, index) => ({
    from,
    to: keys[index + 1],
    seconds: normalizeCameraDuration(from.duration),
    ease: CAMERA_EASINGS[normalizeCameraEasing(from.easing)].fn,
  }));
}

function cameraPathDuration(keys: CameraKey[]) {
  return cameraPathSegments(keys).reduce((sum, item) => sum + item.seconds, 0);
}

/** 按时间从路径上取一个机位；超过总时长停在最后一段末尾。 */
function sampleCameraPath(keys: CameraKey[], elapsed: number) {
  const segments = cameraPathSegments(keys);
  if (segments.length === 0) return null;
  let cursor = 0;
  let active = segments[0];
  for (const item of segments) {
    active = item;
    if (elapsed < cursor + item.seconds || item === segments[segments.length - 1]) break;
    cursor += item.seconds;
  }
  const local = Math.min(1, Math.max(0, (elapsed - cursor) / active.seconds));
  const ratio = active.ease(local);
  const mix = (from: [number, number, number], to: [number, number, number]) =>
    [
      from[0] + (to[0] - from[0]) * ratio,
      from[1] + (to[1] - from[1]) * ratio,
      from[2] + (to[2] - from[2]) * ratio,
    ] as [number, number, number];
  const fromFov = normalizeCameraFov(active.from.fov);
  const toFov = normalizeCameraFov(active.to.fov);
  return {
    position: mix(active.from.position, active.to.position),
    target: mix(active.from.target, active.to.target),
    // 推拉：两个机位视角不同就一路变焦过去。
    fov: fromFov + (toFov - fromFov) * ratio,
  };
}

const MOVE_SECONDS_LIMITS = { min: 0.5, max: 30 };

function normalizeMoveSeconds(value: unknown) {
  const seconds = typeof value === "number" && Number.isFinite(value) ? value : 3;
  return Math.min(MOVE_SECONDS_LIMITS.max, Math.max(MOVE_SECONDS_LIMITS.min, seconds));
}

/** 序列总时长取「相机路径时长」与「角色移动时长」中较长的一条：两者谁长都不能被截断。 */
function sequencePlan(cameraKeys: CameraKey[], moverSeconds: number | null, fps: number) {
  const seconds = Math.max(cameraPathDuration(cameraKeys), moverSeconds ?? 0);
  return sequenceTiming(seconds, fps, SEQUENCE_MAX_FRAMES);
}

/**
 * 参考图专用材质。三种都要盖到场景里所有网格上，渲完必须无条件还原（外面 finally 保证）。
 * 深度用独立的线性深度 shader：近黑远白。
 * 深度范围不能直接用 camera.near/far（0.1–1000），那样几米内的模型只占 1% 灰度，
 * 看上去就是一整张黑图；要按模型自身的包围盒算一段贴身区间。
 */
export function createReferenceMaterial(
  pass: ReferencePass,
  depthRange: { near: number; far: number },
) {
  if (pass === "clay") {
    // 白模要保留灯光关系，只把材质换成中性白。GLB 常见单面片，双面才不会从背后看就消失。
    return new THREE.MeshStandardMaterial({
      color: 0xffffff,
      roughness: 0.95,
      metalness: 0,
      side: THREE.DoubleSide,
    });
  }
  if (pass === "normal") {
    return new THREE.MeshNormalMaterial({ side: THREE.DoubleSide });
  }
  return new THREE.ShaderMaterial({
    side: THREE.DoubleSide,
    uniforms: { uNear: { value: depthRange.near }, uFar: { value: depthRange.far } },
    vertexShader: `
      #include <common>
      #include <morphtarget_pars_vertex>
      #include <skinning_pars_vertex>
      varying float vViewDepth;
      void main() {
        #include <skinbase_vertex>
        #include <morphinstance_vertex>
        #include <begin_vertex>
        #include <morphtarget_vertex>
        #include <skinning_vertex>
        #include <project_vertex>
        vec4 viewPosition = mvPosition;
        vViewDepth = -viewPosition.z;
      }
    `,
    fragmentShader: `
      uniform float uNear;
      uniform float uFar;
      varying float vViewDepth;
      void main() {
        float linearDepth = clamp((vViewDepth - uNear) / (uFar - uNear), 0.0, 1.0);
        gl_FragColor = vec4(vec3(linearDepth), 1.0);
      }
    `,
  });
}

function normalizeCameraEasing(value: unknown): CameraEasing {
  return typeof value === "string" && value in CAMERA_EASINGS
    ? (value as CameraEasing)
    : DEFAULT_CAMERA_EASING;
}

interface PoseSnapshot {
  position: [number, number, number];
  quaternion: [number, number, number, number];
  scale: [number, number, number];
}

// 场景存档：几何体以 GLB 形式内嵌，灯光、机位与每个对象的变换单独记录；
// 变换不依赖导出器是否保留节点 TRS，载入时按名字（缺名字则按顺序）回填。
// v2 起高斯泼溅单独用 splats 内嵌 .spz 字节：SplatMesh 进不了 glTF，只能自带数据。
interface SplatDocument {
  /** Preserve decoder/normalization TRS separately from the user-editable group. */
  contentTransform?: PoseSnapshot;
  id?: string;
  visible?: boolean;
  locked?: boolean;
  name: string;
  /** .spz/.splat/.ksplat 原始字节的 base64。 */
  data: string;
  position: [number, number, number];
  quaternion: [number, number, number, number];
  scale: [number, number, number];
}

interface SceneDocument {
  version: 1 | 2 | 3 | 4 | 5 | 6 | 7;
  procedural?: import('./proceduralArchive').RecipeArchive;
  frame?: { aspect: FrameAspect };
  name?: string;
  preview?: string;
  planes?: Array<{ id: string; visible?: boolean; locked?: boolean; name: string; kind: MediaKind; source: string; settings: PlaneSettings; position: [number, number, number]; quaternion: [number, number, number, number]; scale: [number, number, number] }>;
  skybox?: { source: string; name: string; intensity: number; asEnvironment: boolean } | null;
  createdAt: string;
  lightPreset: LightPresetName;
  camera: { position: [number, number, number]; target: [number, number, number]; fov?: number };
  objects: Array<{
    visible?: boolean;
    locked?: boolean;
    id?: string;
    name: string;
    position: [number, number, number];
    quaternion: [number, number, number, number];
    scale: [number, number, number];
    /** 骨骼摆姿快照；非角色没有这个字段。 */
    bonePose?: BonePoseNode | null;
    initialBonePose?: BonePoseNode | null;
    animations?: SavedAnimationClip[];
    procedural?: number;
  }>;
  /** 高斯泼溅层；v1 存档没有这个字段。 */
  splats?: SplatDocument[];
  /** 相机关键帧序列；v3 起入库，v1/v2 存档没有这个字段。 */
  cameraKeys?: CameraKey[];
  /** 剧本镜头表（可选）：旧存档没有这个字段，读取后不补造。 */
  scriptShots?: import('./toonflowSets').ScriptShotPlan;
  /**
   * 场景环境（可选，version 1）：时间 / 天气 / 地形高度场。
   * 旧存档没有这个字段 → 读出 null，沿用原有的平地与灯光预设，行为不变。
   * 有字段但版本或结构不认识 → 显式报错且不替换当前场景。
   */
  environment?: SceneEnvironment;
  /** 地面 A/B 落点（vias 是 0–3 个途经点，走曲线时才写）；老存档没有就按没有处理。 */
  groundPath?: { start: GroundPoint; end: GroundPoint; vias?: GroundPoint[] } | null;
  /** 角色沿 A→B 移动的计划：谁走、走多久、相机要不要跟拍。没有落点时不写。 */
  move?: { targetId?: string; targetName: string; seconds: number; follow?: boolean; animation?: SavedMoveAnimation } | null;
  model: { name: string; data: string };
}

const DEFAULT_MATERIAL: MaterialSettings = {
  color: "#cfd4dc",
  roughness: 0.55,
  metalness: 0.05,
  opacity: 1,
};

// 每套灯光同时决定背景与三点光，切方案时整场氛围一起变。
const LIGHT_PRESETS: Record<
  LightPresetName,
  {
    label: string;
    background: string;
    hemi: { sky: string; ground: string; intensity: number };
    key: { color: string; intensity: number; position: [number, number, number] };
    fill: { color: string; intensity: number; position: [number, number, number] };
    rim: { color: string; intensity: number; position: [number, number, number] };
  }
> = {
  studio: {
    label: "影棚",
    background: "#121316",
    hemi: { sky: "#ffffff", ground: "#334155", intensity: 1.4 },
    key: { color: "#ffffff", intensity: 2.2, position: [5, 8, 4] },
    fill: { color: "#88aaff", intensity: 0.7, position: [-5, 3, -4] },
    rim: { color: "#ffffff", intensity: 0.4, position: [-3, 6, -8] },
  },
  sunset: {
    label: "日落",
    background: "#2a1712",
    hemi: { sky: "#ffd7a8", ground: "#2b1a12", intensity: 0.9 },
    key: { color: "#ffb066", intensity: 3.2, position: [8, 2, 3] },
    fill: { color: "#6a4bff", intensity: 0.55, position: [-6, 2, -5] },
    rim: { color: "#ff8a4d", intensity: 1.4, position: [-2, 4, -8] },
  },
  night: {
    label: "夜晚",
    background: "#080b14",
    hemi: { sky: "#6c8cff", ground: "#0a0d18", intensity: 0.35 },
    key: { color: "#a8c4ff", intensity: 0.7, position: [4, 7, 5] },
    fill: { color: "#3050ff", intensity: 0.3, position: [-4, 2, -4] },
    rim: { color: "#b7c8ff", intensity: 1.2, position: [-3, 5, -8] },
  },
  rim: {
    label: "逆光",
    background: "#101014",
    hemi: { sky: "#ffffff", ground: "#111827", intensity: 0.6 },
    key: { color: "#ffffff", intensity: 0.9, position: [3, 5, 7] },
    fill: { color: "#8fb4ff", intensity: 0.25, position: [-3, 2, 4] },
    rim: { color: "#ffffff", intensity: 4.0, position: [-2, 5, -9] },
  },
};

// 环境的天气词表与 Toonflow/Vue 预演一致：clear/cloudy/rain/snow/fog（Vue 的 overcast 视为 cloudy）。
const WEATHER_LABELS: Record<WeatherCondition, string> = {
  clear: "晴",
  cloudy: "多云",
  rain: "雨",
  snow: "雪",
  fog: "雾",
};

const POLL_INTERVAL_MS = 2000;
const TASK_TIMEOUT_MS = 10 * 60 * 1000;

async function uploadImage(blob: Blob, name: string) {
  const form = new FormData();
  form.append("file", blob, name);
  form.append("type", "image");
  const response = await fetch("/api/upload", { method: "POST", body: form });
  if (!response.ok) {
    throw new Error(`上传失败：${response.status} ${await response.text()}`);
  }
  return (await response.json()) as { task_id: string; name: string };
}

async function createImageTo3DTask(upload: { task_id: string; name: string }) {
  const templateId = await findTemplateId("image_to_3d_hunyuan3d");
  const response = await fetch("/api/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      template_id: templateId,
      prompt: "",
      params: {},
      files: { image: [{ task_id: upload.task_id, name: upload.name }] },
    }),
  });
  if (!response.ok) {
    throw new Error(`创建任务失败：${response.status} ${await response.text()}`);
  }
  return response.json() as Promise<{ task_id: string }>;
}

/**
 * 图生 3D 世界的抠图引擎 → 模板 code。
 * rmbg3：RMBG-3.0 出软 alpha matte（发丝细节好，但每张多花十几秒、要 8B 权重）；
 * birefnet：ComfyUI 核心 BiRefNet，3 秒级、硬边，权重许可也不是非商用限制。
 */
const SPLAT_MATTE_TEMPLATES = {
  rmbg3: "image_to_3d_triposplat",
  birefnet: "image_to_3d_triposplat_birefnet",
} as const;
type SplatMatteEngine = keyof typeof SPLAT_MATTE_TEMPLATES;

/**
 * 泼溅细节档位。Spark 的 LoD 是按屏幕尺寸在 splat 树里挑代表点，
 * 所以调这个数不会像"截断前 N 个"那样直接把远处砍掉。
 */
const SPLAT_DETAIL_PRESETS = [
  { label: "省显存", count: 500_000 },
  { label: "均衡", count: 1_000_000 },
  { label: "高", count: 2_000_000 },
  { label: "最高", count: 5_000_000 },
] as const;

/** 「自动」从哪一档起步：均衡。之后由帧率守护上下调。 */
const SPLAT_DETAIL_AUTO_START = 1;

/** 素材库缩略图不需要 4K：最长边缩到 512，PNG 体积能小一个数量级。 */
const THUMBNAIL_MAX_EDGE = 512;

/**
 * 把整张画布缩成缩略图。必须在 render 之后的同一个任务里调用：
 * drawImage 是同步取像素的，等 toBlob 期间缓冲被清也不影响结果。
 * 编码用 JPEG：3D 预览不需要透明通道，同尺寸比 PNG 再小一个数量级。
 */
async function shrinkToThumbBlob(canvas: HTMLCanvasElement, maxEdge = THUMBNAIL_MAX_EDGE): Promise<Blob | null> {
  const scale = Math.min(1, maxEdge / Math.max(canvas.width, canvas.height));
  const out = document.createElement("canvas");
  out.width = Math.max(1, Math.round(canvas.width * scale));
  out.height = Math.max(1, Math.round(canvas.height * scale));
  const ctx = out.getContext("2d");
  if (!ctx) return null;
  ctx.drawImage(canvas, 0, 0, out.width, out.height);
  return await new Promise((resolve) => out.toBlob((blob) => resolve(blob), "image/jpeg", 0.85));
}

/** 泼溅首次出现前 Spark 要在线程里建 LoD 树，大文件会有几秒空白，用这条占住 busy。 */
const SPLAT_LOD_BUSY = "正在建泼溅细节层…";
/** onDirty 通常几十秒内就来；万一 Spark 没触发，到时自己收掉，别把界面锁死。 */
const SPLAT_LOD_TIMEOUT_MS = 90_000;

/** 图生 3D 高斯泼溅：走 TripoSplat 模板，输出 .spz。 */
async function createImageToSplatTask(
  upload: { task_id: string; name: string },
  matteEngine: SplatMatteEngine = "rmbg3",
) {
  let templateId: number;
  try {
    templateId = await findTemplateId(SPLAT_MATTE_TEMPLATES[matteEngine]);
  } catch (err) {
    // 装了 RMBG-3 版却缺 BiRefNet 变体模板时，别让按钮直接失败，退回默认模板。
    if (matteEngine === "rmbg3") throw err;
    templateId = await findTemplateId(SPLAT_MATTE_TEMPLATES.rmbg3);
  }
  const response = await fetch("/api/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      template_id: templateId,
      prompt: "",
      params: {},
      files: { image: [{ task_id: upload.task_id, name: upload.name }] },
    }),
  });
  if (!response.ok) {
    throw new Error(`创建任务失败：${response.status} ${await response.text()}`);
  }
  return response.json() as Promise<{ task_id: string }>;
}

/**
 * 身份迁移：复用 Qwen-Image 2.1 多图编辑模板，ref_images 是有序图，
 * prompt 里用 <image1>/<image2> 指代。第 1 张是 3D 场景快照（定机位与氛围），
 * 第 2 张是角色主形象（定脸与服装）。
 */
async function createIdentityTransferTask(options: {
  snapshot: { task_id: string; name: string };
  character: { task_id: string; name: string };
  prompt: string;
}) {
  const templateId = await findTemplateId("qwen_image_2_1_multi_edit");
  const response = await fetch("/api/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      template_id: templateId,
      prompt: options.prompt,
      params: { prompt: options.prompt },
      files: {
        ref_images: [
          { task_id: options.snapshot.task_id, name: options.snapshot.name },
          { task_id: options.character.task_id, name: options.character.name },
        ],
      },
    }),
  });
  if (!response.ok) {
    throw new Error(`创建任务失败：${response.status} ${await response.text()}`);
  }
  return response.json() as Promise<{ task_id: string }>;
}

async function waitForTask(taskId: string) {
  const started = Date.now();
  while (Date.now() - started < TASK_TIMEOUT_MS) {
    const response = await fetch(`/api/tasks/${taskId}`);
    if (!response.ok) throw new Error(`查询任务失败：${response.status}`);
    const task = await response.json();
    if (task.status === "success") return task;
    if (task.status === "failed" || task.status === "cancelled") {
      throw new Error(`任务${task.status === "failed" ? "失败" : "已取消"}`);
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
  }
  throw new Error("任务超时");
}

function taskResultUrl(task: {
  gpu_index?: number;
  instance_id?: number;
  result_files?: string;
}) {
  let files: { filename?: string; subfolder?: string }[] = [];
  try {
    files = JSON.parse(task.result_files || "[]");
  } catch {
    return null;
  }
  const first = files.find((file) => file.filename);
  if (!first?.filename) return null;
  const path = first.subfolder
    ? `${first.subfolder}/${first.filename}`
    : first.filename;
  // 优先走 /api/output：/api/instances/:id/output 是 307 跳转到 ComfyUI 的 /view，
  // 跨源重定向会让 fetch 直接抛 "Failed to fetch"（<img> 不受限，fetch 受限）。
  return `/api/output/${task.gpu_index ?? 0}/${path}`;
}

/** 泼溅存库时用的文件名：来源本来就是 .spz/.splat/.ksplat 就原样保留，其它按 .spz 交上去。 */
function splatFileName(name: string): string {
  const lower = name.toLowerCase();
  const keep = ["spz", "splat", "ksplat"].find((ext) => lower.endsWith(`.${ext}`));
  return keep ? name : `${name}.spz`;
}

function arrayBufferToBase64(buffer: ArrayBuffer) {
  const bytes = new Uint8Array(buffer);
  let text = "";
  // 分块拼接，避免 String.fromCharCode 在大缓冲区上爆栈。
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    text += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(text);
}

function base64ToArrayBuffer(text: string) {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

function normalizeObject(object: THREE.Object3D) {
  const box = new THREE.Box3().setFromObject(object);
  const size = box.getSize(new THREE.Vector3());
  const center = box.getCenter(new THREE.Vector3());
  const maxSize = Math.max(size.x, size.y, size.z) || 1;
  const scale = 2 / maxSize;
  object.position.set(-center.x * scale, -center.y * scale, -center.z * scale);
  object.scale.setScalar(scale);
  object.position.y += (size.y * scale) / 2;
}

export default function Scene3DStage({ exitTo = "/studio/draw" }: Scene3DStageProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<THREE.Scene | null>(null);
  const cameraRef = useRef<THREE.PerspectiveCamera | null>(null);
  const rendererRef = useRef<THREE.WebGLRenderer | null>(null);
  const controlsRef = useRef<OrbitControls | null>(null);
  const transformRef = useRef<TransformControls | null>(null);
  const objectsRef = useRef<SceneObjectEntry[]>([]);
  const historyRef = useRef(new ObjectHistory<SceneObjectEntry>(disposeSceneEntry));
  const animationRef = useRef<{ id: string; preview: AnimationPreview; pose: BonePoseNode | null; pathClock?: boolean } | null>(null);
  const cancelMoveRef = useRef<(() => void) | null>(null);
  const cancelCameraPreviewRef = useRef<(() => void) | null>(null);
  // Export owns only the synchronous render/copy window, never PNG encoding/upload.
  const exportSampleRef = useRef<(() => () => void) | null>(null);
  const [moveAnimationIndex, setMoveAnimationIndex] = useState(-1);
  const [animationState, setAnimationState] = useState<'stopped' | 'playing' | 'paused'>('stopped');
  const [animationIndex, setAnimationIndex] = useState(0);
  const scrubRef = useRef<{ session: PreviewPlayhead; id?: string; pose: BonePoseNode | null } | null>(null);
  // 逐镜走位播放期间对象被临时移动/摆骨：指纹按播放前基线计算，避免把预演当成未保存编辑。
  const motionBaselineRef = useRef(new Map<string, { position: THREE.Vector3; quaternion: THREE.Quaternion; pose: ReturnType<typeof serializeBonePose> }>());
  // 连续逐镜播放：rAF 推进时间轴、兜底定时器收尾、token 作废迟到的懒加载启动。
  const shotPlaybackRef = useRef<number | null>(null);
  const shotPlaybackTimerRef = useRef<number | null>(null);
  const shotPlaybackTokenRef = useRef(0);
  const shotVideoExportRef = useRef<AbortSignal | null>(null);
  const [previewFrame, setPreviewFrame] = useState(0);
  const stopAnimation = useCallback(() => {
    // 作废尚未落地的逐镜播放启动（模块是懒加载的），避免停止后迟到启动。
    shotPlaybackTokenRef.current += 1;
    if (scrubRef.current) {
      scrubRef.current.session.stop(); scrubRef.current = null; setPreviewFrame(0);
      const selected = objectsRef.current.find(e => e.id === selectedIdRef.current);
      if (selected && !selected.locked && selected.object.visible) transformRef.current?.attach(selected.object);
    }
    cancelCameraPreviewRef.current?.();
    cancelCameraPreviewRef.current = null;
    cancelMoveRef.current?.();
    cancelMoveRef.current = null;
    const active = animationRef.current;
    if (!active) return;
    active.preview.stop();
    animationRef.current = null;
    setAnimationState('stopped');
    const entry = objectsRef.current.find(e => e.id === active.id);
    if (entry && !entry.locked && entry.object.visible) transformRef.current?.attach(entry.object);
  }, []);
  const sceneEpochRef = useRef(0);
  const saveGuardRef = useRef(new SceneSaveGuard());
  const cleanOnRenderRef = useRef(true);
  const fingerprintRef = useRef<() => SceneFingerprint>(() => ({ structure: '', media: [], full: () => '' }));
  const [dirty, setDirty] = useState(false);
  const frameRef = useRef<FrameAspect>('16:9');
  const resizeFrameRef = useRef<() => void>(() => {});
  const [frameAspect, setFrameAspect] = useState<FrameAspect>('16:9');
  const [focal, setFocal] = useState(focalFromFov(45));
  const [archiveName, setArchiveName] = useState('');
  const [archiveProjectId, setArchiveProjectId] = useState('');
  const [archiveProjects, setArchiveProjects] = useState<ProjectChoice[]>([]);
  useEffect(() => {
    let active = true;
    void fetch('/api/projects').then(r => { if (!r.ok) throw new Error('读取项目失败'); return r.json(); })
      .then(data => { if (active) setArchiveProjects(projectChoices(data)); })
      .catch(err => { if (active) setError(String(err)); });
    return () => { active = false; };
  }, []);
  const selectedIdRef = useRef<string | null>(null);
  const lightsRef = useRef<{
    hemi: THREE.HemisphereLight;
    key: THREE.DirectionalLight;
    fill: THREE.DirectionalLight;
    rim: THREE.DirectionalLight;
  } | null>(null);
  const groundRef = useRef<THREE.Mesh | null>(null);
  const gridRef = useRef<THREE.GridHelper | null>(null);
  const groundPathGroupRef = useRef<THREE.Group | null>(null);
  // 环境（时间/天气/地形）：拾取与播放回调也在初始化 effect 的闭包里，同样用 ref 镜像。
  const environmentRef = useRef<SceneEnvironment | null>(null);
  const terrainMeshRef = useRef<THREE.Mesh | null>(null);
  const weatherEffectsRef = useRef<ReturnType<typeof createEnvironmentEffects> | null>(null);
  // 拾取回调在初始化 effect 的闭包里，读 state 会读到挂载时的旧值，用 ref 镜像。
  const placingGroundRef = useRef<"start" | "via" | "end" | null>(null);
  // 拖动微调：pointerdown 时记住动的是哪一格，pointermove 期间直接用闭包变量，
  // 不动 state（否则每帧一次 setState 会把渲染循环拖慢）。
  const draggingGroundRef = useRef<GroundSlot | null>(null);
  const moveRafRef = useRef<number | null>(null);
  const cameraPathRef = useRef<number | null>(null);
  // 播放兜底定时器：切到后台标签页时 rAF 会被浏览器暂停，靠它把「播放中…」收回来。
  const cameraPathTimerRef = useRef<number | null>(null);
  // 旧预览的 blob URL：换新图时释放，避免一直攒着。
  const referenceUrlRef = useRef<string | null>(null);
  const posesRef = useRef<Record<string, { a?: PoseSnapshot; b?: PoseSnapshot }>>({});
  const fileInputRef = useRef<HTMLInputElement>(null);
  const sceneInputRef = useRef<HTMLInputElement>(null);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const splatInputRef = useRef<HTMLInputElement>(null);
  const identityInputRef = useRef<HTMLInputElement>(null);
  /** 「AI 生成」下拉：点完动作要自动收起，否则面板会盖住画布。 */
  const aiMenuRef = useRef<HTMLDetailsElement>(null);
  const imagePlaneInputRef = useRef<HTMLInputElement>(null);
  const videoPlaneInputRef = useRef<HTMLInputElement>(null);
  const skyboxInputRef = useRef<HTMLInputElement>(null);
  const skyboxTextureRef = useRef<THREE.Texture | null>(null);
  const skyboxSourceRef = useRef<string | null>(null);
  const fkRef = useRef<FKHelper | null>(null);
  const sparkRef = useRef<ReturnType<typeof createSplatRenderer> | null>(null);
  // 初始化 effect 只跑一次，细节档位要读 ref 才能拿到最新值；改档位时直接改
  // SparkRenderer 实例上的 lodSplatCount，不用重建渲染器。（用常量初值，
  // 避免引用到下面才声明的 state 触发 TDZ。）
  const splatDetailRef = useRef<number | "auto">("auto");
  // 守护读写的当前档位与滞回状态，都放 ref 里，避免每帧依赖 state。
  const autoDetailRef = useRef<number>(SPLAT_DETAIL_PRESETS[SPLAT_DETAIL_AUTO_START].count);
  const splatGuardRef = useRef<SplatDetailGuardState>({
    index: SPLAT_DETAIL_AUTO_START,
    belowSince: null,
    aboveSince: null,
    cooldownUntil: 0,
  });
  // 是否还在等 Spark 建细节层（只置一次，onDirty 来第一发就收掉）。
  const splatLodPendingRef = useRef(false);
  const splatLodTimerRef = useRef<number | null>(null);
  // 画布指针事件挂在初始化 effect 里，读 ref 才能拿到最新的摆姿开关。
  const posingRef = useRef(false);

  const [objects, setObjects] = useState<SceneObjectEntry[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [mode, setMode] = useState<TransformMode>("translate");
  const [legacyBusy, setBusy] = useState<string | null>(null);
  const [operationBusy, setOperationBusy] = useState<string | null>(null);
  const operationsRef = useRef(new SceneOperations(setOperationBusy));
  const busy = operationBusy ?? legacyBusy;
  const [error, setError] = useState<string | null>(null);
  const [locationGenerationOpen, setLocationGenerationOpen] = useState(false);
  const [proceduralAssetsOpen, setProceduralAssetsOpen] = useState(false);
  const [status, setStatus] = useState("");
  const [lightPreset, setLightPreset] = useState<LightPresetName>("studio");
  // 场景环境：null = 未启用，沿用上面的灯光预设与 20×20 平地（旧存档行为）。
  const [environment, setEnvironment] = useState<SceneEnvironment | null>(null);
  // 地形生成参数：只影响「生成地形」这一次动作，不写进存档（存档存的是生成后的高度场）。
  const [terrainDraft, setTerrainDraft] = useState({ size: 80, amplitude: 6, seed: 41317 });
  const [material, setMaterial] = useState<MaterialSettings>(DEFAULT_MATERIAL);
  const [cameraKeys, setCameraKeys] = useState<CameraKey[]>([]);
  /** 剧本镜头表：可选字段，随场景存档一起保存；旧存档没有时保持 null。 */
  const [scriptShots, setScriptShots] = useState<import('./toonflowSets').ScriptShotPlan | null>(null);
  // 导出首尾帧时算出来的运镜文案：首尾帧只锚两端，中间怎么动得靠这句话写进提示词。
  const [firstLastNote, setFirstLastNote] = useState("");
  // 把运镜文案写进某一场的视频提示词：只追加，人点两次才动手（选项目/场次 + 追加）。
  const [promptPanelOpen, setPromptPanelOpen] = useState(false);
  const [promptProjects, setPromptProjects] = useState<ProjectChoice[]>([]);
  const [promptProjectId, setPromptProjectId] = useState("");
  const [promptScenes, setPromptScenes] = useState<PromptSceneChoice[]>([]);
  const [promptSceneId, setPromptSceneId] = useState("");
  const [promptBusy, setPromptBusy] = useState(false);
  // 相机 Agent：一句话 → 结构化运镜；缺参数就追问，出预览再采用（可撤销）。
  const [agentBrief, setAgentBrief] = useState("");
  const [agentIntent, setAgentIntent] = useState<CameraIntent | null>(null);
  const [agentAnswer, setAgentAnswer] = useState("");
  const [agentPlan, setAgentPlan] = useState<CameraPlan | null>(null);
  // 地面落点：先分别记，两个都有才算一条路径（只点 A 时不显示距离）。
  // vias 是追加的途经点（0–3 个），曲线会穿过每一个。
  const [groundPoints, setGroundPoints] = useState<GroundPoints>({ start: null, end: null });
  // 拾取回调在初始化 effect 的闭包里，读 state 会读到旧值：给个镜像。
  const groundPointsRef = useRef<GroundPoints>(groundPoints);
  groundPointsRef.current = groundPoints;
  // 正在等用户在画布上点哪一端；null = 没在选点。
  const [placingGround, setPlacingGround] = useState<"start" | "via" | "end" | null>(null);
  // 沿 A→B 移动的对象（按 id 指向场景对象）与走完全程的秒数。
  const [moverId, setMoverId] = useState<string | null>(null);
  const previousMoverRef = useRef<string | null>(null);
  useEffect(() => {
    stopAnimation();
    if (previousMoverRef.current !== moverId) setMoveAnimationIndex(-1);
    previousMoverRef.current = moverId;
  }, [moverId, stopAnimation]);
  const [moveSeconds, setMoveSeconds] = useState(3);
  const [playingMove, setPlayingMove] = useState(false);
  // 跟拍：播放/导出时相机整体随角色平移，构图不变，只是跟着人走。
  const [followMover, setFollowMover] = useState(false);
  // 自动避障：路径撞上模型就在 XZ 上绕开它（模型一挪就自动重算，不用手点拐点）。
  const [avoidObstacles, setAvoidObstacles] = useState(true);
  const avoidObstaclesRef = useRef(avoidObstacles);
  avoidObstaclesRef.current = avoidObstacles;
  // editorGroundPath 在渲染期被调用多次，不能在里面 setState：只记一个标记，画线 effect 里报告。
  const routeStarvedRef = useRef(false);
  // 环绕运镜的三个参数：转几圈、转完总几秒、收尾时抬高/压低多少度（0 = 平视环绕）。
  const [orbitTurns, setOrbitTurns] = useState(1);
  const [orbitSeconds, setOrbitSeconds] = useState(DEFAULT_ORBIT_SECONDS);
  const [orbitPitch, setOrbitPitch] = useState(DEFAULT_ORBIT_PITCH);
  // 希区柯克：推进到起始距离的多少倍（<1 推进）、几秒。
  const [dollyRange, setDollyRange] = useState(0.35);
  const [dollySeconds, setDollySeconds] = useState(DEFAULT_DOLLY_SECONDS);
  // 最近一张参考图：面板里直接预览，同时给下载按钮用。
  const [referenceShot, setReferenceShot] = useState<{
    pass: ReferencePass;
    url: string;
    bytes: number;
    width: number;
    height: number;
  } | null>(null);
  // 序列导出的帧率：8fps 够当视频参考，再高只是白白多渲。
  const [sequenceFps, setSequenceFps] = useState(DEFAULT_SEQUENCE_FPS);
  // 序列宽度：0 跟随视口；小图导出能少很多体积，当视频参考够用。
  const [sequenceWidth, setSequenceWidth] = useState(0);
  // 最近一次导出的序列帧（存素材库用，避免再渲一遍）。
  const sequenceFramesRef = useRef<{ pass: ReferencePass; stamp: string; blobs: Blob[]; fps: number; projectId: number | null; frameIds: number[] } | null>(null);
  const [playingPath, setPlayingPath] = useState(false);
  // 逐镜播放的当前镜下标与总镜数；null 表示没有在播放。
  const [shotPlayback, setShotPlayback] = useState<{ index: number; shots: number; line: string | null } | null>(null);
  const executionUnavailableRef = useRef(false);
  executionUnavailableRef.current = !!busy || !!shotPlayback;
  const [poseSlots, setPoseSlots] = useState<{ a: boolean; b: boolean }>({ a: false, b: false });
  const [poseBlend, setPoseBlend] = useState(0);
  const [transform, setTransform] = useState({ x: 0, y: 0, z: 0, rotateX: 0, rotateY: 0, rotateZ: 0, scale: 1, scaleX: 1, scaleY: 1, scaleZ: 1 });
  // 骨骼摆姿：posing 开启时画布点击用于选骨，主 gizmo 让位给骨骼 gizmo。
  const [posing, setPosing] = useState(false);
  const [posingBones, setPosingBones] = useState(0);
  const [activeBone, setActiveBone] = useState<string | null>(null);
  // 选中对象是否有骨骼（决定「骨骼摆姿」入口是否可用）。
  const [selectedHasSkeleton, setSelectedHasSkeleton] = useState(false);
  // 动作目录（Toonflow CC0）：按需懒加载，避免把 1.2 MB 动作数据静态塞进主视口包。
  const [animationCatalog, setAnimationCatalog] = useState<{ summary: CharacterAnimationCatalogSummary; entries: readonly CharacterAnimationCatalogEntry[] } | null>(null);
  const [animationCatalogId, setAnimationCatalogId] = useState("");
  const animationCatalogRef = useRef<typeof import("./toonflow/characterAnimations") | null>(null);
  // 身份迁移：3D 快照 + 角色主形象 → 多图编辑。
  const [identityPrompt, setIdentityPrompt] = useState(DEFAULT_IDENTITY_PROMPT);
  // 图生 3D 世界的抠图引擎，默认 RMBG-3.0 软 matte。
  const [splatMatteEngine, setSplatMatteEngine] = useState<SplatMatteEngine>("rmbg3");
  // 泼溅细节上限：凭 LoD 按屏幕尺寸挑 splat，而不是硬砍尾部。
  // 数字 = 用户手选的档位；"auto" = 交给帧率守护。
  const [splatDetail, setSplatDetail] = useState<number | "auto">("auto");
  /** 守护当前停在哪一档（只在自动模式下有意义的显示值） */
  const [autoDetail, setAutoDetail] = useState<number>(SPLAT_DETAIL_PRESETS[SPLAT_DETAIL_AUTO_START].count);
  // 3D 合成：全景天空盒与平面层设置。
  const [skybox, setSkybox] = useState<SkyboxSettings>(DEFAULT_SKYBOX);
  const [skyboxName, setSkyboxName] = useState<string | null>(null);
  const [planeSettings, setPlaneSettings] = useState(DEFAULT_PLANE_SETTINGS);
  // 素材库里能当背景的图片；不给文件选择器也能铺背景板/天空盒。
  const [backgroundImages, setBackgroundImages] = useState<LibraryImage[]>([]);
  const [backgroundImageId, setBackgroundImageId] = useState("");
  const [backgroundLoading, setBackgroundLoading] = useState(false);

  useEffect(() => {
    document.body.classList.add("studio-theme");
    document.body.style.overflow = "hidden";
    return () => {
      document.body.classList.remove("studio-theme");
      document.body.style.overflow = "";
    };
  }, []);

  const documentMetaRef = useRef('');
  documentMetaRef.current = JSON.stringify({ archiveName, frameAspect, lightPreset, skybox, skyboxName, cameraKeys, scriptShots, environment, groundPoints, moverId, moveSeconds, followMover, moveAnimationIndex });
  fingerprintRef.current = () => buildSceneFingerprint(
    { epoch: sceneEpochRef.current, meta: documentMetaRef.current },
    objectsRef.current.map(e => {
      const scrub = scrubRef.current?.id === e.id ? scrubRef.current : null;
      const motion = motionBaselineRef.current.get(e.id);
      return { id: e.id, name: e.name, p: (scrub ? scrub.session.position : motion ? motion.position : e.object.position)?.toArray(), q: (scrub ? scrub.session.quaternion : motion ? motion.quaternion : e.object.quaternion)?.toArray(), s: e.object.scale.toArray(), visible: e.object.visible, locked: !!e.locked, plane: e.plane, source: e.mediaSource, splat: e.splatUrl, pose: scrub ? scrub.pose : animationRef.current?.id === e.id ? animationRef.current.pose : motion ? motion.pose : serializeBonePose(e.object), initial: e.initialBonePose,
        // 动作资源也是场景内容：加入/替换动作必须触发未保存提示，否则重开会静默丢动作。
        clips: e.object.animations.map(clip => `${clip.uuid}:${clip.name}@${clip.duration}#${clip.tracks.length}`).join("|"),
        materials: (() => { const rows: unknown[] = []; e.object.traverse(o => { if (isPoseEditorHelper(o)) return; const m = (o as THREE.Mesh).material; if (m) for (const v of Array.isArray(m) ? m : [m]) rows.push(materialRevision(v as THREE.MeshStandardMaterial)); }); return rows; })() };
    }),
  );
  if (cleanOnRenderRef.current) { const fresh = fingerprintRef.current(); saveGuardRef.current.markSaved(fresh.full(), fresh); cleanOnRenderRef.current = false; }
  useEffect(() => {
    // 500ms 轮询只看结构 + 媒体来源，不重新拼接内嵌媒体正文。
    const timer = window.setInterval(() => setDirty(saveGuardRef.current.isDirtyState(fingerprintRef.current())), 500);
    const unload = (e: BeforeUnloadEvent) => { if (saveGuardRef.current.isDirty(fingerprintRef.current().full())) { e.preventDefault(); e.returnValue = ''; } };
    const leave = (e: MouseEvent) => { const a = (e.target as HTMLElement)?.closest('a[href]'); if (a && saveGuardRef.current.isDirty(fingerprintRef.current().full()) && !window.confirm('场景有未保存的修改，仍要离开？')) { e.preventDefault(); e.stopPropagation(); } };
    window.addEventListener('beforeunload', unload); document.addEventListener('click', leave, true);
    return () => { clearInterval(timer); window.removeEventListener('beforeunload', unload); document.removeEventListener('click', leave, true); };
  }, []);

  const syncObjects = useCallback(() => {
    setObjects([...objectsRef.current]);
  }, []);

  /**
   * 场景里挡路的模型：取每个对象在世界坐标下的包围盒，只留 XZ 两维。
   * 泼溅（.spz）不参与：它本身就是整片世界，绕开它等于没有路可走。
   */
  const collectGroundObstacles = (): GroundObstacle[] => {
    const list: GroundObstacle[] = [];
    for (const entry of objectsRef.current) {
      if (entry.splat || !entry.object.visible) continue;
      const box = new THREE.Box3().setFromObject(entry.object);
      if (box.isEmpty()) continue;
      list.push({ min: [box.min.x, box.min.z], max: [box.max.x, box.max.z] });
    }
    return list;
  };

  /**
   * 编辑器里真正要走的那条线：用户的 A/B/途经点 + 自动避障插的点。
   *
   * 渲染、预览、导出、面板上的长度都走这一个函数，四处才不会各算各的；
   * 存档仍旧只存 toGroundPath（用户点的点），模型一挪就能按新障碍重算。
   */
  const editorGroundPath = (points: GroundPoints): GroundPath | null => {
    const path = toGroundPath(points);
    if (!path || !avoidObstaclesRef.current) {
      routeStarvedRef.current = false;
      return path;
    }
    // 绕行预算用尽时这里是 true：路径会直穿模型，得让界面说一声（见下面画线的 effect）。
    const routed = routeAroundObstaclesDetailed(path, collectGroundObstacles());
    routeStarvedRef.current = routed.exhausted;
    return routed.path;
  };

  /**
   * 地面路径取样：渲染、预览、首尾帧与序列都走这一个入口。
   * 有地形时 Y 压到地形上，没有地形时与原来的插值完全一致（旧档不变）。
   */
  const groundPathPoint = (path: GroundPath, t: number): GroundPoint =>
    groundPathPointOnTerrain(environmentRef.current?.terrain ?? null, path, t);

  const selectObject = useCallback((id: string | null) => {
    stopAnimation();
    setAnimationIndex(0);
    selectedIdRef.current = id;
    setSelectedId(id);
    const transform = transformRef.current;
    const entry = objectsRef.current.find((item) => item.id === id);
    setPlaneSettings(normalizePlaneSettings(entry?.plane));

    // 骨骼摆姿只对当前选中的模型生效：换对象就换 FK 目标，换到没有骨骼的对象则退出摆姿。
    const skeleton = entry && !entry.locked && entry.object.visible ? hasSkeleton(entry.object) : false;
    setSelectedHasSkeleton(skeleton);
    const fk = fkRef.current;
    if (fk) {
      if (entry && skeleton && posingRef.current) {
        setPosingBones(fk.setTarget(entry.object) ? fk.boneCount : 0);
      } else {
        fk.clear();
        setPosingBones(0);
        setActiveBone(null);
      }
      if (!skeleton || !entry) {
        posingRef.current = false;
        setPosing(false);
      }
    }

    if (!transform) return;
    if (entry) {
      if (entry.locked || !entry.object.visible) transform.detach();
      else transform.attach(entry.object);
      // 选中的新对象自带材质，面板显示它的真实参数，避免滑块与画面不一致。
      entry.object.traverse((child) => {
        const mesh = child as THREE.Mesh;
        if (!mesh.isMesh) return;
        const materials = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
        const standard = materials.find((item) => item && "color" in item) as
          | THREE.MeshStandardMaterial
          | undefined;
        if (!standard) return;
        setMaterial({
          color: `#${standard.color.getHexString()}`,
          roughness: standard.roughness ?? DEFAULT_MATERIAL.roughness,
          metalness: standard.metalness ?? DEFAULT_MATERIAL.metalness,
          opacity: standard.opacity ?? 1,
        });
      });
      const poses = posesRef.current[entry.id] ?? {};
      setPoseSlots({ a: Boolean(poses.a), b: Boolean(poses.b) });
      setTransform({
        x: Number(entry.object.position.x.toFixed(2)),
        y: Number(entry.object.position.y.toFixed(2)),
        z: Number(entry.object.position.z.toFixed(2)),
        rotateX: THREE.MathUtils.radToDeg(entry.object.rotation.x),
        rotateZ: THREE.MathUtils.radToDeg(entry.object.rotation.z),
        scaleX: entry.object.scale.x, scaleY: entry.object.scale.y, scaleZ: entry.object.scale.z,
        rotateY: Math.round((entry.object.rotation.y * 180) / Math.PI),
        scale: Number(entry.object.scale.x.toFixed(2)),
      });
    } else {
      transform.detach();
    }
  }, []);

  const addObject = useCallback(
    (object: THREE.Object3D, name: string, dropPoint?: THREE.Vector3 | null) => {
      const scene = sceneRef.current;
      if (!scene) return;
      stopAnimation();
      normalizeObject(object);
      const group = new THREE.Group();
      group.name = name;
      group.add(object);
      group.animations = skeletalClips(group, object.animations);
      // 拖进来的资产落在鼠标指到的地面位置，而不是永远堆在原点。
      if (dropPoint) group.position.copy(dropPoint);
      scene.add(group);
      const entry: SceneObjectEntry = { id: crypto.randomUUID(), name, object: group };
      // 泼溅要留一份句柄：释放走 Spark 自己的 dispose，不能按几何/材质处理。
      const splatChild = object.userData.isSplat ? object : null;
      if (splatChild) entry.splat = splatChild;
      // 带骨骼的模型（Mixamo 角色等）记一份绑定姿势，供摆姿与「恢复初始」使用。
      if (hasSkeleton(group)) {
        const bindPose = serializeBonePose(group);
        entry.bonePose = bindPose;
        entry.initialBonePose = bindPose;
      }
      historyRef.current.checkpoint(objectsRef.current);
      objectsRef.current = [...objectsRef.current, entry];
      historyRef.current.collect(objectsRef.current);
      syncObjects();
      selectObject(entry.id);
      setStatus(`已加载 ${name}`);
      // 返回 entry：泼溅要把来源地址记下来，存档时才好把字节取回来内嵌。
      return entry;
    },
    [selectObject, syncObjects],
  );

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const scene = new THREE.Scene();
    scene.background = new THREE.Color("#121316");
    sceneRef.current = scene;

    const camera = new THREE.PerspectiveCamera(
      45,
      FRAME_ASPECTS[frameRef.current],
      0.1,
      1000,
    );
    camera.position.set(4.5, 3.2, 6);
    cameraRef.current = camera;

    const renderer = new THREE.WebGLRenderer({
      antialias: true,
      preserveDrawingBuffer: true,
    });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    const initialFrame = fitFrame(container.clientWidth, container.clientHeight, frameRef.current);
    renderer.setSize(initialFrame.width, initialFrame.height);
    Object.assign(renderer.domElement.style, { position: 'absolute', left: '50%', top: '50%', transform: 'translate(-50%, -50%)' });
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    container.appendChild(renderer.domElement);
    rendererRef.current = renderer;

    // Spark 负责把高斯泼溅光栅化；不加进场景，SplatMesh 只是数据不会被画出来。
    const spark = createSplatRenderer(renderer, {
      lodSplatCount: typeof splatDetailRef.current === "number" ? splatDetailRef.current : autoDetailRef.current,
      onDirty: () => {
        // onDirty 一帧可能触发多次，只有确实挂着“建细节层”提示时才动 state，
        // 免得每帧都 setState 把渲染循环拖慢。
        if (!splatLodPendingRef.current) return;
        splatLodPendingRef.current = false;
        setBusy((current) => (current === SPLAT_LOD_BUSY ? null : current));
      },
    });
    spark.name = "__SparkRenderer__";
    scene.add(spark);
    sparkRef.current = spark;

    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.set(0, 1, 0);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controlsRef.current = controls;

    const transform = new TransformControls(camera, renderer.domElement);
    transform.setSize(0.9);
    transform.addEventListener("dragging-changed", (event) => {
      controls.enabled = !event.value;
      if (event.value) historyRef.current.checkpoint(objectsRef.current);
      else { historyRef.current.collect(objectsRef.current); selectObject(selectedIdRef.current); }
    });
    transform.setTranslationSnap(0.1);
    transform.setRotationSnap(THREE.MathUtils.degToRad(15));
    transform.setScaleSnap(0.1);
    scene.add(transform.getHelper());
    transformRef.current = transform;

    // 骨骼摆姿：红球标骨 + 局部旋转 gizmo，拖动时暂停轨道控制。
    const fk = new FKHelper({
      camera,
      domElement: renderer.domElement,
      scene,
      onDragChange: (dragging) => {
        controls.enabled = !dragging;
      },
      onBoneSelected: (boneName) => setActiveBone(boneName),
    });
    fkRef.current = fk;

    // 点击选骨：拖动超过阈值算转相机，不当作点选，避免摆姿势时误切骨。
    // 落点微调：按在 A/B/途经点的圆盘上就是拖它，期间锁住轨道控制（否则拖点顺手转视角）。
    let pointerStart: { x: number; y: number } | null = null;
    const ndcOf = (event: PointerEvent | MouseEvent) => {
      const rect = renderer.domElement.getBoundingClientRect();
      if (!rect.width || !rect.height) return null;
      return {
        ndc: new THREE.Vector2(
          ((event.clientX - rect.left) / rect.width) * 2 - 1,
          -((event.clientY - rect.top) / rect.height) * 2 + 1,
        ),
        raycaster: new THREE.Raycaster(),
      };
    };
    // 射线打地面：先打 20×20 的地面网格，打空就退回无限平面（远处也能落点）。
    // 射线朝天（地平线以上）时两条路都没有交点，返回 null。
    const groundPointOf = (event: PointerEvent | MouseEvent): GroundPoint | null => {
      const hit = ndcOf(event);
      if (!hit) return null;
      hit.raycaster.setFromCamera(hit.ndc, camera);
      // 有地形时先打地形网格：落点要贴在地面上，而不是穿过山体的 y=0 平面。
      const terrainMesh = terrainMeshRef.current;
      const onTerrain = terrainMesh ? hit.raycaster.intersectObject(terrainMesh, false)[0] : undefined;
      const onGround = onTerrain ?? hit.raycaster.intersectObject(ground, false)[0];
      const onPlane = onGround
        ? onGround.point
        : hit.raycaster.ray.intersectPlane(
            new THREE.Plane(new THREE.Vector3(0, 1, 0), 0),
            new THREE.Vector3(),
          );
      if (!onPlane) return null;
      const terrain = environmentRef.current?.terrain ?? null;
      // 没有地形：保持原来的平地落点（XZ 钳到 ±9.5，y=0.02）。
      if (!terrain) return clampGroundPoint([onPlane.x, 0.02, onPlane.z]);
      return clampGroundPointToTerrain(terrain, [onPlane.x, onPlane.y, onPlane.z]);
    };
    const onPointerDown = (event: PointerEvent) => {
      pointerStart = { x: event.clientX, y: event.clientY };
      draggingGroundRef.current = null;
      // 正在落点时不抢拖动：否则想点空白处落点会先被标记圆盘吃掉。
      if (placingGroundRef.current) return;
      const group = groundPathGroupRef.current;
      if (!group || group.children.length === 0) return;
      const hit = ndcOf(event);
      if (!hit) return;
      hit.raycaster.setFromCamera(hit.ndc, camera);
      const marker = hit.raycaster
        .intersectObjects(group.children, false)
        .find((item) => item.object.userData?.groundSlot);
      const slot = marker?.object.userData?.groundSlot as GroundSlot | undefined;
      if (!slot) return;
      draggingGroundRef.current = slot;
      pointerStart = null;
      controls.enabled = false;
      renderer.domElement.setPointerCapture?.(event.pointerId);
    };
    const onPointerMove = (event: PointerEvent) => {
      const slot = draggingGroundRef.current;
      if (!slot) return;
      const point = groundPointOf(event);
      if (!point) return;
      setGroundPoints((current) => moveGroundPoint(current, slot, point));
    };
    const onPointerUp = (event: PointerEvent) => {
      const start = pointerStart;
      pointerStart = null;
      const slot = draggingGroundRef.current;
      if (slot) {
        draggingGroundRef.current = null;
        controls.enabled = true;
        renderer.domElement.releasePointerCapture?.(event.pointerId);
        const point = groundPointOf(event);
        if (point) {
          setStatus(
            `${slot.kind === "start" ? "A" : slot.kind === "end" ? "B" : `途经点 ${slot.index + 1}`} 已移动到（${point[0].toFixed(1)}, ${point[2].toFixed(1)}）`,
          );
        }
        return;
      }
      const placing = placingGroundRef.current;
      if (!start || (!placing && !posingRef.current)) return;
      const moved = Math.hypot(event.clientX - start.x, event.clientY - start.y);
      // 拖过 4px 算转相机，不当作点选，免得转视角时误落点。
      if (moved > 4) return;
      if (placing) {
        // 只对地面做拾取：模型身上的点击不该变成落点。地面网格只有 20×20，
        // 点远处会打空（用户会以为「点了没反应」），所以再退回无限平面，
        // 越界由 clampGroundPoint 拉回地面范围内。
        const point = groundPointOf(event);
        // 射线朝天（地平线以上）时两条路都没有交点，不落点。
        if (!point) return;
        placingGroundRef.current = null;
        setPlacingGround(null);
        if (placing === "via") {
          // 途经点是追加：连点几下就能拉出「绕桌子再避开柱子」的多段走位。
          const taken = groundPointsRef.current?.vias?.length ?? 0;
          if (taken >= MAX_GROUND_VIAS) {
            setStatus(`途经点最多 ${MAX_GROUND_VIAS} 个，想改就「撤销」或「清除途经」`);
            return;
          }
          setGroundPoints((current) => ({
            ...current,
            vias: [...(current.vias ?? []), point].slice(0, MAX_GROUND_VIAS),
          }));
          setStatus(
            `途经点 ${Math.min(taken + 1, MAX_GROUND_VIAS)} 已记录（${point[0].toFixed(1)}, ${point[2].toFixed(1)}）`,
          );
          return;
        }
        setGroundPoints((current) => ({ ...current, [placing]: point }));
        setStatus(
          `${placing === "start" ? "A" : "B"} 已记录（${point[0].toFixed(1)}, ${point[2].toFixed(1)}）`,
        );
        return;
      }
      const ndc = ndcOf(event)?.ndc;
      if (ndc) setActiveBone(fk.handleClick(ndc));
    };
    renderer.domElement.addEventListener("pointerdown", onPointerDown);
    renderer.domElement.addEventListener("pointermove", onPointerMove);
    renderer.domElement.addEventListener("pointerup", onPointerUp);
    renderer.domElement.addEventListener("pointercancel", onPointerUp);

    const grid = new THREE.GridHelper(20, 40, 0x4b5563, 0x252a33);
    scene.add(grid);

    const ground = new THREE.Mesh(
      new THREE.PlaneGeometry(20, 20),
      new THREE.MeshStandardMaterial({ color: 0x1a1d23, roughness: 1 }),
    );
    ground.rotation.x = -Math.PI / 2;
    ground.position.y = -0.001;
    scene.add(ground);

    scene.add(new THREE.HemisphereLight(0xffffff, 0x334155, 1.4));
    const key = new THREE.DirectionalLight(0xffffff, 2.2);
    key.position.set(5, 8, 4);
    scene.add(key);
    const fill = new THREE.DirectionalLight(0x88aaff, 0.7);
    fill.position.set(-5, 3, -4);
    scene.add(fill);
    const rim = new THREE.DirectionalLight(0xffffff, 0.4);
    rim.position.set(-3, 6, -8);
    scene.add(rim);
    lightsRef.current = {
      hemi: scene.children.find((child) => child instanceof THREE.HemisphereLight) as THREE.HemisphereLight,
      key,
      fill,
      rim,
    };
    groundRef.current = ground;
    gridRef.current = grid;
    // A/B 落点标记单独一层：导出参考图时整层隐藏，它不是要被约束的几何。
    const groundPathGroup = new THREE.Group();
    groundPathGroup.name = "__GroundPath__";
    scene.add(groundPathGroup);
    groundPathGroupRef.current = groundPathGroup;

    let frame = 0;
    // 帧率守护：每秒统计一次，只有处在「自动」时才真正改档。
    let guardFrames = 0;
    let guardSince = performance.now();
    let animationTime = performance.now();
    const animate = () => {
      frame = requestAnimationFrame(animate);
      guardFrames += 1;
      const now = performance.now();
      weatherEffectsRef.current?.sampleAt(now / 1000);
      const active = animationRef.current;
      if (active) {
        const entry = objectsRef.current.find(e => e.id === active.id);
        if (!entry || entry.locked || !entry.object.visible) stopAnimation();
        else if (!active.pathClock) active.preview.update(Math.min((now - animationTime) / 1000, 0.1));
      }
      animationTime = now;
      if (now - guardSince >= 1_000) {
        const fps = (guardFrames * 1_000) / (now - guardSince);
        guardFrames = 0;
        guardSince = now;
        if (splatDetailRef.current === "auto") {
          const next = stepSplatDetail(
            splatGuardRef.current,
            fps,
            now,
            SPLAT_DETAIL_PRESETS.length - 1,
          );
          splatGuardRef.current = next;
          const count = SPLAT_DETAIL_PRESETS[next.index].count;
          if (count !== autoDetailRef.current) {
            autoDetailRef.current = count;
            spark.lodSplatCount = count;
            setAutoDetail(count);
          }
        }
      }
      controls.update();
      // 红球按相机距离定大小，每帧跟一次（22 根骨，开销可忽略）。
      // 注意是 fkRef（FKHelper 实例），posingRef 只是摆姿开关的布尔值。
      fkRef.current?.updateCameraScale();
      // Empty Spark still traverses the entire scene several times in onBeforeRender.
      // Keep the original path for ANY registered splat, including hidden/loading entries.
      spark.visible = objectsRef.current.some(entry => !!entry.splat);
      renderer.render(scene, camera);
    };
    animate();

    resizeFrameRef.current = () => {
      // The video session owns drawing-buffer size until its finally releases it.
      if (shotVideoExportRef.current) return;
      const { width, height, aspect } = fitFrame(container.clientWidth || 1, container.clientHeight || 1, frameRef.current);
      camera.aspect = aspect;
      camera.updateProjectionMatrix();
      renderer.setSize(width, height);
    };
    const observer = new ResizeObserver(() => resizeFrameRef.current());
    observer.observe(container);

    return () => {
      cancelAnimationFrame(frame);
      cancelCameraPreviewRef.current?.();
      scrubRef.current?.session.stop(); scrubRef.current = null;
      cancelCameraPreviewRef.current = null;
      cancelMoveRef.current?.();
      cancelMoveRef.current = null;
      animationRef.current?.preview.stop();
      animationRef.current = null;
      observer.disconnect();
      renderer.domElement.removeEventListener("pointerdown", onPointerDown);
      renderer.domElement.removeEventListener("pointermove", onPointerMove);
      renderer.domElement.removeEventListener("pointerup", onPointerUp);
      renderer.domElement.removeEventListener("pointercancel", onPointerUp);
      fk.dispose();
      fkRef.current = null;
      // 卸载时把视频平面的解码与全景纹理一起收掉，否则离开页面后仍在后台跑。
      sceneEpochRef.current++;
      weatherEffectsRef.current?.dispose(); weatherEffectsRef.current = null;
      if (terrainMeshRef.current) {
        scene.remove(terrainMeshRef.current); disposeEnvironmentTerrainMesh(terrainMeshRef.current); terrainMeshRef.current = null;
      }
      sceneRef.current = null;
      historyRef.current.clear(objectsRef.current);
      objectsRef.current.forEach(entry => { scene.remove(entry.object); disposeSceneEntry(entry); });
      skyboxTextureRef.current?.dispose();
      skyboxTextureRef.current = null;
      sparkRef.current?.dispose?.();
      sparkRef.current = null;
      transform.dispose();
      controls.dispose();
      renderer.dispose();
      container.removeChild(renderer.domElement);
      scene.traverse((child) => {
        const mesh = child as THREE.Mesh;
        mesh.geometry?.dispose();
        const material = mesh.material;
        if (Array.isArray(material)) material.forEach((item) => item.dispose());
        else material?.dispose();
      });
    };
  }, []);

  // 细节档位只在初始化时传给 SparkRenderer 一次，之后靠实例属性热改。
  // 落点标记：绿=A（起点）、橙=B（终点），两点之间拉一条虚线。
  useEffect(() => {
    const scene = sceneRef.current;
    if (!scene) return;
    let group = groundPathGroupRef.current;
    if (!group) {
      group = new THREE.Group();
      group.name = "__GroundPath__";
      scene.add(group);
      groundPathGroupRef.current = group;
    }
    // 每次重建：落点最多两个，重建比逐帧改属性简单也不会有残留。
    for (const child of group.children.slice()) {
      group.remove(child);
      const mesh = child as THREE.Mesh;
      mesh.geometry?.dispose();
      const material = mesh.material;
      if (Array.isArray(material)) material.forEach((item) => item.dispose());
      else material?.dispose();
    }
    const ring = (point: GroundPoint, color: number, slot: GroundSlot) => {
      const marker = new THREE.Mesh(
        new THREE.RingGeometry(0.26, 0.36, 32),
        new THREE.MeshBasicMaterial({ color, side: THREE.DoubleSide, depthWrite: false }),
      );
      marker.rotation.x = -Math.PI / 2;
      marker.position.set(point[0], point[1] + 0.004, point[2]);
      marker.renderOrder = 2;
      group?.add(marker);
      // 拖动微调的命中盘：环本身太细，按不准。用半径 0.55 的透明圆盘当把手，
      // 全透明但要能被射线打中（visible=false 会被 raycast 跳过，所以走 opacity 0）。
      const handle = new THREE.Mesh(
        new THREE.CircleGeometry(0.55, 24),
        new THREE.MeshBasicMaterial({ transparent: true, opacity: 0, depthWrite: false }),
      );
      handle.rotation.x = -Math.PI / 2;
      handle.position.set(point[0], point[1] + 0.006, point[2]);
      handle.userData.groundSlot = slot;
      group?.add(handle);
      // 竖杆让落点在俯视/远距离下也能看见。
      const pole = new THREE.Mesh(
        new THREE.CylinderGeometry(0.02, 0.02, 0.9, 8),
        new THREE.MeshBasicMaterial({ color }),
      );
      pole.position.set(point[0], point[1] + 0.45, point[2]);
      group?.add(pole);
    };
    if (groundPoints.start) ring(groundPoints.start, 0x22c55e, { kind: "start" });
    if (groundPoints.end) ring(groundPoints.end, 0xf59e0b, { kind: "end" });
    // 途经点：蓝色，路径会穿过每一个（最多 MAX_GROUND_VIAS 个）。
    (groundPoints.vias ?? []).forEach((point, index) => ring(point, 0x38bdf8, { kind: "via", index }));
    const path = editorGroundPath(groundPoints);
    if (path) {
      // 有途经点就沿曲线采样，没有就是一条直线；虚线长度由 computeLineDistances 算。
      const samples = path.vias?.length ? 32 * path.vias.length : 1;
      const points: THREE.Vector3[] = [];
      for (let index = 0; index <= samples; index += 1) {
        const at = groundPathPoint(path, index / samples);
        points.push(new THREE.Vector3(at[0], at[1] + 0.02, at[2]));
      }
      const geometry = new THREE.BufferGeometry().setFromPoints(points);
      const line = new THREE.Line(
        geometry,
        new THREE.LineDashedMaterial({ color: 0x94a3b8, dashSize: 0.18, gapSize: 0.12 }),
      );
      line.computeLineDistances();
      group.add(line);
    }
    // 绕行预算用尽：路径这一段会从模型身上穿过去，界面必须说清楚，否则只能靠盯预览发现。
    if (routeStarvedRef.current) {
      setStatus("自动避障绕不过去：有一小段会直接穿过模型——删掉挡路的模型，或再加一个途经点。");
    }
  }, [groundPoints, avoidObstacles]);

  useEffect(() => {
    splatDetailRef.current = splatDetail;
    const spark = sparkRef.current;
    if (!spark) return;
    spark.lodSplatCount = splatDetail === "auto" ? autoDetailRef.current : splatDetail;
  }, [splatDetail]);

  useEffect(() => {
    const transform = transformRef.current;
    if (transform) transform.setMode(mode);
  }, [mode]);

  const loadFile = async (file: File) => {
    const operation = operationsRef.current.start(`正在加载 ${file.name}…`);
    setError(null);
    setBusy(`正在加载 ${file.name}…`);
    const epoch = sceneEpochRef.current;
    try {
      const extension = file.name.split(".").pop()?.toLowerCase();
      if (extension === "obj") {
        const text = await file.text();
        const parsed = new OBJLoader().parse(text);
        if (epoch !== sceneEpochRef.current || !sceneRef.current) { disposeSceneEntry({ id: '', name: '', object: parsed }); return; }
        addObject(parsed, file.name);
      } else {
        const buffer = await file.arrayBuffer();
        const gltf = await new GLTFLoader().parseAsync(buffer, "");
        if (epoch !== sceneEpochRef.current || !sceneRef.current) { disposeSceneEntry({ id: '', name: '', object: gltf.scene }); return; }
        gltf.scene.animations = gltf.animations;
        addObject(gltf.scene, file.name);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      operation.finish(); setBusy(null);
    }
  };

  const loadUrl = async (url: string, name: string) => {
    const epoch = sceneEpochRef.current;
    const gltf = await new GLTFLoader().loadAsync(url);
    if (epoch !== sceneEpochRef.current || !sceneRef.current) { disposeSceneEntry({ id: '', name: '', object: gltf.scene }); return; }
    gltf.scene.animations = gltf.animations;
    addObject(gltf.scene, name);
  };

  // 泼溅：Spark 自己解码，拿到 mesh 后交给 addObject 统一做归一化与列表登记。
  // 贴地交给 normalizeObject，这里不再重复摆一次。
  // 挂忙态 + 起一个兜底计时器：两条载入路径共用，免得各自复制一份。
  const beginSplatLodWait = useCallback(() => {
    splatLodPendingRef.current = true;
    setBusy(SPLAT_LOD_BUSY);
    if (splatLodTimerRef.current !== null) window.clearTimeout(splatLodTimerRef.current);
    splatLodTimerRef.current = window.setTimeout(() => {
      splatLodPendingRef.current = false;
      setBusy((current) => (current === SPLAT_LOD_BUSY ? null : current));
    }, SPLAT_LOD_TIMEOUT_MS);
  }, []);

  const loadSplatUrl = useCallback(
    async (url: string, name: string, dropPoint?: THREE.Vector3 | null) => {
      const epoch = sceneEpochRef.current;
      const mesh = await loadSplatMesh(url, { placeOnGround: false });
      if (epoch !== sceneEpochRef.current || !sceneRef.current) { disposeSplat(mesh); if (url.startsWith('blob:')) URL.revokeObjectURL(url); return; }
      const entry = addObject(mesh, name, dropPoint);
      // 存下来源地址，场景存档时按它把 .spz 字节取回来内嵌。
      if (entry) entry.splatUrl = url;
      // LoD 树要建完才能画第一帧，这几秒挂上提示，等 SparkRenderer.onDirty 来收。
      beginSplatLodWait();
    },
    [addObject, beginSplatLodWait],
  );

  // 从素材库取资产放进来：glb/gltf 直接加载，obj 要取文本再解析，泼溅交给 Spark 解码。
  const loadLibraryAsset = useCallback(
    async (asset: LibraryAsset, dropPoint?: THREE.Vector3 | null) => {
      const kind = assetKind(asset.path || asset.name);
      setError(null);
      if (kind === "unsupported") {
        setError(`暂不支持这个格式，请先转成 GLB 再放进场景：${asset.name}`);
        return;
      }
      const operation = operationsRef.current.start(`正在载入 ${asset.name}…`);
      setBusy(`正在载入 ${asset.name}…`);
      const epoch = sceneEpochRef.current;
      const commit = (object: THREE.Object3D) => { if (epoch !== sceneEpochRef.current || !sceneRef.current) { disposeSceneEntry({ id: '', name: '', object }); return; } return addObject(object, asset.name, dropPoint); };
      try {
        const url = assetUrl(asset.path);
        if (kind === "splat") {
          // 泼溅不能走 GLTFLoader（.spz 不是 glTF）；贴地与归一化仍由 addObject 统一做。
          await loadSplatUrl(url, asset.name, dropPoint);
        } else if (kind === "obj") {
          const response = await fetch(url);
          if (!response.ok) throw new Error(`读取素材失败（HTTP ${response.status}）`);
          commit(new OBJLoader().parse(await response.text()));
        } else {
          const gltf = await new GLTFLoader().loadAsync(url);
          gltf.scene.animations = gltf.animations;
          commit(gltf.scene);
        }
        setStatus(dropPoint ? `已放入 ${asset.name}` : `已放入 ${asset.name}（原点）`);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        operation.finish();
        // 泼溅的 busy 归 SparkRenderer.onDirty 管，这里别提前抹掉。
        setBusy((current) => (current === SPLAT_LOD_BUSY ? current : null));
      }
    },
    [addObject, loadSplatUrl],
  );

  // 把拖放点的屏幕坐标投到 y=0 地面上，得到摆放位置。
  const dropPointFromEvent = useCallback((event: DragEvent) => {
    const renderer = rendererRef.current;
    const camera = cameraRef.current;
    if (!renderer || !camera) return null;
    const rect = renderer.domElement.getBoundingClientRect();
    if (!rect.width || !rect.height) return null;
    const ndc = new THREE.Vector2(
      ((event.clientX - rect.left) / rect.width) * 2 - 1,
      -((event.clientY - rect.top) / rect.height) * 2 + 1,
    );
    const raycaster = new THREE.Raycaster();
    raycaster.setFromCamera(ndc, camera);
    // 有地形时把地形网格也当落点目标：拖进来的物件应站在坡上，而不是穿到 y=0。
    return surfaceDrop(raycaster, objectsRef.current, terrainMeshRef.current);
  }, []);

  // 图片/视频平面层：当背景板或参考板用，参与 3D 遮挡。
  const addPlane = useCallback(
    async (url: string, name: string, kind: MediaKind, dropPoint?: THREE.Vector3 | null, settings: PlaneSettings = DEFAULT_PLANE_SETTINGS, detached = false) => {
      const scene = sceneRef.current;
      if (!scene) return;
      const epoch = sceneEpochRef.current;
      const operation = detached ? null : operationsRef.current.start(`正在载入 ${name}…`);
      if (!detached) { setBusy(`正在载入 ${name}…`); setError(null); }
      let decodingVideo: HTMLVideoElement | null = null;
      let completed = false;
      try {
        const source = detached ? url : await mediaDataURL(url);
        let texture: THREE.Texture;
        let aspect = DEFAULT_PLANE_ASPECT;
        let video: SceneObjectEntry["video"] = null;

        if (kind === "video") {
          const element = document.createElement("video");
          decodingVideo = element;
          element.src = source;
          element.loop = true;
          element.muted = true;
          element.playsInline = true;
          element.crossOrigin = "anonymous";
          await new Promise<void>((resolve, reject) => {
            element.onloadedmetadata = () => resolve();
            element.onerror = () => reject(new Error("视频无法解码（浏览器不支持这个编码）"));
          });
          aspect = element.videoWidth / Math.max(1, element.videoHeight);
          // 自动播放可能被浏览器拦下，此时保留首帧静态效果，不当作失败。
          await element.play().catch(() => undefined);
          const videoTexture = new THREE.VideoTexture(element);
          videoTexture.colorSpace = THREE.SRGBColorSpace;
          texture = videoTexture;
          video = { element, texture: videoTexture };
        } else {
          texture = await new THREE.TextureLoader().loadAsync(source);
          texture.colorSpace = THREE.SRGBColorSpace;
          const image = texture.image as { naturalWidth?: number; naturalHeight?: number; width?: number; height?: number };
          const width = image.naturalWidth ?? image.width ?? 1;
          const height = image.naturalHeight ?? image.height ?? 1;
          aspect = width / Math.max(1, height);
        }

        if (!detached && (epoch !== sceneEpochRef.current || !sceneRef.current)) { texture.dispose(); return; }
        const { width, height } = planeSize(aspect);
        const normalizedSettings = normalizePlaneSettings(settings);
        // 图片已经解码进纹理，blob URL 可以立刻释放；视频要等删除对象时再收。
        if (kind === "image" && url.startsWith("blob:")) URL.revokeObjectURL(url);
        const material = planeMaterial(texture, normalizedSettings);
        const mesh = new THREE.Mesh(new THREE.PlaneGeometry(width, height), material);
        mesh.name = name;
        const group = new THREE.Group();
        group.name = name;
        group.add(mesh);
        // 默认立在原点前方 3 米，拖进来则落在鼠标位置、底边贴地。
        if (dropPoint) group.position.set(dropPoint.x, dropPoint.y + height / 2, dropPoint.z);
        else group.position.set(0, height / 2, -3);
        if (!detached) scene.add(group);

        const entry: SceneObjectEntry = {
          id: crypto.randomUUID(),
          name,
          object: group,
          mediaKind: kind,
          plane: normalizedSettings,
          mediaSource: source,
          video,
        };
        if (!detached) {
          historyRef.current.checkpoint(objectsRef.current);
          objectsRef.current = [...objectsRef.current, entry];
          historyRef.current.collect(objectsRef.current);
          syncObjects();
          selectObject(entry.id);
          setStatus(`已加入${kind === "video" ? "视频" : "图片"}平面 ${name}`);
        }
        completed = true;
        return entry;
      } catch (err) {
        if (detached) throw err;
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!completed && decodingVideo) { decodingVideo.pause(); decodingVideo.removeAttribute("src"); decodingVideo.load(); }
        if (url.startsWith("blob:")) URL.revokeObjectURL(url);
        operation?.finish();
        if (!detached) setBusy(null);
      }
    },
    [selectObject, syncObjects],
  );

  // 只对选中的平面层生效：透明度、单双面、透明裁切。
  const applyPlaneSettings = useCallback(
    (patch: Partial<PlaneSettings>) => {
      const id = selectedIdRef.current;
      const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
      if (!entry?.mediaKind) return;
      const next = normalizePlaneSettings({ ...entry.plane, ...patch });
      entry.plane = next;
      setPlaneSettings(next);
      entry.object.traverse((child) => {
        const mesh = child as THREE.Mesh;
        const material = mesh.material as THREE.MeshBasicMaterial | undefined;
        if (!mesh.isMesh || !material || !("map" in material)) return;
        mesh.material = planeMaterial(material.map!, next);
        material.dispose();
      });
    },
    [],
  );

  // 等距柱状全景图当背景与环境光照。
  const applySkybox = useCallback(
    async (url: string, name: string) => {
      const scene = sceneRef.current;
      if (!scene) return;
      const epoch = sceneEpochRef.current;
      setBusy(`正在载入全景图 ${name}…`);
      setError(null);
      try {
        const source = await mediaDataURL(url);
        const texture = await new THREE.TextureLoader().loadAsync(source);
        if (epoch !== sceneEpochRef.current || !sceneRef.current) { texture.dispose(); return; }
        if (url.startsWith("blob:")) URL.revokeObjectURL(url);
        skyboxSourceRef.current = source;
        texture.mapping = THREE.EquirectangularReflectionMapping;
        texture.colorSpace = THREE.SRGBColorSpace;
        skyboxTextureRef.current?.dispose();
        skyboxTextureRef.current = texture;
        scene.background = texture;
        scene.backgroundIntensity = skybox.intensity;
        scene.environment = skybox.asEnvironment ? texture : null;
        setSkybox((current) => ({ ...current, enabled: true }));
        setSkyboxName(name);
        setStatus(`已设置天空盒 ${name}`);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setBusy(null);
      }
    },
    [skybox.asEnvironment, skybox.intensity],
  );

  const clearSkybox = useCallback(() => {
    const scene = sceneRef.current;
    if (!scene) return;
    scene.environment = null;
    skyboxTextureRef.current?.dispose();
    skyboxTextureRef.current = null;
    // 背景回到当前灯光预设的底色；环境启用时天色归环境管，预设不能把它顶掉。
    const environment = environmentRef.current;
    scene.background = environment
      ? new THREE.Color(environmentLight(environment).background)
      : new THREE.Color(LIGHT_PRESETS[lightPreset].background);
    scene.backgroundIntensity = 1;
    setSkybox((current) => ({ ...current, enabled: false }));
    setSkyboxName(null);
    skyboxSourceRef.current = null;
    setStatus("已清除天空盒");
  }, [lightPreset]);

  const setSkyboxIntensity = useCallback((value: number) => {
    setSkybox((current) => ({ ...current, intensity: value }));
    const scene = sceneRef.current;
    if (scene && skyboxTextureRef.current) scene.backgroundIntensity = value;
  }, []);

  const setSkyboxAsEnvironment = useCallback((enabled: boolean) => {
    setSkybox((current) => ({ ...current, asEnvironment: enabled }));
    const scene = sceneRef.current;
    if (!scene || !skyboxTextureRef.current) return;
    scene.environment = enabled ? skyboxTextureRef.current : null;
  }, []);

  // 从素材库拉一页图片当背景候选：只读 /api/materials?type=image，不另存副本。
  const loadBackgroundImages = useCallback(async () => {
    setBackgroundLoading(true);
    try {
      const response = await fetch("/api/materials?type=image&page=1&page_size=40");
      if (!response.ok) throw new Error(`读取素材库失败（HTTP ${response.status}）`);
      const options = imageMaterialOptions(await response.json());
      setBackgroundImages(options);
      if (options.length === 0) {
        setStatus("素材库里还没有图片素材");
        setBackgroundImageId("");
      } else {
        setBackgroundImageId((current) => (findImage(options, current) ? current : String(options[0].id)));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBackgroundLoading(false);
    }
  }, []);

  const applyBackgroundAsPlane = useCallback(() => {
    const image = findImage(backgroundImages, backgroundImageId);
    if (!image) {
      setError("先在下拉里选一张素材库图片");
      return;
    }
    const url = materialUrl(image.path);
    void addPlane(url, imagePlaneName(image), "image");
  }, [backgroundImages, backgroundImageId, addPlane]);

  // Explicit task only; preserve the original and commit a separately decoded RGBA plane.
  const cutoutSelected = async () => {
    const source = objectsRef.current.find((entry) => entry.id === selectedId);
    if (!source || source.mediaKind !== 'image' || source.locked || !source.object.visible || !source.mediaSource) return;
    const epoch = sceneEpochRef.current;
    const live = () => epoch === sceneEpochRef.current && !!sceneRef.current && objectsRef.current.includes(source) && !source.locked && source.object.visible;
    const operation = operationsRef.current.start('正在抠图…');
    let result: SceneObjectEntry | undefined;
    setError(null);
    try {
      const response = await fetch(source.mediaSource);
      if (!response.ok) throw new Error('无法读取抠图原图');
      const blob = await response.blob();
      if (!live()) return;
      const upload = await uploadImage(blob, `cutout-source-${Date.now()}.png`);
      if (!live()) return;
      const templateId = await findTemplateId('image_remove_background');
      if (!live()) return;
      const created = await fetch('/api/tasks', { method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ template_id: templateId, prompt: '', params: {}, files: { image: [upload] } }) });
      if (!created.ok) throw new Error(`创建抠图任务失败：HTTP ${created.status}`);
      const task = await created.json();
      const finished = await waitForTask(task.task_id);
      if (!live()) return;
      const url = taskResultUrl(finished);
      if (!url) throw new Error('抠图任务没有图片输出');
      const portable = await mediaDataURL(url);
      if (!live()) return;
      result = await addPlane(portable, `${source.name} 抠图`, 'image', undefined,
        { ...source.plane, chromaKey: false }, true);
      if (!result) throw new Error('抠图结果无法解码');
      if (!live()) { disposeSceneEntry(result); result = undefined; return; }
      // Background removal retains canvas dimensions; keep the source plane's exact placement.
      const sourceMesh = source.object.children[0] as THREE.Mesh;
      const resultMesh = result.object.children[0] as THREE.Mesh;
      resultMesh.geometry.dispose(); resultMesh.geometry = sourceMesh.geometry.clone();
      result.object.position.copy(source.object.position);
      result.object.quaternion.copy(source.object.quaternion);
      result.object.scale.copy(source.object.scale);
      historyRef.current.checkpoint(objectsRef.current);
      sceneRef.current!.add(result.object);
      objectsRef.current = [...objectsRef.current, result];
      historyRef.current.collect(objectsRef.current);
      syncObjects(); selectObject(result.id);
      setStatus('抠图已作为独立图片平面加入；原图保留，可在对象列表隐藏');
      result = undefined;
    } catch (err) {
      if (result) disposeSceneEntry(result);
      if (epoch === sceneEpochRef.current) setError(err instanceof Error ? err.message : String(err));
    } finally { operation.finish(); }
  };

  const applyBackgroundAsSkybox = useCallback(() => {
    const image = findImage(backgroundImages, backgroundImageId);
    if (!image) {
      setError("先在下拉里选一张素材库图片");
      return;
    }
    const url = materialUrl(image.path);
    void applySkybox(url, skyboxLabel(image));
  }, [backgroundImages, backgroundImageId, applySkybox]);

  const runImageTo3D = async (file: File) => {
    const operation = operationsRef.current.start('正在生成3D模型…');
    const epoch = sceneEpochRef.current;
    setBusy("正在上传参考图…");
    setError(null);
    setStatus("");
    try {
      const upload = await uploadImage(file, `scene-ref-${Date.now()}.png`);
      const task = await createImageTo3DTask(upload);
      setBusy("正在生成 3D 模型，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      if (epoch !== sceneEpochRef.current || !sceneRef.current) throw new Error(`任务 ${task.task_id} 已返回，但原场景已关闭或替换；未放入新场景`);
      const url = taskResultUrl(finished);
      if (!url) throw new Error("任务没有 GLB 输出");
      setBusy("正在加载生成结果…");
      await loadUrl(url, `hunyuan3d-${task.task_id}.glb`);
      setStatus("3D 生成完成");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      operation.finish(); setBusy(null);
    }
  };

  // 图生 3D 世界：参考图 → TripoSplat → .spz 泼溅，直接放进当前场景。
  const runImageToSplat = async (file: File) => {
    const operation = operationsRef.current.start('正在生成高斯泼溅…');
    const epoch = sceneEpochRef.current;
    setBusy("正在上传参考图…");
    setError(null);
    setStatus("");
    try {
      const upload = await uploadImage(file, `splat-ref-${Date.now()}.png`);
      const task = await createImageToSplatTask(upload, splatMatteEngine);
      setBusy("正在生成高斯泼溅，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      if (epoch !== sceneEpochRef.current || !sceneRef.current) throw new Error(`任务 ${task.task_id} 已返回，但原场景已关闭或替换；未放入新场景`);
      const url = taskResultUrl(finished);
      if (!url) throw new Error("任务没有泼溅输出");
      setBusy("正在解码泼溅…");
      await loadSplatUrl(url, `splat-${task.task_id}.spz`);
      setStatus("3D 世界生成完成");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      operation.finish(); setBusy(null);
    }
  };

  // 身份迁移：当前 3D 画面当 image1（定机位），角色主形象当 image2（定脸与服装），
  // 出来的一张图直接进场景当参考板，方便继续对着它摆位。
  const runIdentityTransfer = async (character: File) => {
    const epoch = sceneEpochRef.current;
    const renderer = rendererRef.current;
    if (!renderer) return;
    if (!identityPrompt.trim()) {
      setError("请先写清楚要生成什么，用 <image1> 指 3D 快照、<image2> 指角色主形象");
      return;
    }
    const operation = operationsRef.current.start('正在身份迁移…');
    setBusy("正在上传 3D 快照与角色主形象…");
    setError(null);
    setStatus("");
    try {
      const snapshotBlob = await captureCurrentView();
      const snapshot = await uploadImage(snapshotBlob, `scene-snapshot-${Date.now()}.png`);
      const characterUpload = await uploadImage(character, `character-${Date.now()}.png`);
      const task = await createIdentityTransferTask({
        snapshot,
        character: characterUpload,
        prompt: identityPrompt,
      });
      setBusy("正在生成身份迁移图，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      if (epoch !== sceneEpochRef.current || !sceneRef.current) throw new Error(`任务 ${task.task_id} 已返回，但原场景已关闭或替换；未放入新场景`);
      const url = taskResultUrl(finished);
      if (!url) throw new Error("任务没有图片输出");
      setBusy("正在把结果放进场景…");
      await addPlane(url, `identity-${task.task_id}.png`, "image");
      setStatus("身份迁移完成，已放进场景当参考板");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      operation.finish(); setBusy(null);
    }
  };

  const removeSelected = () => {
    stopAnimation();
    const entry = objectsRef.current.find(item => item.id === selectedIdRef.current);
    if (!entry || entry.locked) return;
    historyRef.current.checkpoint(objectsRef.current);
    sceneRef.current?.remove(entry.object);
    entry.video?.element.pause();
    objectsRef.current = objectsRef.current.filter(item => item !== entry);
    historyRef.current.collect(objectsRef.current);
    syncObjects(); selectObject(null);
  };
  const restoreHistory = (redo = false) => {
    const current = objectsRef.current;
    selectObject(null);
    const restored = redo ? historyRef.current.redo(current) : historyRef.current.undo(current);
    if (!restored) return;
    current.forEach(e => { sceneRef.current?.remove(e.object); e.video?.element.pause(); });
    objectsRef.current = restored;
    restored.forEach(e => { sceneRef.current?.add(e.object); if (e.video && e.object.visible) void e.video.element.play().catch(() => {}); });
    historyRef.current.collect(restored); syncObjects();
  };
  const toggleEntry = (entry: SceneObjectEntry, key: 'visible' | 'locked') => {
    stopAnimation();
    historyRef.current.checkpoint(objectsRef.current);
    if (key === 'visible') { entry.object.visible = !entry.object.visible; if (!entry.object.visible) entry.video?.element.pause(); else void entry.video?.element.play().catch(() => {}); }
    else entry.locked = !entry.locked;
    historyRef.current.collect(objectsRef.current); syncObjects(); selectObject(entry.id);
  };
  const focusEntry = (entry: SceneObjectEntry) => {
    const camera = cameraRef.current, controls = controlsRef.current;
    if (!camera || !controls || !entry.object.visible) return;
    const box = new THREE.Box3().setFromObject(clonePosedContent(entry.object));
    if (box.isEmpty()) return;
    const center = box.getCenter(new THREE.Vector3());
    const distance = Math.max(1, box.getSize(new THREE.Vector3()).length() / Math.sin(THREE.MathUtils.degToRad(camera.fov / 2)));
    const direction = camera.position.clone().sub(controls.target).normalize();
    camera.position.copy(center).addScaledVector(direction.lengthSq() ? direction : new THREE.Vector3(0, 0, 1), distance);
    controls.target.copy(center); controls.update();
  };
  const duplicateSelected = async () => {
    stopAnimation();
    const entry = objectsRef.current.find(e => e.id === selectedIdRef.current);
    if (!entry) return;
    const epoch = sceneEpochRef.current;
    let copy: SceneObjectEntry;
    try {
      if (entry.mediaKind) {
        if (!entry.mediaSource) throw new Error('平面缺少媒体来源');
        const restored = await addPlane(entry.mediaSource, `${entry.name} 副本`, entry.mediaKind, null, entry.plane, true);
        if (!restored) return; copy = restored;
      } else if (entry.splat) {
        if (!entry.splatUrl) throw new Error('泼溅缺少来源');
        const response = await fetch(entry.splatUrl); if (!response.ok) throw new Error('泼溅读取失败');
        const url = URL.createObjectURL(await response.blob());
        let mesh: THREE.Object3D;
        try { mesh = await loadSplatMesh(url, { placeOnGround: false }); } catch (e) { URL.revokeObjectURL(url); throw e; }
        mesh.position.copy(entry.splat.position); mesh.quaternion.copy(entry.splat.quaternion); mesh.scale.copy(entry.splat.scale);
        const group = new THREE.Group(); group.add(mesh);
        copy = { id: crypto.randomUUID(), name: `${entry.name} 副本`, object: group, splat: mesh, splatUrl: url };
      } else {
        const resources = saveAnimationClips(entry.object);
        const object = duplicateMesh(entry.object);
        object.animations = restoreAnimationClips(object, resources);
        copy = { ...entry, id: crypto.randomUUID(), name: `${entry.name} 副本`, object, locked: false };
      }
      copy.object.position.copy(entry.object.position).add(new THREE.Vector3(.5, 0, .5)); copy.object.quaternion.copy(entry.object.quaternion); copy.object.scale.copy(entry.object.scale); copy.object.visible = entry.object.visible;
      if (!copy.object.visible) copy.video?.element.pause();
      if (epoch !== sceneEpochRef.current || !sceneRef.current || !objectsRef.current.includes(entry)) { disposeSceneEntry(copy); return; }
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); return; }
    historyRef.current.checkpoint(objectsRef.current);
    objectsRef.current = [...objectsRef.current, copy]; sceneRef.current?.add(copy.object);
    historyRef.current.collect(objectsRef.current); syncObjects(); selectObject(copy.id);
  };
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') stopAnimation();
      if ((event.target as HTMLElement)?.closest('input,textarea,select,[contenteditable="true"]')) return;
      if (event.ctrlKey || event.metaKey) {
        if (event.key.toLowerCase() === 'z') { event.preventDefault(); restoreHistory(event.shiftKey); }
        if (event.key.toLowerCase() === 'y') { event.preventDefault(); restoreHistory(true); }
        if (event.key.toLowerCase() === 'd') { event.preventDefault(); void duplicateSelected(); }
      } else if (event.key === 'Delete') removeSelected();
    };
    window.addEventListener('keydown', keydown); return () => window.removeEventListener('keydown', keydown);
  });

  const captureCurrentView = async () => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) throw new Error("相机尚未就绪");
    const shot = await captureViewportAt({ position: camera.position.toArray() as [number, number, number],
      target: controls.target.toArray() as [number, number, number], fov: camera.fov });
    if (!shot) throw new Error("取景失败");
    return shot.blob;
  };

  const downloadScreenshot = async () => {
    try {
      const url = URL.createObjectURL(await captureCurrentView());
      const link = document.createElement("a");
      link.download = `scene-${Date.now()}.png`;
      link.href = url;
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) { setError(err instanceof Error ? err.message : String(err)); }
  };

  const applyLightPreset = useCallback((name: LightPresetName) => {
    const lights = lightsRef.current;
    const scene = sceneRef.current;
    if (!lights || !scene) return;
    const preset = LIGHT_PRESETS[name];
    // 设了天空盒时背景归全景图管，切灯光预设只换灯，不把背景顶掉。
    if (!skyboxTextureRef.current) scene.background = new THREE.Color(preset.background);
    lights.hemi.color.set(preset.hemi.sky);
    lights.hemi.groundColor.set(preset.hemi.ground);
    lights.hemi.intensity = preset.hemi.intensity;
    lights.key.color.set(preset.key.color);
    lights.key.intensity = preset.key.intensity;
    lights.key.position.set(...preset.key.position);
    lights.fill.color.set(preset.fill.color);
    lights.fill.intensity = preset.fill.intensity;
    lights.fill.position.set(...preset.fill.position);
    lights.rim.color.set(preset.rim.color);
    lights.rim.intensity = preset.rim.intensity;
    lights.rim.position.set(...preset.rim.position);
    setLightPreset(name);
  }, []);

  /**
   * 用高度场重建地形网格。三角划分与 getTerrainHeight 的插值一致（a/b/d、b/c/d），
   * 否则落点会与画面差半个三角形。
   */
  const rebuildTerrainMesh = useCallback((terrain: SceneTerrain | null) => {
    const scene = sceneRef.current;
    const previous = terrainMeshRef.current;
    if (previous) {
      scene?.remove(previous);
      disposeEnvironmentTerrainMesh(previous);
      terrainMeshRef.current = null;
    }
    // 有地形时隐藏原来的 20×20 平地；关掉地形后恢复，旧场景行为不变。
    if (groundRef.current) groundRef.current.visible = !terrain;
    if (!scene || !terrain) return;
    const mesh = createEnvironmentTerrainMesh(terrain);
    scene.add(mesh);
    terrainMeshRef.current = mesh;
  }, []);

  /**
   * 应用环境：太阳方向/强度、天空底色、雾与地形一次改完。
   *
   * 视口、缩略图、截图、序列与参考图取样都读这份场景状态，所以同一份环境在同一时刻
   * 得到同一画面；关闭后回到原来的灯光预设 + 20×20 平地（旧存档行为）。
   */
  const applyEnvironment = useCallback((next: SceneEnvironment | null, fallbackPreset: LightPresetName = lightPreset) => {
    environmentRef.current = next;
    setEnvironment(next);
    if (terrainMeshRef.current?.userData.sourceTerrain !== (next?.terrain ?? null)) rebuildTerrainMesh(next?.terrain ?? null);
    weatherEffectsRef.current?.dispose();
    weatherEffectsRef.current = null;
    const scene = sceneRef.current;
    if (scene && next) {
      const effects = createEnvironmentEffects(next);
      scene.add(effects.object); weatherEffectsRef.current = effects;
    }
    const lights = lightsRef.current;
    if (!scene || !lights) return;
    if (!next) {
      scene.fog = null;
      applyLightPreset(fallbackPreset);
      return;
    }
    const light = environmentLight(next);
    const modernTerrain = terrainMeshRef.current?.userData.terrainMaterial as ReturnType<typeof import('./toonflow/terrainMaterial').createTerrainMaterial> | undefined;
    modernTerrain?.setRain(next.weather.condition === 'rain' ? next.weather.intensity : 0);
    modernTerrain?.setEnvironment(new THREE.Color(light.skyColor), new THREE.Color(light.groundColor), new THREE.Color(light.sunColor), new THREE.Vector3(...light.sunDirection));
    scene.fog = light.fogDensity > 0 ? new THREE.FogExp2(light.fogColor, light.fogDensity) : null;
    // 天空盒是用户显式选的背景，环境只换灯不顶掉它。
    if (!skyboxTextureRef.current) scene.background = new THREE.Color(light.background);
    const distance = 30;
    lights.key.position.set(
      light.sunDirection[0] * distance,
      light.sunDirection[1] * distance,
      light.sunDirection[2] * distance,
    );
    lights.key.color.set(light.sunColor);
    lights.key.intensity = light.sunIntensity;
    lights.hemi.color.set(light.skyColor);
    lights.hemi.groundColor.set(light.groundColor);
    lights.hemi.intensity = light.ambientIntensity;
    // 环境接管主光与半球光：补光/轮廓光归零，免得同一份环境被预设补光改出两种结果。
    lights.fill.intensity = 0;
    lights.rim.intensity = 0;
  }, [applyLightPreset, lightPreset, rebuildTerrainMesh]);

  /**
   * 换地形：同时把已有 A/B/途经点贴到新地面。
   *
   * 落点的语义就是「地面上的点」，地形换了还留着旧高度会飘在半空或埋进山里；
   * 这是用户点「生成/清除地形」触发的显式动作，状态栏会写明改了什么。
   */
  const applyTerrainChange = useCallback((terrain: SceneTerrain | null) => {
    const current = environmentRef.current;
    if (!current) return;
    applyEnvironment({ ...current, terrain });
    setGroundPoints((points) => {
      const snap = (point: GroundPoint | null): GroundPoint | null => {
        if (!point) return null;
        return terrain ? clampGroundPointToTerrain(terrain, point) : [point[0], 0.02, point[2]];
      };
      return {
        start: snap(points.start),
        end: snap(points.end),
        vias: points.vias ? points.vias.map((point) => snap(point)!) : points.vias,
      };
    });
  }, [applyEnvironment]);

  // 把材质设置写到选中对象下所有网格上，兼容 GLTF 里的多材质数组。
  const applyMaterial = useCallback((patch: Partial<MaterialSettings>) => {
    setMaterial((current) => {
      const next = { ...current, ...patch };
      const id = selectedIdRef.current;
      const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
      entry?.object.traverse((child) => {
        const mesh = child as THREE.Mesh;
        if (!mesh.isMesh) return;
        const materials = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
        materials.forEach((item) => {
          const standard = item as THREE.MeshStandardMaterial;
          if (!standard || !("color" in standard)) return;
          standard.color?.set(next.color);
          standard.roughness = next.roughness;
          standard.metalness = next.metalness;
          standard.opacity = next.opacity;
          standard.transparent = next.opacity < 1;
          standard.needsUpdate = true;
        });
      });
      return next;
    });
  }, []);

  const recordCameraKey = useCallback(() => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    const key: CameraKey = {
      id: crypto.randomUUID(),
      position: [camera.position.x, camera.position.y, camera.position.z],
      target: [controls.target.x, controls.target.y, controls.target.z],
      duration: DEFAULT_CAMERA_SEGMENT_SECONDS,
      fov: normalizeCameraFov(camera.fov),
      easing: DEFAULT_CAMERA_EASING,
    };
    setCameraKeys((current) => {
      setStatus(`已记录第 ${current.length + 1} 个机位`);
      return [...current, key];
    });
  }, [cameraKeys.length]);

  const updateCameraKey = useCallback(
    (id: string, patch: Partial<Pick<CameraKey, "duration" | "easing">>) => {
      setCameraKeys((current) =>
        current.map((key) =>
          key.id === id ? { ...key, ...patch, duration: normalizeCameraDuration(patch.duration ?? key.duration) } : key,
        ),
      );
    },
    [],
  );

  // 顺序就是运镜顺序：上移/下移直接换位置，别让用户重录一遍。
  const moveCameraKey = useCallback((id: string, delta: -1 | 1) => {
    setCameraKeys((current) => {
      const index = current.findIndex((key) => key.id === id);
      const target = index + delta;
      if (index < 0 || target < 0 || target >= current.length) return current;
      const next = current.slice();
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }, []);

  const removeCameraKey = useCallback((id: string) => {
    setCameraKeys((current) => current.filter((key) => key.id !== id));
  }, []);

  const playCameraPath = useCallback(() => {
    if (busy) return;
    stopAnimation();
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls || cameraKeys.length < 2) {
      setStatus("至少要两个机位才能播放：转好角度点「记录机位」");
      return;
    }
    if (cameraPathRef.current !== null) cancelAnimationFrame(cameraPathRef.current);
    // 每段时长与缓动各自独立：改一段不影响别的段，总时长由各段相加。
    const total = cameraPathDuration(cameraKeys);
    if (total <= 0) {
      setStatus("机位总时长为 0，先把每段秒数调到 0.2 秒以上");
      return;
    }
    const started = performance.now();
    const epoch = sceneEpochRef.current;
    // 跟拍得知道“谁在走”与“往哪走”；没有角色或路径就只放相机。
    const followPath = followMover ? editorGroundPath(groundPoints) : null;
    const followMoverEntry =
      followPath && moverId ? objectsRef.current.find((entry) => entry.id === moverId && canMoveEntry(entry)) ?? null : null;
    const followSeconds = followPath && followMoverEntry ? normalizeMoveSeconds(moveSeconds) : null;
    // 播完把角色放回原处，否则看一次预览角色就跑到 B 点了。
    const savedMover = followMoverEntry
      ? {
          position: followMoverEntry.object.position.clone(),
          rotationY: followMoverEntry.object.rotation.y,
        }
      : null;
    const clip = followMoverEntry?.object.animations[moveAnimationIndex];
    if (followMoverEntry && moveAnimationIndex >= 0 && !clip) { setError('移动对象的动作片段已变化，请重新选择'); return; }
    let preview: AnimationPreview | null = null;
    if (clip && followMoverEntry) {
      fkRef.current?.clear(); posingRef.current = false; setPosing(false); setPosingBones(0); setActiveBone(null);
      transformRef.current?.detach();
      try {
        const pose = serializeBonePose(followMoverEntry.object);
        preview = new AnimationPreview(followMoverEntry.object, clip);
        animationRef.current = { id: followMoverEntry.id, preview, pose, pathClock: true };
        setAnimationState('playing');
      } catch (err) { setError(String(err)); transformRef.current?.attach(followMoverEntry.object); return; }
    }
    // 收尾只走这一条：清 rAF、清兜底定时器、还原骨姿与角色变换。
    let finished = false;
    const finish = () => {
      if (finished) return;
      finished = true;
      cancelCameraPreviewRef.current = null;
      preview?.stop();
      if (preview && animationRef.current?.preview === preview) {
        animationRef.current = null;
        setAnimationState('stopped');
      }
      if (cameraPathRef.current !== null) {
        cancelAnimationFrame(cameraPathRef.current);
        cameraPathRef.current = null;
      }
      if (cameraPathTimerRef.current !== null) {
        window.clearTimeout(cameraPathTimerRef.current);
        cameraPathTimerRef.current = null;
      }
      if (epoch === sceneEpochRef.current && followMoverEntry && savedMover && canMoveEntry(followMoverEntry) && objectsRef.current.includes(followMoverEntry)) {
        followMoverEntry.object.position.copy(savedMover.position);
        followMoverEntry.object.rotation.y = savedMover.rotationY;
        followMoverEntry.object.updateMatrixWorld(true);
        const selected = objectsRef.current.find(e => e.id === selectedIdRef.current);
        if (selected && canMoveEntry(selected)) transformRef.current?.attach(selected.object);
      }
      setPlayingPath(false);
    };
    cancelCameraPreviewRef.current = finish;
    setPlayingPath(true);
    // 大兜底：rAF 被节流或挂起时也保证按钮不会永久卡在「播放中…」。
    if (cameraPathTimerRef.current !== null) window.clearTimeout(cameraPathTimerRef.current);
    cameraPathTimerRef.current = window.setTimeout(finish, total * 1000 + 80);
    const step = () => {
      if (finished) return;
      if (epoch !== sceneEpochRef.current) { finish(); return; }
      if (followMoverEntry && (!objectsRef.current.includes(followMoverEntry) || !canMoveEntry(followMoverEntry))) { finish(); return; }
      const elapsed = (performance.now() - started) / 1000;
      // 找到当前落在哪一段（超过总时长时停在最后一段的末尾）。
      const pose = sampleCameraPath(cameraKeys, elapsed);
      if (pose) {
        // 跟拍：角色按同一时钟走 A→B，相机整体加上它的位移（构图不变）。
        let delta: [number, number, number] = [0, 0, 0];
        if (followPath && followMoverEntry && followSeconds && canMoveEntry(followMoverEntry) && objectsRef.current.includes(followMoverEntry)) {
          const progress = Math.min(1, elapsed / followSeconds);
          followMoverEntry.object.position.set(...groundPathPoint(followPath, progress));
          followMoverEntry.object.rotation.y = groundPathYaw(followPath, progress);
          preview?.sampleAt(Math.min(elapsed, followSeconds));
          delta = groundPathFollowDelta(followPath, progress);
        }
        camera.position.set(
          pose.position[0] + delta[0],
          pose.position[1] + delta[1],
          pose.position[2] + delta[2],
        );
        controls.target.set(
          pose.target[0] + delta[0],
          pose.target[1] + delta[1],
          pose.target[2] + delta[2],
        );
        if (Math.abs(camera.fov - pose.fov) > 1e-4) {
          camera.fov = pose.fov;
          camera.updateProjectionMatrix();
        }
        controls.update();
      }
      if (elapsed >= total) {
        finish();
        return;
      }
      cameraPathRef.current = requestAnimationFrame(step);
    };
    cameraPathRef.current = requestAnimationFrame(step);
  }, [cameraKeys, followMover, groundPoints, moveSeconds, moverId, moveAnimationIndex, busy]);

  /**
   * 连续逐镜播放：按已确认镜头表的时间轴，在现有视口里切换人工记录的机位，
   * 与参考包/视频复用同一绝对时间采样器。旧镜保持直线障碍拒绝；仅显式启用的镜头
   * 使用避让、动作混合与双腿IK；结束时还原机位、位置、骨姿及指纹遮罩。
   */
  const playScriptShots = useCallback(async (plan: import('./toonflowSets').ScriptShotPlan) => {
    if (busy) return;
    stopAnimation();
    const token = ++shotPlaybackTokenRef.current;
    const epoch = sceneEpochRef.current;
    let module: typeof import('./toonflowSets');
    let ready: import('./toonflowSets').ScriptShotPlan;
    let makeSampler: typeof import('./scriptShotSampler').createScriptShotSampler;
    try {
      // 保持 toonflowSets 懒加载：静态 import 会把整套素材塞进主视口包。
      module = await import('./toonflowSets');
      // 机位就绪后，再用当前场景的真实对象校验走位目标：存在、可见、未锁定、clip 仍匹配。
      ready = module.assertScriptShotMotionTargets(module.assertScriptShotPlaybackReady(plan), objectsRef.current.map(entry => ({
        id: entry.id, name: entry.name, visible: entry.object.visible, locked: !!entry.locked,
        animations: entry.object.animations.map((clip, index) => ({ index, name: clip.name, duration: clip.duration })),
      })));
      // 与视频/参考包使用同一编辑基线障碍快照；失败发生在任何临时位移之前。
      const sampling = await import('./scriptShotSampler');
      makeSampler = sampling.createScriptShotSampler;
    } catch (cause) {
      if (token === shotPlaybackTokenRef.current) setError(cause instanceof Error ? cause.message : String(cause));
      return;
    }
    if (token !== shotPlaybackTokenRef.current || epoch !== sceneEpochRef.current || busy) return;
    const camera = cameraRef.current; const controls = controlsRef.current;
    if (!camera || !controls) { setError('视口尚未就绪，无法连续逐镜播放'); return; }
    const total = ready.shots[ready.shots.length - 1]!.end;
    let session: ReturnType<typeof makeSampler>;
    try { session = makeSampler(ready, { camera, controls, entries: () => objectsRef.current, environment: environmentRef.current, baselines: motionBaselineRef.current, isCurrent: () => epoch === sceneEpochRef.current }); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); return; }
    const started = performance.now();
    let finished = false;
    let lastIndex = -1;
    let lastLine: string | null = null;
    // Viewport, reference captures and videos now own exactly the same absolute
    // actor/IK/blend/navigation evaluator and editing-baseline restoration.
    // 收尾只走这一条：清 rAF/兜底定时器，再由 PreviewPlayhead 还原机位、把角色与骨姿还原到基线。
    const finish = (message: string) => {
      if (finished) return;
      finished = true;
      if (cancelCameraPreviewRef.current === cancelPlayback) cancelCameraPreviewRef.current = null;
      if (shotPlaybackRef.current !== null) {
        cancelAnimationFrame(shotPlaybackRef.current);
        shotPlaybackRef.current = null;
      }
      if (shotPlaybackTimerRef.current !== null) {
        window.clearTimeout(shotPlaybackTimerRef.current);
        shotPlaybackTimerRef.current = null;
      }
      session.stop();
      setShotPlayback(null);
      setStatus(message);
    };
    const cancelPlayback = () => finish('已停止连续逐镜播放，视口机位、角色位置与骨姿已还原');
    cancelCameraPreviewRef.current = cancelPlayback;
    setShotPlayback({ index: 0, shots: ready.shots.length, line: null });
    setStatus(`开始连续逐镜播放：${ready.shots.length} 镜 · ${total.toFixed(2)} 秒（人工机位 ${ready.shots.length} 镜 · 角色走位 ${ready.shots.filter(shot => shot.motion).length} 镜；按镜头表启用避让/骨骼增强）`);
    // 大兜底：rAF 被节流或挂起时也保证不会永久停在播放中。
    shotPlaybackTimerRef.current = window.setTimeout(() => finish('连续逐镜播放结束，视口机位、角色位置与骨姿已还原'), total * 1000 + 80);
    const step = () => {
      if (finished) return;
      if (epoch !== sceneEpochRef.current) { finish('场景已替换，连续逐镜播放已停止'); return; }
      let sample: import('./toonflowSets').ScriptShotPlaybackSample;
      try {
        sample = session.sampleAt((performance.now() - started) / 1000);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
        finish('连续逐镜播放失败，视口机位、角色位置与骨姿已还原');
        return;
      }
      const shotIndex = lastIndex;
      if (sample.index !== lastIndex) {
        lastIndex = sample.index;
        // Camera cuts are applied by the same absolute sampler used in exports.
      }
      const line = sample.line ? `${sample.line.speaker}：${sample.line.line || '（无台词）'}` : null;
      if (line !== lastLine || sample.index !== shotIndex) {
        lastLine = line;
        setShotPlayback({ index: sample.index, shots: ready.shots.length, line });
      }
      if (sample.seconds >= total) { finish(`连续逐镜播放结束：${ready.shots.length} 镜，视口机位、角色位置与骨姿已还原`); return; }
      shotPlaybackRef.current = requestAnimationFrame(step);
    };
    shotPlaybackRef.current = requestAnimationFrame(step);
  }, [busy, stopAnimation]);

  // Session-only random access. Configuration edits invalidate the captured baseline.
  useEffect(() => { if (scrubRef.current) stopAnimation(); }, [cameraKeys, groundPoints, moverId, moveSeconds, followMover, moveAnimationIndex, stopAnimation]);
  const scrubMover = objectsRef.current.find(e => e.id === moverId && canMoveEntry(e));
  const scrubPath = scrubMover ? editorGroundPath(groundPoints) : null;
  const scrubDuration = Math.max(cameraPathDuration(cameraKeys), scrubPath ? normalizeMoveSeconds(moveSeconds) : 0);
  const scrubLastFrame = previewFrameCount(scrubDuration);
  const seekPreviewFrame = (requestedFrame: number) => {
    if (busy || operationBusy || !scrubLastFrame) return;
    const camera = cameraRef.current, controls = controlsRef.current;
    if (!camera || !controls) return;
    const { frame, seconds } = previewFrameTime(requestedFrame, scrubDuration);
    try {
      if (!scrubRef.current) {
        stopAnimation();
        fkRef.current?.clear(); posingRef.current = false; setPosing(false); setPosingBones(0); setActiveBone(null);
        const mover = scrubPath ? scrubMover : undefined;
        const clip = mover?.object.animations[moveAnimationIndex];
        if (mover && moveAnimationIndex >= 0 && !clip) throw new Error('移动对象的动作片段已变化，请重新选择');
        const pose = mover ? serializeBonePose(mover.object) : null;
        scrubRef.current = { session: new PreviewPlayhead(camera, controls, mover?.object, clip), id: mover?.id, pose };
        transformRef.current?.detach();
      }
      scrubRef.current.session.sample(seconds, normalizeMoveSeconds(moveSeconds), time => {
        let delta: [number, number, number] = [0, 0, 0];
        if (scrubPath && scrubMover) {
          const progress = Math.min(1, time / normalizeMoveSeconds(moveSeconds));
          scrubMover.object.position.set(...groundPathPoint(scrubPath, progress));
          scrubMover.object.rotation.y = groundPathYaw(scrubPath, progress);
          if (followMover) delta = groundPathFollowDelta(scrubPath, progress);
        }
        const pose = sampleCameraPath(cameraKeys, time);
        if (pose) {
          camera.position.set(pose.position[0] + delta[0], pose.position[1] + delta[1], pose.position[2] + delta[2]);
          controls.target.set(pose.target[0] + delta[0], pose.target[1] + delta[1], pose.target[2] + delta[2]);
          camera.fov = pose.fov; camera.updateProjectionMatrix(); controls.update();
        }
      });
      setPreviewFrame(frame);
    } catch (err) { stopAnimation(); setError(String(err)); }
  };

  // 机位一改，上次算出来的运镜文案就过期了（它描述的是旧的两端）。
  // 导出首尾帧本身不动机位，所以文案能留住；录机位、删机位、清空才会清掉。
  useEffect(() => {
    setFirstLastNote("");
  }, [cameraKeys]);

  // ---------------- 相机 Agent：一句话 → 结构化机位 ----------------
  // 只排相机：不选角色、不改场景、不写提示词。缺参数一次问一句，出预览再采用，采用可撤销。
  const agentUndoRef = useRef<CameraKey[] | null>(null);

  const parseAgentBrief = useCallback(() => {
    const intent = parseCameraBrief(agentBrief);
    setAgentIntent(intent);
    setAgentPlan(null);
    if (intent.questions.length > 0) {
      setStatus(`还差一句：${intent.questions[0]}`);
      return;
    }
    setStatus(`解析出 ${intent.moves.length} 段：${intent.moves.map(describeMove).join(" → ")}`);
  }, [agentBrief]);

  const answerAgentQuestion = useCallback(() => {
    if (!agentIntent) return;
    const next = answerCameraQuestion(agentIntent, agentAnswer);
    setAgentIntent(next);
    setAgentAnswer("");
    setAgentPlan(null);
    setStatus(next.questions.length > 0 ? `下一个问题：${next.questions[0]}` : "参数齐了，可以生成预览");
  }, [agentAnswer, agentIntent]);

  /** 预览只算机位，不动相机：看说明与数量够了再「采用」。 */
  const previewAgentPlan = useCallback(() => {
    const camera = cameraRef.current as THREE.PerspectiveCamera | null;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    if (!agentIntent) {
      setError("先写一句运镜并点「解析」");
      return;
    }
    const path = editorGroundPath(groundPoints);
    const plan = planCameraMoves(
      agentIntent,
      {
        position: [camera.position.x, camera.position.y, camera.position.z],
        target: [controls.target.x, controls.target.y, controls.target.z],
        fov: normalizeCameraFov(camera.fov),
      },
      path ? (t) => groundPathPoint(path, t) : null,
    );
    setAgentPlan(plan);
    if (plan.problems.length > 0) {
      setStatus(`预览不成立：${plan.problems[0]}`);
      return;
    }
    const seconds = plan.keys.slice(0, -1).reduce((sum, key) => sum + key.duration, 0);
    setStatus(`预览：${plan.keys.length} 个机位，共 ${seconds.toFixed(1)} 秒。确认无误点「采用」`);
  }, [agentIntent, groundPoints]);

  const applyAgentPlan = useCallback(() => {
    if (!agentPlan || agentPlan.keys.length < 2) return;
    agentUndoRef.current = cameraKeys;
    setCameraKeys(agentPlan.keys.map((key) => ({ id: crypto.randomUUID(), ...key })));
    const seconds = agentPlan.keys.slice(0, -1).reduce((sum, key) => sum + key.duration, 0);
    setStatus(`已采用 ${agentPlan.keys.length} 个机位（${seconds.toFixed(1)} 秒），可用「撤销」回退`);
  }, [agentPlan, cameraKeys]);

  const undoAgentPlan = useCallback(() => {
    const previous = agentUndoRef.current;
    if (!previous) {
      setStatus("没有可撤销的 Agent 操作");
      return;
    }
    agentUndoRef.current = null;
    setCameraKeys(previous);
    setStatus("已撤销，机位回到采用之前");
  }, []);

  /**
   * 环绕：绕选中对象（没选中就绕当前相机目标）转一圈，把采样出的机位追加到序列末尾。
   * 半径和高度都照当前机位量，所以看到什么角度就绕什么角度。
   */
  const addOrbitShot = useCallback(() => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    const entry =
      (selectedIdRef.current
        ? objectsRef.current.find((item) => item.id === selectedIdRef.current) ?? null
        : null) ?? null;
    const center: [number, number, number] = entry
      ? [entry.object.position.x, entry.object.position.y, entry.object.position.z]
      : [controls.target.x, controls.target.y, controls.target.z];
    const keys = orbitCameraKeys({
      center,
      start: [camera.position.x, camera.position.y, camera.position.z],
      turns: orbitTurns,
      seconds: orbitSeconds,
      segments: Math.max(ORBIT_SEGMENTS_LIMITS.min, Math.round(orbitTurns * 8)),
      pitch: orbitPitch,
    });
    if (keys.length === 0) {
      setStatus("相机就在环绕中心上，先拉开一点距离再环绕");
      return;
    }
    const fov = normalizeCameraFov(camera.fov);
    setCameraKeys((current) => [
      ...current,
      ...keys.map((key) => ({
        id: crypto.randomUUID(),
        position: key.position,
        target: key.target,
        duration: normalizeCameraDuration(key.duration),
        easing: DEFAULT_CAMERA_EASING,
        fov,
      })),
    ]);
    setStatus(
      `已追加环绕 ${orbitTurns} 圈、${keys.length} 个机位（${orbitSeconds} 秒${orbitPitch === 0 ? "" : `，收尾抬高 ${orbitPitch}°`}）${entry ? `，绕「${entry.name}」` : "，绕当前相机目标"}`,
    );
  }, [orbitPitch, orbitSeconds, orbitTurns]);

  /**
   * 希区柯克：对着选中对象（没选中就对着当前相机目标）推进，距离变、视角跟着反向变，
   * 目标在画面里的大小基本不变。同样把整段机位追加到序列末尾。
   */
  const addDollyShot = useCallback(() => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    const entry = selectedIdRef.current
      ? objectsRef.current.find((item) => item.id === selectedIdRef.current) ?? null
      : null;
    const target: [number, number, number] = entry
      ? [entry.object.position.x, entry.object.position.y, entry.object.position.z]
      : [controls.target.x, controls.target.y, controls.target.z];
    const keys = dollyZoomKeys({
      start: [camera.position.x, camera.position.y, camera.position.z],
      target,
      fov: camera.fov,
      range: dollyRange,
      seconds: dollySeconds,
    });
    if (keys.length === 0) {
      setStatus("相机已经贴在目标上了，先拉开距离再推");
      return;
    }
    setCameraKeys((current) => [
      ...current,
      ...keys.map((key) => ({
        id: crypto.randomUUID(),
        position: key.position,
        target: key.target,
        duration: normalizeCameraDuration(key.duration),
        easing: DEFAULT_CAMERA_EASING,
        fov: normalizeCameraFov(key.fov),
      })),
    ]);
    setStatus(
      `已追加希区柯克 ${keys.length} 个机位（${dollySeconds} 秒，推进到 ${Math.round(dollyRange * 100)}%）${entry ? `，对着「${entry.name}」` : ""}`,
    );
  }, [dollyRange, dollySeconds]);

  const setTransformValue = useCallback((axis: "x" | "y" | "z" | "scale" | "rotateX" | "rotateY" | "rotateZ" | "scaleX" | "scaleY" | "scaleZ", value: number) => {
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry || entry.locked || !entry.object.visible || !Number.isFinite(value)) return;
    historyRef.current.checkpoint(objectsRef.current);
    const object = entry.object;
    if (axis === "scale") {
      object.scale.setScalar(Math.max(0.05, value));
    } else if (axis.startsWith('rotate')) {
      object.rotation[axis.slice(-1).toLowerCase() as 'x' | 'y' | 'z'] = THREE.MathUtils.degToRad(value);
    } else if (axis.startsWith('scale')) {
      object.scale[axis.slice(-1).toLowerCase() as 'x' | 'y' | 'z'] = Math.max(.05, value);
    } else {
      object.position[axis as 'x' | 'y' | 'z'] = value;
    }
    setTransform({
      x: object.position.x,
      y: object.position.y,
      z: object.position.z,
      rotateX: THREE.MathUtils.radToDeg(object.rotation.x), rotateZ: THREE.MathUtils.radToDeg(object.rotation.z),
      scaleX: object.scale.x, scaleY: object.scale.y, scaleZ: object.scale.z,
      rotateY: Math.round((object.rotation.y * 180) / Math.PI),
      scale: Number(object.scale.x.toFixed(2)),
    });
  }, []);

  const recordPose = useCallback((slot: "a" | "b") => {
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!id || !entry) return;
    const object = entry.object;
    const snapshot: PoseSnapshot = {
      position: object.position.toArray() as [number, number, number],
      quaternion: object.quaternion.toArray() as [number, number, number, number],
      scale: object.scale.toArray() as [number, number, number],
    };
    posesRef.current[id] = { ...posesRef.current[id], [slot]: snapshot };
    setPoseSlots((current) => ({ ...current, [slot]: true }));
    setStatus(`已记录姿势 ${slot.toUpperCase()}`);
  }, []);

  // 动作/姿势混合：位置与缩放线性插值，旋转走四元数球面插值，避免中间穿插翻转。
  const blendPose = useCallback((value: number) => {
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    const poses = id ? posesRef.current[id] : undefined;
    if (!entry || entry.locked || !entry.object.visible || !poses?.a || !poses?.b) return;
    historyRef.current.checkpoint(objectsRef.current);
    const a = poses.a;
    const b = poses.b;
    const object = entry.object;
    object.position.set(
      a.position[0] + (b.position[0] - a.position[0]) * value,
      a.position[1] + (b.position[1] - a.position[1]) * value,
      a.position[2] + (b.position[2] - a.position[2]) * value,
    );
    object.scale.set(
      a.scale[0] + (b.scale[0] - a.scale[0]) * value,
      a.scale[1] + (b.scale[1] - a.scale[1]) * value,
      a.scale[2] + (b.scale[2] - a.scale[2]) * value,
    );
    const from = new THREE.Quaternion(...a.quaternion);
    const to = new THREE.Quaternion(...b.quaternion);
    object.quaternion.copy(from).slerp(to, value);
    setPoseBlend(value);
  }, []);

  // 骨骼摆姿开关：开启时把红球标到当前角色骨上，主 gizmo 让位给骨骼旋转 gizmo。
  const togglePosing = useCallback(() => {
    stopAnimation();
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    const fk = fkRef.current;
    if (!entry || !fk || entry.locked || !entry.object.visible) return;
    if (posingRef.current) {
      posingRef.current = false;
      setPosing(false);
      fk.clear();
      setPosingBones(0);
      setActiveBone(null);
      transformRef.current?.attach(entry.object);
      setStatus("已退出骨骼摆姿");
      return;
    }
    if (!fk.setTarget(entry.object)) {
      setError("这个模型没有可摆的骨骼（需要带 Mixamo 等骨架的 GLB/FBX）");
      return;
    }
    posingRef.current = true;
    setPosing(true);
    setPosingBones(fk.boneCount);
    setActiveBone(null);
    // 整体 gizmo 与骨骼 gizmo 同时出现会互相遮挡，摆姿时先收起来。
    transformRef.current?.detach();
    setStatus(`骨骼摆姿已开启，点红球选骨（共 ${fk.boneCount} 根）`);
  }, []);

  // 把当前骨姿存进对象条目，随场景一起保存。
  const saveBonePose = useCallback(() => {
    stopAnimation();
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry) return;
    const pose = serializeBonePose(entry.object);
    if (!pose) {
      setError("这个模型没有可保存的骨骼姿势");
      return;
    }
    entry.bonePose = pose;
    syncObjects();
    setStatus("已记住当前骨骼姿势（仅本次编辑）；持久保存请点击保存场景或存到后端");
  }, [syncObjects]);

  // 还原到上次保存的骨姿（载入场景后或摆乱之后回来）。
  const restoreBonePose = useCallback(() => {
    stopAnimation();
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry || entry.locked || !entry.object.visible) return;
    historyRef.current.checkpoint(objectsRef.current);
    if (!applyBonePose(entry.object, entry.bonePose)) {
      setError("没有可还原的骨骼姿势，或骨架与记忆不兼容（未修改当前姿势）");
      return;
    }
    fkRef.current?.refresh();
    setStatus("已还原到保存的骨骼姿势");
  }, []);

  // 恢复模型载入时的绑定姿势。
  const resetBonePose = useCallback(() => {
    stopAnimation();
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry || entry.locked || !entry.object.visible) return;
    historyRef.current.checkpoint(objectsRef.current);
    if (!applyBonePose(entry.object, entry.initialBonePose)) {
      setError("没有可恢复的初始骨骼姿势，或骨架已变化（未修改当前姿势）");
      return;
    }
    fkRef.current?.refresh();
    setStatus("已恢复初始姿势");
  }, []);

  // 场景存档与 GLB 导出共用同一份几何导出逻辑：只打包用户模型，辅助线与灯光不落盘。
  const exportGeometry = useCallback(async (options: { flattenTransforms?: boolean; recipeIds?: ReadonlySet<string> } = {}) => {
    stopAnimation();
    // Export a Scene, not a Group: a Group becomes an extra glTF node and breaks object-ID matching on reload.
    const root = new THREE.Scene();
    objectsRef.current.forEach((entry) => {
      // 视频平面不进导出：glTF 内嵌不了正在播放的视频帧，硬塞会得到一个静止首帧。
      if (!isExportablePlane(entry.mediaKind ?? "image")) return;
      // 高斯泼溅也进不了 glTF：Spark 的 SplatMesh 不是普通几何体，场景存档另有 splats 字段。
      if (entry.splat) return;
      if (options.recipeIds?.has(entry.id)) return;
      if (options.flattenTransforms && entry.mediaKind) return;
      const clone = clonePosedContent(entry.object);
      markAnimationArchivePaths(clone);
      if (options.flattenTransforms) {
        // 场景存档里变换单独记录，几何体按局部坐标导出，避免节点 TRS 与存档重复叠加。
        clone.position.set(0, 0, 0);
        clone.quaternion.identity();
        clone.scale.set(1, 1, 1);
        clone.userData.sceneDocumentId = entry.id;
      }
      root.add(clone);
    });
    return new Promise<ArrayBuffer>((resolve, reject) => {
      new GLTFExporter().parse(
        root,
        (result) => resolve(result as ArrayBuffer),
        (err) => reject(err),
        { binary: true, onlyVisible: !options.flattenTransforms },
      );
    });
  }, []);

  const exportGlb = useCallback(async () => {
    setBusy("正在导出 GLB…");
    try {
      // 泼溅进不了 glTF，纯泼溅场景导出会得到一个空 GLB，不如直接说明。
      if (!objectsRef.current.some((entry) => !entry.splat && isExportablePlane(entry.mediaKind ?? "image"))) {
        throw new Error("没有可导出GLB的对象（视频或泼溅请保存场景存档）");
      }
      const binary = await exportGeometry();
      const url = URL.createObjectURL(new Blob([binary], { type: "model/gltf-binary" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = `scene-${Date.now()}.glb`;
      link.click();
      URL.revokeObjectURL(url);
      setStatus(`已导出 ${(binary.byteLength / 1024 / 1024).toFixed(1)} MB GLB`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, []);

  // 缩略图不能「抓到什么算什么」：泼溅首帧要等 Spark 建完细节层，太早截会
  // 得到一张空视口图（素材库里就是一格空地网格）。所以先强制重画一帧，再采样
  // 确认画面不是纯底色，必要时最多等 timeoutMs。
  const captureThumbnail = useCallback(async (timeoutMs = 60_000): Promise<Blob | null> => {
    const renderer = rendererRef.current;
    const scene = sceneRef.current;
    const camera = cameraRef.current;
    if (!renderer) return null;
    const canvas = renderer.domElement;
    const probe = document.createElement("canvas");
    probe.width = 32;
    probe.height = 18;
    const ctx = probe.getContext("2d", { willReadFrequently: true });
    // 以左上角为底色，差异化像素不到 5% 就当空视口。
    const looksBlank = () => {
      if (!ctx) return false;
      ctx.drawImage(canvas, 0, 0, probe.width, probe.height);
      const { data } = ctx.getImageData(0, 0, probe.width, probe.height);
      const [r0, g0, b0] = data;
      let diff = 0;
      for (let i = 0; i < data.length; i += 4) {
        if (Math.abs(data[i] - r0) + Math.abs(data[i + 1] - g0) + Math.abs(data[i + 2] - b0) > 24) {
          diff += 1;
        }
      }
      return diff < probe.width * probe.height * 0.05;
    };
    // 选中对象的红绿蓝箭头是编辑器 chrome，不该烤进素材缩略图。
    const restorePoseHelpers = scene ? hidePoseEditorHelpers(scene) : () => {};
    const gizmo = transformRef.current?.getHelper();
    const gizmoVisible = gizmo?.visible ?? false;
    if (gizmo) gizmo.visible = false;
    const controls = controlsRef.current;
    const savedPosition = camera?.position.clone();
    const savedTarget = controls?.target.clone();
    try {
      // 1) 先等画面真的有内容：泼溅还在建细节层时截出来是空的。
      const deadline = performance.now() + timeoutMs;
      for (;;) {
        if (scene && camera) renderer.render(scene, camera);
        if (!splatLodPendingRef.current && !looksBlank()) break;
        if (performance.now() >= deadline) break;
        await new Promise((resolve) => setTimeout(resolve, 200));
      }
      // 2) 照当前视角截，模型常常只占中间一小块；把相机临时推到能框住全部
      //    可见对象的位置（留 35% 余量，泼溅毛边不至于出画），截完立刻还原，用户视角不受影响。
      const box = new THREE.Box3();
      for (const entry of objectsRef.current) {
        if (!entry.object.visible) continue;
        entry.object.updateMatrixWorld(true);
        if (entry.splat) {
          box.union(entry.splat.getBoundingBox(false).applyMatrix4(entry.object.matrixWorld));
        } else {
          box.union(new THREE.Box3().setFromObject(clonePosedContent(entry.object)));
        }
      }
      if (camera && controls && !box.isEmpty()) {
        const center = box.getCenter(new THREE.Vector3());
        const radius = Math.max(box.getSize(new THREE.Vector3()).length() / 2, 0.001);
        const fov = (camera.fov * Math.PI) / 180;
        const distance = (radius / Math.sin(fov / 2)) * 1.35;
        const direction = camera.position.clone().sub(controls.target).normalize();
        if (direction.lengthSq() === 0) direction.set(0.35, 0.3, 0.9).normalize();
        camera.position.copy(center).addScaledVector(direction, distance);
        camera.lookAt(center);
        controls.target.copy(center);
        controls.update();
      }
      // 取景后必须在同一个任务里立刻抓，中间不能 await，否则绘制缓冲可能已被清掉。
      if (scene && camera) renderer.render(scene, camera);
      const shrunk = await shrinkToThumbBlob(canvas);
      if (shrunk) return shrunk;
      // 极少数环境拿不到 2D 上下文时，退回整张原图（PNG），别让存库直接失败。
      const dataUrl = canvas.toDataURL("image/png");
      return await (await fetch(dataUrl)).blob();
    } finally {
      // 还原视角与 gizmo，并补一帧，免得屏幕上留下临时取景的残影。
      if (camera && savedPosition) camera.position.copy(savedPosition);
      if (controls && savedTarget) {
        controls.target.copy(savedTarget);
        controls.update();
      }
      if (gizmo) gizmo.visible = gizmoVisible;
      restorePoseHelpers();
      if (scene && camera) renderer.render(scene, camera);
    }
  }, []);

  // 3D 资产入库：模型本体进素材库（type=3d，可被预演绑定），预览图取当前渲染帧。
  // 泼溅进不了 GLB，要单层单独存——见下面 saveSplatToLibrary。
  const saveToLibrary = useCallback(async () => {
    if (objectsRef.current.length === 0) return;
    if (!objectsRef.current.some((entry) => !entry.splat)) {
      setError("场景里只有泼溅，造不了 GLB 素材；请在左侧对象列表里逐层「存库」，或先存成场景存档");
      return;
    }
    setBusy("正在存入素材库…");
    setError(null);
    try {
      const binary = await exportGeometry();
      const glb = new Blob([binary], { type: "model/gltf-binary" });
      const thumb = await captureThumbnail();
      const form = new FormData();
      form.append("file", glb, `scene-${Date.now()}.glb`);
      form.append("type", "3d");
      if (thumb) form.append("thumbnail", thumb, "thumb.png");
      const response = await fetch("/api/materials", { method: "POST", body: form });
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(payload.error || `存入素材库失败（HTTP ${response.status}）`);
      }
      const material = await response.json();
      const skipped = objectsRef.current.filter((entry) => entry.splat).length;
      setStatus(
        `已存入素材库：${material.name}（约 ${(binary.byteLength / 1024 / 1024).toFixed(1)} MB）` +
          (skipped > 0 ? `；${skipped} 层泼溅进不了 GLB，需逐层「存库」` : ""),
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [captureThumbnail, exportGeometry]);

  // 泼溅单独入库：把 .spz 原始字节按原样交上去，预览图取当前渲染帧。
  const saveSplatToLibrary = useCallback(async (entry: SceneObjectEntry) => {
    if (!entry.splatUrl) {
      setError(`泼溅「${entry.name}」没有来源地址，存不了素材库`);
      return;
    }
    setBusy(`正在存入泼溅「${entry.name}」…`);
    setError(null);
    try {
      const source = await fetch(entry.splatUrl);
      if (!source.ok) throw new Error(`读取泼溅失败（HTTP ${source.status}）`);
      const bytes = await source.arrayBuffer();
      const thumbnail = await captureThumbnail();
      const form = new FormData();
      form.append("file", new Blob([bytes], { type: "application/octet-stream" }), splatFileName(entry.name));
      form.append("type", "3d");
      if (thumbnail) form.append("thumbnail", thumbnail, "thumb.png");
      const response = await fetch("/api/materials", { method: "POST", body: form });
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(payload.error || `存入素材库失败（HTTP ${response.status}）`);
      }
      const material = await response.json();
      setStatus(`已存入素材库：${material.name}（约 ${(bytes.byteLength / 1024 / 1024).toFixed(1)} MB）`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [captureThumbnail]);

  // 本地下载与「存到后端」共用同一份文档：GLB、泼溅、机位序列都在这里成型。
  const buildSceneDocument = useCallback(async (): Promise<SceneDocument> => {
    stopAnimation();
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (objectsRef.current.length === 0 || !camera || !controls) {
      throw new Error("视口里还没有对象，先放入模型再存场景");
    }
    {
      const before = fingerprintRef.current().full();
      const epoch = sceneEpochRef.current;
      const recipeModule = objectsRef.current.some(entry => entry.object.userData.proceduralSource?.generator === 'toonflow-set') ? await import('./proceduralArchive') : null;
      const recipes = recipeModule?.createRecipeArchive(objectsRef.current);
      if (recipes) recipeModule!.validateRecipeArchive(recipes.archive);
      const binary = await exportGeometry({ flattenTransforms: true, recipeIds: new Set(recipes?.references.keys()) });
      // 泼溅不在 GLB 里，得先把 .spz 字节取回来内嵌；取不到就整体失败，
      // 否则用户会得到一个默默丢了泼溅层的存档。
      const splats: SplatDocument[] = [];
      for (const entry of objectsRef.current) {
        if (!entry.splat) continue;
        if (!entry.splatUrl) throw new Error(`泼溅「${entry.name}」没有来源地址，无法写入存档`);
        const response = await fetch(entry.splatUrl);
        if (!response.ok) {
          throw new Error(`读取泼溅失败（${entry.name}，HTTP ${response.status}）`);
        }
        splats.push({
          id: entry.id, visible: entry.object.visible, locked: !!entry.locked,
          name: entry.name,
          data: arrayBufferToBase64(await response.arrayBuffer()),
          contentTransform: { position: entry.splat.position.toArray() as [number, number, number], quaternion: entry.splat.quaternion.toArray() as [number, number, number, number], scale: entry.splat.scale.toArray() as [number, number, number] },
          position: entry.object.position.toArray() as [number, number, number],
          quaternion: entry.object.quaternion.toArray() as [number, number, number, number],
          scale: entry.object.scale.toArray() as [number, number, number],
        });
      }
      const moverEntry = moverId
        ? objectsRef.current.find((entry) => entry.id === moverId) ?? null
        : null;
      // 落点齐了、也选了对象，才把移动计划写进存档。
      const movePlan =
        moverEntry && toGroundPath(groundPoints)
          ? { targetId: moverEntry.id, targetName: moverEntry.name, seconds: normalizeMoveSeconds(moveSeconds), follow: followMover,
              animation: saveMoveAnimation(moverEntry.object.animations, moveAnimationIndex) }
          : null;
      const doc: SceneDocument = {
        version: recipes?.references.size ? 7 : 6,
        ...(recipes?.references.size ? { procedural: recipes.archive } : {}),
        name: archiveName.trim(),
        frame: { aspect: frameRef.current },
        planes: objectsRef.current.filter((entry) => entry.mediaKind).map((entry) => {
          if (!entry.mediaSource) throw new Error(`平面${entry.name}缺少可恢复的媒体来源`);
          return { id: entry.id, visible: entry.object.visible, locked: !!entry.locked, name: entry.name, kind: entry.mediaKind!, source: entry.mediaSource,
            settings: normalizePlaneSettings(entry.plane), position: entry.object.position.toArray() as [number, number, number],
            quaternion: entry.object.quaternion.toArray() as [number, number, number, number], scale: entry.object.scale.toArray() as [number, number, number] };
        }),
        skybox: skyboxSourceRef.current ? { source: skyboxSourceRef.current, name: skyboxName || "全景", intensity: skybox.intensity, asEnvironment: skybox.asEnvironment } : null,
        createdAt: new Date().toISOString(),
        lightPreset,
        camera: {
          position: camera.position.toArray() as [number, number, number],
          target: controls.target.toArray() as [number, number, number],
          fov: camera.fov,
        },
        objects: objectsRef.current
          // 泼溅的变换记在 splats 里，不能混进 GLB 节点名单，否则载入时按名回填会错位。
          .filter((entry) => !entry.splat && !entry.mediaKind)
          .map((entry) => ({
            id: entry.id, visible: entry.object.visible, locked: !!entry.locked,
            name: entry.name,
            position: entry.object.position.toArray() as [number, number, number],
            quaternion: entry.object.quaternion.toArray() as [number, number, number, number],
            scale: entry.object.scale.toArray() as [number, number, number],
            // 存盘始终记录当前骨姿，不依赖是否点过内存记忆按钮。
            bonePose: serializeBonePose(entry.object),
            initialBonePose: entry.initialBonePose ?? null,
            animations: saveAnimationClips(entry.object),
            ...(recipes?.references.has(entry.id) ? { procedural: recipes.references.get(entry.id) } : {}),
          })),
        splats,
        // 机位序列以前只活在内存里，刷新就没；v3 起跟场景一起存。
        groundPath: toGroundPath(groundPoints),
        move: movePlan,
        cameraKeys: cameraKeys.map((key) => ({
          id: key.id,
          position: key.position,
          target: key.target,
          duration: normalizeCameraDuration(key.duration),
          easing: normalizeCameraEasing(key.easing),
          fov: normalizeCameraFov(key.fov),
        })),
        // 镜头表是可选的：没有计划时不写字段，旧读取路径不受影响。
        ...(scriptShots ? { scriptShots } : {}),
        // 环境同样是可选成员：没启用就不写字段，旧档保持「平地 + 灯光预设」。
        // 写前再校验一次（存档版本由 environment.version 自己承担），未知结构不会落盘。
        ...(environment ? { environment: validateSceneEnvironment(environment) } : {}),
        model: {
          name: objectsRef.current.map((entry) => entry.name).join("+") || "scene",
          data: arrayBufferToBase64(binary),
        },
      };
      if (epoch !== sceneEpochRef.current) throw new Error('保存期间场景已替换，请重新保存');
      saveGuardRef.current.assertUnchanged(before, fingerprintRef.current().full());
      validateSceneSize(JSON.stringify(doc));
      return doc;
    }
  }, [cameraKeys, exportGeometry, followMover, groundPoints, lightPreset, moveSeconds, moverId, moveAnimationIndex, skybox, skyboxName, archiveName, scriptShots, environment]);

  const saveScene = useCallback(async () => {
    if (objectsRef.current.length === 0) return;
    const fingerprint = fingerprintRef.current();
    const before = fingerprint.full();
    const operation = operationsRef.current.start('正在保存场景…');
    setBusy("正在保存场景…");
    try {
      const snapshotDoc = await buildSceneDocument();
      const image = await createImageBitmap(await captureCurrentView());
      const previewCanvas = window.document.createElement('canvas'); previewCanvas.width = 240; previewCanvas.height = Math.max(1, Math.round(240 * image.height / image.width));
      previewCanvas.getContext('2d')?.drawImage(image, 0, 0, previewCanvas.width, previewCanvas.height); image.close();
      snapshotDoc.preview = previewCanvas.toDataURL('image/jpeg', .65);
      const json = JSON.stringify(snapshotDoc); validateSceneSize(json);
      saveGuardRef.current.assertUnchanged(before, fingerprintRef.current().full());
      const url = URL.createObjectURL(new Blob([json], { type: "application/json" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = archiveName.trim() ? `${archiveName.trim().replace(/[\\/:*?"<>|]/g, '_')}.json` : sceneArchiveFileName(Date.now());
      link.click();
      URL.revokeObjectURL(url);
      saveGuardRef.current.markSaved(before, fingerprint); setDirty(false);
      setStatus(`已保存场景 ${Math.max(1, Math.round(json.length / 1024))} KB（本地文件）`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      operation.finish(); setBusy(null);
    }
  }, [buildSceneDocument, archiveName]);

  // 存到后端：JSON 落 data/scene-archives/<id>.json，前端刷新或换机器都能选回来。
  const saveSceneToServer = useCallback(async () => {
    if (objectsRef.current.length === 0) return;
    const fingerprint = fingerprintRef.current();
    const before = fingerprint.full();
    const operation = operationsRef.current.start('正在存到后端…');
    setBusy("正在存到后端…");
    try {
      const snapshotDoc = await buildSceneDocument();
      const image = await createImageBitmap(await captureCurrentView());
      const previewCanvas = window.document.createElement('canvas'); previewCanvas.width = 240; previewCanvas.height = Math.max(1, Math.round(240 * image.height / image.width));
      previewCanvas.getContext('2d')?.drawImage(image, 0, 0, previewCanvas.width, previewCanvas.height); image.close();
      snapshotDoc.preview = previewCanvas.toDataURL('image/jpeg', .65);
      const json = JSON.stringify(snapshotDoc); validateSceneSize(json);
      saveGuardRef.current.assertUnchanged(before, fingerprintRef.current().full());
      const blocker = sceneArchiveBlocker(json);
      if (blocker) throw new Error(blocker);
      const name = archiveName.trim() ? `${archiveName.trim().replace(/[\\/:*?"<>|]/g, '_')}.json` : sceneArchiveFileName(Date.now());
      const projectId = Number(archiveProjectId) || null;
      const response = await fetch("/api/scene-archives", {
        method: "POST",
        body: sceneArchiveFormBody(json, name, projectId),
      });
      if (!response.ok) {
        const detail = await response.text().catch(() => "");
        throw new Error(`存到后端失败（HTTP ${response.status}）${detail ? `：${detail.slice(0, 200)}` : ""}`);
      }
      const saved = (await response.json().catch(() => null)) as { id?: number } | null;
      const unchanged = before === fingerprintRef.current().full();
      if (unchanged) { saveGuardRef.current.markSaved(before, fingerprint); setDirty(false); }
      else { setDirty(true); setError('快照已存入后端，但场景随后发生变化；当前修改尚未保存'); }
      // 列表在打开时按独立筛选与页码刷新；保存不覆盖正在浏览的分页上下文。
      setStatus(
        `已存到后端${saved?.id ? `（存档 #${saved.id}）` : ""}：${archiveSizeLabel(json.length)}，${name}`,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      operation.finish(); setBusy(null);
    }
  }, [buildSceneDocument, archiveProjectId, archiveName]);

  // 本地文件与后端存档都走这一条：解析、校验 version/model.data、回填对象与机位。
  const importSceneText = useCallback(
    async (source: string | (() => Promise<string>)) => {
      stopAnimation();
      if (saveGuardRef.current.isDirty(fingerprintRef.current().full()) && !window.confirm('场景有未保存的修改，仍要载入其他场景？')) return false;
      const operation = operationsRef.current.start('正在载入场景…');
      // Retain the current path action until the replacement and its reference validate.
      setBusy("正在载入场景…");
      setError(null);
      const epoch = ++sceneEpochRef.current;
      // Cancel synchronously: throttled rAF must not leave playback UI or an old timer alive.
      if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
      if (cameraPathRef.current !== null) cancelAnimationFrame(cameraPathRef.current);
      if (cameraPathTimerRef.current !== null) window.clearTimeout(cameraPathTimerRef.current);
      moveRafRef.current = null; cameraPathRef.current = null; cameraPathTimerRef.current = null;
      setPlayingMove(false); setPlayingPath(false);
      const pending: SceneObjectEntry[] = [];
      let pendingSky: THREE.Texture | null = null;
      let committed = false;
      try {
        // Reserve this generation at selection time, before File.text() can resolve out of order.
        const text = typeof source === 'function' ? await source() : source;
        if (epoch !== sceneEpochRef.current) return false;
        const doc = JSON.parse(text) as SceneDocument;
        validateCompositionDocument(doc);
        // v1 是没泼溅层的老存档、v2 多了泼溅层，都仍然要能读；v3 多了机位序列。
        if ((doc.version !== 1 && doc.version !== 2 && doc.version !== 3 && doc.version !== 4 && doc.version !== 5 && doc.version !== 6 && doc.version !== 7) || !doc.model?.data) {
          throw new Error("不是可识别的场景文件");
        }
        validateSceneSize(text);
        // 镜头表是可选字段：老存档没有就保持 null；有但不认识的版本/结构直接失败，不替换当前场景。
        const restoredShots = doc.scriptShots === undefined ? null : (await import('./toonflowSets')).validateScriptShotPlan(doc.scriptShots);
        // 环境是可选成员：旧档没有就保持 null（平地 + 灯光预设）；有但不认识就显式失败。
        // 这一步在替换当前场景之前，失败不会动到现在打开的场景。
        const restoredEnvironment = doc.environment === undefined || doc.environment === null
          ? null
          : validateSceneEnvironment(doc.environment);
        const recipeModule = doc.version === 7 ? await import('./proceduralArchive') : null;
        if (recipeModule) recipeModule.validateRecipeArchive(doc.procedural);
        const gltf = await new GLTFLoader().parseAsync(base64ToArrayBuffer(doc.model.data), "");
        pending.push({ id: "pending-geometry", name: doc.model.name, object: gltf.scene });
        const restoredPlanes: SceneObjectEntry[] = [];
        for (const saved of doc.planes ?? []) {
          if (!/^data:(image|video)\//.test(saved.source) || !["image", "video"].includes(saved.kind)) throw new Error("平面媒体来源无效");
          const entry = await addPlane(saved.source, saved.name, saved.kind, null, saved.settings, true);
          if (!entry) throw new Error(`平面恢复失败：${saved.name}`);
          entry.id = saved.id || entry.id;
          entry.locked = saved.locked ?? false;
          entry.object.visible = saved.visible ?? true;
          if (!entry.object.visible) entry.video?.element.pause();
          entry.object.position.fromArray(saved.position);
          entry.object.quaternion.fromArray(saved.quaternion);
          entry.object.scale.fromArray(saved.scale);
          restoredPlanes.push(entry);
          pending.push(entry);
        }
        if (doc.skybox && !/^data:image\//.test(doc.skybox.source)) throw new Error("天空盒媒体来源无效");
        const restoredSky = doc.skybox ? await new THREE.TextureLoader().loadAsync(doc.skybox.source) : null;
        pendingSky = restoredSky;
        // 导出时每个对象都是带自身变换的顶层节点，直接接管为场景对象，
        // 不再套一层空 Group，否则面板读到的是那层空壳的 0 变换。
        const imported = gltf.scene.children.slice();
        const geometryObjects = doc.objects?.filter(item => item.procedural === undefined);
        if (doc.version >= 6 && (imported.length !== geometryObjects?.length || new Set(imported.map(child => child.userData.sceneDocumentId)).size !== imported.length || imported.some(child => !geometryObjects.some(item => item.id === child.userData.sceneDocumentId)))) {
          throw new Error('对象动作归属与场景几何不兼容，场景未替换');
        }
        const entries: SceneObjectEntry[] = imported.map((child, index) => {
          const saved = doc.version >= 4 ? doc.objects?.find((item) => item.id === child.userData.sceneDocumentId) ?? doc.objects?.[index] : doc.objects?.find((item) => item.name === child.name) ?? doc.objects?.[index];
          if (saved) {
            child.position.fromArray(saved.position);
            child.quaternion.fromArray(saved.quaternion);
            child.scale.fromArray(saved.scale);
          }
          const entry: SceneObjectEntry = {
            id: saved?.id || crypto.randomUUID(),
            name: saved?.name || child.name || doc.model.name,
            object: child, locked: saved?.locked ?? false,
          };
          pending.push(entry);
          child.animations = restoreAnimationClips(child, saved?.animations);
          child.visible = saved?.visible ?? true;
          // 先在绑定姿势上记一份初始快照，再把存档里的摆姿套回去。
          const bindPose = serializeBonePose(child);
          if (bindPose) {
            entry.initialBonePose = saved?.initialBonePose ?? bindPose;
            entry.bonePose = saved?.bonePose ?? bindPose;
            if (saved?.bonePose && !applyBonePose(child, saved.bonePose)) {
              disposeSceneEntry(entry);
              throw new Error("骨骼姿势与模型不兼容，场景未替换");
            }
          }
          return entry;
        });
        gltf.scene.clear(); // Keep unprocessed children owned by pending geometry until all bindings validate.
        if (recipeModule && doc.procedural) {
          for (const saved of doc.objects.filter(item => item.procedural !== undefined)) {
            if (saved.animations?.length || saved.bonePose || saved.initialBonePose) throw new Error('程序化布景不支持骨骼资源，请保留GLB');
            const object = recipeModule.restoreRecipeObject(doc.procedural, saved.procedural!);
            const entry: SceneObjectEntry = { id: saved.id!, name: saved.name, locked: saved.locked, object };
            pending.push(entry);
            object.position.fromArray(saved.position); object.quaternion.fromArray(saved.quaternion); object.scale.fromArray(saved.scale); object.visible = saved.visible!;
            entries.push(entry);
          }
          const order = new Map(doc.objects.map((item, index) => [item.id, index]));
          entries.sort((a, b) => order.get(a.id)! - order.get(b.id)!);
        }
        // 泼溅层：存档里自带 .spz 字节，解成 blob URL 重新交给 Spark，
        // 这样存档不依赖 ComfyUI output 或素材库文件还在不在。
        const splatEntries: SceneObjectEntry[] = [];
        for (const saved of doc.splats ?? []) {
          const blob = new Blob([base64ToArrayBuffer(saved.data)], { type: "application/octet-stream" });
          const url = URL.createObjectURL(blob);
          let mesh: Awaited<ReturnType<typeof loadSplatMesh>>;
          try { mesh = await loadSplatMesh(url, { placeOnGround: false }); }
          catch (err) { URL.revokeObjectURL(url); throw err; }
          if (saved.contentTransform) {
            mesh.position.fromArray(saved.contentTransform.position);
            mesh.quaternion.fromArray(saved.contentTransform.quaternion);
            mesh.scale.fromArray(saved.contentTransform.scale);
          }
          // 和其他对象一样包一层 Group：归一化与用户摆位都记在这层上。
          const group = new THREE.Group();
          group.name = saved.name;
          group.add(mesh);
          group.position.fromArray(saved.position);
          group.quaternion.fromArray(saved.quaternion);
          group.scale.fromArray(saved.scale);
          group.visible = saved.visible ?? true;
          const splatEntry: SceneObjectEntry = {
            id: saved.id || crypto.randomUUID(), locked: saved.locked ?? false,
            name: saved.name,
            object: group,
            splat: mesh,
            splatUrl: url,
          };
          splatEntries.push(splatEntry);
          pending.push(splatEntry);
        }
        const savedPath = normalizeGroundPath(doc.groundPath);
        const savedMove = doc.move && typeof doc.move.targetName === "string" ? doc.move : null;
        const loadedMover = savedMove
          ? [...entries, ...splatEntries, ...restoredPlanes].find(entry => savedMove.targetId ? entry.id === savedMove.targetId : entry.name === savedMove.targetName) ?? null
          : null;
        if (doc.move?.animation !== undefined && (!savedMove?.targetId || !savedPath || !loadedMover || loadedMover.id !== savedMove.targetId)) {
          throw new Error('路径动作缺少有效移动目标或路径，场景未替换');
        }
        const loadedAnimationIndex = restoreMoveAnimation(loadedMover?.object.animations ?? [], doc.move?.animation);
        const scene = sceneRef.current;
        if (!scene || epoch !== sceneEpochRef.current) throw new Error("场景已关闭或载入已被较新的请求替代");
        selectObject(null);
        historyRef.current.clear(objectsRef.current);
        objectsRef.current.forEach((entry) => { scene.remove(entry.object); disposeSceneEntry(entry); });
        clearSkybox();
        if (restoredSky && doc.skybox) {
          restoredSky.mapping = THREE.EquirectangularReflectionMapping;
          restoredSky.colorSpace = THREE.SRGBColorSpace;
          skyboxTextureRef.current = restoredSky;
          skyboxSourceRef.current = doc.skybox.source;
          scene.background = restoredSky;
          scene.backgroundIntensity = doc.skybox.intensity;
          scene.environment = doc.skybox.asEnvironment ? restoredSky : null;
          setSkybox({ enabled: true, intensity: doc.skybox.intensity, asEnvironment: doc.skybox.asEnvironment });
          setSkyboxName(doc.skybox.name);
        }
        objectsRef.current = [...entries, ...splatEntries, ...restoredPlanes];
        objectsRef.current.forEach((entry) => scene.add(entry.object));
        committed = true;
        sequenceFramesRef.current = null;
        if (referenceUrlRef.current) URL.revokeObjectURL(referenceUrlRef.current);
        referenceUrlRef.current = null; setReferenceShot(null);
        cleanOnRenderRef.current = true;
        setDirty(false);
        // 载入的存档带泼溅：同样要等 LoD 树建完才能画，挂上提示等 onDirty 收。
        if (splatEntries.length > 0) beginSplatLodWait();
        syncObjects();
        selectObject(objectsRef.current[0]?.id ?? null);
        // 地面路径与移动目标（v3 可选字段）：老存档没有就清空，别把上一个场景的带走。
        // 这两行必须先取出来：下面「按名字找移动目标」要用，写在后面会撞上暂时性死区。
        // Resources/references were validated before replacing the old scene.
        previousMoverRef.current = loadedMover?.id ?? null;
        setMoverId(loadedMover?.id ?? null);
        setMoveAnimationIndex(loadedAnimationIndex);
        setMoveSeconds(normalizeMoveSeconds(savedMove?.seconds));
        setFollowMover(Boolean(savedMove?.follow));
        applyLightPreset(doc.lightPreset);
        // 环境在场景替换后应用：地形网格、太阳、雾与天空一起换，旧档读出 null 就回平地。
        applyEnvironment(restoredEnvironment, doc.lightPreset);
        // 落点恢复（vias 是可选途经点数组：老存档没有、或只有单个 via，都由 normalizeGroundPath 归一）
        setGroundPoints({
          start: savedPath?.start ?? null,
          end: savedPath?.end ?? null,
          vias: savedPath?.vias ?? null,
        });
        placingGroundRef.current = null;
        setPlacingGround(null);
        // 机位序列（v3）：老存档没有就清空，免得上一个场景的机位串进来。
        setCameraKeys(
          (doc.cameraKeys ?? []).map((key) => ({
            id: key.id || crypto.randomUUID(),
            position: key.position,
            target: key.target,
            duration: normalizeCameraDuration(key.duration),
            easing: normalizeCameraEasing(key.easing),
            fov: normalizeCameraFov(key.fov),
          })),
        );
        const camera = cameraRef.current;
        const controls = controlsRef.current;
        if (camera && controls) {
          camera.position.set(...doc.camera.position);
          controls.target.set(...doc.camera.target);
          camera.fov = normalizeCameraFov(doc.camera.fov);
          camera.updateProjectionMatrix();
          controls.update();
        }
        setScriptShots(restoredShots);
        frameRef.current = doc.frame?.aspect ?? '16:9'; setFrameAspect(frameRef.current); resizeFrameRef.current();
        setFocal(focalFromFov(cameraRef.current?.fov ?? 45)); setArchiveName(doc.name ?? '');
        setLocationGenerationOpen(false);
        setStatus(
          splatEntries.length > 0
            ? `已载入场景（${entries.length} 个对象 + ${splatEntries.length} 个泼溅）`
            : `已载入场景（${entries.length} 个对象）`,
        );
        return true;
      } catch (err) {
        if (epoch === sceneEpochRef.current) setError(err instanceof Error ? err.message : String(err));
        return false;
      } finally {
        if (!committed) { pending.forEach(disposeSceneEntry); pendingSky?.dispose(); }
        operation.finish();
        // A stale load owns its resources, never the current load's error or busy state.
        // 带泼溅的存档把 busy 交给 SparkRenderer.onDirty 收尾。
        if (epoch === sceneEpochRef.current) setBusy((current) => (current === SPLAT_LOD_BUSY ? current : null));
      }
    },
    [applyEnvironment, applyLightPreset, beginSplatLodWait, syncObjects, addPlane, clearSkybox],
  );

  const loadScene = useCallback(
    async (file: File) => {
      await importSceneText(() => file.text());
    },
    [importSceneText],
  );

  // 存档项目归属与列表筛选独立，不受提示词面板影响。
  const [archivesOpen, setArchivesOpen] = useState(false);
  const [archives, setArchives] = useState<SceneArchiveItem[]>([]);
  const [archivesBusy, setArchivesBusy] = useState(false);
  const [archiveFilter, setArchiveFilter] = useState('all');
  const [archivePage, setArchivePage] = useState(1);
  const [archiveTotal, setArchiveTotal] = useState(0);
  const [renameId, setRenameId] = useState<number | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [deleteArchive, setDeleteArchive] = useState<SceneArchiveItem | null>(null);
  const [archivePreview, setArchivePreview] = useState<{ name: string; preview?: string; count: number; aspect: string; restore: () => Promise<void> } | null>(null);
  const archiveRequestRef = useRef(0);
  const archiveListRequestRef = useRef(0);

  const loadSceneArchives = useCallback(async (page = 1, filter = archiveFilter) => {
    const ticket = ++archiveListRequestRef.current;
    setArchivesBusy(true);
    setError(null);
    try {
      const query = `?page=${page}&page_size=10${filter === 'all' ? '' : `&project_id=${encodeURIComponent(filter)}`}`;
      const response = await fetch(`/api/scene-archives${query}`);
      if (!response.ok) throw new Error(`读取后端存档失败（HTTP ${response.status}）`);
      const data = await response.json();
      if (ticket !== archiveListRequestRef.current) return false;
      setArchives(parseSceneArchiveList(data));
      setArchiveTotal(data.total); setArchivePage(page); setArchiveFilter(filter);
      setArchivesOpen(true);
      setStatus("后端存档列表已刷新");
      return true;
    } catch (err) {
      if (ticket === archiveListRequestRef.current) setError(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      if (ticket === archiveListRequestRef.current) setArchivesBusy(false);
    }
  }, [archiveFilter]);

  const manageArchive = async (id: number, method: 'PATCH' | 'DELETE') => {
    const ticket = ++archiveListRequestRef.current;
    setArchivesBusy(true);
    setStatus('');
    try {
      const response = await fetch(`/api/scene-archives/${id}`, {
        method, headers: { 'Content-Type': 'application/json' },
        ...(method === 'PATCH' ? { body: JSON.stringify({ name: renameValue.trim() }) } : {}),
      });
      if (!response.ok) throw new Error(`存档操作失败（HTTP ${response.status}）`);
      setRenameId(null); setDeleteArchive(null);
      if (ticket !== archiveListRequestRef.current) return;
      const refreshed = await loadSceneArchives(method === 'DELETE' && archives.length === 1 ? Math.max(1, archivePage-1) : archivePage);
      if (refreshed) setStatus(method === 'DELETE' ? '已移入回收区，原始文件保留' : '已更新存档列表名称');
    } catch (err) { if (ticket === archiveListRequestRef.current) setError(String(err)); }
    finally { if (ticket === archiveListRequestRef.current) setArchivesBusy(false); }
  };

  const openSceneArchive = useCallback(
    async (item: SceneArchiveItem) => {
      const ticket = ++archiveRequestRef.current;
      const operation = operationsRef.current.start(`正在载入后端存档 #${item.id}…`);
      setError(null);
      try {
        const response = await fetch(`/api/scene-archives/${item.id}`);
        if (!response.ok) throw new Error(`读取存档 #${item.id} 失败（HTTP ${response.status}）`);
        const text = await response.text(); validateSceneSize(text);
        const doc = JSON.parse(text) as SceneDocument; validateCompositionDocument(doc);
        if (ticket !== archiveRequestRef.current) return;
        setArchivePreview({ name: item.name, preview: typeof doc.preview === 'string' && doc.preview.length < 256_000 && /^data:image\/(png|jpeg);base64,/.test(doc.preview) ? doc.preview : undefined,
          count: (doc.objects?.length ?? 0) + (doc.planes?.length ?? 0) + (doc.splats?.length ?? 0), aspect: doc.frame?.aspect ?? '16:9',
          restore: async () => {
            if (ticket !== archiveRequestRef.current) return;
            const restored = await importSceneText(text);
            if (restored && ticket === archiveRequestRef.current) { setArchivePreview(null); setArchivesOpen(false); }
          } });
      } catch (err) {
        if (ticket === archiveRequestRef.current) setError(err instanceof Error ? err.message : String(err));
      } finally {
        operation.finish();
      }
    },
    [importSceneText],
  );

  // 渲一帧参考图并取回 PNG：藏编辑器痕迹、换材质、黑底、取图、还原都在这里。
  // 单帧导出和序列导出共用同一条路径，免得两条路各修一遍。
  const captureReference = useCallback(async (pass: ReferencePass) => {
    stopAnimation();
    const sampleExport = exportSampleRef.current;
    exportSampleRef.current = null;
    const restoreExportSample = sampleExport?.();
    try {
    const renderer = rendererRef.current;
    const scene = sceneRef.current;
    const camera = cameraRef.current as THREE.PerspectiveCamera | null;
    if (!renderer || !scene || !camera) return null;
    const previousOverride = scene.overrideMaterial;
    const previousBackground = scene.background;
    // 参考图是几何/法线/深度 pass，不烘天气：雾是画面效果，混进去会让同一份几何两张图不一样。
    // 取图期间临时关掉，finally 里按原值放回。
    const previousFog = scene.fog;
    const hidden: THREE.Object3D[] = [];
    // 逐个网格换材质：scene.overrideMaterial 会把模型整块画没了（只留黑底），
    // 逐网格替换才能稳定出图。
    const swapped: Array<{ mesh: THREE.Mesh; material: THREE.Material | THREE.Material[] }> = [];
    const alphaMaterials: THREE.Material[] = [];
    // 深度范围按模型自己的包围盒算：直接用 camera.near/far（0.1–1000）时，
    // 几米内的模型只占 1% 灰度，看上去就是一整张黑图。
    const bounds = new THREE.Box3();
    objectsRef.current.filter(entry => entry.object.visible).forEach((entry) => bounds.expandByObject(entry.splat ? entry.object : clonePosedContent(entry.object)));
    // 地形是有内容的地面（不是编辑器地面网格）：参与参考图，也参与深度范围计算。
    if (terrainMeshRef.current) bounds.expandByObject(terrainMeshRef.current);
    const depthRange = { near: camera.near, far: camera.far };
    if (!bounds.isEmpty()) {
      const center = bounds.getCenter(new THREE.Vector3());
      const radius = Math.max(0.25, bounds.getSize(new THREE.Vector3()).length() / 2);
      const distance = camera.position.distanceTo(center);
      depthRange.near = Math.max(0.01, distance - radius);
      depthRange.far = Math.max(depthRange.near + 0.5, distance + radius);
    }
    const material = createReferenceMaterial(pass, depthRange);
    const restoreReferencePoseHelpers = hidePoseEditorHelpers(scene);
    const hide = (object: THREE.Object3D | null | undefined) => {
      if (!object || !object.visible) return;
      object.visible = false;
      hidden.push(object);
    };
    try {
      // 网格线、地面、gizmo、骨架线都不进参考图：它们不是要被约束的几何。
      hide(gridRef.current);
      hide(groundRef.current);
      hide(groundPathGroupRef.current);
      hide(transformRef.current?.getHelper?.() ?? null);
      scene.children.forEach((child) => {
        if (child instanceof THREE.SkeletonHelper) hide(child);
      });
      // 泼溅由 Spark 自己光栅化，逐网格换材质管不到它；混进灰度图会变成彩色噪点。
      hide(sparkRef.current);
      hide(scene.getObjectByName('__Weather__')); // Particles are atmosphere, never geometry/depth constraints.
      const paint = (object: THREE.Object3D) => {
        if (isPoseEditorHelper(object)) return;
        const mesh = object as THREE.Mesh;
        if (mesh.isMesh) {
          const geometry = mesh.geometry as THREE.BufferGeometry | undefined;
          // 没存法线的 GLB 很常见；换成 Standard / Normal 材质后光照拿不到法线，
          // 整块会渲成纯黑。先在原几何体上补一份。
          if (geometry && !geometry.getAttribute("normal")) {
            geometry.computeVertexNormals();
          }
          swapped.push({ mesh, material: mesh.material });
          const entry = objectsRef.current.find((item) => item.mediaKind && (item.object === mesh || item.object.children.includes(mesh)));
          const sourceMaterial = Array.isArray(mesh.material) ? mesh.material[0] : mesh.material;
          const texture = (sourceMaterial as THREE.MeshBasicMaterial)?.map;
          if (entry?.plane && texture) {
            const alphaMaterial = referencePlaneMaterial(material, texture, entry.plane);
            alphaMaterials.push(alphaMaterial);
            mesh.material = alphaMaterial;
          } else mesh.material = material;
        }
        object.children.forEach(paint);
      };
      objectsRef.current.forEach((entry) => paint(entry.object));
      if (terrainMeshRef.current) paint(terrainMeshRef.current);
      // 黑色背景：深度图里天空=0，稳定；白模/法线也不想要灰底混进来。
      scene.background = new THREE.Color(0x000000);
      scene.fog = null;
      // render 与取图必须在同一个任务里，隔帧再取可能已被下一帧清掉。
      renderer.clear();
      renderer.render(scene, camera);
      const width = renderer.domElement.width;
      const height = renderer.domElement.height;
      // 先把这一帧同步拷进 2D 画布再编码：WebGL 缓冲在等 toBlob 期间会被下一帧清掉，
      // 直接 toBlob 会拿到黑图（缩略图那条路踩过同一个坑）。
      const snapshot = document.createElement("canvas");
      snapshot.width = width;
      snapshot.height = height;
      const context = snapshot.getContext("2d");
      if (!context) throw new Error("取不到 2D 画布上下文");
      context.drawImage(renderer.domElement, 0, 0, width, height);
      // Return the encoding promise without awaiting: finally restores live scene state synchronously.
      return new Promise<Blob | null>((resolve) => {
        snapshot.toBlob((value) => resolve(value), "image/png");
      }).then((blob) => {
        if (!blob) throw new Error("取不到参考图（画布为空）");
        return { blob, width, height };
      });
    } finally {
      // 无论如何都要还原，否则编辑器会一直停在参考图材质上。
      scene.overrideMaterial = previousOverride;
      scene.background = previousBackground;
      scene.fog = previousFog;
      swapped.forEach((item) => {
        item.mesh.material = item.material;
      });
      hidden.forEach((object) => {
        object.visible = true;
      });
      alphaMaterials.forEach((temporary) => temporary.dispose());
      material.dispose();
      restoreReferencePoseHelpers();
    }
    } finally { restoreExportSample?.(); }
  }, []);

  /**
   * 按指定机位渲一帧「视口原样」画面（带材质/灯光/天空盒/泼溅），只藏编辑器痕迹。
   *
   * 和 captureReference 的区别：不换材质、不拉黑背景。首尾帧是交给 H3 的定妆图，
   * 白模那种灰板子本身就是「内容」，会被照着画出来，所以这里必须是真实画面。
   */
  const captureViewportAt = useCallback(async (pose: MovableCamera) => {
    const sampleExport = exportSampleRef.current;
    exportSampleRef.current = null;
    const restoreExportSample = sampleExport?.();
    try {
    const renderer = rendererRef.current;
    const scene = sceneRef.current;
    const camera = cameraRef.current as THREE.PerspectiveCamera | null;
    const controls = controlsRef.current;
    if (!renderer || !scene || !camera) return null;
    const previous = {
      position: camera.position.clone(),
      target: controls ? controls.target.clone() : null,
      fov: camera.fov,
    };
    const hidden: THREE.Object3D[] = [];
    const restorePoseHelpers = hidePoseEditorHelpers(scene);
    const hide = (object: THREE.Object3D | null | undefined) => {
      if (!object || !object.visible) return;
      object.visible = false;
      hidden.push(object);
    };
    try {
      hide(gridRef.current);
      hide(groundRef.current);
      hide(groundPathGroupRef.current);
      hide(transformRef.current?.getHelper?.() ?? null);
      scene.children.forEach((child) => {
        if (child instanceof THREE.SkeletonHelper) hide(child);
      });
      camera.position.set(pose.position[0], pose.position[1], pose.position[2]);
      camera.fov = Math.min(90, Math.max(15, Number(pose.fov) || camera.fov));
      camera.updateProjectionMatrix();
      if (controls) {
        controls.target.set(pose.target[0], pose.target[1], pose.target[2]);
        controls.update();
      }
      camera.lookAt(new THREE.Vector3(pose.target[0], pose.target[1], pose.target[2]));
      renderer.clear();
      renderer.render(scene, camera);
      const width = renderer.domElement.width;
      const height = renderer.domElement.height;
      // 同步拷进 2D 画布再编码：WebGL 缓冲在等 toBlob 期间会被下一帧清掉。
      const snapshot = document.createElement("canvas");
      snapshot.width = width;
      snapshot.height = height;
      const context = snapshot.getContext("2d");
      if (!context) throw new Error("取不到 2D 画布上下文");
      context.drawImage(renderer.domElement, 0, 0, width, height);
      // Do not keep the temporary camera/helper state alive while PNG encoding is pending.
      return new Promise<Blob | null>((resolve) => {
        snapshot.toBlob((value) => resolve(value), "image/png");
      }).then((blob) => {
        if (!blob) throw new Error("取不到画面（画布为空）");
        return { blob, width, height };
      });
    } finally {
      // 无论如何都要把视角还回去，否则用户眼前会突然跳到落幅机位。
      restorePoseHelpers();
      hidden.forEach((object) => {
        object.visible = true;
      });
      camera.position.copy(previous.position);
      camera.fov = previous.fov;
      camera.updateProjectionMatrix();
      if (controls && previous.target) {
        controls.target.copy(previous.target);
        controls.update();
      }
      renderer.render(scene, camera);
    }
    } finally { restoreExportSample?.(); }
  }, []);

  /**
   * 导出首尾帧并存进素材库：起幅取第一个机位、落幅取最后一个机位。
   *
   * 存库而不是直接塞进场景：H3 首尾帧模板吃的是素材库里的图片文件名，
   * 用户在「视频」页选完这两张、模板选 minimax_h3_first_last 就能生成。
   */
  const exportFirstLastFrames = useCallback(async () => {
    if (busy) return;
    stopAnimation();
    const pair = firstLastCameraFrames(cameraKeys);
    if (!pair) {
      setError("先记录两个机位（起幅和落幅），再导首尾帧");
      return;
    }
    const epoch = sceneEpochRef.current;
    const sourceState = fingerprintRef.current().full();
    const viewState = () => [...(cameraRef.current?.position.toArray() ?? []), ...(controlsRef.current?.target.toArray() ?? []), cameraRef.current?.fov ?? 0];
    const sourceView = viewState();
    const path = editorGroundPath(groundPoints);
    const mover = path && moverId ? objectsRef.current.find(e => e.id === moverId && canMoveEntry(e)) ?? null : null;
    const seconds = normalizeMoveSeconds(moveSeconds);
    const assertCurrent = () => {
      if (epoch !== sceneEpochRef.current) throw new Error('场景已替换，已停止首尾帧导出');
      if (mover && (!canMoveEntry(mover) || !objectsRef.current.includes(mover))) throw new Error('移动对象已隐藏、锁定或删除，已停止首尾帧导出');
      if (sourceState !== fingerprintRef.current().full() || viewState().some((value, index) => Math.abs(value - sourceView[index]) > 1e-7)) throw new Error('导出期间场景或机位发生变化，已停止首尾帧导出，请重新核对；已入库的帧不会自动删除');
    };
    const operation = operationsRef.current.start('正在渲起幅/落幅两帧…');
    setBusy("正在渲起幅/落幅两帧…");
    setError(null);
    try {
      const stamp = fileNameStamp(new Date());
      const results: Array<{ edge: "first" | "last"; name: string; size: number }> = [];
      for (const edge of ["first", "last"] as const) {
        const elapsed = edge === 'first' ? 0 : cameraPathDuration(cameraKeys);
        const basePose = edge === "first" ? pair.first : pair.last;
        const progress = Math.min(1, elapsed / seconds);
        const delta = followMover && mover && path ? groundPathFollowDelta(path, progress) : [0, 0, 0];
        const pose = { ...basePose, position: basePose.position.map((v, i) => v + delta[i]) as [number, number, number], target: basePose.target.map((v, i) => v + delta[i]) as [number, number, number] };
        assertCurrent();
        exportSampleRef.current = () => {
          if (!mover || !path) return () => {};
          const p = mover.object.position.clone(), q = mover.object.quaternion.clone();
          let preview: AnimationPreview | null = null;
          try {
            const clip = mover.object.animations[moveAnimationIndex];
            if (moveAnimationIndex >= 0 && !clip) throw new Error('移动动作已失效，请重新选择');
            if (clip) { preview = new AnimationPreview(mover.object, clip); preview.sampleAt(Math.min(elapsed, seconds)); }
            mover.object.position.set(...groundPathPoint(path, progress));
            mover.object.rotation.y = groundPathYaw(path, progress);
            mover.object.updateMatrixWorld(true);
          } catch (err) { preview?.stop(); mover.object.position.copy(p); mover.object.quaternion.copy(q); throw err; }
          return () => { preview?.stop(); mover.object.position.copy(p); mover.object.quaternion.copy(q); mover.object.updateMatrixWorld(true); };
        };
        const shot = await captureViewportAt(pose);
        assertCurrent();
        if (!shot) throw new Error("取不到画面（画布为空）");
        const form = new FormData();
        form.append("file", shot.blob, cameraFrameFileName(edge, stamp));
        form.append("type", "image");
        const response = await fetch("/api/materials", { method: "POST", body: form });
        if (!response.ok) {
          const payload = await response.json().catch(() => ({}));
          throw new Error(payload.error || `存素材库失败（HTTP ${response.status}）`);
        }
        const material = await response.json();
        assertCurrent();
        results.push({
          edge,
          name: material.name ?? cameraFrameFileName(edge, stamp),
          size: Math.max(1, Math.round(shot.blob.size / 1024)),
        });
      }
      setFirstLastNote(firstLastMotionNote(cameraKeys));
      setStatus(
        `已渲出首尾帧并存进素材库：起幅 ${results[0].name}（${results[0].size} KB）、落幅 ${results[1].name}（${results[1].size} KB）。运镜文案在下面，粘进视频提示词一起提交，模板选 minimax_h3_first_last。`,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      exportSampleRef.current = null;
      operation.finish(); setBusy(null);
    }
  }, [cameraKeys, captureViewportAt, groundPoints, moverId, moveSeconds, moveAnimationIndex, followMover, busy]);

  /** 运镜文案只读展示 + 一键复制：首尾帧只锚两端，提交时必须把这句话一起写进提示词。 */
  const copyFirstLastNote = useCallback(async () => {
    if (!firstLastNote) return;
    try {
      await navigator.clipboard.writeText(firstLastNote);
      setStatus(`运镜文案已复制：${firstLastNote}`);
    } catch {
      setError("复制失败，请手动选中下面的文案");
    }
  }, [firstLastNote]);

  /**
   * 拉取某个项目的场次列表。写提示词之前必须先看清「写到哪一场」，
   * 所以这里只读 GET /api/projects/:id，不缓存、不改任何东西。
   */
  const loadPromptScenes = useCallback(async (projectId: string) => {
    if (!projectId) {
      setPromptScenes([]);
      setPromptSceneId("");
      return;
    }
    const response = await fetch(`/api/projects/${projectId}`);
    if (!response.ok) throw new Error(`读取项目失败（HTTP ${response.status}）`);
    const choices = scenePromptChoices(await response.json());
    setPromptScenes(choices);
    setPromptSceneId(choices.length ? String(choices[0].id) : "");
  }, []);

  const openPromptPanel = useCallback(async () => {
    setPromptPanelOpen(true);
    setPromptBusy(true);
    setError(null);
    try {
      const response = await fetch("/api/projects");
      if (!response.ok) throw new Error(`读取项目列表失败（HTTP ${response.status}）`);
      const projects = projectChoices(await response.json());
      setPromptProjects(projects);
      if (projects.length === 0) {
        setPromptScenes([]);
        setPromptSceneId("");
        setStatus("还没有项目：先建一个项目，再回来写运镜");
        return;
      }
      const next = projects.some((item) => item.id === promptProjectId) ? promptProjectId : projects[0].id;
      setPromptProjectId(next);
      await loadPromptScenes(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPromptBusy(false);
    }
  }, [promptProjectId, loadPromptScenes]);

  const switchPromptProject = useCallback(
    async (projectId: string) => {
      setPromptProjectId(projectId);
      setPromptBusy(true);
      setError(null);
      try {
        await loadPromptScenes(projectId);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setPromptBusy(false);
      }
    },
    [loadPromptScenes],
  );

  /**
   * 把运镜这一句追加到选定场次的视频提示词。
   * 只改 video_full_prompt（后端顺带重算执行稿），模板 / 首尾帧 / 3D 参考视频一概不动，
   * 也不会提交任务；已经在稿里就跳过，不重复写。
   */
  const appendNoteToScene = useCallback(async () => {
    const choice = promptScenes.find((item) => String(item.id) === promptSceneId);
    if (!choice) {
      setError("先选要写进哪一场");
      return;
    }
    if (!firstLastNote) {
      setError("还没有运镜文案：先导出首尾帧");
      return;
    }
    const next = appendMotionToPrompt(choice.prompt, firstLastNote);
    if (!next.added) {
      setStatus(`${choice.label} 的提示词里已经有这句运镜了，没有重复写`);
      return;
    }
    setPromptBusy(true);
    setError(null);
    try {
      const response = await fetch(`/api/projects/${promptProjectId}/scenes/${choice.id}/video/prompt`, {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ prompt: next.prompt }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error || `写入失败（HTTP ${response.status}）`);
      setPromptScenes((current) =>
        current.map((item) =>
          item.id === choice.id ? { ...item, prompt: String(payload.video_full_prompt ?? next.prompt) } : item,
        ),
      );
      setStatus(`已把「${next.line}」追加到 ${choice.label} 的视频提示词（模板与首尾帧没动）`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPromptBusy(false);
    }
  }, [promptScenes, promptSceneId, promptProjectId, firstLastNote]);

  const exportReference = useCallback(
    async (pass: ReferencePass) => {
      stopAnimation();
      const epoch = sceneEpochRef.current;
      const sourceState = fingerprintRef.current().full();
      const viewState = () => [...(cameraRef.current?.position.toArray() ?? []), ...(controlsRef.current?.target.toArray() ?? []), cameraRef.current?.fov ?? 0];
      const sourceView = viewState();
      const operation = operationsRef.current.start(`${REFERENCE_BUSY_PREFIX}（${REFERENCE_PASS_LABELS[pass]}）…`);
      setBusy(`${REFERENCE_BUSY_PREFIX}（${REFERENCE_PASS_LABELS[pass]}）…`);
      setError(null);
      try {
        const shot = await captureReference(pass);
        if (!shot || epoch !== sceneEpochRef.current) return;
        if (sourceState !== fingerprintRef.current().full() || viewState().some((value, index) => Math.abs(value - sourceView[index]) > 1e-7)) throw new Error('导出期间场景或机位发生变化，已放弃参考图，请重新核对后导出');
        if (referenceUrlRef.current) URL.revokeObjectURL(referenceUrlRef.current);
        const url = URL.createObjectURL(shot.blob);
        referenceUrlRef.current = url;
        setReferenceShot({ pass, url, bytes: shot.blob.size, width: shot.width, height: shot.height });
        setStatus(
          `已生成${REFERENCE_PASS_LABELS[pass]}参考图 ${shot.width}×${shot.height}（${Math.max(1, Math.round(shot.blob.size / 1024))} KB）`,
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        operation.finish();
        setBusy((current) => (current && current.startsWith(REFERENCE_BUSY_PREFIX) ? null : current));
      }
    },
    [captureReference],
  );

  /** Local shot video reuses the Vue previs encoder; no generation requests. */
  const exportScriptVideo = useCallback(async (plan: import('./toonflowSets').ScriptShotPlan, fps: number, mode: ScriptVideoMode, signal: AbortSignal): Promise<ScriptVideoArtifact> => {
    if (busy || shotVideoExportRef.current) throw new Error('请等待当前场景操作完成');
    stopAnimation();
    const renderer = rendererRef.current, camera = cameraRef.current, controls = controlsRef.current;
    if (!renderer || !camera || !controls) throw new Error('视口尚未就绪');
    if (objectsRef.current.some(entry => entry.mediaKind === 'video')) throw new Error('视频平面尚未按逐镜时间同步，请显式移除后再导出');
    if (mode !== 'viewport' && objectsRef.current.some(entry => entry.splat)) throw new Error('泼溅不支持白模/法线/深度视频，请选择材质原样或显式移除泼溅');
    shotVideoExportRef.current = signal;
    const epoch = sceneEpochRef.current, source = fingerprintRef.current().full();
    const sourceFrame = frameRef.current, outputSize = FRAME_VIDEO_SIZES[sourceFrame];
    const ratio = renderer.getPixelRatio();
    let outputSized = false;
    const operation = operationsRef.current.start('正在准备逐镜视频…'); setBusy('正在准备逐镜视频…'); setError(null);
    const assertSource = () => {
      if (signal.aborted) throw new Error('导出已取消');
      if (epoch !== sceneEpochRef.current || sourceFrame !== frameRef.current || source !== fingerprintRef.current().full()) throw new Error('场景在导出期间变化，视频已放弃，请重新审阅后导出');
      if (outputSized && (renderer.domElement.width !== outputSize.width || renderer.domElement.height !== outputSize.height || Math.abs(camera.aspect - outputSize.width / outputSize.height) > 1e-7)) throw new Error('导出画布尺寸发生变化，视频已放弃，请重新导出');
    };
    try {
      const [{ renderScriptShotVideo }, module] = await Promise.all([import('./scriptShotVideo'), import('./toonflowSets')]);
      assertSource();
      renderer.setPixelRatio(1); renderer.setSize(outputSize.width, outputSize.height, false); camera.aspect = outputSize.width / outputSize.height; camera.updateProjectionMatrix();
      outputSized = true;
      assertSource();
      const result = await renderScriptShotVideo(plan, {
        camera, controls, entries: () => objectsRef.current, environment: environmentRef.current ? validateSceneEnvironment(environmentRef.current) : null,
        baselines: motionBaselineRef.current, isCurrent: () => epoch === sceneEpochRef.current,
        fps, mode, size: outputSize, Encoder: globalThis.VideoEncoder, Frame: globalThis.VideoFrame, signal, sourceFingerprint: module.scriptFingerprint(source), assertSource,
        onProgress: (done, total) => { const label = `正在渲染逐镜视频（${done}/${total} 帧）…`; operation.update(label); setBusy(label); },
        renderFrame: async (sample, seconds, timing) => {
          assertSource();
          weatherEffectsRef.current?.sampleAt(seconds);
          const shot = mode === 'viewport' ? await captureViewportAt(sample.camera) : await captureReference(mode);
          assertSource(); if (!shot) throw new Error('逐镜视频未取到完整画面');
          if (shot.width !== outputSize.width || shot.height !== outputSize.height) throw new Error('逐镜画面尺寸与视频输出不一致，视频已放弃');
          const bitmap = await createImageBitmap(shot.blob);
          try { assertSource(); return new globalThis.VideoFrame(bitmap, timing); } finally { bitmap.close(); }
        },
      });
      assertSource(); setStatus(`逐镜视频已生成：${result.manifest.frameCount} 帧，尚未入库、未关联Scene、未提交生成`);
      return { ...result, source, epoch, projectId: Number(archiveProjectId) || null };
    } finally {
      shotVideoExportRef.current = null;
      if (renderer === rendererRef.current) {
        renderer.setPixelRatio(ratio);
        // A resize may have been deferred while exporting. Restore the CURRENT
        // container and frame, never stale pre-export viewport dimensions.
        resizeFrameRef.current();
      }
      operation.finish(); setBusy(null);
    }
  }, [busy, archiveProjectId, captureReference, captureViewportAt, stopAnimation]);

  /** 沿相机路径逐帧渲参考图，打包成一个 zip 下载。 */
  const exportSequence = useCallback(
    async (pass: ReferencePass) => {
      if (busy) return;
      stopAnimation();
      const camera = cameraRef.current as THREE.PerspectiveCamera | null;
      const controls = controlsRef.current;
      if (!camera || !controls) return;
      if (cameraKeys.length < 2) {
        setError("先记录至少两个机位，序列才有路径可走");
        return;
      }
      const epoch = sceneEpochRef.current;
      const sourceState = fingerprintRef.current().full();
      const sourceFrame = frameRef.current;
      const viewState = () => [...camera.position.toArray(), ...controls.target.toArray(), camera.fov];
      const sourceView = viewState();
      const assertCurrent = () => {
        if (epoch !== sceneEpochRef.current) throw new Error('场景已替换，已停止序列导出');
        if (mover && (!canMoveEntry(mover) || !objectsRef.current.includes(mover))) throw new Error('移动对象已隐藏、锁定或删除，已停止序列导出');
        if (sourceState !== fingerprintRef.current().full() || viewState().some((value, index) => Math.abs(value - sourceView[index]) > 1e-7)) throw new Error('导出期间场景或机位发生变化，已放弃整份序列，请重新核对后导出');
      };
      sequenceFramesRef.current = null;
      const path = editorGroundPath(groundPoints);
      const mover =
        moverId ? objectsRef.current.find((entry) => entry.id === moverId && canMoveEntry(entry)) ?? null : null;
      const moverSeconds = mover && path ? normalizeMoveSeconds(moveSeconds) : null;
      const fps = sequenceFps;
      const plan = sequencePlan(cameraKeys, moverSeconds, fps);
      const frames = plan.frames;
      if (plan.exceedsLimit) { setError(`序列需要 ${frames} 帧，超过 ${SEQUENCE_MAX_FRAMES} 帧上限；请降低帧率或缩短路径，不会截断导出`); return; }
      setError(null);
      const operation = operationsRef.current.start(`${REFERENCE_BUSY_PREFIX}（序列 0/${frames}）…`);
      setBusy(`${REFERENCE_BUSY_PREFIX}（序列 0/${frames}）…`);
      // 导出会一路推相机，结束后必须放回原处，否则用户回到编辑器发现机位跑了。
      const savedPosition = camera.position.clone();
      const savedTarget = controls.target.clone();
      const savedFov = camera.fov;
      // 分辨率：选了非 0 宽度就把画布临时缩到那个宽度，导完再还原。
      // 视口是 3840 宽时每帧 200–370 KB，1280 宽够当视频参考、体积小一大截。
      const renderer = rendererRef.current;
      const savedSize = renderer ? renderer.getSize(new THREE.Vector2()) : null;
      const savedPixelRatio = renderer?.getPixelRatio() ?? 1;
      const savedAspect = camera.aspect;
      if (renderer && sequenceWidth > 0) {
        const height = Math.max(1, Math.round(sequenceWidth / savedAspect));
        renderer.setPixelRatio(1); // Export width is physical pixels, not device-scaled CSS pixels.
        renderer.setSize(sequenceWidth, height, false);
        camera.aspect = sequenceWidth / height;
        camera.updateProjectionMatrix();
      }
      // 角色走完停在 B 点，也要还原，否则编辑器里对象被挪走了。
      const savedMover = mover
        ? { position: mover.object.position.clone(), quaternion: mover.object.quaternion.clone() }
        : null;
      const entries: Array<{ name: string; data: Uint8Array }> = [];
      let restoreFrame: (() => void) | null = null;
      try {
        for (let index = 0; index < frames; index += 1) {
          assertCurrent();
          const sampleTime = sequenceSampleTime(index, frames, plan.seconds, fps);
          const sample = sampleCameraPath(cameraKeys, sampleTime);
          if (!sample) throw new Error('相机路径未返回完整采样，已放弃整份序列');
          restoreFrame = () => {
            camera.position.copy(savedPosition); controls.target.copy(savedTarget); camera.fov = savedFov; camera.updateProjectionMatrix(); controls.update();
            if (mover && savedMover && canMoveEntry(mover) && objectsRef.current.includes(mover)) {
              mover.object.position.copy(savedMover.position); mover.object.quaternion.copy(savedMover.quaternion); mover.object.updateMatrixWorld(true);
            }
          };
          camera.position.set(...sample.position);
          controls.target.set(...sample.target);
          controls.update();
          if (Math.abs(camera.fov - sample.fov) > 1e-4) {
            camera.fov = sample.fov;
            camera.updateProjectionMatrix();
          }
          if (mover && path && moverSeconds && canMoveEntry(mover)) {
            // 移动与相机各走各的时钟：按同一帧时间 / 移动总时长取进度，超出就停在 B。
            const progress = Math.min(1, sampleTime / moverSeconds);
            mover.object.position.set(...groundPathPoint(path, progress));
            mover.object.rotation.y = groundPathYaw(path, progress);
            if (followMover) {
              // 跟拍：相机与目标整体加上角色的水平位移，构图不变。
              const delta = groundPathFollowDelta(path, progress);
              camera.position.set(
                camera.position.x + delta[0],
                camera.position.y + delta[1],
                camera.position.z + delta[2],
              );
              controls.target.set(
                controls.target.x + delta[0],
                controls.target.y + delta[1],
                controls.target.z + delta[2],
              );
              controls.update();
            }
          }
          exportSampleRef.current = () => {
            let preview: AnimationPreview | null = null;
            try {
              if (mover && path && moverSeconds) {
                const clip = mover.object.animations[moveAnimationIndex];
                if (moveAnimationIndex >= 0 && !clip) throw new Error('移动动作已失效，请重新选择');
                if (clip) { preview = new AnimationPreview(mover.object, clip); preview.sampleAt(Math.min(sampleTime, moverSeconds)); }
              }
            } catch (err) { preview?.stop(); throw err; }
            return () => {
              preview?.stop();
              restoreFrame?.(); restoreFrame = null;
            };
          };
          const shot = await captureReference(pass);
          assertCurrent();
          if (!shot) throw new Error('序列取帧失败，已放弃整份序列');
          entries.push({
            name: `${pass}-${String(index).padStart(3, "0")}.png`,
            data: new Uint8Array(await shot.blob.arrayBuffer()),
          });
          assertCurrent();
          operation.update(`${REFERENCE_BUSY_PREFIX}（序列 ${index + 1}/${frames}）…`);
          setBusy(`${REFERENCE_BUSY_PREFIX}（序列 ${index + 1}/${frames}）…`);
        }
        if (entries.length !== frames) throw new Error('序列帧数不完整，已放弃导出');
        assertCurrent();
        // 先留一份帧，用户想存素材库时不用再渲一遍。
        sequenceFramesRef.current = {
          pass,
          stamp: `${fileNameStamp(new Date())}-${crypto.randomUUID().replaceAll('-', '')}`,
          fps, projectId: Number(archiveProjectId) || null, frameIds: [],
          blobs: entries.map((entry) => new Blob([entry.data], { type: "image/png" })),
        };
        const zip = createStoreZip(entries);
        const url = URL.createObjectURL(new Blob([zip], { type: "application/zip" }));
        const link = document.createElement("a");
        link.href = url;
        link.download = `${pass}-sequence-${entries.length}f.zip`;
        link.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
        setStatus(
          `已导出${REFERENCE_PASS_LABELS[pass]}序列 ${entries.length} 帧（${Math.max(1, Math.round(zip.byteLength / 1024 / 1024))} MB）`,
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        exportSampleRef.current = null;
        operation.finish();
        if (epoch === sceneEpochRef.current) restoreFrame?.();
        // Sampling state was already restored synchronously by captureReference.
        // Never roll back camera/object edits made while PNG encoding was pending.
        if (epoch === sceneEpochRef.current && sourceFrame === frameRef.current) {
          // 画布尺寸与投影一起还原，否则编辑器停在导出分辨率上。
          if (renderer && savedSize && sequenceWidth > 0) {
            renderer.setPixelRatio(savedPixelRatio);
            renderer.setSize(savedSize.x, savedSize.y, false);
            camera.aspect = savedAspect;
            camera.updateProjectionMatrix();
          }
        } else if (renderer && renderer === rendererRef.current) {
          renderer.setPixelRatio(savedPixelRatio);
          resizeFrameRef.current(); // Keep the new scene's frame, not the old export dimensions.
        }
        setBusy((current) => (current && current.startsWith(REFERENCE_BUSY_PREFIX) ? null : current));
      }
    },
    [cameraKeys, captureReference, followMover, groundPoints, moveSeconds, moveAnimationIndex, busy, moverId, sequenceFps, sequenceWidth, archiveProjectId],
  );

  /**
   * 逐镜有序参考包：按已确认镜头表的镜序，在每镜开始秒数用人工记录的机位渲白模/法线/深度三张 PNG，
   * 与 manifest.json 一起打成一个 zip 本地下载。清单先整份算好；缺机位、取图失败或场景被替换都整包放弃，
   * 不下载半份。结束后把视口还原到导出前机位。不编码视频、不上传素材库、不写业务库。
   */
  const exportShotReferencePack = useCallback(
    async (plan: import('./toonflowSets').ScriptShotPlan) => {
      if (busy) return false;
      stopAnimation();
      const camera = cameraRef.current as THREE.PerspectiveCamera | null;
      const controls = controlsRef.current;
      if (!camera || !controls) { setError('视口尚未就绪，无法导出逐镜参考包'); return false; }
      const epoch = sceneEpochRef.current;
      let module: typeof import('./toonflowSets');
      let pack: import('./toonflowSets').ScriptReferencePack & { exportedSceneFingerprint?: string; scope?: string; environment?: SceneEnvironment | null; motionSampling?: string; motionPlan?: import('./toonflowSets').ScriptShotPlan; artifactId?: string; walkProof?: unknown; characterAnimationCatalogRevision?: number; output?: Array<{ name: string; width: number; height: number }> };
      const sourceState = fingerprintRef.current().full();
      const assertSource = () => {
        if (epoch !== sceneEpochRef.current || sourceState !== fingerprintRef.current().full()) throw new Error('导出期间场景发生变化，已放弃整包，请重新核对后导出');
      };
      try {
        // 保持 toonflowSets 懒加载：静态 import 会把整套素材塞进主视口包。
        module = await import('./toonflowSets');
        if (objectsRef.current.some(entry => entry.splat || entry.mediaKind === 'video')) throw new Error('逐镜参考包暂不支持包含泼溅或视频平面的场景：不能省略或使用未同步画面，请先显式移除后导出');
        pack = { ...module.buildScriptShotReferencePack(plan, { now: new Date() }), exportedSceneFingerprint: module.scriptFingerprint(sourceState), scope: 'static-mesh-reference-passes', environment: environmentRef.current ? validateSceneEnvironment(environmentRef.current) : null, motionSampling: 'script-shot-timeline-v1', motionPlan: plan };
        // 纯清单函数不负责渲染；本入口明确声明已经统一时间轴采样，不沿用静态取图警告。
        pack.notes = [...pack.notes.filter(note => !note.startsWith('镜头表含人工走位；')), '每镜开始秒数与视频共用机位/走位/骨姿采样；已结束镜次保留走位终点，雨雪不烘入结构参考图。'];
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
        return false;
      }
      if (epoch !== sceneEpochRef.current || busy) { setError('场景已替换，已停止导出逐镜参考包'); return false; }
      const savedPosition = camera.position.clone();
      const savedTarget = controls.target.clone();
      const savedQuaternion = camera.quaternion.clone();
      const savedFov = camera.fov;
      const savedEnabled = controls.enabled;
      const savedDamping = controls.enableDamping;
      const total = pack.shots.length;
      const operation = operationsRef.current.start(`${REFERENCE_BUSY_PREFIX}（逐镜参考包 0/${total} 镜）…`);
      setBusy(`${REFERENCE_BUSY_PREFIX}（逐镜参考包 0/${total} 镜）…`);
      setError(null);
      const entries: Array<{ name: string; data: Uint8Array }> = [];
      let sampler: ReturnType<typeof import('./scriptShotSampler')['createScriptShotSampler']> | null = null;
      try {
        controls.enabled = false;
        controls.enableDamping = false;
        controls.update();
        const { createScriptShotSampler } = await import('./scriptShotSampler');
        assertSource();
        sampler = createScriptShotSampler(plan, { camera, controls, entries: () => objectsRef.current, environment: pack.environment ?? null, baselines: motionBaselineRef.current, isCurrent: () => epoch === sceneEpochRef.current });
        pack.artifactId = crypto.randomUUID(); pack.walkProof = sampler.walkProof; pack.output = [];
        pack.characterAnimationCatalogRevision = (await import('./toonflow/characterAnimations')).characterAnimationCatalogRevision;
        for (let index = 0; index < total; index += 1) {
          const shot = pack.shots[index]!;
          assertSource();
          // 采样时刻来自清单（本镜开始秒数），不读实时播放头，也不在镜间插值。
          sampler.sampleAt(shot.sampleSeconds);
          for (const file of shot.files) {
            const frame = await captureReference(file.pass);
            if (!frame) throw new Error(`第 ${shot.order} 镜的${file.pass}参考图没有取到`);
            assertSource();
            const data = new Uint8Array(await frame.blob.arrayBuffer());
            assertSource();
            pack.output!.push({ name: file.name, width: frame.width, height: frame.height });
            entries.push({ name: file.name, data });
          }
          const progress = `${REFERENCE_BUSY_PREFIX}（逐镜参考包 ${index + 1}/${total} 镜）…`;
          operation.update(progress);
          setBusy(progress);
        }
        if (entries.length !== total * pack.passes.length) throw new Error('参考包张数与清单不一致，已放弃导出');
        assertSource();
        entries.push({ name: pack.manifestFile, data: new TextEncoder().encode(module.scriptShotReferenceManifestText(pack)) });
        const zip = createStoreZip(entries);
        const url = URL.createObjectURL(new Blob([zip], { type: "application/zip" }));
        const link = document.createElement("a");
        link.href = url;
        link.download = `${pack.sceneId}-shot-reference-pack-${total}shots.zip`;
        link.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
        setStatus(
          `已导出逐镜参考包：${total} 镜 × ${pack.passes.length} 张 + manifest.json（${Math.max(1, Math.round(zip.byteLength / 1024))} KB）；仅本地下载，未存素材库、未提交生成。`,
        );
        return true;
      } catch (err) {
        // 半成品不落地：entries 只有在全部取图成功后才打包下载。
        setError(err instanceof Error ? err.message : String(err));
        return false;
      } finally {
        sampler?.stop();
        if (epoch === sceneEpochRef.current) {
          camera.position.copy(savedPosition);
          controls.target.copy(savedTarget);
          if (Math.abs(camera.fov - savedFov) > 1e-4) { camera.fov = savedFov; camera.updateProjectionMatrix(); }
          controls.update();
          camera.position.copy(savedPosition); controls.target.copy(savedTarget);
          camera.quaternion.copy(savedQuaternion); camera.updateMatrixWorld(true);
          controls.enabled = savedEnabled;
          controls.enableDamping = savedDamping;
        }
        operation.finish();
        setBusy((current) => (current && current.startsWith(REFERENCE_BUSY_PREFIX) ? null : current));
      }
    },
    [busy, captureReference, stopAnimation],
  );

  /**
   * 把刚导出的那张参考图存进素材库（POST /api/materials，type=image）。
   * 用途：白模/深度图当视频生成的参考底图，存进库就能在其他页面复用。
   */
  const saveReferenceToLibrary = useCallback(async () => {
    const shot = referenceShot;
    if (!shot) {
      setError("先导出一张参考图，再存素材库");
      return;
    }
    setBusy("正在把参考图存进素材库…");
    setError(null);
    try {
      const blob = await fetch(shot.url).then((response) => response.blob());
      const form = new FormData();
      form.append("file", blob, referenceFileName(shot.pass, fileNameStamp(new Date())));
      form.append("type", "image");
      const response = await fetch("/api/materials", { method: "POST", body: form });
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(payload.error || `存素材库失败（HTTP ${response.status}）`);
      }
      const material = await response.json();
      setStatus(`已把${REFERENCE_PASS_LABELS[shot.pass]}参考图存进素材库：${material.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [referenceShot]);

  /** 把刚导出的序列逐帧存进素材库；帧数超过上限就明说，不硬刷库。 */
  const saveSequenceToLibrary = useCallback(async () => {
    const cache = sequenceFramesRef.current;
    if (!cache) {
      setError("先导出一次序列，再把帧存进素材库");
      return;
    }
    const blocker = sequenceLibraryBlocker(cache.blobs.length);
    if (blocker) {
      setError(blocker);
      return;
    }
    const epoch = sceneEpochRef.current;
    const operation = operationsRef.current.start('正在存序列…');
    const assertCurrent = () => { if (epoch !== sceneEpochRef.current || cache !== sequenceFramesRef.current) throw new Error('场景或序列已替换，停止存库'); };
    let saved = cache.frameIds.length;
    setBusy(`正在把 ${cache.blobs.length} 帧存进素材库…`);
    setError(null);
    try {
      for (let index = saved; index < cache.blobs.length; index += 1) {
        assertCurrent();
        const form = new FormData();
        form.append("file", cache.blobs[index], sequenceFrameFileName(cache.pass, cache.stamp, index));
        form.append("type", "image");
        if (cache.projectId !== null) form.append('project_id', String(cache.projectId));
        const response = await fetch("/api/materials", { method: "POST", body: form });
        if (!response.ok) {
          const payload = await response.json().catch(() => ({}));
          throw new Error(payload.error || `第 ${index + 1} 帧存库失败（HTTP ${response.status}）`);
        }
        const material = await response.json();
        if (!Number.isSafeInteger(material.id) || material.id <= 0) throw new Error('序列入库未返回有效素材ID');
        cache.frameIds.push(material.id);
        saved += 1;
        assertCurrent();
        setBusy(`正在存序列进素材库（${saved}/${cache.blobs.length}）…`);
      }
      setStatus(
        `已把 ${saved} 帧${REFERENCE_PASS_LABELS[cache.pass]}序列存进素材库（素材库页可直接看）`,
      );
    } catch (err) {
      setError(`已成功存入 ${saved}/${cache.blobs.length} 帧；${err instanceof Error ? err.message : String(err)}（再次存库将续传剩余帧）`);
    } finally {
      operation.finish(); setBusy(null);
    }
  }, []);

  /**
   * 把素材库里这组序列帧合成 mp4（后端 ffmpeg）：H3 的视频参考要的是视频，
   * 图片序列直接喂不进去。合成结果也在素材库里（type=video）。
   */
  const composeSequenceVideo = useCallback(async () => {
    const cache = sequenceFramesRef.current;
    if (!cache) {
      setError("先导出一次序列，再合成视频");
      return;
    }
    const blocker = sequenceLibraryBlocker(cache.blobs.length);
    if (blocker) {
      setError(blocker);
      return;
    }
    if (cache.frameIds.length !== cache.blobs.length) { setError('请先完成序列存库，再合成视频'); return; }
    setBusy(`正在把 ${cache.blobs.length} 帧合成视频…`);
    setError(null);
    try {
      const response = await fetch("/api/materials/sequence-video", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pass: cache.pass, stamp: cache.stamp, fps: cache.fps, project_id: cache.projectId, frame_ids: cache.frameIds }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) {
        throw new Error(payload.error || `合成视频失败（HTTP ${response.status}）`);
      }
      setStatus(`已合成 ${payload.name}（${cache.blobs.length} 帧 · ${cache.fps} fps，在素材库里）`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [sequenceFps]);
  const previewMove = useCallback(() => {
    if (busy || playingPath) return;
    stopAnimation();
    const path = editorGroundPath(groundPoints);
    const mover = moverId ? objectsRef.current.find((entry) => entry.id === moverId && canMoveEntry(entry)) ?? null : null;
    if (!path || !mover || !canMoveEntry(mover)) return;
    const seconds = normalizeMoveSeconds(moveSeconds);
    historyRef.current.checkpoint(objectsRef.current);
    const clip = mover.object.animations[moveAnimationIndex];
    if (moveAnimationIndex >= 0 && !clip) { setError('移动对象的动作片段已变化，请重新选择'); return; }
    // Both local bone pose and world movement are evaluated from this one clock.
    if (clip) {
      fkRef.current?.clear(); posingRef.current = false; setPosing(false); setPosingBones(0); setActiveBone(null);
      transformRef.current?.detach();
      try {
        animationRef.current = { id: mover.id, pose: serializeBonePose(mover.object), preview: new AnimationPreview(mover.object, clip), pathClock: true };
        setAnimationState('playing');
      } catch (err) { setError(String(err)); transformRef.current?.attach(mover.object); return; }
    }
    setPlayingMove(true);
    cancelMoveRef.current = () => {
      if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
      moveRafRef.current = null;
      setPlayingMove(false);
    };
    const started = performance.now();
    const epoch = sceneEpochRef.current;
    const step = () => {
      if (epoch !== sceneEpochRef.current || !objectsRef.current.includes(mover) || !canMoveEntry(mover)) { stopAnimation(); return; }
      const elapsed = Math.min(seconds, (performance.now() - started) / 1000);
      const progress = elapsed / seconds;
      mover.object.position.set(...groundPathPoint(path, progress));
      mover.object.rotation.y = groundPathYaw(path, progress);
      animationRef.current?.preview.sampleAt(elapsed);
      if (progress < 1) {
        moveRafRef.current = requestAnimationFrame(step);
        return;
      }
      stopAnimation();
      syncObjects();
      setStatus(clip ? '路径动作预览完成：已到达终点并恢复编辑骨姿（动作未接导出）' : '路径预览完成');
    };
    step();
  }, [groundPoints, moveSeconds, moverId, moveAnimationIndex, busy, playingPath, syncObjects]);

  // 卸载时别留 rAF 或逐镜播放定时器；同时作废尚未落地的懒加载启动。
  useEffect(
    () => () => {
      if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
      if (shotPlaybackRef.current !== null) cancelAnimationFrame(shotPlaybackRef.current);
      if (shotPlaybackTimerRef.current !== null) window.clearTimeout(shotPlaybackTimerRef.current);
      shotPlaybackTokenRef.current += 1;
    },
    [],
  );

  const closeAiMenu = useCallback(() => {
    if (aiMenuRef.current) aiMenuRef.current.open = false;
  }, []);

  const moverAnimationClips = objectsRef.current.find(e => e.id === moverId)?.object.animations ?? [];
  const selectedAnimationEntry = objectsRef.current.find(e => e.id === selectedId);
  const animationClips = selectedAnimationEntry?.object.animations ?? [];
  // 目录片段的可绑定情况按当前选中角色实时算：缺哪根骨骼必须说出来，不静默禁用。
  const catalogBinding: CharacterAnimationBinding | null = animationCatalog && animationCatalogId && selectedAnimationEntry && animationCatalogRef.current
    ? animationCatalogRef.current.characterAnimationBinding(selectedAnimationEntry.object, animationCatalogId)
    : null;

  /** 动作目录按需加载：目录数据约 1.2 MB，不静态进主视口包。 */
  const loadAnimationCatalog = useCallback(async () => {
    try {
      const module = await import("./toonflow/characterAnimations");
      animationCatalogRef.current = module;
      const entries = module.listCharacterAnimationCatalog();
      setAnimationCatalog({ summary: module.summarizeCharacterAnimationCatalog(), entries });
      setAnimationCatalogId(current => current || entries[0]?.id || "");
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, []);

  /**
   * 把目录片段作为普通动作资源加到选中角色：先按同名骨骼核对可绑定，再按保存规则整份校验，
   * 都通过才改对象。不做骨架重定向、不覆盖同名片段、不自动播放、不写提示词。
   */
  const addCatalogAnimation = useCallback(async (id: string) => {
    if (busy) { setError("请等待当前场景操作完成再加入动作"); return; }
    if (animationRef.current) { setError("先停止正在播放的动作，再加入新的动作资源"); return; }
    const entry = objectsRef.current.find(item => item.id === selectedId);
    if (!entry) { setError("先在左栏选中要加动作的角色"); return; }
    if (entry.locked || !entry.object.visible) { setError("对象已锁定或隐藏：先解锁/显示再加入动作"); return; }
    try {
      const module = animationCatalogRef.current ?? (await import("./toonflow/characterAnimations"));
      animationCatalogRef.current = module;
      if (!objectsRef.current.includes(entry) || selectedIdRef.current !== entry.id || entry.locked || !entry.object.visible || busy) throw new Error("角色或场景状态已变化，动作未加入");
      const clip = module.createCharacterAnimationClipFor(entry.object, id);
      stopAnimation();
      entry.object.animations = attachAnimationClip(entry.object, clip);
      setAnimationIndex(entry.object.animations.length - 1);
      syncObjects();
      setStatus(`已加入动作「${clip.name}」（${clip.duration.toFixed(2)} 秒 · CC0）到「${entry.name}」；随场景存档保存，不会自动播放`);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }, [busy, selectedId, syncObjects]);
  const playAnimation = () => {
    if (scrubRef.current) stopAnimation();
    if (busy || playingPath || playingMove || operationBusy) return;
    const entry = objectsRef.current.find(e => e.id === selectedIdRef.current);
    if (!entry || entry.locked || !entry.object.visible) return;
    if (animationRef.current) { animationRef.current.preview.resume(); setAnimationState('playing'); return; }
    const clip = entry.object.animations[animationIndex];
    if (!clip) return;
    fkRef.current?.clear(); posingRef.current = false; setPosing(false); setPosingBones(0); setActiveBone(null);
    transformRef.current?.detach();
    try {
      const pose = serializeBonePose(entry.object);
      animationRef.current = { id: entry.id, pose, preview: new AnimationPreview(entry.object, clip) };
      setAnimationState('playing');
      setStatus(`动作预览：${clip.name || '未命名片段'}；停止将恢复编辑姿势`);
    } catch (err) { setError(String(err)); transformRef.current?.attach(entry.object); }
  };
  const endPreviewForEditing = (target: EventTarget | null) => {
    if (target instanceof Element && !target.closest('[data-animation-preview],canvas')) stopAnimation();
  };

  return (
    <div
      onClickCapture={event => endPreviewForEditing(event.target)}
      onChangeCapture={event => endPreviewForEditing(event.target)}
      className="studio-root studio-theme fixed inset-0 z-[100] overflow-hidden bg-[#121316] text-[#f2f1ee]"
      onPointerDownCapture={(event) => {
        // details 自己不会因为点外面而收起，落在面板之外就手动关一次。
        const target = event.target as Node | null;
        if (aiMenuRef.current && target && !aiMenuRef.current.contains(target)) {
          aiMenuRef.current.open = false;
        }
      }}
    >
      <div
        ref={containerRef}
        className="absolute inset-0"
        onDragOver={(event) => {
          // 只接管本面板的拖放，别影响浏览器默认的「拖文件进来」。
          if (!event.dataTransfer.types.includes(ASSET_DRAG_MIME)) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = "copy";
        }}
        onDrop={(event) => {
          const raw = event.dataTransfer.getData(ASSET_DRAG_MIME);
          if (!raw) return;
          event.preventDefault();
          let asset: LibraryAsset;
          try {
            asset = JSON.parse(raw) as LibraryAsset;
          } catch {
            setError("拖放的资产信息已损坏，请从面板重新拖一次");
            return;
          }
          void loadLibraryAsset(asset, dropPointFromEvent(event));
        }}
      />

      {locationGenerationOpen && <LocationGenerationPanel disabled={Boolean(busy)} close={() => setLocationGenerationOpen(false)} waitForTask={waitForTask} capture={async () => {
        const camera = cameraRef.current, controls = controlsRef.current;
        if (!camera || !controls) throw new Error("相机尚未就绪");
        const pose = { position: camera.position.toArray(), target: controls.target.toArray(), fov: camera.fov, frame: { aspect: frameRef.current }, sensorHeight: 24 };
        return { blob: await captureCurrentView(), camera: pose };
      }} />}
      <div className="fixed inset-x-0 top-0 z-40 flex h-14 items-center gap-2 border-b border-white/10 bg-[#101014]/95 px-3 backdrop-blur">
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm text-white/80 hover:bg-white/10"
          onClick={() => window.location.assign(exitTo)}
        >
          返回
        </button>
        <span className="mr-2 text-sm font-medium">3D 场景</span>
        <button type="button" className="rounded-md border border-violet-400/40 px-3 py-1.5 text-sm" disabled={Boolean(busy)} onClick={() => setLocationGenerationOpen(true)}>机位出图</button>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
          onClick={() => fileInputRef.current?.click()}
        >
          导入模型
        </button>
        <details ref={aiMenuRef} className="relative">
          <summary
            className="cursor-pointer list-none rounded-md border border-sky-400/40 px-3 py-1.5 text-sm text-sky-200 marker:hidden hover:bg-sky-400/10 [&::-webkit-details-marker]:hidden"
            title="AI 生成：图生 3D 模型、图生 3D 世界、身份迁移出图"
          >
            AI 生成 ▾
          </summary>
          <div className="absolute left-0 top-full z-50 mt-1 w-80 space-y-2 rounded-lg border border-white/15 bg-[#16171c] p-3 shadow-2xl">
            <button
              type="button"
              className="w-full rounded-md border border-sky-400/40 px-3 py-1.5 text-left text-sm text-sky-200 hover:bg-sky-400/10 disabled:opacity-50"
              onClick={() => {
                closeAiMenu();
                imageInputRef.current?.click();
              }}
              disabled={Boolean(busy)}
              title="参考图 → Hunyuan3D → GLB 网格模型"
            >
              AI 图生 3D
            </button>
            <div className="flex items-center gap-2">
              <button
                type="button"
                className="flex-1 rounded-md border border-violet-400/40 px-3 py-1.5 text-left text-sm text-violet-200 hover:bg-violet-400/10 disabled:opacity-50"
                onClick={() => {
                  closeAiMenu();
                  splatInputRef.current?.click();
                }}
                disabled={Boolean(busy)}
                title="参考图 → TripoSplat → .spz 高斯泼溅，作为可绕行的 3D 世界"
              >
                AI 图生 3D 世界
              </button>
              <select
                value={splatMatteEngine}
                onChange={(event) => setSplatMatteEngine(event.target.value as SplatMatteEngine)}
                disabled={Boolean(busy)}
                title="图生 3D 世界的抠图方式：RMBG-3.0 边缘细致但慢，BiRefNet 快但硬边"
                className="h-9 rounded-md border border-violet-400/30 bg-white/5 px-2 text-sm text-violet-100 disabled:opacity-50"
              >
                <option value="rmbg3">RMBG-3.0 抠图</option>
                <option value="birefnet">BiRefNet 抠图</option>
              </select>
            </div>
            <label className="block text-[11px] text-white/45" htmlFor="identity-prompt">
              身份迁移提示词（当前 3D 画面当 image1，角色主形象当 image2）
            </label>
            <input
              id="identity-prompt"
              value={identityPrompt}
              onChange={(event) => setIdentityPrompt(event.target.value)}
              placeholder="身份迁移：用 <image1> 指 3D 快照、<image2> 指角色主形象"
              className="h-9 w-full rounded-md border border-white/10 bg-white/5 px-3 text-sm text-white placeholder:text-white/40"
            />
            <button
              type="button"
              className="w-full rounded-md border border-amber-400/40 px-3 py-1.5 text-left text-sm text-amber-200 hover:bg-amber-400/10 disabled:opacity-50"
              onClick={() => {
                closeAiMenu();
                identityInputRef.current?.click();
              }}
              disabled={Boolean(busy)}
              title="当前 3D 画面当 image1、角色主形象当 image2，出一张身份迁移图放进场景"
            >
              身份迁移出图
            </button>
          </div>
        </details>
        <div className="mx-2 flex rounded-md border border-white/10">
          {(["translate", "rotate", "scale"] as TransformMode[]).map((item) => (
            <button
              key={item}
              type="button"
              className={`px-3 py-1.5 text-sm ${
                mode === item ? "bg-white/15 text-white" : "text-white/60 hover:bg-white/10"
              }`}
              disabled={posing}
              title={posing ? "骨骼摆姿中仅支持局部旋转；退出摆姿后可变换整个对象" : "变换整个对象"}
              onClick={() => setMode(item)}
            >
              {item === "translate" ? "移动" : item === "rotate" ? "旋转" : "缩放"}
            </button>
          ))}
        </div>
        <button
          type="button"
          className="rounded-md border border-red-400/40 px-3 py-1.5 text-sm text-red-200 hover:bg-red-400/10 disabled:opacity-40"
          onClick={removeSelected}
          disabled={!selectedId}
        >
          删除
        </button>
        <button
          type="button"
          className="rounded-md border border-emerald-400/40 px-3 py-1.5 text-sm text-emerald-200 hover:bg-emerald-400/10 disabled:opacity-40"
          onClick={() => void exportGlb()}
          disabled={objects.length === 0 || Boolean(busy)}
          title="把场景里的模型导出为 GLB"
        >
          导出 GLB
        </button>
        <button
          type="button"
          className="rounded-md border border-sky-400/40 px-3 py-1.5 text-sm text-sky-200 hover:bg-sky-400/10 disabled:opacity-40"
          onClick={() => void saveToLibrary()}
          disabled={objects.length === 0 || Boolean(busy)}
          title="把模型存入素材库（含当前画面预览图），供漫剧调度预演绑定"
        >
          存入素材库
        </button>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
          onClick={() => void saveScene()}
          disabled={objects.length === 0 || Boolean(busy)}
          title="把模型、灯光和机位存成场景文件（下载到本地）"
        >
          保存场景
        </button>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
          onClick={() => void saveSceneToServer()}
          disabled={objects.length === 0 || Boolean(busy)}
          title="把场景存到后端，刷新或换机器后还能从「后端存档」里选回来"
        >
          存到后端
        </button>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
          onClick={() => sceneInputRef.current?.click()}
          disabled={Boolean(busy)}
          title="载入之前保存到本地的场景文件"
        >
          载入场景
        </button>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
          onClick={() => void loadSceneArchives()}
          disabled={Boolean(busy) || archivesBusy}
          title="列出存在后端的场景存档，点一条就载入"
        >
          {archivesBusy ? "读取存档…" : "后端存档"}
        </button>
        <button
          type="button"
          className="ml-auto rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
          onClick={downloadScreenshot}
        >
          截图
        </button>
        <input
          ref={sceneInputRef}
          type="file"
          accept=".json,application/json"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) void loadScene(file);
            event.target.value = "";
          }}
        />
        {archivePreview ? <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-6" role="dialog" aria-label="存档恢复预览"><div className="max-w-md rounded bg-neutral-900 p-4 space-y-3">
          <h3>{archivePreview.name}</h3>
          {archivePreview.preview ? <img src={archivePreview.preview} alt="存档取景预览" className="max-h-64 mx-auto" /> : <p>旧存档无缩略图</p>}
          <p>{archivePreview.count} 个对象 · {archivePreview.aspect} · 确认后替换当前场景</p>
          <button onClick={() => void archivePreview.restore()}>确认恢复</button> <button onClick={() => setArchivePreview(null)}>取消</button>
        </div></div> : null}
        {archivesOpen ? (
          <div
            className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-6"
            onClick={() => setArchivesOpen(false)}
          >
            <div
              className="max-h-[70vh] w-full max-w-md overflow-auto rounded-lg border border-white/10 bg-neutral-900 p-4 text-sm shadow-xl"
              onClick={(event) => event.stopPropagation()}
            >
              <div className="mb-3 flex items-center justify-between">
                <span className="font-medium">
                  后端存档（共 {archiveTotal} 份）
                </span>
                <button
                  type="button"
                  className="text-xs text-white/60 hover:text-white"
                  onClick={() => setArchivesOpen(false)}
                >
                  关闭
                </button>
              </div>
              <select aria-label="存档列表项目筛选" value={archiveFilter} disabled={archivesBusy} className="w-full bg-neutral-800" onChange={e => void loadSceneArchives(1, e.target.value)}>
                <option value="all">全部项目</option><option value="0">无项目归属</option>
                {archiveProjects.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}
              </select>
              {archives.length === 0 ? (
                <p className="text-xs text-white/50">
                  {archivesBusy
                    ? "正在读取…"
                    : archiveFilter !== 'all'
                      ? "这个项目下还没有存档：先在视口里搭好，再点「存到后端」。"
                      : "还没有存档：先在视口里搭好，再点「存到后端」。"}
                </p>
              ) : (
                <ul className="space-y-1">
                  {archives.map((item) => (
                    <li key={item.id}>
                      <button
                        type="button"
                        className="w-full rounded px-2 py-1.5 text-left hover:bg-white/10 disabled:opacity-40"
                        disabled={Boolean(busy) || archivesBusy}
                        onClick={() => void openSceneArchive(item)}
                        title={`载入存档 #${item.id}`}
                      >
                        <span className="block truncate">{item.name}</span>
                        <span className="block text-xs text-white/50">
                          #{item.id} · {item.sizeLabel}
                          {item.createdLabel ? ` · ${item.createdLabel}` : ""}
                        </span>
                      </button>
                      {renameId === item.id ? <div className="flex gap-2">
                        <input aria-label="存档新名称" maxLength={200} value={renameValue} onChange={e => setRenameValue(e.target.value)} className="bg-neutral-800" />
                        <button disabled={archivesBusy || !renameValue.trim()} onClick={() => void manageArchive(item.id, 'PATCH')}>保存名称</button>
                        <button onClick={() => setRenameId(null)}>取消改名</button>
                      </div> : <button disabled={archivesBusy} onClick={() => { setRenameId(item.id); setRenameValue(item.name); }}>重命名 #{item.id}</button>}
                      <button className="ml-3 text-red-300" disabled={archivesBusy} onClick={() => setDeleteArchive(item)}>移入回收区 #{item.id}</button>
                    </li>
                  ))}
                </ul>
              )}
              {deleteArchive ? <div className="my-3 border border-red-400 p-2">
                将「{deleteArchive.name}」移入回收区？原始文件保留，恢复需管理员操作。
                <button disabled={archivesBusy} onClick={() => void manageArchive(deleteArchive.id, 'DELETE')}>确认移入回收区</button>
                <button disabled={archivesBusy} onClick={() => setDeleteArchive(null)}>取消删除</button>
              </div> : null}
              <div className="mt-3 flex justify-between">
                <button disabled={archivesBusy || archivePage <= 1} onClick={() => void loadSceneArchives(archivePage-1)}>上一页</button>
                <span>第 {archivePage} / {Math.max(1, Math.ceil(archiveTotal/10))} 页</span>
                <button disabled={archivesBusy || archivePage*10 >= archiveTotal} onClick={() => void loadSceneArchives(archivePage+1)}>下一页</button>
              </div>
            </div>
          </div>
        ) : null}
        <input
          ref={fileInputRef}
          type="file"
          accept=".glb,.gltf,.obj"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) void loadFile(file);
            event.target.value = "";
          }}
        />
        <input
          ref={imageInputRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) void runImageTo3D(file);
            event.target.value = "";
          }}
        />
        <input
          ref={splatInputRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) void runImageToSplat(file);
            event.target.value = "";
          }}
        />
        <input
          ref={identityInputRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) void runIdentityTransfer(file);
            event.target.value = "";
          }}
        />
        <input
          ref={imagePlaneInputRef}
          type="file"
          accept={IMAGE_PLANE_ACCEPT}
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (!file) return;
            const kind = detectMediaKind(file.name);
            if (kind !== "image") {
              setError(`不是可识别的图片格式：${file.name}`);
              return;
            }
            void addPlane(URL.createObjectURL(file), file.name, "image");
          }}
        />
        <input
          ref={videoPlaneInputRef}
          type="file"
          accept={VIDEO_PLANE_ACCEPT}
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (!file) return;
            const kind = detectMediaKind(file.name);
            if (kind !== "video") {
              setError(`不是可识别的视频格式：${file.name}`);
              return;
            }
            void addPlane(URL.createObjectURL(file), file.name, "video");
          }}
        />
        <input
          ref={skyboxInputRef}
          type="file"
          accept={IMAGE_PLANE_ACCEPT}
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = "";
            if (!file) return;
            void applySkybox(URL.createObjectURL(file), file.name);
          }}
        />
      </div>

      <div className="fixed left-3 top-20 z-30 w-56 max-h-[calc(100vh-6rem)] overflow-y-auto rounded-lg border border-white/10 bg-[#101014]/90 p-2 backdrop-blur">
        <div role="status" className="mb-1 text-xs text-amber-200">{dirty ? '● 未保存' : '已保存 / 无修改'}</div>
        <div className="mb-2 space-y-1 text-xs">
          <label>画幅 <select aria-label="场景画幅" value={frameAspect} onChange={e => { frameRef.current = e.target.value as FrameAspect; setFrameAspect(frameRef.current); resizeFrameRef.current(); setLocationGenerationOpen(false); }}>{Object.keys(FRAME_ASPECTS).map(a => <option key={a}>{a}</option>)}</select></label>
          <label className="block">焦距 mm（24mm传感器高）<input aria-label="焦距毫米" type="number" min="12" max="92" value={Math.round(focal * 10) / 10} onChange={e => { const mm = Number(e.target.value); if (!Number.isFinite(mm) || mm < 12 || mm > 92 || !cameraRef.current) return; cameraRef.current.fov = fovFromFocal(mm); cameraRef.current.updateProjectionMatrix(); setFocal(mm); setLocationGenerationOpen(false); }} className="w-full bg-black/30" /></label>
          <label className="block text-xs">新存档归属（不改变旧档）
            <select aria-label="新存档项目" value={archiveProjectId} onChange={e => setArchiveProjectId(e.target.value)} className="w-full bg-neutral-800">
              <option value="">无项目归属</option>
              {archiveProjects.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}
            </select>
          </label>
          <input aria-label="存档名称" placeholder="存档名称（另存为）" value={archiveName} onChange={e => setArchiveName(e.target.value)} className="w-full bg-black/30" />
        </div>
        <div className="mb-2 px-1 text-xs uppercase tracking-wide text-white/40">场景对象</div>
        <div className="flex gap-2 text-xs mb-2">
          <button onClick={() => restoreHistory()}>撤销</button><button onClick={() => restoreHistory(true)}>重做</button>
          <button disabled={!selectedId} onClick={() => void duplicateSelected()}>复制</button>
        </div>
        <div className="flex gap-2 text-xs mb-2">
          <select aria-label="变换坐标系" onChange={e => transformRef.current?.setSpace(e.target.value as 'world' | 'local')}><option value="world">世界</option><option value="local">局部</option></select>
          <label><input type="checkbox" defaultChecked onChange={e => { transformRef.current?.setTranslationSnap(e.target.checked ? .1 : null); transformRef.current?.setRotationSnap(e.target.checked ? Math.PI / 12 : null); transformRef.current?.setScaleSnap(e.target.checked ? .1 : null); }} />吸附 0.1 / 15°</label>
        </div>
        {objects.length === 0 ? (
          <div className="px-1 py-2 text-xs text-white/40">还没有模型</div>
        ) : (
          objects.map((entry) => (
            <div
              key={entry.id}
              className={`mb-1 flex items-center gap-1 rounded-md ${
                selectedId === entry.id ? "bg-sky-500/20" : ""
              }`}
            >
              <button
                type="button"
                className={`min-w-0 flex-1 rounded-md px-2 py-1.5 text-left text-sm ${
                  selectedId === entry.id
                    ? "text-sky-100"
                    : "text-white/70 hover:bg-white/10"
                }`}
                onClick={() => selectObject(entry.id)}
              >
                <span className="block truncate">{entry.name}</span>
              </button>
              <button title="显隐" aria-label={`显隐 ${entry.name}`} aria-pressed={entry.object.visible} onClick={() => toggleEntry(entry, 'visible')}>{entry.object.visible ? '显' : '隐'}</button>
              <button title="锁定" aria-label={`锁定 ${entry.name}`} aria-pressed={!!entry.locked} onClick={() => toggleEntry(entry, 'locked')}>{entry.locked ? '锁' : '解'}</button>
              <button title="聚焦" aria-label={`聚焦 ${entry.name}`} onClick={() => focusEntry(entry)}>◎</button>
              {entry.splat ? (
                <button
                  type="button"
                  className="shrink-0 rounded px-1.5 py-1 text-[11px] text-violet-200 hover:bg-violet-400/10 disabled:opacity-40"
                  onClick={() => void saveSplatToLibrary(entry)}
                  disabled={Boolean(busy)}
                  title="把这层泼溅存进素材库（含当前画面预览图）；泼溅进不了 GLB，只能逐层存"
                >
                  存库
                </button>
              ) : null}
            </div>
          ))
        )}
        <div className="mt-2 flex flex-wrap gap-2 text-xs">
          {(['立方体', '球体', '圆柱', '圆环'] as const).map((label, index) => <button key={label} onClick={() => {
            const geometry = index === 0 ? new THREE.BoxGeometry() : index === 1 ? new THREE.SphereGeometry(.5, 24, 16) : index === 2 ? new THREE.CylinderGeometry(.5, .5, 1, 24) : new THREE.TorusGeometry(.5, .15, 12, 32);
            addObject(new THREE.Mesh(geometry, new THREE.MeshStandardMaterial({ color: '#cfd4dc' })), label);
          }}>{label}</button>)}
        </div>
        <div className="mt-2 border-t border-white/10 pt-2">
          <div className="mb-2 px-1 text-xs uppercase tracking-wide text-white/40">
            资产库（拖进画面摆位）
          </div>
          <AssetLibraryPanel
            onPlace={(asset) => void loadLibraryAsset(asset)}
            onError={(message) => setError(message)}
          />
          <details className="mt-2 border-t border-white/10 pt-2" onToggle={event => setProceduralAssetsOpen(event.currentTarget.open)}>
            <summary className="cursor-pointer text-xs">本地程序化素材</summary>
            {proceduralAssetsOpen && <>
              <ProceduralAssetPanel disabled={Boolean(busy)} onAdd={(object, name) => {
                const entry = addObject(object, name);
                if (!entry) disposeSceneEntry({ id: '', name, object });
              }} />
              <SetTemplatePanel disabled={Boolean(busy)} captureExecutionContext={async () => {
                const { captureStudioExecutionContext } = await import('./studioExecutionContext');
                return captureStudioExecutionContext(objectsRef.current);
              }} onApplyExecution={async (plan, reviewed, isCurrent) => {
                if (executionUnavailableRef.current) throw Error('场景忙碌或正在播放，不能应用');
                const scene = sceneRef.current; const epoch = sceneEpochRef.current;
                if (!scene) throw Error('场景尚未就绪');
                stopAnimation(); // Restore preview baselines before validating/staging edits, not after publication.
                const token = shotPlaybackTokenRef.current; const baseline = fingerprintRef.current().full();
                const [{ applyStudioExecution }, { captureStudioExecutionContext }] = await Promise.all([import('./studioExecutionApply'), import('./studioExecutionContext')]);
                await applyStudioExecution(plan, reviewed, {
                  fingerprint: () => fingerprintRef.current().full(),
                  isCurrent: () => isCurrent() && sceneRef.current === scene && sceneEpochRef.current === epoch && !executionUnavailableRef.current && shotPlaybackTokenRef.current === token && !animationRef.current && !cancelMoveRef.current && !cancelCameraPreviewRef.current && !scrubRef.current && fingerprintRef.current().full() === baseline,
                  captureContext: () => captureStudioExecutionContext(objectsRef.current),
                  readSource: async () => {
                    const response = await fetch(`/api/projects/${reviewed.scene.project_id}/scenes/${reviewed.scene.id}/studio-execution/source`, { method: 'GET' });
                    const value = await response.json(); if (!response.ok) throw Error(value?.error || `读取来源失败 HTTP ${response.status}`); return value;
                  },
                  commit: batch => {
                    const before = objectsRef.current;
                    const updates = batch.updates.map(value => {
                      const entry = before.find(item => item.id === value.id);
                      if (!entry || entry.locked || !entry.object.visible || entry.splat) throw Error('复用对象已不可编辑');
                      return { entry, value, name: entry.name, objectName: entry.object.name, position: entry.object.position.clone(), rotation: entry.object.rotation.clone(), scale: entry.object.scale.clone() };
                    });
                    if (batch.additions.some(entry => before.some(item => item.id === entry.id))) throw Error('新增对象ID冲突');
                    historyRef.current.checkpoint(before);
                    try {
                      updates.forEach(({ entry, value }) => { entry.name = value.name; entry.object.name = value.name; entry.object.position.set(...value.position); entry.object.rotation.set(...value.rotation); entry.object.scale.set(...value.scale); entry.object.updateMatrixWorld(true); });
                      batch.additions.forEach(entry => scene.add(entry.object));
                      objectsRef.current = [...before, ...batch.additions];
                      historyRef.current.collect(objectsRef.current);
                    } catch (cause) {
                      batch.additions.forEach(entry => entry.object.removeFromParent()); objectsRef.current = before;
                      updates.forEach(saved => { saved.entry.name = saved.name; saved.entry.object.name = saved.objectName; saved.entry.object.position.copy(saved.position); saved.entry.object.rotation.copy(saved.rotation); saved.entry.object.scale.copy(saved.scale); saved.entry.object.updateMatrixWorld(true); });
                      throw cause;
                    }
                    syncObjects(); setScriptShots(batch.shotPlan);
                    setStatus(`已确认应用导演执行草稿：追加${batch.additions.length}对象、更新${updates.length}对象、${batch.shotPlan.shots.length}镜；请播放审阅并保存，未生成媒体`);
                  },
                });
              }} onApply={(draft, entries, x, z) => {
                const scene = sceneRef.current;
                if (!scene) throw new Error('场景尚未就绪，请重试');
                const ids = new Set(objectsRef.current.map(entry => entry.id));
                if (entries.some(entry => ids.has(entry.id)) || new Set(entries.map(entry => entry.id)).size !== entries.length) throw new Error('布景实例身份冲突，请重新生成草稿');
                stopAnimation();
                const empty = objectsRef.current.length === 0;
                entries.forEach(entry => { entry.object.position.x += x; entry.object.position.z += z; });
                historyRef.current.checkpoint(objectsRef.current);
                entries.forEach(entry => scene.add(entry.object));
                objectsRef.current = [...objectsRef.current, ...entries];
                historyRef.current.collect(objectsRef.current);
                syncObjects();
                if (empty && cameraRef.current && controlsRef.current) {
                  const view = draft.document.view;
                  cameraRef.current.position.set(view.position.x + x, view.position.y, view.position.z + z);
                  cameraRef.current.fov = view.fov ?? 45; cameraRef.current.updateProjectionMatrix();
                  controlsRef.current.target.set(view.target.x + x, view.target.y, view.target.z + z);
                  controlsRef.current.update();
                }
                selectObject(entries[0]?.id ?? null);
                setStatus(`已追加 ${draft.name}：${entries.length} 个可编辑实例（静态布景）`);
              }} onCaptureCamera={() => {
                const camera = cameraRef.current; const controls = controlsRef.current;
                if (!camera || !controls) throw new Error('视口尚未就绪，无法记录机位');
                return { position: camera.position.toArray() as [number, number, number], target: controls.target.toArray() as [number, number, number], fov: camera.fov };
              }} onPreviewShot={(shotCamera) => {
                const camera = cameraRef.current; const controls = controlsRef.current;
                if (!camera || !controls) throw new Error('视口尚未就绪，无法预览机位');
                // 复用现有视口相机与 OrbitControls，不新建预览器；这只是取景预览，不是成片渲染。
                stopAnimation();
                camera.position.set(...shotCamera.position);
                camera.fov = shotCamera.fov; camera.updateProjectionMatrix();
                controls.target.set(...shotCamera.target); controls.update();
                setStatus('已用视口相机预览镜头机位（取景预览，未渲染序列）');
              }} onApplyShots={(plan) => {
                // 换计划前还原旧播放，避免界面台词与仍在播放的旧机位错配。
                stopAnimation();
                // 只把已确认的镜头表接入场景状态，随现有存档保存；不改对象、不触发生成。
                setScriptShots(plan);
                setStatus(`已接入镜头表：${plan.shots.length} 镜 · ${plan.shots[plan.shots.length - 1]!.end.toFixed(2)} 秒（未指定机位 ${plan.shots.filter(shot => !shot.camera).length} 镜），保存场景后随存档保留`);
              }} appliedPlan={scriptShots} shotPlayback={shotPlayback} onPlayShots={(plan) => { void playScriptShots(plan); }} onStopShots={stopAnimation} onExportShotPack={exportShotReferencePack}
                listMotionTargets={() => objectsRef.current.map(entry => ({
                  id: entry.id, name: entry.name, visible: entry.object.visible, locked: !!entry.locked,
                  animations: entry.object.animations.map((clip, index) => ({ index, name: clip.name, duration: clip.duration })),
                }))} />
            </>}
          </details>
        </div>
      </div>

      <div className="fixed right-3 top-20 z-30 max-h-[calc(100vh-6rem)] w-64 space-y-3 overflow-y-auto rounded-lg border border-white/10 bg-[#101014]/90 p-3 text-sm text-white/80 backdrop-blur">
        {scriptShots && <ShotEnhancementPanel plan={scriptShots} entries={objects} environment={environment} disabled={!!busy || !!shotPlayback} onApply={plan => { stopAnimation(); setScriptShots(plan); setStatus('镜头增强已应用，请播放审阅并保存场景'); }} />}
        <CharacterMotionPanel entries={objects} disabled={!!busy || !!shotPlayback} onChanged={() => { stopAnimation(); syncObjects(); setStatus('重定向动作已加入目标角色，请审阅并保存场景'); }} />
        {scriptShots && <ScriptVideoPanel plan={scriptShots} frameAspect={frameAspect} disabled={!!busy || !!shotPlayback} onExport={exportScriptVideo} isCurrent={artifact => artifact.epoch === sceneEpochRef.current && artifact.source === fingerprintRef.current().full()} />}
        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">灯光</div>
          <div className="grid grid-cols-4 gap-1">
            {(Object.keys(LIGHT_PRESETS) as LightPresetName[]).map((name) => (
              <button
                key={name}
                type="button"
                onClick={() => {
                  // 环境启用时预设只改「记录用」的预设名，灯光/背景立刻回到环境，
                  // 否则点一下预设就把环境画面顶掉，同一个场景出现两种结果。
                  applyLightPreset(name);
                  const current = environmentRef.current;
                  if (current) applyEnvironment(current);
                }}
                className={`rounded-md px-1 py-1 text-xs ${
                  lightPreset === name ? "bg-amber-400/20 text-amber-100" : "bg-white/5 text-white/60 hover:bg-white/10"
                }`}
              >
                {LIGHT_PRESETS[name].label}
              </button>
            ))}
          </div>
          {environment ? (
            <div className="mt-1 px-1 text-[10px] leading-snug text-amber-200/70">
              环境接管主光/半球光与背景，灯光预设此时只作参考。
            </div>
          ) : null}
        </section>

        <section>
          <div className="mb-1.5 flex items-center justify-between px-1 text-xs uppercase tracking-wide text-white/40">
            <span>环境（时间/天气/地形）</span>
            <button
              type="button"
              onClick={() => (environment ? applyEnvironment(null) : applyEnvironment(defaultSceneEnvironment()))}
              className={`rounded px-1.5 py-0.5 text-[10px] normal-case tracking-normal ${
                environment ? "bg-emerald-400/25 text-emerald-100" : "bg-white/5 text-white/60 hover:bg-white/10"
              }`}
            >
              {environment ? "关闭" : "启用"}
            </button>
          </div>
          {environment ? (
            <div className="space-y-2">
              <label className="flex items-center justify-between text-xs">
                <span className="text-white/60">时间 {environment.hour.toFixed(2)} 时</span>
                <input
                  type="range"
                  min={0}
                  max={24}
                  step={0.25}
                  value={environment.hour}
                  onChange={(event) => applyEnvironment({ ...environment, hour: Number(event.target.value) })}
                  className="w-28"
                />
              </label>
              <label className="flex items-center justify-between text-xs">
                <span className="text-white/60">天气</span>
                <select
                  value={environment.weather.condition}
                  onChange={(event) => applyEnvironment({
                    ...environment,
                    weather: { ...environment.weather, condition: event.target.value as WeatherCondition },
                  })}
                  className="rounded border border-white/10 bg-[#17171d] px-1 py-0.5"
                >
                  {WEATHER_CONDITIONS.map((condition) => (
                    <option key={condition} value={condition}>{WEATHER_LABELS[condition]}</option>
                  ))}
                </select>
              </label>
              <label className="flex items-center justify-between text-xs">
                <span className="text-white/60">强度 {environment.weather.intensity.toFixed(2)}</span>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={environment.weather.intensity}
                  onChange={(event) => applyEnvironment({
                    ...environment,
                    weather: { ...environment.weather, intensity: Number(event.target.value) },
                  })}
                  className="w-28"
                />
              </label>
              {(['latitude', 'northOffset'] as const).map(key => <label key={key} className="flex items-center justify-between text-xs">
                <span>{key === 'latitude' ? '纬度' : '北向偏移/度'}</span>
                <input aria-label={key === 'latitude' ? '环境纬度' : '环境北向'} type="range" min={key === 'latitude' ? -66 : -180} max={key === 'latitude' ? 66 : 180} value={environment[key]} onChange={event => applyEnvironment({ ...environment, [key]: Number(event.target.value) })} className="w-24" />
                <span>{environment[key]}°</span>
              </label>)}
              <label className="flex items-center justify-between text-xs">风力 {environment.weather.wind.toFixed(2)}
                <input aria-label="环境风力" type="range" min={0} max={1} step={0.05} value={environment.weather.wind} onChange={event => applyEnvironment({ ...environment, weather: { ...environment.weather, wind: Number(event.target.value) } })} className="w-28" />
              </label>
              <div className="rounded border border-white/10 p-1.5">
                <div className="flex items-center justify-between text-xs">
                  <span className="text-white/60">
                    {environment.terrain
                      ? `地形 ${environment.terrain.size} 米 / ${environment.terrain.segments} 段`
                      : "地形（未启用，地面为 20×20 平地）"}
                  </span>
                  {environment.terrain ? (
                    <button
                      type="button"
                      onClick={() => {
                        applyTerrainChange(null);
                        setStatus("已清除地形：地面回到 20×20 平地，落点 Y 复位为 0.02；未保存前不会写进存档");
                      }}
                      className="rounded bg-white/5 px-1.5 py-0.5 text-[10px] text-white/60 hover:bg-white/10"
                    >
                      清除
                    </button>
                  ) : null}
                </div>
                <div className="mt-1 grid grid-cols-3 gap-1 text-[10px]">
                  <label className="flex flex-col gap-0.5">
                    <span className="text-white/35">边长/米</span>
                    <input
                      type="number"
                      min={20}
                      max={1024}
                      value={terrainDraft.size}
                      onChange={(event) => setTerrainDraft((draft) => ({ ...draft, size: Number(event.target.value) }))}
                      className="rounded border border-white/10 bg-[#17171d] px-1 py-0.5 text-white/80"
                    />
                  </label>
                  <label className="flex flex-col gap-0.5">
                    <span className="text-white/35">起伏/米</span>
                    <input
                      type="number"
                      min={0}
                      max={16}
                      value={terrainDraft.amplitude}
                      onChange={(event) => setTerrainDraft((draft) => ({ ...draft, amplitude: Number(event.target.value) }))}
                      className="rounded border border-white/10 bg-[#17171d] px-1 py-0.5 text-white/80"
                    />
                  </label>
                  <label className="flex flex-col gap-0.5">
                    <span className="text-white/35">种子</span>
                    <input
                      type="number"
                      min={0}
                      max={2147483647}
                      value={terrainDraft.seed}
                      onChange={(event) => setTerrainDraft((draft) => ({ ...draft, seed: Number(event.target.value) }))}
                      className="rounded border border-white/10 bg-[#17171d] px-1 py-0.5 text-white/80"
                    />
                  </label>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    try {
                      const terrain = createEnvironmentTerrain(terrainDraft);
                      applyTerrainChange(terrain);
                      setStatus(`已生成地形（${summarizeEnvironment({ ...environment, terrain })}）；落点已贴到新地面，保存场景才会写进存档`);
                    } catch (err) {
                      setError(err instanceof Error ? err.message : String(err));
                    }
                  }}
                  className="mt-1 w-full rounded bg-emerald-400/20 px-1.5 py-1 text-[11px] text-emerald-100 hover:bg-emerald-400/30"
                >
                  {environment.terrain ? "按当前参数重建地形" : "生成地形"}
                </button>
                {environment.terrain && <EnvironmentBrushPanel terrain={environment.terrain} disabled={!!busy || !!shotPlayback} onChange={applyTerrainChange} />}
              </div>
              <div className="px-1 text-[10px] leading-snug text-white/35">
                {summarizeEnvironment(environment)}
              </div>
              <div className="px-1 text-[10px] leading-snug text-white/30">
                同一份环境在视口、截图、序列与参考图取样里结果一致；逐镜走位会按地形坡度上限
                检查坡度，参考图不烘雾/雨雪。风力控制雨雪漂移；视频按时间轴确定性取样。
              </div>
            </div>
          ) : (
            <div className="px-1 text-[10px] leading-snug text-white/35">
              未启用：沿用灯光预设与 20×20 平地（旧存档行为）。启用后太阳/雾/地形接管地面。
            </div>
          )}
        </section>

        <section>
          <div className="mb-1.5 flex items-center justify-between px-1 text-xs uppercase tracking-wide text-white/40">
            <span>泼溅细节</span>
            <button
              type="button"
              onClick={() => setSplatDetail("auto")}
              title={`按实测帧率自动在四档间升降，当前 ${autoDetail.toLocaleString()} 个 splat；手点任一档位就退出自动`}
              className={`rounded px-1.5 py-0.5 text-[10px] normal-case tracking-normal ${
                splatDetail === "auto"
                  ? "bg-violet-400/35 text-white"
                  : "bg-white/5 text-white/60 hover:bg-white/10"
              }`}
            >
              自动
            </button>
          </div>
          <div className="grid grid-cols-4 gap-1">
            {SPLAT_DETAIL_PRESETS.map((preset) => (
              <button
                key={preset.count}
                type="button"
                onClick={() => setSplatDetail(preset.count)}
                title={`目标 ${preset.count.toLocaleString()} 个 splat；LoD 按屏幕尺寸挑代表点，不是直接砍掉远处`}
                className={`rounded-md px-1 py-1 text-xs ${
                  splatDetail === preset.count
                    ? "bg-violet-400/35 text-white"
                    : "bg-white/5 text-white/60 hover:bg-white/10"
                }`}
              >
                {preset.label}
              </button>
            ))}
          </div>
          <div className="mt-1 px-1 text-[10px] leading-snug text-white/35">
            {splatDetail === "auto"
              ? `自动：掉到 28fps 以下降档，回到 55fps 以上升档，改动后等 30 秒再看。当前 ${autoDetail.toLocaleString()}。`
              : "手动锁档：不会自动调解，想要自动就点上面的「自动」"}
          </div>
        </section>

        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">材质</div>
          {selectedId ? (
            <div className="space-y-2">
              <label className="flex items-center justify-between text-xs">
                <span className="text-white/60">颜色</span>
                <input
                  type="color"
                  value={material.color}
                  onChange={(event) => applyMaterial({ color: event.target.value })}
                  className="h-6 w-12 cursor-pointer rounded border border-white/10 bg-transparent"
                />
              </label>
              {(
                [
                  ["粗糙度", "roughness", 0, 1, 0.01],
                  ["金属度", "metalness", 0, 1, 0.01],
                  ["不透明度", "opacity", 0.05, 1, 0.01],
                ] as const
              ).map(([label, key, min, max, step]) => (
                <label key={key} className="block text-xs">
                  <span className="flex items-center justify-between text-white/60">
                    <span>{label}</span>
                    <span>{material[key].toFixed(2)}</span>
                  </span>
                  <input
                    type="range"
                    min={min}
                    max={max}
                    step={step}
                    value={material[key]}
                    onChange={(event) => applyMaterial({ [key]: Number(event.target.value) } as Partial<MaterialSettings>)}
                    className="w-full"
                  />
                </label>
              ))}
            </div>
          ) : (
            <p className="px-1 text-xs text-white/40">先选中一个对象再调材质</p>
          )}
        </section>

        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">变换</div>
          {selectedId ? (
            <div className="space-y-1">
              <div className="grid grid-cols-3 gap-1">
                {(["x", "y", "z"] as const).map((axis) => (
                  <label key={axis} className="block text-[11px] text-white/50">
                    <span className="uppercase">{axis}</span>
                    <input
                      type="number"
                      step={0.1}
                      value={transform[axis]}
                      onChange={(event) => setTransformValue(axis, Number(event.target.value))}
                      className="w-full rounded border border-white/10 bg-black/30 px-1 py-0.5 text-xs text-white/80"
                    />
                  </label>
                ))}
              </div>
              <div className="grid grid-cols-3 gap-1">
                {(['rotateX', 'rotateZ', 'scaleX', 'scaleY', 'scaleZ'] as const).map(axis => <label key={axis} className="text-[11px] text-white/50">{axis}<input aria-label={axis} type="number" step={axis.startsWith('rotate') ? 15 : .1} value={transform[axis]} onChange={event => setTransformValue(axis, Number(event.target.value))} className="w-full rounded bg-black/30" /></label>)}
                <label className="block text-[11px] text-white/50">
                  <span>旋转 Y°</span>
                  <input
                    type="number"
                    step={15}
                    value={transform.rotateY}
                    onChange={(event) => setTransformValue("rotateY", Number(event.target.value))}
                    className="w-full rounded border border-white/10 bg-black/30 px-1 py-0.5 text-xs text-white/80"
                  />
                </label>
                <label className="block text-[11px] text-white/50">
                  <span>缩放</span>
                  <input
                    type="number"
                    step={0.1}
                    min={0.05}
                    value={transform.scale}
                    onChange={(event) => setTransformValue("scale", Number(event.target.value))}
                    className="w-full rounded border border-white/10 bg-black/30 px-1 py-0.5 text-xs text-white/80"
                  />
                </label>
              </div>
            </div>
          ) : (
            <p className="px-1 text-xs text-white/40">先选中一个对象</p>
          )}
        </section>

        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">背景与平面层</div>
          <div className="space-y-1">
            <div className="flex gap-1">
              <button
                type="button"
                onClick={() => imagePlaneInputRef.current?.click()}
                className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                title="把图片当背景板/参考板放进 3D 场景"
              >
                图片平面
              </button>
              <button
                type="button"
                onClick={() => videoPlaneInputRef.current?.click()}
                className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                title="把视频当动态背景板放进 3D 场景（不参与 GLB 导出）"
              >
                视频平面
              </button>
            </div>
            <div className="flex gap-1">
              <button
                type="button"
                onClick={() => skyboxInputRef.current?.click()}
                className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                title="导入等距柱状全景图作为天球背景"
              >
                导入天空盒
              </button>
              <button
                type="button"
                onClick={clearSkybox}
                disabled={!skybox.enabled}
                className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/50 hover:bg-white/10 disabled:opacity-40"
              >
                清除
              </button>
            </div>
            <div className="space-y-1 rounded-md bg-white/5 px-2 py-1.5">
              <div className="flex items-center justify-between text-[11px] text-white/45">
                <span>从素材库选图</span>
                <button
                  type="button"
                  onClick={() => void loadBackgroundImages()}
                  className="rounded px-1 text-[11px] text-white/55 hover:bg-white/10"
                >
                  {backgroundLoading ? "读取中…" : "刷新"}
                </button>
              </div>
              <select
                value={backgroundImageId}
                onChange={(event) => setBackgroundImageId(event.target.value)}
                onFocus={() => {
                  if (backgroundImages.length === 0 && !backgroundLoading) void loadBackgroundImages();
                }}
                className="w-full rounded bg-black/30 px-1 py-1 text-[11px] text-white/70"
              >
                <option value="">
                  {backgroundImages.length ? "请选择一张图…" : "点「刷新」读取素材库"}
                </option>
                {backgroundImages.map((image) => (
                  <option key={image.id} value={image.id}>
                    {image.name}
                  </option>
                ))}
              </select>
              <div className="flex gap-1">
                <button
                  type="button"
                  onClick={applyBackgroundAsPlane}
                  disabled={!findImage(backgroundImages, backgroundImageId)}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10 disabled:opacity-40"
                  title="把素材库里的这张图当背景板放进 3D 场景（不再需要文件选择器）"
                >
                  铺成图片平面
                </button>
                <button
                  type="button"
                  onClick={applyBackgroundAsSkybox}
                  disabled={!findImage(backgroundImages, backgroundImageId)}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10 disabled:opacity-40"
                  title="把素材库里的这张图当天球背景（等距柱状全景最理想）"
                >
                  设为天空盒
                </button>
              </div>
            </div>
            {skybox.enabled && (
              <div className="space-y-1 rounded-md bg-white/5 px-2 py-1.5">
                <div className="truncate text-[11px] text-white/45">{skyboxName}</div>
                <label className="block text-[11px] text-white/50">
                  <span className="flex justify-between">
                    <span>亮度</span>
                    <span>{skybox.intensity.toFixed(2)}</span>
                  </span>
                  <input
                    type="range"
                    min={0}
                    max={2}
                    step={0.05}
                    value={skybox.intensity}
                    onChange={(event) => setSkyboxIntensity(Number(event.target.value))}
                    className="w-full"
                  />
                </label>
                <label className="flex items-center gap-1.5 text-[11px] text-white/50">
                  <input
                    type="checkbox"
                    checked={skybox.asEnvironment}
                    onChange={(event) => setSkyboxAsEnvironment(event.target.checked)}
                  />
                  同时当环境光（影响金属反射）
                </label>
              </div>
            )}
            {selectedId && objects.find((item) => item.id === selectedId)?.mediaKind && (
              <div className="space-y-1 rounded-md bg-white/5 px-2 py-1.5">
                {objects.find((item) => item.id === selectedId)?.mediaKind === 'image' && (
                  <button type="button" onClick={() => void cutoutSelected()}
                    disabled={Boolean(busy) || !!objects.find((item) => item.id === selectedId)?.locked || !objects.find((item) => item.id === selectedId)?.object.visible}
                    className="w-full rounded bg-emerald-500/15 px-2 py-1 text-xs text-emerald-200 disabled:opacity-40">
                    去背景并加入独立平面
                  </button>
                )}
                <label className="block text-[11px] text-white/50">
                  <span className="flex justify-between">
                    <span>平面不透明度</span>
                    <span>{planeSettings.opacity.toFixed(2)}</span>
                  </span>
                  <input
                    type="range"
                    min={0}
                    max={1}
                    step={0.01}
                    value={planeSettings.opacity}
                    onChange={(event) => applyPlaneSettings({ opacity: Number(event.target.value) })}
                    className="w-full"
                  />
                </label>
                <label className="flex items-center gap-1.5 text-[11px] text-white/50">
                  <input
                    type="checkbox"
                    checked={planeSettings.doubleSided}
                    onChange={(event) => applyPlaneSettings({ doubleSided: event.target.checked })}
                  />
                  双面可见
                </label>
                <label className="flex items-center gap-1.5 text-[11px] text-white/50">
                  <input type="checkbox" checked={planeSettings.chromaKey} onChange={(event) => applyPlaneSettings({ chromaKey: event.target.checked })} />色键抠像
                  <input aria-label="抠像颜色" type="color" value={planeSettings.keyColor} onChange={(event) => applyPlaneSettings({ keyColor: event.target.value })} />
                </label>
                {planeSettings.chromaKey && (["similarity", "smoothness", "spill"] as const).map((key) => (
                  <label key={key} className="block text-[11px] text-white/50">{key === "similarity" ? "相似度" : key === "smoothness" ? "边缘柔化" : "溢色抑制"}
                    <input aria-label={key} type="range" min="0.001" max="1" step="0.001" value={planeSettings[key]} onChange={(event) => applyPlaneSettings({ [key]: Number(event.target.value) })} className="w-full" />
                  </label>
                ))}
                <p className="text-[10px] leading-3 text-white/30">
                  视频平面随场景存档保存；GLB格式不支持视频
                </p>
              </div>
            )}
          </div>
        </section>

        <section data-animation-preview>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">内置骨骼动作（随场景保存）</div>
          {animationClips.length ? <div className="space-y-2">
            <select aria-label="内置动作片段" value={animationIndex} className="w-full rounded bg-[#222] p-1 text-xs" onChange={event => { stopAnimation(); setAnimationIndex(Number(event.target.value)); }}>
              {animationClips.map((clip, index) => <option key={index} value={index}>{clip.name || `片段 ${index + 1}`} · {clip.duration.toFixed(2)} 秒</option>)}
            </select>
            <div className="flex gap-1">
              <button type="button" className="flex-1 rounded bg-white/10 p-1 text-xs disabled:opacity-40" disabled={Boolean(busy || operationBusy || playingPath || playingMove || selectedAnimationEntry?.locked || !selectedAnimationEntry?.object.visible || animationState === 'playing')} onClick={playAnimation}>{animationState === 'paused' ? '继续动作' : '播放动作'}</button>
              <button type="button" className="flex-1 rounded bg-white/10 p-1 text-xs disabled:opacity-40" disabled={animationState !== 'playing' || playingMove || playingPath} onClick={() => { animationRef.current?.preview.pause(); setAnimationState('paused'); }}>暂停动作</button>
              <button type="button" className="flex-1 rounded bg-white/10 p-1 text-xs disabled:opacity-40" disabled={animationState === 'stopped'} onClick={stopAnimation}>停止并还原</button>
            </div>
            <p role="status" className="text-[11px] text-white/50">{animationState === 'playing' ? '动作播放中' : animationState === 'paused' ? '动作已暂停（仍占用骨骼）' : '动作已停止'}</p>
          </div> : <p className="text-[11px] text-white/40">选中带内置骨骼动作的 GLB 或 v6 场景角色；静态模型与无动作旧档没有可用片段。</p>}
          <p className="mt-1 text-[10px] text-white/40">仅使用骨骼局部动作；编辑、FK、保存前自动停止并还原，不保存临时帧。动作资源随 v6 场景保存，重开不自动播放；路径、相机跟拍、参考序列及首尾帧复用路径面板动作选择，路径动作选择也随场景保存，重开后需主动预览或导出。</p>
          <div className="mt-2 space-y-1 border-t border-white/10 pt-2">
            <div className="px-1 text-[11px] text-white/50">动作目录（CC0 · 按需载入）</div>
            {!animationCatalog ? (
              <button type="button" onClick={loadAnimationCatalog} className="w-full rounded bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10">
                载入动作目录
              </button>
            ) : (
              <>
                <p className="px-1 text-[10px] leading-4 text-white/40">
                  {animationCatalog.summary.total} 条 = 源 {animationCatalog.summary.source} + 镜像 {animationCatalog.summary.mirrored} · 数据修订 {animationCatalog.summary.revision} · CC0-1.0（不重授权为本仓库 MIT）
                </p>
                <select aria-label="动作目录片段" value={animationCatalogId} onChange={event => setAnimationCatalogId(event.target.value)} className="w-full rounded bg-[#222] p-1 text-xs">
                  {animationCatalog.entries.map(entry => <option key={entry.id} value={entry.id}>{entry.category} · {entry.name} · {entry.duration.toFixed(2)} 秒{entry.mirrored ? "（镜像）" : ""}</option>)}
                </select>
                <button
                  type="button"
                  onClick={() => addCatalogAnimation(animationCatalogId)}
                  disabled={!animationCatalogId || !catalogBinding?.bindable || Boolean(busy) || Boolean(selectedAnimationEntry?.locked) || !selectedAnimationEntry?.object.visible}
                  className="w-full rounded bg-white/10 px-2 py-1 text-xs disabled:opacity-40"
                >
                  加入该角色动作
                </button>
                <p role="status" className="px-1 text-[10px] leading-4 text-white/40">
                  {!selectedAnimationEntry
                    ? "先在左栏选中要加动作的角色"
                    : !selectedHasSkeleton
                      ? "这个对象没有骨骼；目录只支持同名 23 根人形骨骼"
                      : catalogBinding
                        ? catalogBinding.bindable
                          ? `可绑定：${catalogBinding.bones.length} 根同名骨骼全部存在`
                          : `不可绑定：缺少 ${catalogBinding.missing.length} 根骨骼（${catalogBinding.missing.join("、")}）`
                        : "选一个目录片段"}
                </p>
                <p className="px-1 text-[10px] leading-4 text-white/30">
                  只按同名骨骼绑定，不做骨架重定向、IK 或脚底贴地；加入后就是普通 v6 动作资源，播放、逐镜视频与参考包读同一份轨道。
                </p>
              </>
            )}
          </div>
        </section>

        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">骨骼摆姿</div>
          {!selectedId ? (
            <p className="px-1 text-xs text-white/40">先选中一个对象</p>
          ) : !selectedHasSkeleton ? (
            <p className="px-1 text-[11px] leading-4 text-white/40">
              这个模型没有骨骼，导入带骨架的 GLB 才能摆姿势
            </p>
          ) : (
            <div className="space-y-2">
              <button
                type="button"
                onClick={togglePosing}
                aria-pressed={posing}
                className={`w-full rounded-md px-2 py-1 text-xs ${
                  posing ? "bg-sky-500/25 text-sky-100" : "bg-white/5 text-white/70 hover:bg-white/10"
                }`}
              >
                {posing ? "退出摆姿" : "开启摆姿"}
              </button>
              {posing && (
                <p className="px-1 text-[11px] leading-4 text-white/45">
                  点画面里的红球选骨，用圆环绕骨自身旋转，共 {posingBones} 根可摆骨。当前骨：
                  {activeBone ?? "还没选"}
                </p>
              )}
              <p className="px-1 text-[11px] leading-4 text-white/45">保存姿势仅记在本次编辑内存中；保存场景/存到后端会持久保存当前画面姿势与初始姿势，无需先点此按钮。</p>
              <div className="flex gap-1">
                <button
                  type="button"
                  onClick={saveBonePose}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                >
                  保存姿势（内存）
                </button>
                <button
                  type="button"
                  title="回到上次内存记忆的姿势"
                  onClick={() => { if (window.confirm("还原到上次记住的姿势？当前未记忆的调整将被覆盖。")) restoreBonePose(); }}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                >
                  还原
                </button>
                <button
                  type="button"
                  title="恢复模型最初载入时的姿势"
                  onClick={() => { if (window.confirm("恢复模型初始姿势？当前调整将被覆盖。")) resetBonePose(); }}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/50 hover:bg-white/10"
                >
                  初始
                </button>
              </div>
            </div>
          )}
        </section>

        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">整体姿势混合</div>
          {selectedId ? (
            <div className="space-y-2">
              <div className="flex gap-1">
                <button
                  type="button"
                  className={`flex-1 rounded-md px-2 py-1 text-xs ${
                    poseSlots.a ? "bg-sky-500/25 text-sky-100" : "bg-white/5 text-white/60 hover:bg-white/10"
                  }`}
                  onClick={() => recordPose("a")}
                >
                  记姿势 A
                </button>
                <button
                  type="button"
                  className={`flex-1 rounded-md px-2 py-1 text-xs ${
                    poseSlots.b ? "bg-sky-500/25 text-sky-100" : "bg-white/5 text-white/60 hover:bg-white/10"
                  }`}
                  onClick={() => recordPose("b")}
                >
                  记姿势 B
                </button>
              </div>
              <label className="block text-xs">
                <span className="flex items-center justify-between text-white/60">
                  <span>混合</span>
                  <span>{poseBlend.toFixed(2)}</span>
                </span>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.01}
                  value={poseBlend}
                  disabled={!poseSlots.a || !poseSlots.b}
                  onChange={(event) => blendPose(Number(event.target.value))}
                  className="w-full disabled:opacity-40"
                />
              </label>
              <p className="px-1 text-[11px] leading-4 text-white/35">
                先用移动/旋转/缩放摆好角色，记 A、再摆一次记 B，最后拖混滑杆生成中间姿势。
              </p>
            </div>
          ) : (
            <p className="px-1 text-xs text-white/40">先选中一个对象再摆姿势</p>
          )}
        </section>

        <section>
          <div className="mb-1.5 flex items-center justify-between px-1 text-xs uppercase tracking-wide text-white/40">
            <span>相机路径</span>
            <span className="text-white/30">{cameraKeys.length} 个机位</span>
          </div>
          <div className="flex gap-1">
            <button
              type="button"
              className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
              onClick={recordCameraKey}
            >
              记录机位
            </button>
            <button
              type="button"
              className="flex-1 rounded-md bg-emerald-400/15 px-2 py-1 text-xs text-emerald-200 hover:bg-emerald-400/25 disabled:opacity-40"
              onClick={playCameraPath}
              disabled={cameraKeys.length < 2 || playingPath || playingMove || Boolean(busy)}
            >
              {playingPath ? "播放中…" : "播放"}
            </button>
            <button type="button" disabled={!playingPath} onClick={stopAnimation} className="rounded bg-white/5 px-1 py-1 text-[11px] disabled:opacity-40">停止跟拍预览</button>
            <button
              type="button"
              className="rounded-md bg-white/5 px-2 py-1 text-xs text-white/50 hover:bg-white/10"
              onClick={() => setCameraKeys([])}
            >
              清空
            </button>
          </div>
          {/* 首尾帧：改成 H3 首尾帧模板后不再传 ref_video，白模画面就漏不进成片了。 */}
          <button
            type="button"
            className="mt-1 w-full rounded-md bg-white/5 px-2 py-1 text-xs text-white/60 hover:bg-white/10 disabled:opacity-40"
            onClick={exportFirstLastFrames}
            disabled={cameraKeys.length < 2}
            title="按第一个机位和最后一个机位各渲一帧当前画面，存进素材库当 H3 的首帧/尾帧（模板选 minimax_h3_first_last）"
          >
            导出首尾帧（存素材库）
          </button>
          {/* 运镜文案：首尾帧只定两端，中间怎么动得靠这句话；空着就等于让模型自由发挥。 */}
          {firstLastNote ? (
            <div className="mt-1 rounded-md border border-white/10 bg-black/20 px-2 py-1">
              <div className="flex items-center justify-between gap-1">
                <span className="text-[11px] text-white/40">运镜文案（写进提示词）</span>
                <button
                  type="button"
                  className="rounded bg-white/5 px-1.5 py-0.5 text-[11px] text-white/50 hover:bg-white/10"
                  onClick={copyFirstLastNote}
                >
                  复制
                </button>
              </div>
              <div className="mt-0.5 text-[11px] leading-snug text-white/70">{firstLastNote}</div>
              {/* 免复制粘贴：把这一句直接追加进某一场的视频提示词（只追加，其他字段不动）。 */}
              <button
                type="button"
                className="mt-1 w-full rounded bg-white/5 px-1.5 py-0.5 text-[11px] text-white/55 hover:bg-white/10"
                onClick={() => (promptPanelOpen ? setPromptPanelOpen(false) : void openPromptPanel())}
              >
                {promptPanelOpen ? "收起「写进场景提示词」" : "写进场景提示词…"}
              </button>
              {promptPanelOpen ? (
                <div className="mt-1 rounded border border-white/10 bg-black/20 px-1.5 py-1">
                  <div className="flex items-center gap-1">
                    <select
                      value={promptProjectId}
                      onChange={(event) => void switchPromptProject(event.target.value)}
                      className="min-w-0 flex-1 rounded bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
                    >
                      {promptProjects.length === 0 ? <option value="">读取中…</option> : null}
                      {promptProjects.map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.label}
                        </option>
                      ))}
                    </select>
                    <select
                      value={promptSceneId}
                      onChange={(event) => setPromptSceneId(event.target.value)}
                      className="min-w-0 flex-1 rounded bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
                    >
                      {promptScenes.length === 0 ? <option value="">没有场次</option> : null}
                      {promptScenes.map((item) => (
                        <option key={item.id} value={String(item.id)}>
                          {item.label}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="mt-1 text-[11px] leading-snug text-white/45">
                    将追加这一行（其他文字、模板、首尾帧都不动）：{motionPromptLine(firstLastNote) || "（空）"}
                  </div>
                  <button
                    type="button"
                    className="mt-1 w-full rounded-md bg-sky-400/20 px-2 py-1 text-xs text-sky-100 hover:bg-sky-400/30 disabled:opacity-40"
                    onClick={() => void appendNoteToScene()}
                    disabled={promptBusy || !promptSceneId}
                  >
                    {promptBusy ? "处理中…" : "追加到这一场"}
                  </button>
                </div>
              ) : null}
            </div>
          ) : null}

          {/* 相机 Agent：一句话排运镜。追问 → 预览 → 采用 → 撤销，都不碰角色与提示词。 */}
          <div className="mt-2 rounded-md border border-white/10 bg-black/20 px-2 py-1">
            <div className="text-[11px] text-white/40">相机 Agent（一句话排运镜）</div>
            <textarea
              rows={2}
              value={agentBrief}
              onChange={(event) => setAgentBrief(event.target.value)}
              placeholder="例：从正面绕到背后半圈，然后推近 40%"
              className="mt-1 w-full resize-none rounded border border-white/10 bg-black/30 px-1.5 py-1 text-[11px] text-white/70"
            />
            <div className="mt-1 flex items-center gap-1">
              <button
                type="button"
                className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/60 hover:bg-white/10 disabled:opacity-40"
                onClick={parseAgentBrief}
                disabled={!agentBrief.trim()}
              >
                解析
              </button>
              <button
                type="button"
                className="flex-1 rounded-md bg-emerald-400/15 px-2 py-1 text-xs text-emerald-200 hover:bg-emerald-400/25 disabled:opacity-40"
                onClick={previewAgentPlan}
                disabled={!agentIntent || agentIntent.questions.length > 0}
              >
                生成预览
              </button>
              <button
                type="button"
                className="rounded-md bg-white/5 px-2 py-1 text-xs text-white/50 hover:bg-white/10"
                onClick={undoAgentPlan}
              >
                撤销
              </button>
            </div>
            {agentIntent && agentIntent.questions.length > 0 ? (
              <div className="mt-1">
                <div className="text-[11px] leading-snug text-amber-200/80">{agentIntent.questions[0]}</div>
                <div className="mt-1 flex items-center gap-1">
                  <input
                    value={agentAnswer}
                    onChange={(event) => setAgentAnswer(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") answerAgentQuestion();
                    }}
                    placeholder="回答这一句"
                    className="flex-1 rounded border border-white/10 bg-black/30 px-1.5 py-0.5 text-[11px] text-white/70"
                  />
                  <button
                    type="button"
                    className="rounded bg-white/5 px-2 py-0.5 text-[11px] text-white/60 hover:bg-white/10 disabled:opacity-40"
                    onClick={answerAgentQuestion}
                    disabled={!agentAnswer.trim()}
                  >
                    回答
                  </button>
                </div>
              </div>
            ) : null}
            {agentPlan && agentPlan.keys.length > 0 ? (
              <div className="mt-1 rounded border border-white/10 bg-black/20 px-1.5 py-1">
                <div className="text-[11px] text-white/40">
                  预览：{agentPlan.keys.length} 个机位，共{" "}
                  {agentPlan.keys
                    .slice(0, -1)
                    .reduce((sum, key) => sum + key.duration, 0)
                    .toFixed(1)}{" "}
                  秒
                </div>
                <div className="mt-0.5 text-[11px] leading-snug text-white/70">{agentPlan.notes.join(" → ")}</div>
                <button
                  type="button"
                  className="mt-1 w-full rounded-md bg-emerald-400/20 px-2 py-1 text-xs text-emerald-100 hover:bg-emerald-400/30"
                  onClick={applyAgentPlan}
                >
                  采用（替换当前机位，可撤销）
                </button>
              </div>
            ) : null}
            {agentPlan && agentPlan.problems.length > 0 ? (
              <div className="mt-1 text-[11px] leading-snug text-rose-200/80">{agentPlan.problems[0]}</div>
            ) : null}
          </div>

          {/* 环绕：绕选中对象转一圈，一键把整段机位追加到序列末尾。 */}
          <div className="mt-1 flex items-center gap-1 px-1">
            <span className="text-[11px] text-white/40">环绕</span>
            <input
              type="number"
              min={0.25}
              max={3}
              step={0.25}
              value={orbitTurns}
              onChange={(event) => setOrbitTurns(normalizeOrbitTurns(Number(event.target.value)))}
              title="绕几圈"
              className="w-12 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            />
            <span className="text-[11px] text-white/40">圈</span>
            <input
              type="number"
              min={0.5}
              max={60}
              step={0.5}
              value={orbitSeconds}
              onChange={(event) => setOrbitSeconds(normalizeOrbitSeconds(Number(event.target.value)))}
              title="绕完用几秒"
              className="w-12 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            />
            <span className="text-[11px] text-white/40">秒</span>
            {/* 俯仰：收尾时机位比起幅抬高/压低多少度；0 就是老的平视环绕。 */}
            <input
              type="number"
              min={-60}
              max={60}
              step={5}
              value={orbitPitch}
              onChange={(event) => setOrbitPitch(normalizeOrbitPitch(Number(event.target.value)))}
              title="收尾时机位抬高多少度（0=保持起幅高度；负数压低）。转着圈升空就是俯拍环绕"
              className="w-12 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            />
            <span className="text-[11px] text-white/40">°</span>
            <button
              type="button"
              onClick={addOrbitShot}
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10"
              title="绕左栏选中的对象转圈（没选中就绕当前相机目标），机位追加到序列末尾"
            >
              环绕
            </button>
          </div>

          {/* 希区柯克：位置推进 + 视角反向补偿，目标大小不变、背景被拉。 */}
          <div className="mt-1 flex items-center gap-1 px-1">
            <span className="text-[11px] text-white/40">希区柯克</span>
            <span className="text-[11px] text-white/30">推进到</span>
            <input
              type="number"
              min={10}
              max={300}
              step={5}
              value={Math.round(dollyRange * 100)}
              onChange={(event) => setDollyRange(normalizeDollyRange(Number(event.target.value) / 100))}
              title="结束时相机离目标是初始距离的百分之几（小于 100 是推进，大于 100 是后退）"
              className="w-12 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            />
            <span className="text-[11px] text-white/30">%</span>
            <input
              type="number"
              min={0.5}
              max={30}
              step={0.5}
              value={dollySeconds}
              onChange={(event) => setDollySeconds(normalizeDollySeconds(Number(event.target.value)))}
              title="推完用几秒"
              className="w-12 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            />
            <span className="text-[11px] text-white/40">秒</span>
            <button
              type="button"
              onClick={addDollyShot}
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10"
              title="对着左栏选中的对象推进并反向变焦（没选中就对着当前相机目标），机位追加到序列末尾"
            >
              推
            </button>
          </div>

          <div data-animation-preview className="mt-2 rounded bg-white/5 p-2 text-xs">
            <div>播放头 · {PREVIEW_FPS}fps · {previewFrameTime(previewFrame, scrubDuration).seconds.toFixed(3)} / {scrubDuration.toFixed(3)}秒</div>
            <input aria-label="3D预览播放头" type="range" min={0} max={scrubLastFrame} step={1} value={previewFrame}
              disabled={!scrubLastFrame || !!busy || operationBusy} onChange={event => seekPreviewFrame(Number(event.target.value))} className="w-full" />
            <div className="flex gap-2">
              <button type="button" disabled={!scrubLastFrame || previewFrame <= 0 || !!busy || operationBusy} onClick={() => seekPreviewFrame(previewFrame - 1)}>上一帧</button>
              <button type="button" disabled={!scrubLastFrame || previewFrame >= scrubLastFrame || !!busy || operationBusy} onClick={() => seekPreviewFrame(previewFrame + 1)}>下一帧</button>
              <button type="button" onClick={stopAnimation}>停止逐帧预览并还原</button>
            </div>
            <p className="mt-1 text-white/40">拖动保留临时姿态；编辑或保存前还原，不写入存档。</p>
          </div>

          {/* 关键帧列表：顺序就是运镜顺序，每段自己带时长与缓动。 */}
          {cameraKeys.length > 0 && (
            <div className="mt-1.5 space-y-1">
              {cameraKeys.map((key, index) => {
                const isLast = index === cameraKeys.length - 1;
                return (
                  <div
                    key={key.id}
                    className="flex items-center gap-1 rounded-md bg-white/5 px-1.5 py-1 text-[11px] text-white/60"
                  >
                    <span className="w-4 shrink-0 text-center text-white/40">{index + 1}</span>
                    {isLast ? (
                      <span className="flex-1 text-white/25">终点</span>
                    ) : (
                      <>
                        <input
                          type="number"
                          min={CAMERA_SEGMENT_LIMITS.min}
                          max={CAMERA_SEGMENT_LIMITS.max}
                          step={0.1}
                          value={key.duration}
                          onChange={(event) => updateCameraKey(key.id, { duration: Number(event.target.value) })}
                          className="w-12 shrink-0 rounded bg-black/30 px-1 py-0.5 text-right text-[11px] text-white/70"
                          title="这一段走多少秒"
                        />
                        <span className="shrink-0 text-white/30">秒</span>
                        <select
                          value={key.easing}
                          onChange={(event) =>
                            updateCameraKey(key.id, { easing: event.target.value as CameraEasing })
                          }
                          className="ml-1 shrink-0 rounded bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
                          title="这一段怎么加减速"
                        >
                          {(Object.keys(CAMERA_EASINGS) as CameraEasing[]).map((name) => (
                            <option key={name} value={name}>
                              {CAMERA_EASINGS[name].label}
                            </option>
                          ))}
                        </select>
                      </>
                    )}
                    {/* 视角对终点也要能改：最后一段的「推/拉」目标就是终点的 fov。 */}
                    <input
                      type="number"
                      min={CAMERA_FOV_LIMITS.min}
                      max={CAMERA_FOV_LIMITS.max}
                      step={1}
                      value={key.fov}
                      onChange={(event) =>
                        updateCameraKey(key.id, { fov: normalizeCameraFov(Number(event.target.value)) })
                      }
                      className="w-12 shrink-0 rounded bg-black/30 px-1 py-0.5 text-right text-[11px] text-white/70"
                      title="这个机位的视角（度）：越小越推近，越大越拉远"
                    />
                    <span className="flex-1" />
                    <button
                      type="button"
                      onClick={() => moveCameraKey(key.id, -1)}
                      disabled={index === 0}
                      className="rounded px-1 text-white/40 hover:bg-white/10 disabled:opacity-20"
                      title="上移"
                    >
                      ↑
                    </button>
                    <button
                      type="button"
                      onClick={() => moveCameraKey(key.id, 1)}
                      disabled={isLast}
                      className="rounded px-1 text-white/40 hover:bg-white/10 disabled:opacity-20"
                      title="下移"
                    >
                      ↓
                    </button>
                    <button
                      type="button"
                      onClick={() => removeCameraKey(key.id)}
                      className="rounded px-1 text-white/40 hover:bg-rose-500/20 hover:text-rose-200"
                      title="删除这个机位"
                    >
                      ✕
                    </button>
                  </div>
                );
              })}
              <p className="px-1 text-[11px] leading-4 text-white/35">
                按顺序播放，共{" "}
                {cameraKeys
                  .slice(0, -1)
                  .reduce((sum, key) => sum + normalizeCameraDuration(key.duration), 0)
                  .toFixed(1)}{" "}
                秒。时长与缓动跟场景一起存档（v3）。
              </p>
            </div>
          )}
        </section>

        <section>
          <div className="mb-1.5 flex items-center justify-between px-1 text-xs uppercase tracking-wide text-white/40">
            <span>地面路径 A → B</span>
            <span className="text-white/30">
              {(() => {
                const path = editorGroundPath(groundPoints);
                return path ? `${groundPathLength(path).toFixed(1)} m` : "起点 / 终点";
              })()}
            </span>
          </div>
          <div className="flex gap-1">
            {(["start", "via", "end"] as const).map((which) => (
              <button
                key={which}
                type="button"
                onClick={() => {
                  const next = placingGround === which ? null : which;
                  placingGroundRef.current = next;
                  setPlacingGround(next);
                }}
                title={
                  which === "via"
                    ? `在地面上点途经点（最多 ${MAX_GROUND_VIAS} 个），A→B 的走位会弯成穿过每一个的曲线（绕桌子/绕柱子时用）`
                    : which === "start"
                      ? "在地面上点起点 A"
                      : "在地面上点终点 B"
                }
                className={`flex-1 rounded-md px-2 py-1 text-xs ${
                  placingGround === which
                    ? "bg-emerald-500/20 text-emerald-200"
                    : "bg-white/5 text-white/70 hover:bg-white/10"
                }`}
              >
                {which === "start" ? "点选 A" : which === "via" ? "途经" : "点选 B"}
              </button>
            ))}
            <button
              type="button"
              onClick={() => {
                placingGroundRef.current = null;
                setPlacingGround(null);
                setGroundPoints({ start: null, end: null });
              }}
              disabled={!groundPoints.start && !groundPoints.end && !(groundPoints.vias?.length ?? 0)}
              className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10 disabled:opacity-40"
            >
              清除
            </button>
          </div>
          {(groundPoints.vias?.length ?? 0) > 0 ? (
            <div className="mt-1 flex items-center gap-1 px-1">
              <span className="text-[11px] text-sky-300/70">
                途经点 {groundPoints.vias?.length}/{MAX_GROUND_VIAS}
              </span>
              <button
                type="button"
                onClick={() => {
                  // 状态条也更新：否则「最多 3 个」这类提示会一直挂着，看不出撤销生效没有。
                  const left = Math.max(0, (groundPoints.vias?.length ?? 0) - 1);
                  setGroundPoints((current) => ({
                    ...current,
                    vias: (current.vias ?? []).slice(0, -1),
                  }));
                  setStatus(left > 0 ? `已撤掉最后一个途经点，还剩 ${left} 个` : "已撤掉全部途经点，走直线 A→B");
                }}
                title="撤掉最后一个途经点"
                className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10"
              >
                撤销
              </button>
              <button
                type="button"
                onClick={() => {
                  setGroundPoints((current) => ({ ...current, vias: [] }));
                  setStatus("已去掉全部途经点，走直线 A→B");
                }}
                title="去掉全部途经点，回到直线 A→B"
                className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10"
              >
                清除途经
              </button>
            </div>
          ) : null}
          <div className="mt-1 flex items-center gap-1 px-1">
            <select
              value={moverId ?? ""}
              onChange={(event) => { stopAnimation(); setMoverId(event.target.value || null); setMoveAnimationIndex(-1); }}
              title="谁沿 A→B 移动"
              className="min-w-0 flex-1 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            >
              <option value="">不移动</option>
              {objects.map((entry) => (
                <option key={entry.id} value={entry.id}>
                  {entry.name}
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={() => setMoverId(selectedId)}
              disabled={!selectedId}
              title="用左栏选中的对象当移动目标"
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10 disabled:opacity-40"
            >
              用选中
            </button>
          </div>
          <label className="mt-1 block px-1 text-[11px] text-white/50">移动动作（预览／参考帧导出）
            <select aria-label="路径移动动作" value={moveAnimationIndex} onChange={event => { stopAnimation(); setMoveAnimationIndex(Number(event.target.value)); }} className="mt-1 w-full rounded bg-[#222] p-1 text-xs">
              <option value={-1}>不播放动作（仅位移）</option>
              {moverAnimationClips.map((clip, index) => <option key={index} value={index}>{clip.name || `片段 ${index + 1}`} · 原速循环</option>)}
            </select>
          </label>
          <p className="px-1 text-[10px] text-white/40">动作与位移共用时间；优先用原地走跑片段，不自动匹配步幅，可能滑步。结束或停止恢复骨姿，保留当前位置；撤销可还原位移。</p>
          <div className="mt-1 flex items-center gap-1 px-1">
            <span className="text-[11px] text-white/40">走完用</span>
            <input
              type="number"
              min={MOVE_SECONDS_LIMITS.min}
              max={MOVE_SECONDS_LIMITS.max}
              step={0.5}
              value={moveSeconds}
              onChange={(event) => setMoveSeconds(normalizeMoveSeconds(Number(event.target.value)))}
              title="走完全程用几秒"
              className="w-12 rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            />
            <span className="text-[11px] text-white/40">秒</span>
            <button
              type="button"
              onClick={previewMove}
              disabled={!toGroundPath(groundPoints) || !moverId || playingMove || playingPath || Boolean(busy)}
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10 disabled:opacity-40"
            >
              {playingMove ? "移动中…" : "预览"}
            </button>
            <button type="button" disabled={!playingMove} onClick={stopAnimation} className="rounded bg-white/5 px-1.5 py-0.5 text-[11px] disabled:opacity-40">停止移动</button>
          </div>
          <label className="mt-1 flex items-center gap-1.5 px-1 text-[11px] text-white/50">
            <input
              type="checkbox"
              checked={followMover}
              onChange={(event) => setFollowMover(event.target.checked)}
              disabled={!toGroundPath(groundPoints) || !moverId}
              title="跟拍：相机整体随角色平移，构图不变，角色始终停在画框里的同一位置"
            />
            跟拍（相机跟着角色走）
          </label>
          <label className="mt-1 flex items-center gap-1.5 px-1 text-[11px] text-white/50">
            <input
              type="checkbox"
              checked={avoidObstacles}
              onChange={(event) => setAvoidObstacles(event.target.checked)}
              disabled={!toGroundPath(groundPoints)}
              title="自动避障：路径撞上模型就在地面上绕过去（自动插入的拐点不进存档，挪模型会重算）"
            />
            自动避障（撞上模型就绕开）
          </label>
          <p className="mt-1 px-1 text-[11px] leading-4 text-white/35">
            {placingGround
              ? `在地面上点一下，记下${
                  placingGround === "start"
                    ? " A（绿，起点）"
                    : placingGround === "end"
                      ? " B（橙，终点）"
                      : `第 ${(groundPoints.vias?.length ?? 0) + 1} 个途经点（蓝，路径会穿过它）`
                }。`
              : `A=起点（绿）、B=终点（橙）、途经点（蓝，最多 ${MAX_GROUND_VIAS} 个，曲线穿过每一个）。选好谁在走，导出序列时角色会沿这条路径移动，相机按机位路径走；勾上「跟拍」相机就跟着角色平移。`}
          </p>
        </section>

        <section>
          <div className="mb-1.5 flex items-center justify-between px-1 text-xs uppercase tracking-wide text-white/40">
            <span>参考图导出</span>
            <span className="text-white/30">
              {referenceShot ? `${referenceShot.width}×${referenceShot.height}` : "白模 / 法线 / 深度"}
            </span>
          </div>
          <div className="flex gap-1">
            {(Object.keys(REFERENCE_PASS_LABELS) as ReferencePass[]).map((pass) => (
              <button
                key={pass}
                type="button"
                onClick={() => void exportReference(pass)}
                disabled={Boolean(busy)}
                className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10 disabled:opacity-40"
              >
                {REFERENCE_PASS_LABELS[pass]}
              </button>
            ))}
          </div>
          <div className="mt-1 flex items-center gap-1 px-1">
            <span className="text-[11px] text-white/40">序列</span>
            <select
              value={sequenceFps}
              onChange={(event) => setSequenceFps(Number(event.target.value) || DEFAULT_SEQUENCE_FPS)}
              className="rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            >
              {SEQUENCE_FPS_OPTIONS.map((value) => (
                <option key={value} value={value}>
                  {value} fps
                </option>
              ))}
            </select>
            <select
              value={sequenceWidth}
              onChange={(event) => setSequenceWidth(Number(event.target.value) || 0)}
              title="序列导出的画面宽度：原尺寸最大也最清楚，小一点体积小很多"
              className="rounded border border-white/10 bg-black/30 px-1 py-0.5 text-[11px] text-white/70"
            >
              {SEQUENCE_WIDTH_OPTIONS.map((value) => (
                <option key={value} value={value}>
                  {SEQUENCE_WIDTH_LABELS[value]}
                </option>
              ))}
            </select>
            <span className="text-[11px] text-white/35">
              {cameraKeys.length < 2
                ? "先记录两个机位"
                : `${
                    sequencePlan(
                      cameraKeys,
                      moverId && toGroundPath(groundPoints) ? normalizeMoveSeconds(moveSeconds) : null,
                      sequenceFps,
                    ).frames
                  } 帧`}
            </span>
            {(["clay", "normal", "depth"] as ReferencePass[]).map((pass) => (
              <button
                key={pass}
                type="button"
                onClick={() => void exportSequence(pass)}
                disabled={Boolean(busy) || cameraKeys.length < 2}
                title={`沿相机路径导出${REFERENCE_PASS_LABELS[pass]}序列（zip）`}
                className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10 disabled:opacity-40"
              >
                {REFERENCE_PASS_LABELS[pass]}
              </button>
            ))}
            <button
              type="button"
              onClick={() => void saveSequenceToLibrary()}
              disabled={Boolean(busy) || cameraKeys.length < 2}
              title="把最近一次导出的序列逐帧存进素材库（一次最多 60 帧）"
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-emerald-200/80 hover:bg-white/10 disabled:opacity-40"
            >
              序列存库
            </button>
            <button
              type="button"
              onClick={() => void composeSequenceVideo()}
              disabled={Boolean(busy) || cameraKeys.length < 2}
              title="把这些帧合成 mp4（后端 ffmpeg），H3 视频参考要视频；先把序列存库"
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-sky-200/80 hover:bg-white/10 disabled:opacity-40"
            >
              合成视频
            </button>
          </div>
          {referenceShot ? (
            <div className="mt-1.5 space-y-1">
              {/* 点开原图：面板里只有 256px 宽，细节（比如深度灰度、法线朝向）必须放大看。 */}
              <a
                href={referenceShot.url}
                target="_blank"
                rel="noreferrer"
                title="在新标签打开原图"
                className="block"
              >
                <img
                  src={referenceShot.url}
                  alt={`${REFERENCE_PASS_LABELS[referenceShot.pass]}参考图`}
                  className="w-full rounded border border-white/10"
                />
              </a>
              <div className="flex items-center justify-between px-1 text-[11px] text-white/40">
                <span>
                  {REFERENCE_PASS_LABELS[referenceShot.pass]} ·{" "}
                  {Math.max(1, Math.round(referenceShot.bytes / 1024))} KB
                </span>
                <a
                  href={referenceShot.url}
                  download={`${referenceShot.pass}-reference.png`}
                  className="text-sky-300 hover:text-sky-200"
                >
                  下载
                </a>
                <button
                  type="button"
                  onClick={() => void saveReferenceToLibrary()}
                  disabled={Boolean(busy)}
                  title="把这张参考图（白模/法线/深度）存进素材库，供视频生成当参考底图"
                  className="text-emerald-300 hover:text-emerald-200 disabled:opacity-40"
                >
                  存素材库
                </button>
              </div>
            </div>
          ) : (
            <p className="px-1 text-[11px] leading-4 text-white/35">
              按当前机位渲一张参考图给视频生成当控制图：白模看结构、法线看朝向、深度看前后关系（近黑远白）。
            </p>
          )}
        </section>
      </div>

      {status && !busy && (
        <div className="fixed bottom-6 left-1/2 z-30 -translate-x-1/2 rounded-md border border-white/10 bg-[#101014]/90 px-3 py-1.5 text-xs text-white/60">
          {status}
        </div>
      )}
      {error && (
        <div className="fixed bottom-16 left-1/2 z-50 -translate-x-1/2 rounded-md border border-red-400/40 bg-red-950/90 px-4 py-2 text-sm text-red-100">
          {error}
        </div>
      )}
      {busy && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="rounded-lg border border-white/10 bg-[#101014] px-6 py-4 text-sm">
            {busy}
          </div>
        </div>
      )}
    </div>
  );
}