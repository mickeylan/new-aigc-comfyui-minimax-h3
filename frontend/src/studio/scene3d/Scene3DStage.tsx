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
  applyBonePose,
  findSkeletonRoot,
  hasSkeleton,
  serializeBonePose,
  type BonePoseNode,
} from "./posing/bonePose.js";

interface Scene3DStageProps {
  exitTo?: string;
}

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
}

type TransformMode = "translate" | "rotate" | "scale";

type LightPresetName = "studio" | "sunset" | "night" | "rim";

interface MaterialSettings {
  color: string;
  roughness: number;
  metalness: number;
  opacity: number;
}

interface CameraKey {
  id: string;
  position: [number, number, number];
  target: [number, number, number];
}

interface PoseSnapshot {
  position: [number, number, number];
  quaternion: [number, number, number, number];
  scale: [number, number, number];
}

// 场景存档：几何体以 GLB 形式内嵌，灯光、机位与每个对象的变换单独记录；
// 变换不依赖导出器是否保留节点 TRS，载入时按名字（缺名字则按顺序）回填。
interface SceneDocument {
  version: 1;
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
  const cameraPathRef = useRef<number | null>(null);
  const posesRef = useRef<Record<string, { a?: PoseSnapshot; b?: PoseSnapshot }>>({});
  const fileInputRef = useRef<HTMLInputElement>(null);
  const sceneInputRef = useRef<HTMLInputElement>(null);
  const imageInputRef = useRef<HTMLInputElement>(null);
  const imagePlaneInputRef = useRef<HTMLInputElement>(null);
  const videoPlaneInputRef = useRef<HTMLInputElement>(null);
  const skyboxInputRef = useRef<HTMLInputElement>(null);
  const skyboxTextureRef = useRef<THREE.Texture | null>(null);
  const fkRef = useRef<FKHelper | null>(null);
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
  // 3D 合成：全景天空盒与平面层设置。
  const [skybox, setSkybox] = useState<SkyboxSettings>(DEFAULT_SKYBOX);
  const [skyboxName, setSkyboxName] = useState<string | null>(null);
  const [planeSettings, setPlaneSettings] = useState(DEFAULT_PLANE_LAYER);

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
    let pointerStart: { x: number; y: number } | null = null;
    const onPointerDown = (event: PointerEvent) => {
      pointerStart = { x: event.clientX, y: event.clientY };
    };
    const onPointerUp = (event: PointerEvent) => {
      if (!pointerStart || !posingRef.current) {
        pointerStart = null;
        return;
      }
      const moved = Math.hypot(event.clientX - pointerStart.x, event.clientY - pointerStart.y);
      pointerStart = null;
      if (moved > 4) return;
      const rect = renderer.domElement.getBoundingClientRect();
      if (!rect.width || !rect.height) return;
      const ndc = new THREE.Vector2(
        ((event.clientX - rect.left) / rect.width) * 2 - 1,
        -((event.clientY - rect.top) / rect.height) * 2 + 1,
      );
      setActiveBone(fk.handleClick(ndc));
    };
    renderer.domElement.addEventListener("pointerdown", onPointerDown);
    renderer.domElement.addEventListener("pointerup", onPointerUp);

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

    let frame = 0;
    const animate = () => {
      frame = requestAnimationFrame(animate);
      controls.update();
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
      renderer.domElement.removeEventListener("pointerup", onPointerUp);
      fk.dispose();
      fkRef.current = null;
      // 卸载时把视频平面的解码与全景纹理一起收掉，否则离开页面后仍在后台跑。
      objectsRef.current.forEach((entry) => {
        if (!entry.video) return;
        entry.video.element.pause();
        entry.video.element.removeAttribute("src");
        entry.video.element.load();
        entry.video.texture.dispose();
      });
      skyboxTextureRef.current?.dispose();
      skyboxTextureRef.current = null;
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

  // 从素材库取模型放进来：glb/gltf 直接加载，obj 要取文本再解析。
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
        if (kind === "obj") {
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
        setBusy(null);
      }
    },
    [addObject],
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

