import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Konva from "konva";
import { PaintSurface, INPAINT_LAYER_ID, BG_LAYER_ID, DRAW_LAYER_ID } from "../pagedraw/PaintSurface";
import { captureDocumentCanvas } from "../pagedraw/utilities/exportDocument";
import SideToolbar from "../pagedraw/components/ui/SideToolbar";
import InpaintToolBar from "../pagedraw/components/ui/InpaintToolBar";
import { useSceneStore } from "../pagedraw/stores/SceneState";
import type { LineNode } from "../pagedraw/stores/SceneState";
import type { Node } from "../pagedraw/Node";
import type { BaseSelectorImage } from "../pagedraw/types";
import { findTemplateId } from "../host/template-cache.js";
import {
  Model3DOverlay,
  type Model3DOverlayHandle,
} from "../pagedraw/components/Model3DOverlay";
import {
  DEFAULT_MODEL3D_PARAMS,
  render3DModelToDataUrl,
  type Model3DParams,
} from "../pagedraw/utilities/render3DModel";
import {
  assetKind,
  assetUrl,
  type LibraryAsset,
} from "../scene3d/assetLibrary.js";

interface PageDrawStageProps {
  initialImageUrl?: string;
  exitTo?: string;
  /** 写回分镜：由宿主注入（改哪个 Scene、写图还是写衔接帧）。缺省时不显示按钮。 */
  writeBack?: (blob: Blob, filename?: string) => Promise<string>;
  writeBackLabel?: string;
}

interface UploadResult {
  task_id: string;
  name: string;
  path: string;
}

const POLL_INTERVAL_MS = 2000;
const TASK_TIMEOUT_MS = 10 * 60 * 1000;

/**
 * 覆盖层上的「拖一条滑杆」动作：按下后左右/上下拖，按位移量驱动相机或模型。
 * 用它而不是让用户直接在 3D 视图里拖，是因为覆盖层下面就是画布的
 * 节点拖拽与选中框，直接拖必然打架。
 */
function ScrubPad({
  label,
  onDrag,
}: {
  label: string;
  onDrag: (dx: number, dy: number) => void;
}) {
  const last = useRef<{ x: number; y: number } | null>(null);
  return (
    <button
      type="button"
      className="cursor-ew-resize rounded-md border border-white/20 bg-[#101014]/90 px-3 py-1.5 text-xs text-white/80 hover:bg-white/10"
      onPointerDown={(event) => {
        last.current = { x: event.clientX, y: event.clientY };
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        const start = last.current;
        if (!start) return;
        const dx = event.clientX - start.x;
        const dy = event.clientY - start.y;
        last.current = { x: event.clientX, y: event.clientY };
        onDrag(dx, dy);
      }}
      onPointerUp={() => {
        last.current = null;
      }}
      onPointerCancel={() => {
        last.current = null;
      }}
    >
      {label}
    </button>
  );
}

/** Krea2 微调版本，值为 ComfyUI 里的 diffusion_models 相对路径。 */
const KREA2_UNETS = [
  { value: "krea2\\Krea2-MuseByStable_v15Turbo_fp8.safetensors", label: "MuseByStable turbo（默认）" },
  { value: "krea2\\Krea2-ultra_v16_int8_convrot.safetensors", label: "ultra v16" },
  { value: "krea2\\Krea2-Moody-Mix-premium_int8_convrot.safetensors", label: "Moody Mix premium" },
  { value: "krea2\\Krea2-kreamania_v3_int8.safetensors", label: "kreamania v3" },
];

async function loadImage(src: string): Promise<HTMLImageElement> {
  const image = new Image();
  image.crossOrigin = "anonymous";
  image.src = src;
  await image.decode();
  return image;
}

async function uploadImage(blob: Blob, name: string): Promise<UploadResult> {
  const form = new FormData();
  form.append("file", blob, name);
  form.append("type", "image");
  const response = await fetch("/api/upload", { method: "POST", body: form });
  if (!response.ok) {
    throw new Error(`上传失败：${response.status} ${await response.text()}`);
  }
  return (await response.json()) as UploadResult;
}

async function createTask(options: {
  code: string;
  prompt?: string;
  params?: Record<string, unknown>;
  files?: Record<string, { task_id: string; name: string }[]>;
}) {
  const templateId = await findTemplateId(options.code);
  const response = await fetch("/api/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      template_id: templateId,
      prompt: options.prompt ?? "",
      params: options.params ?? {},
      files: options.files ?? {},
    }),
  });
  if (!response.ok) {
    throw new Error(`创建任务失败：${response.status} ${await response.text()}`);
  }
  return response.json() as Promise<{ task_id: string; status: string }>;
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

