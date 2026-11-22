// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type DragEvent,
} from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { TransformControls } from "three/examples/jsm/controls/TransformControls.js";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { GLTFExporter } from "three/examples/jsm/exporters/GLTFExporter.js";
import { OBJLoader } from "three/examples/jsm/loaders/OBJLoader.js";
import { findTemplateId } from "../host/template-cache.js";
import { FKHelper } from "./posing/FKHelper.js";
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
  GROUND_EXTENT,
  clampGroundPoint,
  groundPathFollowDelta,
  groundPathLength,
  groundPathPointAt,
  groundPathYaw,
  MAX_GROUND_VIAS,
  moveGroundPoint,
  normalizeGroundPath,
  routeAroundObstacles,
  toGroundPath,
  type GroundObstacle,
  type GroundPath,
  type GroundPoint,
  type GroundPoints,
  type GroundSlot,
} from "./ground-path.js";
import {
  cameraFrameFileName,
  firstLastCameraFrames,
  type MovableCamera,
} from "./camera-frames.js";
import AssetLibraryPanel from "./AssetLibraryPanel";
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
  id: string;
  name: string;
  object: THREE.Object3D;
  /** 骨骼摆姿快照；有骨骼的角色才有，随场景存档一起保存。 */
  bonePose?: BonePoseNode | null;
  /** 模型刚载入时的绑定姿势，用于「恢复初始姿势」。 */
  initialBonePose?: BonePoseNode | null;
  /** 图片/视频平面层：记录媒体类型，导出时跳过视频（glTF 内嵌不了播放中的视频帧）。 */
  mediaKind?: MediaKind;
  /** 视频平面自己的 <video> 与纹理，删除对象和卸载时要停掉并释放。 */
  video?: { element: HTMLVideoElement; texture: THREE.VideoTexture } | null;
  /** 高斯泼溅对象：Spark 有独立缓冲，要调它自己的 dispose，不走几何释放。 */
  splat?: THREE.Object3D | null;
  /** 泼溅的来源地址（/api/output 或 /api/input）：场景存档要把字节内嵌，得先取回来。 */
  splatUrl?: string;
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
    if (elapsed < cursor + item.seconds) break;
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
  return {
    seconds,
    frames: Math.min(SEQUENCE_MAX_FRAMES, Math.max(1, Math.round(seconds * fps))),
  };
}

/**
 * 参考图专用材质。三种都要盖到场景里所有网格上，渲完必须无条件还原（外面 finally 保证）。
 * 深度用独立的线性深度 shader：近黑远白。
 * 深度范围不能直接用 camera.near/far（0.1–1000），那样几米内的模型只占 1% 灰度，
 * 看上去就是一整张黑图；要按模型自身的包围盒算一段贴身区间。
 */
