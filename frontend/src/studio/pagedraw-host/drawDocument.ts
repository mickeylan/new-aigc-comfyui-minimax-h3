import { Node } from '../pagedraw/Node';
import { AspectRatioType, type SceneState, type LineNode } from '../pagedraw/stores/SceneState';
import type { BaseSelectorImage } from '../pagedraw/types';

export const DRAW_DOCUMENT_MAX_BYTES = 32 * 1024 * 1024;
export type PreparedDrawDocument = Pick<SceneState, 'drawNodes' | 'inpaintLineNodes' | 'baseImageInfo' | 'baseImageBitmap' | 'aspectRatioType' | 'brushColor' | 'brushSize' | 'eraserSize' | 'fillColor'>;
const fields = ['id', 'type', 'x', 'y', 'width', 'height', 'fill', 'stroke', 'strokeWidth', 'draggable', 'backgroundColor', 'rotation', 'scaleX', 'scaleY', 'offsetX', 'offsetY', 'locked', 'layerOpacity', 'visible', 'points', 'opacity', 'globalCompositeOperation', 'model3dParams'];
const pick = (node: any) => Object.fromEntries(fields.filter(key => node[key] !== undefined).map(key => [key, JSON.parse(JSON.stringify(node[key]))]));
function size(text: string) { if (new Blob([text]).size > DRAW_DOCUMENT_MAX_BYTES) throw new Error('2D工程超过32MiB，请减少素材大小'); }
async function dataURL(source: string | Blob) {
  let blob: Blob;
  if (typeof source === 'string') { const response = await fetch(source); if (!response.ok) throw new Error(`素材读取失败 HTTP ${response.status}`); blob = await response.blob(); }
  else blob = source;
  if (blob.size > DRAW_DOCUMENT_MAX_BYTES * 0.75) throw new Error('工程素材超过32MiB预算');
  return new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = () => reject(new Error('素材读取失败')); reader.readAsDataURL(blob); });
}

/** Snapshot primitive fields before the first await; URLs are replaced by embedded bytes. */
export async function exportDrawDocument(state: SceneState) {
  const base = state.baseImageInfo && { ...state.baseImageInfo };
  const nodes = state.drawNodes.map(node => ({ data: pick(node), image: node.type === 'image' ? (node.imageFile || node.imageUrl || node.imageElement?.src) : null, model: node.type !== 'line' ? node.modelUrl : null }));
  const doc: any = { kind: 'studio-draw', version: '2.2', drawNodes: [], inpaintLineNodes: state.inpaintLineNodes.map(pick),
    aspectRatioType: state.aspectRatioType, brushColor: state.brushColor, brushSize: state.brushSize, eraserSize: state.eraserSize, fillColor: state.fillColor, baseImage: null };
  if (base) {
    doc.baseImage = base.isBlankCanvas ? { isBlankCanvas: true, blankCanvasWidth: base.blankCanvasWidth, blankCanvasHeight: base.blankCanvasHeight } : { url: await dataURL(base.url) };
  }
  for (const node of nodes) {
    if (node.data.type === 'image') {
      if (!node.image) throw new Error('图片层缺失媒体，无法完整保存');
      node.data.imageDataUrl = await dataURL(node.image);
      if (!node.data.imageDataUrl.startsWith('data:image/')) throw new Error('图片层不是图片媒体');
    }
    if (node.model) node.data.modelUrl = await dataURL(node.model);
    doc.drawNodes.push(node.data);
    size(JSON.stringify(doc));
  }
  const text = JSON.stringify(doc); size(text); return text;
}

function validateNode(node: any, ids: Set<string>, mask = false) {
  if (!node || typeof node.id !== 'string' || !node.id || ids.has(node.id) || !['line', 'rectangle', 'circle', 'triangle', 'image'].includes(node.type) || (mask && node.type !== 'line')) throw new Error('工程图层类型或ID无效');
  ids.add(node.id);
  for (const key of ['x', 'y', 'width', 'height', 'strokeWidth', 'rotation', 'scaleX', 'scaleY', 'offsetX', 'offsetY', 'layerOpacity', 'opacity']) {
    if (node[key] !== undefined && (typeof node[key] !== 'number' || !Number.isFinite(node[key]))) throw new Error('工程图层数值无效');
  }
  if (node.type !== 'line' && !['x', 'y', 'width', 'height'].every(key => typeof node[key] === 'number')) throw new Error('工程图层尺寸缺失');
  if (node.width < 0 || node.height < 0 || (node.layerOpacity !== undefined && (node.layerOpacity < 0 || node.layerOpacity > 1))) throw new Error('工程图层尺寸或透明度无效');
  for (const key of ['visible', 'locked', 'draggable']) if (node[key] !== undefined && typeof node[key] !== 'boolean') throw new Error('工程图层状态无效');
  for (const key of ['fill', 'stroke', 'backgroundColor', 'globalCompositeOperation']) if (node[key] !== undefined && typeof node[key] !== 'string') throw new Error('工程图层样式无效');
  if (node.model3dParams !== undefined) {
    const params = node.model3dParams;
    if (!params || typeof params !== 'object' || Array.isArray(params)) throw new Error('3D重编辑参数无效');
    for (const key of ['fov', 'modelScale', 'nativeWidth', 'nativeHeight']) if (params[key] !== undefined && (typeof params[key] !== 'number' || !Number.isFinite(params[key]) || params[key] <= 0 || (key === 'fov' && params[key] >= 180) || (key.startsWith('native') && params[key] > 8192))) throw new Error('3D重编辑数值无效');
    for (const key of ['cameraPosition', 'cameraTarget']) if (params[key] !== undefined && (!params[key] || !['x', 'y', 'z'].every(axis => typeof params[key][axis] === 'number' && Number.isFinite(params[key][axis])))) throw new Error('3D重编辑机位无效');
  }
  if (node.type === 'line' && (!Array.isArray(node.points) || node.points.length % 2 || !node.points.every((n: any) => typeof n === 'number' && Number.isFinite(n)))) throw new Error('工程笔迹无效');
}
async function decode(url: string) {
  const image = new Image(); image.crossOrigin = 'anonymous'; image.src = url;
  try { await image.decode(); } catch { throw new Error('工程图片无法解码'); }
  return image;
}