function resultUrl(task: {
  gpu_index?: number;
  instance_id?: number;
  result_files?: string;
}): string | null {
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

export default function PageDrawStage({
  initialImageUrl,
  exitTo = "/studio/draw",
  writeBack,
  writeBackLabel = "写回分镜（另存候选）",
}: PageDrawStageProps) {
  const store = useSceneStore();
  const fillColor = store.fillColor;
  const setFillColor = store.setFillColor;
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [writing, setWriting] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [inpaintModel, setInpaintModel] = useState("image_inpaint");
  const [kreaUnet, setKreaUnet] = useState(KREA2_UNETS[0].value);
  const [sidePanel, setSidePanel] = useState<"layers" | "history">("layers");
  const [cropMode, setCropMode] = useState(false);
  const [cropRect, setCropRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null);
  const cropAnchorRef = useRef<{ x: number; y: number } | null>(null);
  const [negativePrompt, setNegativePrompt] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const layerInputRef = useRef<HTMLInputElement>(null);
  const opacityDirtyRef = useRef(false);
  const commitOpacity = () => {
    if (!opacityDirtyRef.current) return;
    opacityDirtyRef.current = false;
    useSceneStore.getState().saveState();
  };
  const stageRef = useRef<Konva.Stage>(null);
  const transformerRefs = useRef<Record<string, Konva.Transformer>>({});
  const baseImageRef = useRef<Konva.Image>(null);
  // 3D 模型节点：选材面板 + 实时调视角覆盖层。
  const [modelPickerOpen, setModelPickerOpen] = useState(false);
  const [modelAssets, setModelAssets] = useState<LibraryAsset[]>([]);
  const [modelBusy, setModelBusy] = useState(false);
  const [editing3dNodeId, setEditing3dNodeId] = useState<string | null>(null);
  const overlayRef = useRef<Model3DOverlayHandle>(null);

  useEffect(() => {
    document.body.classList.add("studio-theme");
    document.body.style.overflow = "hidden";
    return () => {
      document.body.classList.remove("studio-theme");
      document.body.style.overflow = "";
    };
  }, []);

  useEffect(() => {
    const info: BaseSelectorImage = initialImageUrl
      ? { url: initialImageUrl, mediaToken: `local:${Date.now()}` }
      : {
          url: "",
          mediaToken: `blank:${Date.now()}`,
          isBlankCanvas: true,
          blankCanvasWidth: 1024,
          blankCanvasHeight: 1024,
        };
    useSceneStore.getState().setBaseImageInfo(info);
  }, [initialImageUrl]);

  const fitCanvas = useCallback(() => {
    const stage = stageRef.current;
    if (!stage) return;
    const container = stage.container();
    const containerWidth = container.offsetWidth || window.innerWidth;
    const containerHeight = container.offsetHeight || window.innerHeight;
    const state = useSceneStore.getState();
    const dimensions = state.getAspectRatioDimensions();
    const canvasWidth = state.baseImageBitmap?.naturalWidth || dimensions.width;
    const canvasHeight =
      state.baseImageBitmap?.naturalHeight || dimensions.height;
    const padding = 40;
    const scale = Math.min(
      (containerWidth - padding * 2) / canvasWidth,
      (containerHeight - padding * 2) / canvasHeight,
      1,
    );
    stage.scale({ x: scale, y: scale });
    stage.position({
      x: (containerWidth - canvasWidth * scale) / 2,
      y: (containerHeight - canvasHeight * scale) / 2,
    });
    stage.batchDraw();
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(fitCanvas, 350);
    window.addEventListener("resize", fitCanvas);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("resize", fitCanvas);
    };
  }, [fitCanvas, store.baseImageBitmap, store.baseImageInfo?.mediaToken]);

  const handleUpload = useCallback(async (file: File) => {
    setError(null);
    const url = URL.createObjectURL(file);
    useSceneStore.getState().setBaseImageInfo({
      url,
      mediaToken: `upload:${file.name}:${Date.now()}`,
    });
  }, []);

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) void handleUpload(file);
    event.target.value = "";
  };

  const handlePaintBrush = (hex: string, size: number, opacity: number) => {
    useSceneStore.getState().setActiveTool("draw");
    useSceneStore.getState().setBrushColor(hex);
    useSceneStore.getState().setBrushSize(size);
    useSceneStore.getState().setBrushOpacity(opacity);
  };

  const handleActivateShapeTool = (
    shape: "rectangle" | "circle" | "triangle",
  ) => {
    const state = useSceneStore.getState();
    state.selectNode(null);
    state.setCurrentShape(shape);
    state.setActiveTool("shape");
  };

  const baseImageBounds = () => {
    const stage = stageRef.current;
    if (!stage) return null;
    const node = baseImageRef.current;
    return node
      ? node.getClientRect({ relativeTo: stage })
      : { x: 0, y: 0, width: stage.width(), height: stage.height() };
  };

  // 把浏览器坐标换算到舞台坐标系，裁剪框按舞台坐标保存，
  // 这样缩放/平移画布后裁剪位置依然正确。
  const clientToStage = (clientX: number, clientY: number) => {
    const stage = stageRef.current;
    if (!stage) return { x: 0, y: 0 };
    const box = stage.container().getBoundingClientRect();
    return {
      x: (clientX - box.left - stage.x()) / stage.scaleX(),
      y: (clientY - box.top - stage.y()) / stage.scaleY(),
    };
  };

  const beginCropDrag = (event: React.MouseEvent<HTMLDivElement>) => {
    if (!cropMode) return;
    const point = clientToStage(event.clientX, event.clientY);
    cropAnchorRef.current = point;
    setCropRect({ x: point.x, y: point.y, w: 0, h: 0 });
  };

  const moveCropDrag = (event: React.MouseEvent<HTMLDivElement>) => {
    const anchor = cropAnchorRef.current;
    if (!cropMode || !anchor) return;
    const point = clientToStage(event.clientX, event.clientY);
    setCropRect({
      x: Math.min(anchor.x, point.x),
      y: Math.min(anchor.y, point.y),
      w: Math.abs(point.x - anchor.x),
      h: Math.abs(point.y - anchor.y),
    });
  };

  const endCropDrag = () => {
    cropAnchorRef.current = null;
  };

  const applyCrop = useCallback(async () => {
    const stage = stageRef.current;
    const node = baseImageRef.current;
    const rect = cropRect;
    const state = useSceneStore.getState();
    const bitmap = state.baseImageBitmap;
    if (!stage || !node || !rect || !bitmap) {
      setError("请先上传一张图片再裁剪");
      return;
    }
    const bounds = node.getClientRect({ relativeTo: stage });
    const ratio = bounds.width > 0 ? bitmap.naturalWidth / bounds.width : 1;
    const sx = Math.max(0, Math.round((rect.x - bounds.x) * ratio));
    const sy = Math.max(0, Math.round((rect.y - bounds.y) * ratio));
    const sw = Math.min(bitmap.naturalWidth - sx, Math.round(rect.w * ratio));
    const sh = Math.min(bitmap.naturalHeight - sy, Math.round(rect.h * ratio));
    if (sw < 8 || sh < 8) {
      setError("裁剪框太小");
      return;
    }
    const canvas = document.createElement("canvas");
    canvas.width = sw;
    canvas.height = sh;
    const context = canvas.getContext("2d");
    if (!context) return;
    context.drawImage(bitmap, sx, sy, sw, sh, 0, 0, sw, sh);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/png"));
    if (!blob) {
      setError("裁剪失败");
      return;
    }
    setError(null);
    state.setBaseImageInfo({
      url: URL.createObjectURL(blob),
      mediaToken: `crop:${Date.now()}`,
    });
    setCropRect(null);
    setCropMode(false);
  }, [cropRect]);

  const layerRows = useMemo(() => [...store.drawNodes].reverse(), [store.drawNodes]);

  const updateLayer = (node: Node | LineNode, updates: { visible?: boolean; layerOpacity?: number }, save = true) => {
    const state = useSceneStore.getState();
    if (node.type === "line") state.updateLineNode(node.id, updates, save);
    else state.updateNode(node.id, updates, save);
    if (updates.visible === false) state.selectNode(null);
  };

  const addImageLayers = async (files: File[]) => {
    setError(null);
    setBusy("正在添加图片层…");
    try {
      for (const file of files) {
        await useSceneStore.getState().createImageFromFile(0, 0, file);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const documentPNG = (stage: Konva.Stage) => {
    const { width, height } = useSceneStore.getState().getAspectRatioDimensions();
    return captureDocumentCanvas(stage, width, height, [BG_LAYER_ID, DRAW_LAYER_ID]).toDataURL("image/png");
  };

  const exportMask = async (
    stage: Konva.Stage,
    bounds: { x: number; y: number; width: number; height: number },
    targetWidth: number,
    targetHeight: number,
  ): Promise<Blob> => {
    const layer = stage.findOne<Konva.Layer>(`#${INPAINT_LAYER_ID}`);
    if (!layer) throw new Error("蒙版层不存在");
    const dataUrl = layer.toDataURL({ pixelRatio: 1 });
    const image = await loadImage(dataUrl);
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(targetWidth));
    canvas.height = Math.max(1, Math.round(targetHeight));
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("无法创建蒙版画布");
    // Keep the canvas transparent so alpha tells us which pixels were painted.
    ctx.drawImage(
      image,
      bounds.x,
      bounds.y,
      bounds.width,
      bounds.height,
      0,
      0,
      canvas.width,
      canvas.height,
    );
    const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height);
    for (let offset = 0; offset < pixels.data.length; offset += 4) {
      const painted = pixels.data[offset + 3] > 0;
      const value = painted ? 255 : 0;
      pixels.data[offset] = value;
      pixels.data[offset + 1] = value;
      pixels.data[offset + 2] = value;
      pixels.data[offset + 3] = 255;
    }
    ctx.putImageData(pixels, 0, 0);
    return new Promise((resolve, reject) =>
      canvas.toBlob(
        (blob) => (blob ? resolve(blob) : reject(new Error("蒙版导出失败"))),
        "image/png",
      ),
    );
  };

  const applyResult = async (task: Awaited<ReturnType<typeof waitForTask>>) => {
    const url = resultUrl(task);
    if (!url) throw new Error("任务没有输出文件");
    const response = await fetch(url);
    if (!response.ok) throw new Error(`读取结果失败：${response.status}`);
    const blob = await response.blob();
    const objectUrl = URL.createObjectURL(blob);
    useSceneStore.getState().setBaseImageInfo({
      url: objectUrl,
      mediaToken: `result:${task.task_id}:${Date.now()}`,
    });
    useSceneStore.setState({ inpaintLineNodes: [] });
  };

  const runBackgroundRemoval = async () => {
    const stage = stageRef.current;
    if (!stage) return;
    setBusy("正在去除背景…");
    setError(null);
    try {
      const state = useSceneStore.getState();
      let blob: Blob;
      if (state.drawNodes.length === 0 && state.baseImageInfo?.url) {
        // Keep the original pixels and resolution when nothing has been drawn.
        blob = await (await fetch(state.baseImageInfo.url)).blob();
      } else {
        const bounds = baseImageBounds();
        if (!bounds) throw new Error("画布尚未就绪");
        const dataUrl = stage.toDataURL({ ...bounds, pixelRatio: 1 });
        blob = await (await fetch(dataUrl)).blob();
      }
      const upload = await uploadImage(blob, `canvas-${Date.now()}.png`);
      const task = await createTask({
        code: "image_remove_background",
        files: { image: [{ task_id: upload.task_id, name: upload.name }] },
      });
      const finished = await waitForTask(task.task_id);
      await applyResult(finished);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const runInpaint = async () => {
    const state = useSceneStore.getState();
    const stage = stageRef.current;
    if (!stage) return;
    if (state.inpaintLineNodes.length === 0) {
      setError("请先在画布上涂抹需要重绘的区域");
      return;
    }
    if (!prompt.trim()) {
      setError("请填写重绘提示词");
      return;
    }
    setBusy("正在上传原图与蒙版…");
    setError(null);
    try {
      const baseUrl = state.baseImageInfo?.url;
      const bounds = baseImageBounds();
      if (!bounds) throw new Error("画布尚未就绪");
      let baseBlob: Blob;
      if (!baseUrl || state.baseImageInfo?.isBlankCanvas) {
        // A blank canvas has no source file: export the rendered pixels.
        const dataUrl = stage.toDataURL({ ...bounds, pixelRatio: 1 });
        baseBlob = await (await fetch(dataUrl)).blob();
      } else {
        baseBlob = await (await fetch(baseUrl)).blob();
      }
      const baseUpload = await uploadImage(
        baseBlob,
        `base-${Date.now()}.png`,
      );
      const targetWidth =
        state.baseImageBitmap?.naturalWidth ?? Math.round(bounds.width);
      const targetHeight =
        state.baseImageBitmap?.naturalHeight ?? Math.round(bounds.height);
      const maskBlob = await exportMask(
        stage,
        bounds,
        targetWidth,
        targetHeight,
      );
      const maskUpload = await uploadImage(
        maskBlob,
        `mask-${Date.now()}.png`,
      );
      setBusy("正在排队局部重绘…");
      const task = await createTask({
        code: inpaintModel,
        prompt,
        params: {
          prompt,
          negative_prompt: negativePrompt,
          ...(inpaintModel === "image_inpaint_krea2" && kreaUnet ? { unet_name: kreaUnet } : {}),
        },
        files: {
          image: [{ task_id: baseUpload.task_id, name: baseUpload.name }],
          mask: [{ task_id: maskUpload.task_id, name: maskUpload.name }],
        },
      });
      setBusy("正在生成，等待 ComfyUI 返回…");
      const finished = await waitForTask(task.task_id);
      await applyResult(finished);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  };

  const handleDownload = () => {
    const stage = stageRef.current;
    if (!stage) return;
    try {
      const link = document.createElement("a");
      link.download = `artcraft-${Date.now()}.png`;
      link.href = documentPNG(stage);
      link.click();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  // 把画布当前画面按 PNG 交给宿主写回分镜；不下载、不关编辑器，用户可继续改。
  const handleWriteBack = async () => {
    const stage = stageRef.current;
    if (!writeBack || !stage) return;
    setWriting(true);
    setError(null);
    setNotice(null);
    try {
      const dataUrl = documentPNG(stage);
      const blob = await (await fetch(dataUrl)).blob();
      const message = await writeBack(blob, `canvas-${Date.now()}.png`);
      setNotice(message);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setWriting(false);
    }
  };

  const inpaintNodes = useMemo(() => store.inpaintLineNodes, [store]);

  // 舞台中心在舞台坐标系里的位置：新节点默认放在用户正在看的地方。
  const stageCenter = () => {
    const stage = stageRef.current;
    if (!stage) return { x: 0, y: 0 };
    const container = stage.container();
    const scale = stage.scaleX() || 1;
    return {
      x: (container.offsetWidth / 2 - stage.x()) / scale,
      y: (container.offsetHeight / 2 - stage.y()) / scale,
    };
  };

  const openModelPicker = async () => {
    setModelPickerOpen((open) => !open);
    if (modelAssets.length > 0) return;
    try {
      const response = await fetch("/api/materials?type=3d");
      if (!response.ok) throw new Error(`读取素材库失败：${response.status}`);
      const payload = await response.json();
      const items: LibraryAsset[] = Array.isArray(payload) ? payload : payload.items ?? [];
      setModelAssets(items);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  // 从素材库挑一个 GLB 放到画布中心，先离屏渲一帧当占位图。
  const placeModel = async (asset: LibraryAsset) => {
    const kind = assetKind(asset.path || asset.name);
    if (kind !== "glb" && kind !== "gltf") {
      setError(`只支持 GLB / glTF 放进画布，请先转换：${asset.name}`);
      return;
    }
    setModelBusy(true);
    setError(null);
    try {
      const url = assetUrl(asset.path);
      const params: Model3DParams = { ...DEFAULT_MODEL3D_PARAMS };
      const center = stageCenter();
      const dataUrl = await render3DModelToDataUrl(url, params, 512, 512);
      useSceneStore.getState().createImageFrom3DModel(
        center.x - 256,
        center.y - 256,
        dataUrl,
        url,
        params,
        512,
        512,
      );
      setModelPickerOpen(false);
      setNotice(`已放入 ${asset.name}，可点「调 3D 视角」改机位`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setModelBusy(false);
    }
  };

  // 选中的单个节点若是 3D 模型渲染出来的，就允许回到 3D 里改机位。
  const selectedModelNode = useMemo(() => {
    if (store.selectedNodeIds.length !== 1) return null;
    const id = store.selectedNodeIds[0];
    const node = store.drawNodes.find((item) => item.id === id) as Node | undefined;
    return node?.modelUrl ? node : null;
  }, [store.selectedNodeIds, store.drawNodes]);

  const commitModelView = async (dataUrl: string, params: Model3DParams) => {
    const id = editing3dNodeId;
    setEditing3dNodeId(null);
    if (!id) return;
    const state = useSceneStore.getState();
    const node = state.drawNodes.find((item) => item.id === id) as Node | undefined;
    if (!node) return;
    try {
      await node.setImageFromUrl(dataUrl);
      // 换一张新的 imageElement 引用，Konva 才会重画；顺带把新机位存进节点。
      state.updateNode(id, {
        imageUrl: dataUrl,
        imageElement: node.imageElement,
        model3dParams: params,
      });
      setNotice("已更新 3D 视角");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const editing3dNode = editing3dNodeId
    ? (store.drawNodes.find((item) => item.id === editing3dNodeId) as Node | undefined)
    : undefined;

  return (
    <div
      className="studio-root studio-theme fixed inset-0 z-[100] overflow-hidden bg-[#121316] text-[#f2f1ee]"
      style={{ ["--pd-bottom-reserve" as string]: "72px" }}
    >
      <div className="absolute inset-x-0 bottom-0 top-14">
        <PaintSurface
          drawNodes={store.drawNodes}
          inpaintLineNodes={inpaintNodes}
          selectedNodeIds={store.selectedNodeIds}
          fillColor={fillColor}
          activeTool={store.activeTool}
          brushColor={store.brushColor}
          brushSize={store.brushSize}
          eraserSize={store.eraserSize}
          inpaintOperation={store.inpaintOperation}
          inpaintBrushSize={store.inpaintBrushSize}
          stageRef={stageRef}
          transformerRefs={transformerRefs}
          baseImageRef={baseImageRef}
          showMaskLayer
          fillParentHeight
        />
      </div>

      <div className="fixed inset-x-0 top-0 z-40 flex h-14 items-center gap-2 border-b border-white/10 bg-[#101014]/95 px-3 backdrop-blur">
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm text-white/80 hover:bg-white/10"
          onClick={() => window.location.assign(exitTo)}
        >
          返回
        </button>
        <span className="mr-2 text-sm font-medium">画布</span>
        <button
          type="button"
          className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
          onClick={() => fileInputRef.current?.click()}
        >
          替换底图
        </button>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={handleFileChange}
        />
        <button type="button" className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
          disabled={Boolean(busy)} onClick={() => layerInputRef.current?.click()}>
          添加图片层
        </button>
        <input ref={layerInputRef} type="file" accept="image/*" multiple className="hidden"
          onChange={(event) => {
            const files = Array.from(event.currentTarget.files || []);
            event.currentTarget.value = "";
            if (files.length) void addImageLayers(files);
          }} />
        <button
          type="button"
          className={`rounded-md border px-3 py-1.5 text-sm ${cropMode ? "border-amber-400/60 bg-amber-400/10 text-amber-200" : "border-white/10 hover:bg-white/10"}`}
          onClick={() => {
            setCropMode((value) => !value);
            setCropRect(null);
          }}
          title="在画布上拖出裁剪框"
        >
          {cropMode ? "裁剪中…" : "裁剪"}
        </button>
        {cropMode && (
          <>
            <button
              type="button"
              className="rounded-md border border-amber-400/60 bg-amber-400/10 px-3 py-1.5 text-sm text-amber-100 disabled:opacity-40"
              onClick={() => void applyCrop()}
              disabled={!cropRect || cropRect.w < 8 || cropRect.h < 8}
            >
              应用裁剪
            </button>
            <button
              type="button"
              className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
              onClick={() => {
                setCropRect(null);
                setCropMode(false);
              }}
            >
              取消
            </button>
          </>
        )}
        <button
          type="button"
          className="rounded-md border border-emerald-400/40 px-3 py-1.5 text-sm text-emerald-200 hover:bg-emerald-400/10 disabled:opacity-50"
          onClick={() => void runBackgroundRemoval()}
          disabled={Boolean(busy)}
        >
          去背景
        </button>
        <button
          type="button"
          className={`rounded-md border px-3 py-1.5 text-sm ${modelPickerOpen ? "border-violet-400/60 bg-violet-400/10 text-violet-200" : "border-white/10 hover:bg-white/10"}`}
          onClick={() => void openModelPicker()}
          title="从素材库放一个 GLB 到画布，渲成一张可再编辑的图"
        >
          放 3D 模型
        </button>
        {selectedModelNode && (
          <button
            type="button"
            className="rounded-md border border-violet-400/40 px-3 py-1.5 text-sm text-violet-200 hover:bg-violet-400/10"
            onClick={() => setEditing3dNodeId(selectedModelNode.id)}
            title="回到 3D 里改机位，改完写回这张图"
          >
            调 3D 视角
          </button>
        )}
        <button
          type="button"
          className="rounded-md border border-sky-400/40 px-3 py-1.5 text-sm text-sky-200 hover:bg-sky-400/10 disabled:opacity-50"
          onClick={() => void runInpaint()}
          disabled={Boolean(busy)}
        >
          AI 局部重绘
        </button>
        <input
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          placeholder="重绘提示词"
          className="h-9 w-56 rounded-md border border-white/10 bg-white/5 px-3 text-sm text-white placeholder:text-white/40"
        />
        <select
          value={inpaintModel}
          onChange={(event) => setInpaintModel(event.target.value)}
          title="局部重绘模型"
          className="h-9 rounded-md border border-white/10 bg-white/5 px-2 text-sm text-white"
        >
          <option value="image_inpaint">Qwen-Image 2.1</option>
          <option value="image_inpaint_krea2">Krea2</option>
        </select>
        {inpaintModel === "image_inpaint_krea2" && (
          <select
            value={kreaUnet}
            onChange={(event) => setKreaUnet(event.target.value)}
            title="Krea2 模型版本"
            className="h-9 max-w-[13rem] rounded-md border border-white/10 bg-white/5 px-2 text-sm text-white"
          >
            {KREA2_UNETS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        )}
        <input
          value={negativePrompt}
          onChange={(event) => setNegativePrompt(event.target.value)}
          placeholder="负面提示词（可选）"
          className="h-9 w-44 rounded-md border border-white/10 bg-white/5 px-3 text-sm text-white placeholder:text-white/40"
        />
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            className="rounded-md border border-white/10 px-2.5 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
            onClick={() => void store.undo()}
            disabled={store.historyIndex <= 0}
            title="撤销"
          >
            撤销
          </button>
          <button
            type="button"
            className="rounded-md border border-white/10 px-2.5 py-1.5 text-sm hover:bg-white/10 disabled:opacity-40"
            onClick={() => void store.redo()}
            disabled={store.historyIndex >= store.history.length - 1}
            title="重做"
          >
            重做
          </button>
          <button
            type="button"
            className="rounded-md border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
            onClick={handleDownload}
          >
            下载 PNG
          </button>
          {writeBack && (
            <button
              type="button"
              className="rounded-md border border-emerald-400/40 px-3 py-1.5 text-sm text-emerald-200 hover:bg-emerald-400/10 disabled:opacity-50"
              onClick={() => void handleWriteBack()}
              disabled={writing || Boolean(busy)}
              title="把画布当前画面写成一张新候选，旧版本仍可在候选中切回"
            >
              {writing ? "写回中…" : writeBackLabel}
            </button>
          )}
        </div>
      </div>

      <SideToolbar
        className="fixed left-0 top-[calc(50%_-_var(--pd-bottom-reserve,0px))] z-30 -translate-y-1/2 transform"
        onSelect={() => useSceneStore.getState().setActiveTool("select")}
        onActivateShapeTool={handleActivateShapeTool}
        onPaintBrush={handlePaintBrush}
        onCanvasBackground={setFillColor}
        onUploadImage={() => fileInputRef.current?.click()}
        supportsMaskTool
        activeToolId={store.activeTool}
        currentShape={store.currentShape}
      />

      {store.activeTool === "inpaint" && (
        <InpaintToolBar
          operation={store.inpaintOperation}
          brushSize={store.inpaintBrushSize}
          onOperationChange={store.setInpaintOperation}
          onBrushSizeChange={store.setInpaintBrushSize}
        />
      )}

      {cropMode && (
        <div
          className="absolute inset-x-0 bottom-0 top-14 z-30 cursor-crosshair"
          onMouseDown={beginCropDrag}
          onMouseMove={moveCropDrag}
          onMouseUp={endCropDrag}
          onMouseLeave={endCropDrag}
        >
          {cropRect && (
            <div
              className="pointer-events-none absolute border border-amber-300 bg-amber-300/10"
              style={{
                left: cropRect.x,
                top: cropRect.y,
                width: cropRect.w,
                height: cropRect.h,
              }}
            />
          )}
        </div>
      )}

      <aside className="fixed right-3 top-20 z-30 w-60 rounded-lg border border-white/10 bg-[#101014]/90 p-3 text-sm text-white/80 backdrop-blur">
        <div className="mb-2 flex gap-1">
          {(["layers", "history"] as const).map((tab) => (
            <button
              key={tab}
              type="button"
              onClick={() => setSidePanel(tab)}
              className={`flex-1 rounded-md px-2 py-1 text-xs ${sidePanel === tab ? "bg-white/15 text-white" : "text-white/60 hover:bg-white/10"}`}
            >
              {tab === "layers" ? "图层" : "历史"}
            </button>
          ))}
        </div>

        {sidePanel === "layers" ? (
          <div className="max-h-[55vh] space-y-1 overflow-auto">
            <div className="flex items-center justify-between rounded-md bg-white/5 px-2 py-1 text-xs text-white/60">
              <span>底图</span>
              <span className="text-white/30">固定</span>
            </div>
            {layerRows.length === 0 && (
              <p className="px-1 py-2 text-xs text-white/40">还没有绘制内容</p>
            )}
            {layerRows.map((node) => {
              const hidden = node.visible === false;
              const label = node.type === "line" ? "笔迹" : node.type === "image" ? (node.imageFile?.name || "图片") : node.type;
              const selected = store.selectedNodeIds.includes(node.id);
              return (
                <div
                  key={node.id}
                  className={`flex flex-wrap items-center gap-1 rounded-md px-2 py-1 ${selected ? "bg-sky-400/15" : "bg-white/5"}`}
                >
                  <button
                    type="button"
                    className="flex-1 truncate text-left text-xs"
                    disabled={hidden}
                    onClick={() => store.selectNode(node.id, false)}
                    title="选中该图层"
                  >
                    {label}
                  </button>
                  <button
                    type="button"
                    className="px-1 text-xs text-white/60 hover:text-white"
                    onClick={() => updateLayer(node, { visible: hidden })}
                    title={hidden ? "显示" : "隐藏"}
                  >
                    {hidden ? "○" : "●"}
                  </button>
                  <button
                    type="button"
                    className="px-1 text-xs text-white/60 hover:text-white"
                    onClick={() => store.removeNode(node.id, true)}
                    title="删除"
                  >
                    ✕
                  </button>
                  <div className="flex w-full items-center gap-1 text-xs">
                    {([
                      ["置底", "sendToBack"], ["下移", "sendBackward"],
                      ["上移", "bringForward"], ["置顶", "bringToFront"],
                    ] as const).map(([label, action]) => (
                      <button key={action} type="button" className="rounded border border-white/10 px-1 hover:bg-white/10"
                        onClick={() => { store[action]([node.id]); }}>{label}</button>
                    ))}
                  </div>
                  <label className="flex w-full items-center gap-1 text-xs">
                    不透明度
                    <input type="range" min="0" max="100" aria-label={`${label}不透明度`}
                      className="min-w-0 flex-1" value={Math.round((node.layerOpacity ?? 1) * 100)}
                      onChange={(event) => {
                        opacityDirtyRef.current = true;
                        updateLayer(node, { layerOpacity: Number(event.currentTarget.value) / 100 }, false);
                      }}
                      onPointerUp={commitOpacity} onKeyUp={commitOpacity}
                      onBlur={commitOpacity} />
                    <span>{Math.round((node.layerOpacity ?? 1) * 100)}%</span>
                  </label>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="space-y-2 text-xs text-white/70">
            <p>
              步骤 {Math.max(0, store.historyIndex + 1)} / {store.history.length}
            </p>
            <div className="flex gap-2">
              <button
                type="button"
                className="flex-1 rounded-md border border-white/10 px-2 py-1 hover:bg-white/10 disabled:opacity-40"
                onClick={() => void store.undo()}
                disabled={store.historyIndex <= 0}
              >
                撤销
              </button>
              <button
                type="button"
                className="flex-1 rounded-md border border-white/10 px-2 py-1 hover:bg-white/10 disabled:opacity-40"
                onClick={() => void store.redo()}
                disabled={store.historyIndex >= store.history.length - 1}
              >
                重做
              </button>
            </div>
            <p className="text-white/40">撤销记录随画笔、形状、抠图等操作自动累积。</p>
          </div>
        )}
      </aside>

      {modelPickerOpen && (
        <aside className="fixed left-3 top-20 z-30 w-72 rounded-lg border border-violet-400/30 bg-[#101014]/95 p-3 text-sm text-white/80 backdrop-blur">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs text-white/60">素材库里的 3D 模型</span>
            <button
              type="button"
              className="px-1 text-xs text-white/60 hover:text-white"
              onClick={() => setModelPickerOpen(false)}
            >
              ✕
            </button>
          </div>
          {modelBusy && <p className="py-2 text-xs text-violet-200">正在渲染缩略图…</p>}
          {!modelBusy && modelAssets.length === 0 && (
            <p className="py-2 text-xs text-white/40">素材库里还没有 3D 模型</p>
          )}
          <div className="max-h-[50vh] space-y-1 overflow-auto">
            {modelAssets.map((asset) => {
              const kind = assetKind(asset.path || asset.name);
              const usable = kind === "glb" || kind === "gltf";
              return (
                <button
                  key={asset.id}
                  type="button"
                  disabled={!usable || modelBusy}
                  onClick={() => void placeModel(asset)}
                  className={`flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-xs ${
                    usable ? "bg-white/5 hover:bg-white/15" : "cursor-not-allowed bg-white/5 opacity-50"
                  }`}
                  title={usable ? "放到画布中心" : "需要先转成 GLB"}
                >
                  {asset.thumb_path ? (
                    <img
                      src={assetUrl(asset.thumb_path)}
                      alt=""
                      className="h-8 w-8 rounded object-cover"
                    />
                  ) : (
                    <span className="flex h-8 w-8 items-center justify-center rounded bg-white/10 text-[10px]">
                      3D
                    </span>
                  )}
                  <span className="flex-1 truncate">{asset.name}</span>
                  {!usable && <span className="text-white/40">需转 GLB</span>}
                </button>
              );
            })}
          </div>
        </aside>
      )}

      {editing3dNode && (
        <>
          <Model3DOverlay
            ref={overlayRef}
            node={editing3dNode}
            stageRef={stageRef}
            onCommit={(dataUrl, params) => void commitModelView(dataUrl, params)}
            onDismiss={() => setEditing3dNodeId(null)}
          />
          {/* toast 在 bottom-20，这一行要抬到它上面，否则两者重叠互相遮挡。 */}
          <div className="fixed inset-x-0 bottom-32 z-[60] flex flex-wrap items-center justify-center gap-2 rounded-lg bg-[#101014]/80 px-2 py-1 backdrop-blur">
            <ScrubPad
              label="左右拖：转相机"
              onDrag={(dx, dy) => overlayRef.current?.onRotateDrag(dx, dy)}
            />
            <ScrubPad
              label="左右拖：缩模型"
              onDrag={(dx, dy) => overlayRef.current?.onScaleDrag(dx, dy)}
            />
            <ScrubPad
              label="左右拖：改视角"
              onDrag={(dx, dy) => overlayRef.current?.onFovDrag(dx, dy)}
            />
            <button
              type="button"
              className="rounded-md border border-emerald-400/50 bg-emerald-400/10 px-3 py-1.5 text-xs text-emerald-200 hover:bg-emerald-400/20"
              onClick={() => overlayRef.current?.commit()}
            >
              完成
            </button>
          </div>
        </>
      )}

      {error && (
        <div className="fixed bottom-20 left-1/2 z-50 -translate-x-1/2 rounded-md border border-red-400/40 bg-red-950/90 px-4 py-2 text-sm text-red-100">
          {error}
        </div>
      )}
      {notice && !error && (
        <div className="fixed bottom-20 left-1/2 z-50 -translate-x-1/2 rounded-md border border-emerald-400/40 bg-emerald-950/90 px-4 py-2 text-sm text-emerald-100">
          {notice}
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