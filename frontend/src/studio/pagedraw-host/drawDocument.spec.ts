import { afterEach, describe, expect, it, vi } from 'vitest';
import { Node } from '../pagedraw/Node';
import { useSceneStore, AspectRatioType } from '../pagedraw/stores/SceneState';
import { exportDrawDocument, prepareDrawDocument } from './drawDocument';
const pixel = 'data:image/png;base64,aGVsbG8=';
const shape = () => new Node({ id: 'shape', type: 'rectangle', x: 1, y: 2, width: 30, height: 40, fill: 'red', layerOpacity: 0.4, visible: false });
const document = (extra: any = {}) => JSON.stringify({ kind: 'studio-draw', version: '2.2', drawNodes: [], ...extra });
afterEach(() => { vi.unstubAllGlobals(); });
function images() { vi.stubGlobal('Image', class { src = ''; crossOrigin = ''; decode() { return this.src.includes('bad') ? Promise.reject(Error('bad')) : Promise.resolve(); } }); }
describe('portable 2D document', () => {
  it('round-trips blank canvas, draw order, lines, opacity and hidden layers', async () => {
    const line: any = { id: 'line', type: 'line', points: [1, 2, 3, 4], stroke: 'black', strokeWidth: 3, draggable: false };
    const state: any = { ...useSceneStore.getState(), baseImageInfo: { isBlankCanvas: true, blankCanvasWidth: 900, blankCanvasHeight: 1600 }, drawNodes: [shape(), line], aspectRatioType: AspectRatioType.PORTRAIT };
    const text = await exportDrawDocument(state);
    const restored = await prepareDrawDocument(text);
    expect(restored.drawNodes.map(n => n.id)).toEqual(['shape', 'line']);
    expect(restored.drawNodes[0]).toBeInstanceOf(Node);
    expect(restored.drawNodes[0].visible).toBe(false);
    expect(restored.drawNodes[0].layerOpacity).toBe(0.4);
    expect(restored.baseImageInfo?.blankCanvasHeight).toBe(1600);
    expect(restored.aspectRatioType).toBe('2:3');
  });
  it('embeds URL-only cutouts, base image and 3D re-edit bytes', async () => {
    images();
    vi.stubGlobal('fetch', vi.fn(async (url: string) => ({ ok: true, blob: async () => new Blob(['abc'], { type: url.includes('model') ? 'model/gltf-binary' : 'image/png' }) })));
    const node = new Node({ ...shape(), type: 'image', imageUrl: '/cutout.png', modelUrl: '/model.glb', model3dParams: { fov: 50 } as any });
    const text = await exportDrawDocument({ ...useSceneStore.getState(), baseImageInfo: { url: '/base.png', mediaToken: 'base' }, drawNodes: [node] } as any);
    expect(text).not.toContain('/cutout.png'); expect(text).not.toContain('/base.png'); expect(text).not.toContain('/model.glb');
    const restored = await prepareDrawDocument(text);
    expect(restored.baseImageBitmap).toBeTruthy();
    expect((restored.drawNodes[0] as Node).imageElement).toBeTruthy();
    expect((restored.drawNodes[0] as Node).modelUrl).toMatch(/^data:model\/gltf-binary/);
    expect((restored.drawNodes[0] as Node).model3dParams?.fov).toBe(50);
  });
  it('reads old 2.1 shape documents without replacing the current store', async () => {
    const before = useSceneStore.getState();
    const restored = await prepareDrawDocument(JSON.stringify({ version: '2.1', drawNodes: [shape()] }));
    expect(restored.drawNodes).toHaveLength(1); expect(useSceneStore.getState()).toBe(before);
  });
  it('rejects duplicate IDs, nonnumeric positions and unsupported formats before mutation', async () => {
    const before = useSceneStore.getState();
    for (const text of [document({ drawNodes: [shape(), shape()] }), document({ drawNodes: [{ ...shape(), x: 'bad' }] }), document({ version: '99' })]) await expect(prepareDrawDocument(text)).rejects.toThrow();
    expect(useSceneStore.getState()).toBe(before);
  });
  it('failed decode leaves existing document untouched', async () => {
    images(); const before = useSceneStore.getState();
    await expect(prepareDrawDocument(document({ drawNodes: [{ ...shape(), type: 'image', imageDataUrl: 'data:image/png;bad' }] }))).rejects.toThrow('无法解码');
    expect(useSceneStore.getState()).toBe(before);
  });
  it('missing media is an error, not a silently dropped image layer', async () => {
    await expect(exportDrawDocument({ ...useSceneStore.getState(), drawNodes: [new Node({ ...shape(), type: 'image' })] } as any)).rejects.toThrow('缺失媒体');
    await expect(prepareDrawDocument(document({ drawNodes: [{ ...shape(), type: 'image' }] }))).rejects.toThrow('缺少自包含');
  });
  it('commits decoded document and cancels older base-image callbacks', async () => {
    const callbacks: any[] = [];
    vi.stubGlobal('Image', class { onload: any; onerror: any; set src(_value: string) { callbacks.push(this); } });
    useSceneStore.getState().setBaseImageInfo({ url: '/old.png', mediaToken: 'old' });
    const prepared = await prepareDrawDocument(document({ drawNodes: [shape()] }));
    useSceneStore.getState().restorePreparedDocument(prepared);
    const restored = useSceneStore.getState().drawNodes;
    callbacks[0].onload();
    expect(useSceneStore.getState().drawNodes).toBe(restored);
    expect(useSceneStore.getState().baseImageInfo).toBe(null);
    expect(useSceneStore.getState().history).toHaveLength(1);
    expect(useSceneStore.getState().selectedNodeIds).toEqual([]);
  });
  it('accepts legacy data image URLs and rejects invalid 3D re-edit parameters', async () => {
    images();
    const restored = await prepareDrawDocument(JSON.stringify({ version: '2.1', drawNodes: [{ ...shape(), type: 'image', imageUrl: pixel }] }));
    expect((restored.drawNodes[0] as Node).imageUrl).toBe(pixel);
    await expect(prepareDrawDocument(document({ drawNodes: [{ ...shape(), model3dParams: { fov: 200 } }] }))).rejects.toThrow('3D重编辑');
  });
  it('rejects oversized documents before JSON parsing or image decode', async () => {
    await expect(prepareDrawDocument(' '.repeat(32 * 1024 * 1024 + 1))).rejects.toThrow('32MiB');
  });
});