function createReferenceMaterial(
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
      varying float vViewDepth;
      void main() {
        vec4 viewPosition = modelViewMatrix * vec4(position, 1.0);
        vViewDepth = -viewPosition.z;
        gl_Position = projectionMatrix * viewPosition;
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
  name: string;
  /** .spz/.splat/.ksplat 原始字节的 base64。 */
  data: string;
  position: [number, number, number];
  quaternion: [number, number, number, number];
  scale: [number, number, number];
}

interface SceneDocument {
  version: 1 | 2 | 3;
  createdAt: string;
  lightPreset: LightPresetName;
  camera: { position: [number, number, number]; target: [number, number, number] };
  objects: Array<{
    name: string;
    position: [number, number, number];
    quaternion: [number, number, number, number];
    scale: [number, number, number];
    /** 骨骼摆姿快照；非角色没有这个字段。 */
    bonePose?: BonePoseNode | null;
  }>;
  /** 高斯泼溅层；v1 存档没有这个字段。 */
  splats?: SplatDocument[];
  /** 相机关键帧序列；v3 起入库，v1/v2 存档没有这个字段。 */
  cameraKeys?: CameraKey[];
  /** 地面 A/B 落点（vias 是 0–3 个途经点，走曲线时才写）；老存档没有就按没有处理。 */
  groundPath?: { start: GroundPoint; end: GroundPoint; vias?: GroundPoint[] } | null;
  /** 角色沿 A→B 移动的计划：谁走、走多久、相机要不要跟拍。没有落点时不写。 */
  move?: { targetName: string; seconds: number; follow?: boolean } | null;
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
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState("");
  const [lightPreset, setLightPreset] = useState<LightPresetName>("studio");
  const [material, setMaterial] = useState<MaterialSettings>(DEFAULT_MATERIAL);
  const [cameraKeys, setCameraKeys] = useState<CameraKey[]>([]);
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
  const [moveSeconds, setMoveSeconds] = useState(3);
  const [playingMove, setPlayingMove] = useState(false);
  // 跟拍：播放/导出时相机整体随角色平移，构图不变，只是跟着人走。
  const [followMover, setFollowMover] = useState(false);
  // 自动避障：路径撞上模型就在 XZ 上绕开它（模型一挪就自动重算，不用手点拐点）。
  const [avoidObstacles, setAvoidObstacles] = useState(true);
  const avoidObstaclesRef = useRef(avoidObstacles);
  avoidObstaclesRef.current = avoidObstacles;
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
  const sequenceFramesRef = useRef<{ pass: ReferencePass; stamp: string; blobs: Blob[] } | null>(null);
  const [playingPath, setPlayingPath] = useState(false);
  const [poseSlots, setPoseSlots] = useState<{ a: boolean; b: boolean }>({ a: false, b: false });
  const [poseBlend, setPoseBlend] = useState(0);
  const [transform, setTransform] = useState({ x: 0, y: 0, z: 0, rotateY: 0, scale: 1 });
  // 骨骼摆姿：posing 开启时画布点击用于选骨，主 gizmo 让位给骨骼 gizmo。
  const [posing, setPosing] = useState(false);
  const [posingBones, setPosingBones] = useState(0);
  const [activeBone, setActiveBone] = useState<string | null>(null);
  // 选中对象是否有骨骼（决定「骨骼摆姿」入口是否可用）。
  const [selectedHasSkeleton, setSelectedHasSkeleton] = useState(false);
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
  const [planeSettings, setPlaneSettings] = useState(DEFAULT_PLANE_LAYER);
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
      if (entry.splat) continue;
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
    if (!path || !avoidObstaclesRef.current) return path;
    return routeAroundObstacles(path, collectGroundObstacles());
  };

  const selectObject = useCallback((id: string | null) => {
    selectedIdRef.current = id;
    setSelectedId(id);
    const transform = transformRef.current;
    const entry = objectsRef.current.find((item) => item.id === id);

    // 骨骼摆姿只对当前选中的模型生效：换对象就换 FK 目标，换到没有骨骼的对象则退出摆姿。
    const skeleton = entry ? findSkeletonRoot(entry.object) !== null : false;
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
      transform.attach(entry.object);
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
      normalizeObject(object);
      const group = new THREE.Group();
      group.name = name;
      group.add(object);
      // 拖进来的资产落在鼠标指到的地面位置，而不是永远堆在原点。
      if (dropPoint) group.position.set(dropPoint.x, 0, dropPoint.z);
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
      objectsRef.current = [...objectsRef.current, entry];
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
      container.clientWidth / Math.max(1, container.clientHeight),
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
    renderer.setSize(container.clientWidth, container.clientHeight);
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
    });
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
      const onGround = hit.raycaster.intersectObject(ground, false)[0];
      const onPlane = onGround
        ? onGround.point
        : hit.raycaster.ray.intersectPlane(
            new THREE.Plane(new THREE.Vector3(0, 1, 0), 0),
            new THREE.Vector3(),
          );
      if (!onPlane) return null;
      return clampGroundPoint([onPlane.x, 0.02, onPlane.z]);
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
    const animate = () => {
      frame = requestAnimationFrame(animate);
      guardFrames += 1;
      const now = performance.now();
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
      renderer.render(scene, camera);
    };
    animate();

    const observer = new ResizeObserver(() => {
      const width = container.clientWidth || 1;
      const height = container.clientHeight || 1;
      camera.aspect = width / height;
      camera.updateProjectionMatrix();
      renderer.setSize(width, height);
    });
    observer.observe(container);

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      renderer.domElement.removeEventListener("pointerdown", onPointerDown);
      renderer.domElement.removeEventListener("pointermove", onPointerMove);
      renderer.domElement.removeEventListener("pointerup", onPointerUp);
      renderer.domElement.removeEventListener("pointercancel", onPointerUp);
      fk.dispose();
      fkRef.current = null;
      // 卸载时把视频平面的解码与全景纹理一起收掉，否则离开页面后仍在后台跑。
      objectsRef.current.forEach((entry) => {
        if (entry.splat) disposeSplat(entry.splat);
        if (!entry.video) return;
        entry.video.element.pause();
        entry.video.element.removeAttribute("src");
        entry.video.element.load();
        entry.video.texture.dispose();
      });
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
        const at = groundPathPointAt(path, index / samples);
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
    setError(null);
    setBusy(`正在加载 ${file.name}…`);
    try {
      const extension = file.name.split(".").pop()?.toLowerCase();
      if (extension === "obj") {
        const text = await file.text();
        const parsed = new OBJLoader().parse(text);
        addObject(parsed, file.name);
      } else {
        const buffer = await file.arrayBuffer();
        const gltf = await new GLTFLoader().parseAsync(buffer, "");
        addObject(gltf.scene, file.name);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const loadUrl = async (url: string, name: string) => {
    const gltf = await new GLTFLoader().loadAsync(url);
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
      const mesh = await loadSplatMesh(url, { placeOnGround: false });
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
      setBusy(`正在载入 ${asset.name}…`);
      try {
        const url = assetUrl(asset.path);
        if (kind === "splat") {
          // 泼溅不能走 GLTFLoader（.spz 不是 glTF）；贴地与归一化仍由 addObject 统一做。
          await loadSplatUrl(url, asset.name, dropPoint);
        } else if (kind === "obj") {
          const response = await fetch(url);
          if (!response.ok) throw new Error(`读取素材失败（HTTP ${response.status}）`);
          addObject(new OBJLoader().parse(await response.text()), asset.name, dropPoint);
        } else {
          const gltf = await new GLTFLoader().loadAsync(url);
          addObject(gltf.scene, asset.name, dropPoint);
        }
        setStatus(dropPoint ? `已放入 ${asset.name}` : `已放入 ${asset.name}（原点）`);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
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
    const point = new THREE.Vector3();
    return raycaster.ray.intersectPlane(new THREE.Plane(new THREE.Vector3(0, 1, 0), 0), point)
      ? point
      : null;
  }, []);

  // 图片/视频平面层：当背景板或参考板用，参与 3D 遮挡。
  const addPlane = useCallback(
    async (url: string, name: string, kind: MediaKind, dropPoint?: THREE.Vector3 | null) => {
      const scene = sceneRef.current;
      if (!scene) return;
      setBusy(`正在载入 ${name}…`);
      setError(null);
      try {
        let texture: THREE.Texture;
        let aspect = DEFAULT_PLANE_ASPECT;
        let video: SceneObjectEntry["video"] = null;

        if (kind === "video") {
          const element = document.createElement("video");
          element.src = url;
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
          texture = await new THREE.TextureLoader().loadAsync(url);
          texture.colorSpace = THREE.SRGBColorSpace;
          const image = texture.image as { naturalWidth?: number; naturalHeight?: number; width?: number; height?: number };
          const width = image.naturalWidth ?? image.width ?? 1;
          const height = image.naturalHeight ?? image.height ?? 1;
          aspect = width / Math.max(1, height);
        }

        const { width, height } = planeSize(aspect);
        const opacity = clampOpacity(planeSettings.opacity);
        // 图片已经解码进纹理，blob URL 可以立刻释放；视频要等删除对象时再收。
        if (kind === "image" && url.startsWith("blob:")) URL.revokeObjectURL(url);
        const material = new THREE.MeshBasicMaterial({
          map: texture,
          side: planeSettings.doubleSided ? THREE.DoubleSide : THREE.FrontSide,
          alphaTest: planeSettings.alphaTest,
          transparent: opacity < 1,
          opacity,
        });
        const mesh = new THREE.Mesh(new THREE.PlaneGeometry(width, height), material);
        mesh.name = name;
        const group = new THREE.Group();
        group.name = name;
        group.add(mesh);
        // 默认立在原点前方 3 米，拖进来则落在鼠标位置、底边贴地。
        if (dropPoint) group.position.set(dropPoint.x, height / 2, dropPoint.z);
        else group.position.set(0, height / 2, -3);
        scene.add(group);

        const entry: SceneObjectEntry = {
          id: crypto.randomUUID(),
          name,
          object: group,
          mediaKind: kind,
          video,
        };
        objectsRef.current = [...objectsRef.current, entry];
        syncObjects();
        selectObject(entry.id);
        setStatus(`已加入${kind === "video" ? "视频" : "图片"}平面 ${name}`);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setBusy(null);
      }
    },
    [planeSettings, selectObject, syncObjects],
  );

  // 只对选中的平面层生效：透明度、单双面、透明裁切。
  const applyPlaneSettings = useCallback(
    (patch: Partial<typeof DEFAULT_PLANE_LAYER>) => {
      const next = { ...planeSettings, ...patch };
      setPlaneSettings(next);
      const id = selectedIdRef.current;
      const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
      if (!entry?.mediaKind) return;
      const opacity = clampOpacity(next.opacity);
      entry.object.traverse((child) => {
        const mesh = child as THREE.Mesh;
        const material = mesh.material as THREE.MeshBasicMaterial | undefined;
        if (!mesh.isMesh || !material || !("map" in material)) return;
        material.opacity = opacity;
        material.transparent = opacity < 1;
        material.side = next.doubleSided ? THREE.DoubleSide : THREE.FrontSide;
        material.alphaTest = next.alphaTest;
        material.needsUpdate = true;
      });
    },
    [planeSettings],
  );

  // 等距柱状全景图当背景与环境光照。
  const applySkybox = useCallback(
    async (url: string, name: string) => {
      const scene = sceneRef.current;
      if (!scene) return;
      setBusy(`正在载入全景图 ${name}…`);
      setError(null);
      try {
        const texture = await new THREE.TextureLoader().loadAsync(url);
        texture.mapping = THREE.EquirectangularReflectionMapping;
        texture.colorSpace = THREE.SRGBColorSpace;
        skyboxTextureRef.current?.dispose();
        skyboxTextureRef.current = texture;
        scene.background = texture;
        scene.backgroundIntensity = skybox.intensity;
        if (skybox.asEnvironment) scene.environment = texture;
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
    // 背景回到当前灯光预设的底色。
    scene.background = new THREE.Color(LIGHT_PRESETS[lightPreset].background);
    scene.backgroundIntensity = 1;
    setSkybox((current) => ({ ...current, enabled: false }));
    setSkyboxName(null);
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
    setBusy("正在上传参考图…");
    setError(null);
    setStatus("");
    try {
      const upload = await uploadImage(file, `scene-ref-${Date.now()}.png`);
      const task = await createImageTo3DTask(upload);
      setBusy("正在生成 3D 模型，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      const url = taskResultUrl(finished);
      if (!url) throw new Error("任务没有 GLB 输出");
      setBusy("正在加载生成结果…");
      await loadUrl(url, `hunyuan3d-${task.task_id}.glb`);
      setStatus("3D 生成完成");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  // 图生 3D 世界：参考图 → TripoSplat → .spz 泼溅，直接放进当前场景。
  const runImageToSplat = async (file: File) => {
    setBusy("正在上传参考图…");
    setError(null);
    setStatus("");
    try {
      const upload = await uploadImage(file, `splat-ref-${Date.now()}.png`);
      const task = await createImageToSplatTask(upload, splatMatteEngine);
      setBusy("正在生成高斯泼溅，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      const url = taskResultUrl(finished);
      if (!url) throw new Error("任务没有泼溅输出");
      setBusy("正在解码泼溅…");
      await loadSplatUrl(url, `splat-${task.task_id}.spz`);
      setStatus("3D 世界生成完成");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  // 身份迁移：当前 3D 画面当 image1（定机位），角色主形象当 image2（定脸与服装），
  // 出来的一张图直接进场景当参考板，方便继续对着它摆位。
  const runIdentityTransfer = async (character: File) => {
    const renderer = rendererRef.current;
    if (!renderer) return;
    if (!identityPrompt.trim()) {
      setError("请先写清楚要生成什么，用 <image1> 指 3D 快照、<image2> 指角色主形象");
      return;
    }
    setBusy("正在上传 3D 快照与角色主形象…");
    setError(null);
    setStatus("");
    try {
      const snapshotBlob = await (await fetch(renderer.domElement.toDataURL("image/png"))).blob();
      const snapshot = await uploadImage(snapshotBlob, `scene-snapshot-${Date.now()}.png`);
      const characterUpload = await uploadImage(character, `character-${Date.now()}.png`);
      const task = await createIdentityTransferTask({
        snapshot,
        character: characterUpload,
        prompt: identityPrompt,
      });
      setBusy("正在生成身份迁移图，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      const url = taskResultUrl(finished);
      if (!url) throw new Error("任务没有图片输出");
      setBusy("正在把结果放进场景…");
      await addPlane(url, `identity-${task.task_id}.png`, "image");
      setStatus("身份迁移完成，已放进场景当参考板");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const removeSelected = () => {
    const id = selectedIdRef.current;
    if (!id) return;
    const entry = objectsRef.current.find((item) => item.id === id);
    if (entry) {
      sceneRef.current?.remove(entry.object);
      // 泼溅有自己的缓冲，先走 Spark 的 dispose；其余按几何/材质释放。
      if (entry.splat) {
        disposeSplat(entry.splat);
      }
      entry.object.traverse((child) => {
        const mesh = child as THREE.Mesh;
        mesh.geometry?.dispose();
        const material = mesh.material;
        if (Array.isArray(material)) material.forEach((item) => item.dispose());
        else material?.dispose();
      });
      // 视频平面要停掉播放并释放纹理，否则删掉后仍在后台解码。
      if (entry.video) {
        entry.video.element.pause();
        const src = entry.video.element.currentSrc || entry.video.element.src;
        entry.video.element.removeAttribute("src");
        entry.video.element.load();
        entry.video.texture.dispose();
        // 本地导入的视频用的是 blob URL，删掉对象时一并释放。
        if (src.startsWith("blob:")) URL.revokeObjectURL(src);
      }
      objectsRef.current = objectsRef.current.filter((item) => item.id !== id);
      syncObjects();
      selectObject(null);
    }
  };

  const downloadScreenshot = () => {
    const renderer = rendererRef.current;
    if (!renderer) return;
    const link = document.createElement("a");
    link.download = `scene-${Date.now()}.png`;
    link.href = renderer.domElement.toDataURL("image/png");
    link.click();
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
    // 跟拍得知道“谁在走”与“往哪走”；没有角色或路径就只放相机。
    const followPath = followMover ? editorGroundPath(groundPoints) : null;
    const followMoverEntry =
      followPath && moverId ? objectsRef.current.find((entry) => entry.id === moverId) ?? null : null;
    const followSeconds = followPath && followMoverEntry ? normalizeMoveSeconds(moveSeconds) : null;
    // 播完把角色放回原处，否则看一次预览角色就跑到 B 点了。
    const savedMover = followMoverEntry
      ? {
          position: followMoverEntry.object.position.clone(),
          rotationY: followMoverEntry.object.rotation.y,
        }
      : null;
    // 收尾只走这一条：清 rAF、清兜底定时器、放开按钮。
    const finish = () => {
      if (cameraPathRef.current !== null) {
        cancelAnimationFrame(cameraPathRef.current);
        cameraPathRef.current = null;
      }
      if (cameraPathTimerRef.current !== null) {
        window.clearTimeout(cameraPathTimerRef.current);
        cameraPathTimerRef.current = null;
      }
      if (followMoverEntry && savedMover) {
        followMoverEntry.object.position.copy(savedMover.position);
        followMoverEntry.object.rotation.y = savedMover.rotationY;
      }
      setPlayingPath(false);
    };
    setPlayingPath(true);
    // 大兜底：rAF 被节流或挂起时也保证按钮不会永久卡在「播放中…」。
    if (cameraPathTimerRef.current !== null) window.clearTimeout(cameraPathTimerRef.current);
    cameraPathTimerRef.current = window.setTimeout(finish, total * 1000 + 80);
    const step = () => {
      const elapsed = (performance.now() - started) / 1000;
      // 找到当前落在哪一段（超过总时长时停在最后一段的末尾）。
      const pose = sampleCameraPath(cameraKeys, elapsed);
      if (pose) {
        // 跟拍：角色按同一时钟走 A→B，相机整体加上它的位移（构图不变）。
        let delta: [number, number, number] = [0, 0, 0];
        if (followPath && followMoverEntry && followSeconds) {
          const progress = Math.min(1, elapsed / followSeconds);
          followMoverEntry.object.position.set(...groundPathPointAt(followPath, progress));
          followMoverEntry.object.rotation.y = groundPathYaw(followPath, progress);
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
  }, [cameraKeys, followMover, groundPoints, moveSeconds, moverId]);

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

  const setTransformValue = useCallback((axis: "x" | "y" | "z" | "scale" | "rotateY", value: number) => {
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry) return;
    const object = entry.object;
    if (axis === "scale") {
      object.scale.setScalar(Math.max(0.05, value));
    } else if (axis === "rotateY") {
      object.rotation.y = (value * Math.PI) / 180;
    } else {
      object.position[axis] = value;
    }
    setTransform({
      x: object.position.x,
      y: object.position.y,
      z: object.position.z,
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
    if (!entry || !poses?.a || !poses?.b) return;
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
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    const fk = fkRef.current;
    if (!entry || !fk) return;
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
    setStatus("已保存当前骨骼姿势");
  }, [syncObjects]);

  // 还原到上次保存的骨姿（载入场景后或摆乱之后回来）。
  const restoreBonePose = useCallback(() => {
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry) return;
    if (!applyBonePose(entry.object, entry.bonePose)) {
      setError("没有可还原的骨骼姿势");
      return;
    }
    fkRef.current?.refresh();
    setStatus("已还原到保存的骨骼姿势");
  }, []);

  // 恢复模型载入时的绑定姿势。
  const resetBonePose = useCallback(() => {
    const id = selectedIdRef.current;
    const entry = id ? objectsRef.current.find((item) => item.id === id) : null;
    if (!entry) return;
    if (!applyBonePose(entry.object, entry.initialBonePose)) {
      setError("没有可恢复的初始骨骼姿势");
      return;
    }
    fkRef.current?.refresh();
    setStatus("已恢复初始姿势");
  }, []);

  // 场景存档与 GLB 导出共用同一份几何导出逻辑：只打包用户模型，辅助线与灯光不落盘。
  const exportGeometry = useCallback(async (options: { flattenTransforms?: boolean } = {}) => {
    const root = new THREE.Group();
    objectsRef.current.forEach((entry) => {
      // 视频平面不进导出：glTF 内嵌不了正在播放的视频帧，硬塞会得到一个静止首帧。
      if (!isExportablePlane(entry.mediaKind ?? "image")) return;
      // 高斯泼溅也进不了 glTF：Spark 的 SplatMesh 不是普通几何体，场景存档另有 splats 字段。
      if (entry.splat) return;
      const clone = entry.object.clone(true);
      if (options.flattenTransforms) {
        // 场景存档里变换单独记录，几何体按局部坐标导出，避免节点 TRS 与存档重复叠加。
        clone.position.set(0, 0, 0);
        clone.quaternion.identity();
        clone.scale.set(1, 1, 1);
      }
      root.add(clone);
    });
    return new Promise<ArrayBuffer>((resolve, reject) => {
      new GLTFExporter().parse(
        root,
        (result) => resolve(result as ArrayBuffer),
        (err) => reject(err),
        { binary: true },
      );
    });
  }, []);

  const exportGlb = useCallback(async () => {
    setBusy("正在导出 GLB…");
    try {
      // 泼溅进不了 glTF，纯泼溅场景导出会得到一个空 GLB，不如直接说明。
      if (!objectsRef.current.some((entry) => !entry.splat)) {
        throw new Error("场景里只有泼溅，导出不了 GLB；请先存成场景存档");
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
        entry.object.updateMatrixWorld(true);
        if (entry.splat) {
          box.union(entry.splat.getBoundingBox(false).applyMatrix4(entry.object.matrixWorld));
        } else {
          box.union(new THREE.Box3().setFromObject(entry.object));
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

  const saveScene = useCallback(async () => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (objectsRef.current.length === 0 || !camera || !controls) return;
    setBusy("正在保存场景…");
    try {
      const binary = await exportGeometry({ flattenTransforms: true });
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
          name: entry.name,
          data: arrayBufferToBase64(await response.arrayBuffer()),
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
          ? { targetName: moverEntry.name, seconds: normalizeMoveSeconds(moveSeconds), follow: followMover }
          : null;
      const doc: SceneDocument = {
        version: 3,
        createdAt: new Date().toISOString(),
        lightPreset,
        camera: {
          position: camera.position.toArray() as [number, number, number],
          target: controls.target.toArray() as [number, number, number],
        },
        objects: objectsRef.current
          // 泼溅的变换记在 splats 里，不能混进 GLB 节点名单，否则载入时按名回填会错位。
          .filter((entry) => !entry.splat)
          .map((entry) => ({
            name: entry.name,
            position: entry.object.position.toArray() as [number, number, number],
            quaternion: entry.object.quaternion.toArray() as [number, number, number, number],
            scale: entry.object.scale.toArray() as [number, number, number],
            // 骨骼姿势只影响骨节点，导出几何仍是绑定姿势，靠这里回填。
            bonePose: entry.bonePose ?? null,
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
        model: {
          name: objectsRef.current.map((entry) => entry.name).join("+") || "scene",
          data: arrayBufferToBase64(binary),
        },
      };
      const json = JSON.stringify(doc);
      const url = URL.createObjectURL(new Blob([json], { type: "application/json" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = `scene-${Date.now()}.json`;
      link.click();
      URL.revokeObjectURL(url);
      setStatus(`已保存场景 ${Math.max(1, Math.round(json.length / 1024))} KB`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [cameraKeys, exportGeometry, groundPoints, lightPreset, moveSeconds, moverId]);

  const loadScene = useCallback(
    async (file: File) => {
      setBusy("正在载入场景…");
      setError(null);
      try {
        const doc = JSON.parse(await file.text()) as SceneDocument;
        // v1 是没泼溅层的老存档、v2 多了泼溅层，都仍然要能读；v3 多了机位序列。
        if ((doc.version !== 1 && doc.version !== 2 && doc.version !== 3) || !doc.model?.data) {
          throw new Error("不是可识别的场景文件");
        }
        const scene = sceneRef.current;
        objectsRef.current.forEach((entry) => {
          scene?.remove(entry.object);
          // 泼溅有自己的 GPU 缓冲，移除后要显式释放。
          if (entry.splat) disposeSplat(entry.splat);
        });
        objectsRef.current = [];
        syncObjects();
        selectObject(null);
        const gltf = await new GLTFLoader().parseAsync(base64ToArrayBuffer(doc.model.data), "");
        // 导出时每个对象都是带自身变换的顶层节点，直接接管为场景对象，
        // 不再套一层空 Group，否则面板读到的是那层空壳的 0 变换。
        const imported = gltf.scene.children.slice();
        gltf.scene.clear();
        const entries: SceneObjectEntry[] = imported.map((child, index) => {
          const saved = doc.objects?.find((item) => item.name === child.name) ?? doc.objects?.[index];
          if (saved) {
            child.position.fromArray(saved.position);
            child.quaternion.fromArray(saved.quaternion);
            child.scale.fromArray(saved.scale);
          }
          const entry: SceneObjectEntry = {
            id: crypto.randomUUID(),
            name: saved?.name || child.name || doc.model.name,
            object: child,
          };
          // 先在绑定姿势上记一份初始快照，再把存档里的摆姿套回去。
          const bindPose = serializeBonePose(child);
          if (bindPose) {
            entry.initialBonePose = bindPose;
            entry.bonePose = saved?.bonePose ?? bindPose;
            if (saved?.bonePose) applyBonePose(child, saved.bonePose);
          }
          return entry;
        });
        entries.forEach((entry) => sceneRef.current?.add(entry.object));
        // 泼溅层：存档里自带 .spz 字节，解成 blob URL 重新交给 Spark，
        // 这样存档不依赖 ComfyUI output 或素材库文件还在不在。
        const splatEntries: SceneObjectEntry[] = [];
        for (const saved of doc.splats ?? []) {
          const blob = new Blob([base64ToArrayBuffer(saved.data)], { type: "application/octet-stream" });
          const url = URL.createObjectURL(blob);
          const mesh = await loadSplatMesh(url, { placeOnGround: false });
          // 和其他对象一样包一层 Group：归一化与用户摆位都记在这层上。
          const group = new THREE.Group();
          group.name = saved.name;
          group.add(mesh);
          group.position.fromArray(saved.position);
          group.quaternion.fromArray(saved.quaternion);
          group.scale.fromArray(saved.scale);
          sceneRef.current?.add(group);
          splatEntries.push({
            id: crypto.randomUUID(),
            name: saved.name,
            object: group,
            splat: mesh,
            splatUrl: url,
          });
        }
        objectsRef.current = [...entries, ...splatEntries];
        // 载入的存档带泼溅：同样要等 LoD 树建完才能画，挂上提示等 onDirty 收。
        if (splatEntries.length > 0) beginSplatLodWait();
        syncObjects();
        selectObject(entries[0]?.id ?? null);
        // 地面路径与移动目标（v3 可选字段）：老存档没有就清空，别把上一个场景的带走。
        // 这两行必须先取出来：下面「按名字找移动目标」要用，写在后面会撞上暂时性死区。
        const savedPath = normalizeGroundPath(doc.groundPath);
        const savedMove = doc.move && typeof doc.move.targetName === "string" ? doc.move : null;
        // 移动目标按名字找：载入后对象 id 是新的，只有名字能对上。
        const loadedMover = savedMove
          ? objectsRef.current.find((entry) => entry.name === savedMove.targetName) ?? null
          : null;
        setMoverId(loadedMover?.id ?? null);
        setMoveSeconds(normalizeMoveSeconds(savedMove?.seconds));
        setFollowMover(Boolean(savedMove?.follow));
        applyLightPreset(doc.lightPreset);
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
          controls.update();
        }
        setStatus(
          splatEntries.length > 0
            ? `已载入场景（${entries.length} 个对象 + ${splatEntries.length} 个泼溅）`
            : `已载入场景（${entries.length} 个对象）`,
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        // 带泼溅的存档把 busy 交给 SparkRenderer.onDirty 收尾。
        setBusy((current) => (current === SPLAT_LOD_BUSY ? current : null));
      }
    },
    [applyLightPreset, beginSplatLodWait, syncObjects],
  );

  // 渲一帧参考图并取回 PNG：藏编辑器痕迹、换材质、黑底、取图、还原都在这里。
  // 单帧导出和序列导出共用同一条路径，免得两条路各修一遍。
  const captureReference = useCallback(async (pass: ReferencePass) => {
    const renderer = rendererRef.current;
    const scene = sceneRef.current;
    const camera = cameraRef.current as THREE.PerspectiveCamera | null;
    if (!renderer || !scene || !camera) return null;
    const previousOverride = scene.overrideMaterial;
    const previousBackground = scene.background;
    const hidden: THREE.Object3D[] = [];
    // 逐个网格换材质：scene.overrideMaterial 会把模型整块画没了（只留黑底），
    // 逐网格替换才能稳定出图。
    const swapped: Array<{ mesh: THREE.Mesh; material: THREE.Material | THREE.Material[] }> = [];
    // 深度范围按模型自己的包围盒算：直接用 camera.near/far（0.1–1000）时，
    // 几米内的模型只占 1% 灰度，看上去就是一整张黑图。
    const bounds = new THREE.Box3();
    objectsRef.current.forEach((entry) => bounds.expandByObject(entry.object));
    const depthRange = { near: camera.near, far: camera.far };
    if (!bounds.isEmpty()) {
      const center = bounds.getCenter(new THREE.Vector3());
      const radius = Math.max(0.25, bounds.getSize(new THREE.Vector3()).length() / 2);
      const distance = camera.position.distanceTo(center);
      depthRange.near = Math.max(0.01, distance - radius);
      depthRange.far = Math.max(depthRange.near + 0.5, distance + radius);
    }
    const material = createReferenceMaterial(pass, depthRange);
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
      const paint = (object: THREE.Object3D) => {
        const mesh = object as THREE.Mesh;
        if (mesh.isMesh) {
          const geometry = mesh.geometry as THREE.BufferGeometry | undefined;
          // 没存法线的 GLB 很常见；换成 Standard / Normal 材质后光照拿不到法线，
          // 整块会渲成纯黑。先在原几何体上补一份。
          if (geometry && !geometry.getAttribute("normal")) {
            geometry.computeVertexNormals();
          }
          swapped.push({ mesh, material: mesh.material });
          mesh.material = material;
        }
        object.children.forEach(paint);
      };
      objectsRef.current.forEach((entry) => paint(entry.object));
      // 黑色背景：深度图里天空=0，稳定；白模/法线也不想要灰底混进来。
      scene.background = new THREE.Color(0x000000);
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
      const blob = await new Promise<Blob | null>((resolve) => {
        snapshot.toBlob((value) => resolve(value), "image/png");
      });
      if (!blob) throw new Error("取不到参考图（画布为空）");
      return { blob, width, height };
    } finally {
      // 无论如何都要还原，否则编辑器会一直停在参考图材质上。
      scene.overrideMaterial = previousOverride;
      scene.background = previousBackground;
      swapped.forEach((item) => {
        item.mesh.material = item.material;
      });
      hidden.forEach((object) => {
        object.visible = true;
      });
      material.dispose();
    }
  }, []);

  /**
   * 按指定机位渲一帧「视口原样」画面（带材质/灯光/天空盒/泼溅），只藏编辑器痕迹。
   *
   * 和 captureReference 的区别：不换材质、不拉黑背景。首尾帧是交给 H3 的定妆图，
   * 白模那种灰板子本身就是「内容」，会被照着画出来，所以这里必须是真实画面。
   */
  const captureViewportAt = useCallback(async (pose: MovableCamera) => {
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
      const blob = await new Promise<Blob | null>((resolve) => {
        snapshot.toBlob((value) => resolve(value), "image/png");
      });
      if (!blob) throw new Error("取不到画面（画布为空）");
      return { blob, width, height };
    } finally {
      // 无论如何都要把视角还回去，否则用户眼前会突然跳到落幅机位。
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
    }
  }, []);

  /**
   * 导出首尾帧并存进素材库：起幅取第一个机位、落幅取最后一个机位。
   *
   * 存库而不是直接塞进场景：H3 首尾帧模板吃的是素材库里的图片文件名，
   * 用户在「视频」页选完这两张、模板选 minimax_h3_first_last 就能生成。
   */
  const exportFirstLastFrames = useCallback(async () => {
    const pair = firstLastCameraFrames(cameraKeys);
    if (!pair) {
      setError("先记录两个机位（起幅和落幅），再导首尾帧");
      return;
    }
    setBusy("正在渲起幅/落幅两帧…");
    setError(null);
    try {
      const stamp = fileNameStamp(new Date());
      const results: Array<{ edge: "first" | "last"; name: string; size: number }> = [];
      for (const edge of ["first", "last"] as const) {
        const pose = edge === "first" ? pair.first : pair.last;
        const shot = await captureViewportAt(pose);
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
        results.push({
          edge,
          name: material.name ?? cameraFrameFileName(edge, stamp),
          size: Math.max(1, Math.round(shot.blob.size / 1024)),
        });
      }
      setStatus(
        `已渲出首尾帧并存进素材库：起幅 ${results[0].name}（${results[0].size} KB）、落幅 ${results[1].name}（${results[1].size} KB）。去「视频」页把这两张挂成首帧/尾帧，模板选 minimax_h3_first_last。`,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [cameraKeys, captureViewportAt]);

  const exportReference = useCallback(
    async (pass: ReferencePass) => {
      setBusy(`${REFERENCE_BUSY_PREFIX}（${REFERENCE_PASS_LABELS[pass]}）…`);
      setError(null);
      try {
        const shot = await captureReference(pass);
        if (!shot) return;
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
        setBusy((current) => (current && current.startsWith(REFERENCE_BUSY_PREFIX) ? null : current));
      }
    },
    [captureReference],
  );

  /** 沿相机路径逐帧渲参考图，打包成一个 zip 下载。 */
  const exportSequence = useCallback(
    async (pass: ReferencePass) => {
      const camera = cameraRef.current as THREE.PerspectiveCamera | null;
      const controls = controlsRef.current;
      if (!camera || !controls) return;
      if (cameraKeys.length < 2) {
        setError("先记录至少两个机位，序列才有路径可走");
        return;
      }
      const path = editorGroundPath(groundPoints);
      const mover =
        moverId ? objectsRef.current.find((entry) => entry.id === moverId) ?? null : null;
      const moverSeconds = mover && path ? normalizeMoveSeconds(moveSeconds) : null;
      const fps = sequenceFps;
      const plan = sequencePlan(cameraKeys, moverSeconds, fps);
      const frames = plan.frames;
      setError(null);
      setBusy(`${REFERENCE_BUSY_PREFIX}（序列 0/${frames}）…`);
      // 导出会一路推相机，结束后必须放回原处，否则用户回到编辑器发现机位跑了。
      const savedPosition = camera.position.clone();
      const savedTarget = controls.target.clone();
      const savedFov = camera.fov;
      // 分辨率：选了非 0 宽度就把画布临时缩到那个宽度，导完再还原。
      // 视口是 3840 宽时每帧 200–370 KB，1280 宽够当视频参考、体积小一大截。
      const renderer = rendererRef.current;
      const savedSize = renderer ? renderer.getSize(new THREE.Vector2()) : null;
      const savedAspect = camera.aspect;
      if (renderer && sequenceWidth > 0) {
        const height = Math.max(1, Math.round(sequenceWidth / savedAspect));
        renderer.setSize(sequenceWidth, height, false);
        camera.aspect = sequenceWidth / height;
        camera.updateProjectionMatrix();
      }
      // 角色走完停在 B 点，也要还原，否则编辑器里对象被挪走了。
      const savedMover = mover
        ? { position: mover.object.position.clone(), quaternion: mover.object.quaternion.clone() }
        : null;
      const entries: Array<{ name: string; data: Uint8Array }> = [];
      try {
        for (let index = 0; index < frames; index += 1) {
          const sample = sampleCameraPath(cameraKeys, index / fps);
          if (!sample) break;
          camera.position.set(...sample.position);
          controls.target.set(...sample.target);
          controls.update();
          if (Math.abs(camera.fov - sample.fov) > 1e-4) {
            camera.fov = sample.fov;
            camera.updateProjectionMatrix();
          }
          if (mover && path && moverSeconds) {
            // 移动与相机各走各的时钟：按同一帧时间 / 移动总时长取进度，超出就停在 B。
            const progress = Math.min(1, index / fps / moverSeconds);
            mover.object.position.set(...groundPathPointAt(path, progress));
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
          const shot = await captureReference(pass);
          if (!shot) break;
          entries.push({
            name: `${pass}-${String(index).padStart(3, "0")}.png`,
            data: new Uint8Array(await shot.blob.arrayBuffer()),
          });
          setBusy(`${REFERENCE_BUSY_PREFIX}（序列 ${index + 1}/${frames}）…`);
        }
        if (entries.length === 0) throw new Error("没有取到任何帧");
        // 先留一份帧，用户想存素材库时不用再渲一遍。
        sequenceFramesRef.current = {
          pass,
          stamp: fileNameStamp(new Date()),
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
        camera.position.copy(savedPosition);
        controls.target.copy(savedTarget);
        if (Math.abs(camera.fov - savedFov) > 1e-4) {
          camera.fov = savedFov;
          camera.updateProjectionMatrix();
        }
        controls.update();
        if (mover && savedMover) {
          mover.object.position.copy(savedMover.position);
          mover.object.quaternion.copy(savedMover.quaternion);
        }
        // 画布尺寸与投影一起还原，否则编辑器停在导出分辨率上。
        if (renderer && savedSize && sequenceWidth > 0) {
          renderer.setSize(savedSize.x, savedSize.y, false);
          camera.aspect = savedAspect;
          camera.updateProjectionMatrix();
        }
        setBusy((current) => (current && current.startsWith(REFERENCE_BUSY_PREFIX) ? null : current));
      }
    },
    [cameraKeys, captureReference, followMover, groundPoints, moveSeconds, moverId, sequenceFps, sequenceWidth],
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
    setBusy(`正在把 ${cache.blobs.length} 帧存进素材库…`);
    setError(null);
    try {
      let saved = 0;
      for (let index = 0; index < cache.blobs.length; index += 1) {
        const form = new FormData();
        form.append("file", cache.blobs[index], sequenceFrameFileName(cache.pass, cache.stamp, index));
        form.append("type", "image");
        const response = await fetch("/api/materials", { method: "POST", body: form });
        if (!response.ok) {
          const payload = await response.json().catch(() => ({}));
          throw new Error(payload.error || `第 ${index + 1} 帧存库失败（HTTP ${response.status}）`);
        }
        saved += 1;
        setBusy(`正在存序列进素材库（${saved}/${cache.blobs.length}）…`);
      }
      setStatus(
        `已把 ${saved} 帧${REFERENCE_PASS_LABELS[cache.pass]}序列存进素材库（素材库页可直接看）`,
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
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
    setBusy(`正在把 ${cache.blobs.length} 帧合成视频…`);
    setError(null);
    try {
      const response = await fetch("/api/materials/sequence-video", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ pass: cache.pass, stamp: cache.stamp, fps: sequenceFps }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) {
        throw new Error(payload.error || `合成视频失败（HTTP ${response.status}）`);
      }
      setStatus(`已合成 ${payload.name}（${cache.blobs.length} 帧 · ${sequenceFps} fps，在素材库里）`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [sequenceFps]);
  const previewMove = useCallback(() => {
    const path = editorGroundPath(groundPoints);
    const mover = moverId ? objectsRef.current.find((entry) => entry.id === moverId) ?? null : null;
    if (!path || !mover) return;
    const seconds = normalizeMoveSeconds(moveSeconds);
    if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
    setPlayingMove(true);
    const started = performance.now();
    const step = () => {
      const progress = Math.min(1, (performance.now() - started) / (seconds * 1000));
      mover.object.position.set(...groundPathPointAt(path, progress));
      // 朝向行进方向：模型默认面朝 +Z，yaw 直接由方向算。
      mover.object.rotation.y = groundPathYaw(path, progress);
      if (progress < 1) {
        moveRafRef.current = requestAnimationFrame(step);
        return;
      }
      moveRafRef.current = null;
      setPlayingMove(false);
    };
    step();
  }, [groundPoints, moveSeconds, moverId]);

  // 卸载时别留 rAF。
  useEffect(
    () => () => {
      if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
    },
    [],
  );

  const closeAiMenu = useCallback(() => {
    if (aiMenuRef.current) aiMenuRef.current.open = false;
  }, []);

  return (
    <div
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

      <div className="fixed inset-x-0 top-0 z-40 flex h-14 items-center gap-2 border-b border-white/10 bg-[#101014]/95 px-3 backdrop-blur">
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm text-white/80 hover:bg-white/10"
          onClick={() => window.location.assign(exitTo)}
        >
          返回
        </button>
        <span className="mr-2 text-sm font-medium">3D 场景</span>
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
          title="把模型、灯光和机位存成场景文件"
        >
          保存场景
        </button>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
          onClick={() => sceneInputRef.current?.click()}
          disabled={Boolean(busy)}
          title="载入之前保存的场景文件"
        >
          载入场景
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
        <div className="mb-2 px-1 text-xs uppercase tracking-wide text-white/40">
          场景对象
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
        <div className="mt-2 border-t border-white/10 pt-2">
          <div className="mb-2 px-1 text-xs uppercase tracking-wide text-white/40">
            资产库（拖进画面摆位）
          </div>
          <AssetLibraryPanel
            onPlace={(asset) => void loadLibraryAsset(asset)}
            onError={(message) => setError(message)}
          />
        </div>
      </div>

      <div className="fixed right-3 top-20 z-30 max-h-[calc(100vh-6rem)] w-64 space-y-3 overflow-y-auto rounded-lg border border-white/10 bg-[#101014]/90 p-3 text-sm text-white/80 backdrop-blur">
        <section>
          <div className="mb-1.5 px-1 text-xs uppercase tracking-wide text-white/40">灯光</div>
          <div className="grid grid-cols-4 gap-1">
            {(Object.keys(LIGHT_PRESETS) as LightPresetName[]).map((name) => (
              <button
                key={name}
                type="button"
                onClick={() => applyLightPreset(name)}
                className={`rounded-md px-1 py-1 text-xs ${
                  lightPreset === name ? "bg-amber-400/20 text-amber-100" : "bg-white/5 text-white/60 hover:bg-white/10"
                }`}
              >
                {LIGHT_PRESETS[name].label}
              </button>
            ))}
          </div>
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
              <div className="grid grid-cols-2 gap-1">
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
                <p className="text-[10px] leading-3 text-white/30">
                  视频平面只在编辑器里预览，不进 GLB 与场景存档
                </p>
              </div>
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
              <div className="flex gap-1">
                <button
                  type="button"
                  onClick={saveBonePose}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                >
                  保存姿势
                </button>
                <button
                  type="button"
                  onClick={restoreBonePose}
                  className="flex-1 rounded-md bg-white/5 px-2 py-1 text-xs text-white/70 hover:bg-white/10"
                >
                  还原
                </button>
                <button
                  type="button"
                  onClick={resetBonePose}
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
              disabled={cameraKeys.length < 2 || playingPath}
            >
              {playingPath ? "播放中…" : "播放"}
            </button>
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
              onChange={(event) => setMoverId(event.target.value || null)}
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
              disabled={!toGroundPath(groundPoints) || !moverId || playingMove}
              className="rounded-md bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/10 disabled:opacity-40"
            >
              {playingMove ? "移动中…" : "预览"}
            </button>
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