/** No store mutation until every image has decoded. Legacy 2.1 URLs are fetched explicitly. */
export async function prepareDrawDocument(text: string): Promise<PreparedDrawDocument> {
  size(text);
  const doc = JSON.parse(text);
  if (!doc || !['2.1', '2.2'].includes(doc.version) || (doc.version === '2.2' && doc.kind !== 'studio-draw') || !Array.isArray(doc.drawNodes) || (doc.inpaintLineNodes !== undefined && !Array.isArray(doc.inpaintLineNodes))) throw new Error('不是可识别的2D工程');
  if (doc.aspectRatioType !== undefined && !Object.values(AspectRatioType).includes(doc.aspectRatioType)) throw new Error('工程画幅无效');
  const ids = new Set<string>();
  doc.drawNodes.forEach((node: any) => validateNode(node, ids));
  (doc.inpaintLineNodes ?? []).forEach((node: any) => validateNode(node, ids, true));
  for (const key of ['brushSize', 'eraserSize']) if (doc[key] !== undefined && (typeof doc[key] !== 'number' || !Number.isFinite(doc[key]) || doc[key] <= 0)) throw new Error('工程笔刷尺寸无效');
  for (const key of ['brushColor', 'fillColor']) if (doc[key] !== undefined && typeof doc[key] !== 'string') throw new Error('工程颜色无效');
  let baseImageInfo: BaseSelectorImage | null = null, baseImageBitmap: HTMLImageElement | null = null;
  if (doc.baseImage) {
    const base = doc.baseImage;
    if (base.isBlankCanvas === true) {
      if (![base.blankCanvasWidth, base.blankCanvasHeight].every(n => typeof n === 'number' && Number.isFinite(n) && n > 0 && n <= 32768)) throw new Error('工程画布尺寸无效');
      baseImageInfo = { url: '', mediaToken: crypto.randomUUID(), isBlankCanvas: true, blankCanvasWidth: base.blankCanvasWidth, blankCanvasHeight: base.blankCanvasHeight };
    } else {
      if (typeof base.url !== 'string' || !base.url.startsWith('data:image/')) throw new Error('工程底图必须自包含');
      baseImageBitmap = await decode(base.url);
      baseImageInfo = { url: base.url, mediaToken: crypto.randomUUID() };
    }
  }
  const drawNodes = await Promise.all(doc.drawNodes.map(async (data: any) => {
    if (data.type === 'line') return pick(data) as LineNode;
    const node = new Node(pick(data) as any);
    if (data.type === 'image') {
      let url = data.imageDataUrl;
      if (doc.version === '2.1' && !url && typeof data.imageUrl === 'string') {
        if (data.imageUrl.startsWith('data:image/')) url = data.imageUrl;
        else if (/^(https?:|blob:|\/)/.test(data.imageUrl)) url = await dataURL(data.imageUrl);
      }
      if (typeof url !== 'string' || !url.startsWith('data:image/')) throw new Error('图片层缺少自包含媒体');
      node.imageElement = await decode(url); node.imageUrl = url;
      if (data.modelUrl !== undefined) {
        if (typeof data.modelUrl !== 'string' || !data.modelUrl.startsWith('data:')) throw new Error('3D重编辑素材必须自包含');
        node.modelUrl = data.modelUrl;
      }
    }
    return node;
  }));
  return { drawNodes, inpaintLineNodes: (doc.inpaintLineNodes ?? []).map(pick), baseImageInfo, baseImageBitmap,
    aspectRatioType: doc.aspectRatioType ?? AspectRatioType.NONE, brushColor: doc.brushColor ?? '#000000', brushSize: doc.brushSize ?? 5, eraserSize: doc.eraserSize ?? 16, fillColor: doc.fillColor ?? 'white' };
}
