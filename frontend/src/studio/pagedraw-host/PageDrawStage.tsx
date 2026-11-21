import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Konva from "konva";
import { PaintSurface, INPAINT_LAYER_ID } from "../pagedraw/PaintSurface";
import SideToolbar from "../pagedraw/components/ui/SideToolbar";
import InpaintToolBar from "../pagedraw/components/ui/InpaintToolBar";
import { useSceneStore } from "../pagedraw/stores/SceneState";
import type { LineNode } from "../pagedraw/stores/SceneState";
import type { Node } from "../pagedraw/Node";
import type { BaseSelectorImage } from "../pagedraw/types";
import { findTemplateId } from "../host/template-cache.js";

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
  const [fillColor, setFillColor] = useState("#ffffff");
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
  const [hiddenLayers, setHiddenLayers] = useState<Array<Node | LineNode>>([]);
  const cropAnchorRef = useRef<{ x: number; y: number } | null>(null);
  const [negativePrompt, setNegativePrompt] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const stageRef = useRef<Konva.Stage>(null);
  const transformerRefs = useRef<Record<string, Konva.Transformer>>({});
  const baseImageRef = useRef<Konva.Image>(null);

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

  const layerRows = useMemo(
    () => [...useSceneStore.getState().drawNodes].reverse(),
    // drawNodes 的引用在每次增删时变化，用 length + 最后一项 id 作为依赖足够
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [store.drawNodes.length, store.drawNodes[store.drawNodes.length - 1]?.id],
  );

  const toggleLayerVisibility = (id: string) => {
    const state = useSceneStore.getState();
    const node = state.drawNodes.find((item) => item.id === id);
    if (!node) return;
    // Konva 的入场淡入会把新节点的 opacity 拉回 1，靠 opacity 做隐藏并不可靠；
    // 这里把节点从渲染列表里摘出来单独记着，再点一次原样放回，绘图层行为一致。
    state.removeNode(id, true);
    setHiddenLayers((current) => [...current, node]);
  };

  const restoreLayer = (id: string) => {
    const state = useSceneStore.getState();
    const node = hiddenLayers.find((item) => item.id === id);
    if (!node) return;
    if (node.type === "line") {
      state.addLineNode(node, true);
    } else {
      state.addNode(node as never);
    }
    setHiddenLayers((current) => current.filter((item) => item.id !== id));
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
    const link = document.createElement("a");
    link.download = `artcraft-${Date.now()}.png`;
    link.href = stage.toDataURL({ pixelRatio: 1 });
    link.click();
  };

  // 把画布当前画面按 PNG 交给宿主写回分镜；不下载、不关编辑器，用户可继续改。
  const handleWriteBack = async () => {
    const stage = stageRef.current;
    if (!writeBack || !stage) return;
    setWriting(true);
    setError(null);
    setNotice(null);
    try {
      const dataUrl = stage.toDataURL({ pixelRatio: 1 });
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
          上传图片
        </button>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={handleFileChange}
        />
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
              const hidden = (node.opacity ?? 1) === 0;
              const label = node.type === "line" ? "笔迹" : node.type;
              const selected = store.selectedNodeIds.includes(node.id);
              return (
                <div
                  key={node.id}
                  className={`flex items-center gap-1 rounded-md px-2 py-1 ${selected ? "bg-sky-400/15" : "bg-white/5"}`}
                >
                  <button
                    type="button"
                    className="flex-1 truncate text-left text-xs"
                    onClick={() => store.selectNode(node.id, false)}
                    title="选中该图层"
                  >
                    {label}
                  </button>
                  <button
                    type="button"
                    className="px-1 text-xs text-white/60 hover:text-white"
                    onClick={() => toggleLayerVisibility(node.id)}
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
                </div>
              );
            })}
            {hiddenLayers.map((node) => (
              <div key={node.id} className="flex items-center gap-1 rounded-md bg-white/5 px-2 py-1 opacity-60">
                <span className="flex-1 truncate text-xs line-through">
                  {node.type === "line" ? "笔迹" : node.type}
                </span>
                <button
                  type="button"
                  className="px-1 text-xs text-white/60 hover:text-white"
                  onClick={() => restoreLayer(node.id)}
                  title="恢复显示"
                >
                  ○
                </button>
                <button
                  type="button"
                  className="px-1 text-xs text-white/60 hover:text-white"
                  onClick={() => setHiddenLayers((current) => current.filter((item) => item.id !== node.id))}
                  title="删除"
                >
                  ✕
                </button>
              </div>
            ))}
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