  const removeSelected = () => {
    const id = selectedIdRef.current;
    if (!id) return;
    const entry = objectsRef.current.find((item) => item.id === id);
    if (entry) {
      sceneRef.current?.remove(entry.object);
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
    };
    setCameraKeys((current) => [...current, key]);
    setStatus(`已记录第 ${cameraKeys.length + 1} 个机位`);
  }, [cameraKeys.length]);

  const playCameraPath = useCallback(() => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls || cameraKeys.length < 2) return;
    if (cameraPathRef.current !== null) cancelAnimationFrame(cameraPathRef.current);
    const segment = 1.6;
    const total = (cameraKeys.length - 1) * segment;
    const started = performance.now();
    setPlayingPath(true);
    const ease = (t: number) => t * t * (3 - 2 * t);
    const step = () => {
      const elapsed = (performance.now() - started) / 1000;
      const index = Math.min(cameraKeys.length - 2, Math.floor(elapsed / segment));
      const local = Math.min(1, Math.max(0, (elapsed - index * segment) / segment));
      const from = cameraKeys[index];
      const to = cameraKeys[index + 1];
      const t = ease(local);
      camera.position.set(
        from.position[0] + (to.position[0] - from.position[0]) * t,
        from.position[1] + (to.position[1] - from.position[1]) * t,
        from.position[2] + (to.position[2] - from.position[2]) * t,
      );
      controls.target.set(
        from.target[0] + (to.target[0] - from.target[0]) * t,
        from.target[1] + (to.target[1] - from.target[1]) * t,
        from.target[2] + (to.target[2] - from.target[2]) * t,
      );
      controls.update();
      if (elapsed >= total) {
        cameraPathRef.current = null;
        setPlayingPath(false);
        return;
      }
      cameraPathRef.current = requestAnimationFrame(step);
    };
    cameraPathRef.current = requestAnimationFrame(step);
  }, [cameraKeys]);

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

  // 3D 资产入库：模型本体进素材库（type=3d，可被预演绑定），预览图取当前渲染帧。
  const saveToLibrary = useCallback(async () => {
    if (objectsRef.current.length === 0) return;
    setBusy("正在存入素材库…");
    setError(null);
    try {
      const binary = await exportGeometry();
      const glb = new Blob([binary], { type: "model/gltf-binary" });
      const renderer = rendererRef.current;
      const thumb = renderer?.domElement.toDataURL("image/png");
      const form = new FormData();
      form.append("file", glb, `scene-${Date.now()}.glb`);
      form.append("type", "3d");
      if (thumb) {
        form.append("thumbnail", await (await fetch(thumb)).blob(), "thumb.png");
      }
      const response = await fetch("/api/materials", { method: "POST", body: form });
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(payload.error || `存入素材库失败（HTTP ${response.status}）`);
      }
      const material = await response.json();
      setStatus(`已存入素材库：${material.name}（约 ${(binary.byteLength / 1024 / 1024).toFixed(1)} MB）`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }, [exportGeometry]);

  const saveScene = useCallback(async () => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (objectsRef.current.length === 0 || !camera || !controls) return;
    setBusy("正在保存场景…");
    try {
      const binary = await exportGeometry({ flattenTransforms: true });
      const doc: SceneDocument = {
        version: 1,
        createdAt: new Date().toISOString(),
        lightPreset,
        camera: {
          position: camera.position.toArray() as [number, number, number],
          target: controls.target.toArray() as [number, number, number],
        },
        objects: objectsRef.current.map((entry) => ({
          name: entry.name,
          position: entry.object.position.toArray() as [number, number, number],
          quaternion: entry.object.quaternion.toArray() as [number, number, number, number],
          scale: entry.object.scale.toArray() as [number, number, number],
          // 骨骼姿势只影响骨节点，导出几何仍是绑定姿势，靠这里回填。
          bonePose: entry.bonePose ?? null,
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
  }, [exportGeometry, lightPreset]);

  const loadScene = useCallback(
    async (file: File) => {
      setBusy("正在载入场景…");
      setError(null);
      try {
        const doc = JSON.parse(await file.text()) as SceneDocument;
        if (doc.version !== 1 || !doc.model?.data) throw new Error("不是可识别的场景文件");
        const scene = sceneRef.current;
        objectsRef.current.forEach((entry) => scene?.remove(entry.object));
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
        objectsRef.current = entries;
        syncObjects();
        selectObject(entries[0]?.id ?? null);
        applyLightPreset(doc.lightPreset);
        const camera = cameraRef.current;
        const controls = controlsRef.current;
        if (camera && controls) {
          camera.position.set(...doc.camera.position);
          controls.target.set(...doc.camera.target);
          controls.update();
        }
        setStatus(`已载入场景（${entries.length} 个对象）`);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setBusy(null);
      }
    },
    [applyLightPreset, syncObjects],
  );

  return (
    <div className="studio-root studio-theme fixed inset-0 z-[100] overflow-hidden bg-[#121316] text-[#f2f1ee]">
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
        <button
          type="button"
          className="rounded-md border border-sky-400/40 px-3 py-1.5 text-sm text-sky-200 hover:bg-sky-400/10 disabled:opacity-50"
          onClick={() => imageInputRef.current?.click()}
          disabled={Boolean(busy)}
        >
          AI 图生 3D
        </button>
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
            <button
              key={entry.id}
              type="button"
              className={`mb-1 flex w-full items-center justify-between rounded-md px-2 py-1.5 text-left text-sm ${
                selectedId === entry.id
                  ? "bg-sky-500/20 text-sky-100"
                  : "text-white/70 hover:bg-white/10"
              }`}
              onClick={() => selectObject(entry.id)}
            >
              <span className="truncate">{entry.name}</span>
            </button>
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

      <div className="fixed right-3 top-20 z-30 w-64 space-y-3 rounded-lg border border-white/10 bg-[#101014]/90 p-3 text-sm text-white/80 backdrop-blur">
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