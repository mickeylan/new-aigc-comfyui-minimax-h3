import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import ScriptVideoPanel, { type ScriptVideoArtifact } from './ScriptVideoPanel';
import EnvironmentBrushPanel from './EnvironmentBrushPanel';
import { createEnvironmentTerrain } from './ground-path';
let host: HTMLDivElement, root: Root;
const plan = { shots: [{ id: 'shot-1', end: 1 }] } as any;
const artifact = (): ScriptVideoArtifact => ({ blob: new Blob(['video'], { type: 'video/webm' }), manifest: { codec: 'VP8', frameCount: 12, artifactId: '00000000-0000-4000-8000-000000000001', samplerVersion: 'script-shot-timeline-v1', characterAnimationCatalogRevision: 3 }, source: 'source', epoch: 1, projectId: 17 });
const button = (text: string) => [...host.querySelectorAll('button')].find(node => node.textContent === text)!;
async function click(node: Element) { await act(async () => (node as HTMLElement).click()); }
beforeEach(() => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true; host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  vi.stubGlobal('fetch', vi.fn(async (_url, request) => ({ ok: true, json: async () => ({ id: 23, name: 'studio-shots.webm', derivation_manifest: request.body.get('derivation_manifest') }) })));
  URL.createObjectURL = vi.fn(() => 'blob:test-video'); URL.revokeObjectURL = vi.fn();
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); vi.unstubAllGlobals(); });
async function mount(onExport = vi.fn(async () => artifact()), isCurrent = () => true) {
  await act(async () => root.render(<ScriptVideoPanel plan={plan} disabled={false} onExport={onExport} isCurrent={isCurrent} />)); return onExport;
}
describe('manual video export and material handoff', () => {
  it('changing frame cancels the old export, resets confirmation, and cannot attach its late result to the new ratio', async () => {
    let release!: (value: ScriptVideoArtifact) => void;
    const onExport = vi.fn(() => new Promise<ScriptVideoArtifact>(resolve => { release = resolve; }));
    await mount(onExport); expect(host.textContent).toContain('16:9 · 1280×720');
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    const oldSignal = onExport.mock.calls[0][3];
    await act(async () => root.render(<ScriptVideoPanel plan={plan} frameAspect="9:16" disabled={false} onExport={onExport} isCurrent={() => true} />));
    expect(oldSignal.aborted).toBe(true); expect(host.textContent).toContain('9:16 · 720×1280');
    expect((host.querySelector('input[type="checkbox"]') as HTMLInputElement).checked).toBe(false);
    await act(async () => release(artifact()));
    expect(host.querySelector('video')).toBeNull(); expect(button('渲染逐镜视频').disabled).toBe(true); expect(fetch).not.toHaveBeenCalled();
  });
  it('未确认不能导出；导出后不上传；显式入库仅一次POST并保留项目，决不自动提交生成', async () => {
    const onExport = await mount(); expect(button('渲染逐镜视频').disabled).toBe(true);
    await click(host.querySelector('[aria-label="确认逐镜视频导出"]')!); await click(button('渲染逐镜视频'));
    expect(onExport).toHaveBeenCalledOnce(); expect(fetch).not.toHaveBeenCalled();
    expect(host.querySelector('video')?.getAttribute('src')).toBe('blob:test-video');
    await click(button('人工确认存视频素材库')); expect(fetch).toHaveBeenCalledOnce();
    const [url, request] = (fetch as any).mock.calls[0]; expect(url).toBe('/api/materials'); expect(request.method).toBe('POST');
    expect(request.body.get('project_id')).toBe('17'); expect(request.body.get('type')).toBe('video'); expect(request.body.get('file').type).toBe('video/webm');
    expect(JSON.parse(request.body.get('derivation_manifest'))).toEqual(artifact().manifest);
    expect(host.querySelector('[role="alert"]')).toBeNull();
    expect(button('已入库').disabled).toBe(true); expect(host.textContent).toContain('3D结构参考视频'); expect(host.textContent).toContain('#23');
  });
  it('旧后端忽略清单或返回不同清单时，显示已入库ID与追溯缺失，禁止重复上传', async () => {
    await mount(); await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    (fetch as any).mockResolvedValue({ ok: true, json: async () => ({ id: 23, name: 'studio-shots.webm', derivation_manifest: '{"artifactId":"other"}' }) });
    await click(button('人工确认存视频素材库'));
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('视频已入库 #23');
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('未确认保留同一来源清单');
    expect(button('已入库').disabled).toBe(true); await click(button('已入库')); expect(fetch).toHaveBeenCalledOnce();
    expect(host.querySelector('a[download="studio-shot-previs.webm"]')).not.toBeNull();
  });
  it('场景变化拒绝入库但保留可下载成品；HTTP错误显式显示，不伪造成功', async () => {
    await mount(undefined, () => false); await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    await click(button('人工确认存视频素材库')); expect(fetch).not.toHaveBeenCalled(); expect(host.querySelector('[role="alert"]')?.textContent).toContain('场景已变化');
    expect(host.querySelector('a[download="studio-shot-previs.webm"]')).not.toBeNull();
  });
  it('入库失败保留成品与错误，用户可手动重试而不重新渲染', async () => {
    const onExport = await mount(); (fetch as any).mockResolvedValue({ ok: false, status: 503, json: async () => ({ error: '素材库不可用' }) });
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频')); await click(button('人工确认存视频素材库'));
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('素材库不可用'); expect(button('人工确认存视频素材库').disabled).toBe(false); expect(onExport).toHaveBeenCalledOnce();
  });
  it('浏览器解码失败明确提示但保留下载和追溯，不自动上传或重试；新产物清除旧播放错误', async () => {
    const onExport = await mount(); await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    await act(async () => host.querySelector('video')!.dispatchEvent(new Event('error')));
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('本地播放器解码失败');
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('不能据此判断文件有效');
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('旧VP9文件不会随升级自动修复');
    expect(host.querySelector('[role="alert"]')?.textContent).not.toContain('降低FPS');
    expect(host.querySelector('[role="status"]')?.textContent).toContain('VP8');
    expect(host.querySelector('a[download="studio-shot-previs.webm"]')).not.toBeNull();
    expect(button('下载追溯清单')).toBeTruthy(); expect(fetch).not.toHaveBeenCalled(); expect(onExport).toHaveBeenCalledOnce();
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    expect(onExport).toHaveBeenCalledTimes(2); expect(host.querySelector('[role="alert"]')).toBeNull(); expect(fetch).not.toHaveBeenCalled();
  });
  it('上传期间切换镜头表，迟到入库回执单独标记为先前视频，不能把新视频误标为已入库', async () => {
    const onExport = await mount();
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    let resolve!: (response: any) => void;
    (fetch as any).mockImplementationOnce(() => new Promise(ok => { resolve = ok; }));
    await click(button('人工确认存视频素材库'));
    const nextPlan = { shots: [{ id: 'new-shot', end: 2 }] } as any;
    await act(async () => root.render(<ScriptVideoPanel plan={nextPlan} disabled={false} onExport={onExport} isCurrent={() => true} />));
    await act(async () => resolve({ ok: true, json: async () => ({ id: 23, name: 'old-video.webm', derivation_manifest: JSON.stringify(artifact().manifest) }) }));
    expect(host.textContent).toContain('先前视频已入库 #23');
    expect(host.textContent).toContain('不属于当前镜头表');
    expect(host.querySelector('video')).toBeNull();
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    expect(button('人工确认存视频素材库').disabled).toBe(false);
    expect(button('已入库')).toBeUndefined(); expect(fetch).toHaveBeenCalledOnce();
    expect(onExport).toHaveBeenCalledTimes(2);
  });
  it('切换镜头表后旧导出错误不污染新面板，旧上传失败单独提示结果未确认且不自动重试', async () => {
    let rejectExport!: (reason: Error) => void;
    const delayed = vi.fn(() => new Promise<ScriptVideoArtifact>((_ok, fail) => { rejectExport = fail; }));
    await mount(delayed); await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    const next = { shots: [{ id: 'next-shot', end: 1 }] } as any;
    const normal = vi.fn(async () => artifact());
    await act(async () => root.render(<ScriptVideoPanel plan={next} disabled={false} onExport={normal} isCurrent={() => true} />));
    await act(async () => rejectExport(Error('obsolete export failure')));
    expect(host.querySelector('[role="alert"]')).toBeNull();
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    let rejectUpload!: (reason: Error) => void;
    (fetch as any).mockImplementationOnce(() => new Promise((_ok, fail) => { rejectUpload = fail; }));
    await click(button('人工确认存视频素材库'));
    await act(async () => root.render(<ScriptVideoPanel plan={plan} disabled={false} onExport={normal} isCurrent={() => true} />));
    await act(async () => rejectUpload(Error('network lost')));
    expect(host.textContent).toContain('先前视频入库结果未确认');
    expect(host.textContent).toContain('network lost');
    expect(host.textContent).toContain('避免重复上传');
    expect(host.querySelector('[role="alert"]')).toBeNull(); expect(fetch).toHaveBeenCalledOnce();
  });
  it('A→B→A仍视为新的镜头表会话，旧后端丢清单的迟到回执保留素材ID但不能标记当前产物', async () => {
    const onExport = await mount();
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    let resolve!: (response: any) => void;
    (fetch as any).mockImplementationOnce(() => new Promise(ok => { resolve = ok; }));
    await click(button('人工确认存视频素材库'));
    const other = { shots: [{ id: 'other', end: 1 }] } as any;
    await act(async () => root.render(<ScriptVideoPanel plan={other} disabled={false} onExport={onExport} isCurrent={() => true} />));
    await act(async () => root.render(<ScriptVideoPanel plan={plan} disabled={false} onExport={onExport} isCurrent={() => true} />));
    await act(async () => resolve({ ok: true, json: async () => ({ id: 51, name: 'old-backend.webm' }) }));
    expect(host.querySelector('[aria-label="先前视频入库回执"]')?.textContent).toContain('视频已入库 #51');
    expect(host.textContent).toContain('未确认保留同一来源清单');
    expect(host.querySelector('[role="alert"]')).toBeNull();
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    expect(button('人工确认存视频素材库').disabled).toBe(false);
    expect(button('已入库')).toBeUndefined(); expect(fetch).toHaveBeenCalledOnce();
    console.log('VIDEO_HANDOFF_ASYNC_OWNERSHIP=PASS staleExport=ignored lateUpload=separate ABA=isolated receiptId=retained autoRetry=0');
  });
  it('取消把AbortSignal交给真实渲染所有者；不产生下载或入库', async () => {
    const onExport = vi.fn((_plan, _fps, _mode, signal: AbortSignal) => new Promise<ScriptVideoArtifact>((_resolve, reject) => signal.addEventListener('abort', () => reject(Error('导出已取消')))));
    await mount(onExport); await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频')); await click(button('取消视频导出'));
    expect(onExport.mock.calls[0][3].aborted).toBe(true); expect(host.textContent).toContain('导出已取消'); expect(fetch).not.toHaveBeenCalled(); expect(host.querySelector('video')).toBeNull();
  });
});
describe('unified delivery UI ownership', () => {
  const label = '下载PNG＋WebM总交付ZIP（双子包，无音频）';
  async function setup(references?: any) {
    const { validateScriptShotPlan } = await import('./scriptShotPlan');
    const p = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', action: [], diagnostics: [], shots: [{ id: 's', order: 1, start: 0, end: 1, camera: null, dialogue: [] }] });
    const a = artifact(); Object.assign(a.manifest, { kind: 'studio-script-shot-video', fps: 24, container: 'webm', frameCount: 24, sourceDurationMs: 1000, durationMs: 1000, width: 832, height: 480, shots: p, sourceFingerprint: 'content', sessionEvents: [] });
    const reference = { blob: new Blob(['reference-child'], { type: 'application/zip' }), manifest: { kind: 'script-shot-reference-pack', version: 1, sceneId: 's', sourceFingerprint: 'src', sceneFingerprint: 'rev', exportedSceneFingerprint: 'content', motionPlan: p, sessionEvents: [] } };
    const onReferences = references ?? vi.fn(async () => reference), onExport = vi.fn(async () => a);
    const current = { value: true };
    await act(async () => root.render(<ScriptVideoPanel plan={p} disabled={false} onExport={onExport} onExportReferences={onReferences} isCurrent={() => current.value} />));
    await click(host.querySelector('input[type="checkbox"]')!); await click(button('渲染逐镜视频'));
    return { reference, onReferences, onExport, current };
  }
  it('downloads exactly one total zip without encoding again or uploading', async () => {
    const f = await setup();
    const downloads: string[] = []; vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () { downloads.push(this.download); });
    await click(button(label));
    await act(async () => { await vi.waitFor(() => expect(downloads).toEqual(['previs-unified-delivery.zip'])); }); expect(f.onReferences).toHaveBeenCalledOnce(); expect(f.onExport).toHaveBeenCalledOnce(); expect(fetch).not.toHaveBeenCalled();
    const blobs = (URL.createObjectURL as any).mock.calls.map((c: any[]) => c[0]);
    const text = new TextDecoder().decode(await blobs.at(-1).arrayBuffer()); expect(text).toContain('reference-pack.zip'); expect(text).toContain('video-delivery.zip');
  });
  it.each(['cancel', 'stale', 'failure', 'unmount'])('does not download partial child or total zip on %s', async action => {
    let release!: (x: any) => void;
    const onReferences = vi.fn((_p, _signal) => new Promise(resolve => { release = resolve; }));
    const f = await setup(onReferences), downloads = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    await click(button(label)); expect(button(label).disabled).toBe(true);
    if (action === 'cancel') await click(button('取消视频导出'));
    if (action === 'stale') f.current.value = false;
    if (action === 'failure') f.reference.manifest.exportedSceneFingerprint = 'changed';
    if (action === 'unmount') await act(async () => root.render(<div />));
    await act(async () => { release(f.reference); });
    if (action !== 'unmount') await act(async () => { await vi.waitFor(() => expect(host.querySelector('[role="alert"]')).not.toBeNull()); });
    expect(downloads).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled(); expect(f.onExport).toHaveBeenCalledOnce();
    if (action === 'cancel') expect(onReferences.mock.calls[0][1].aborted).toBe(true);
  });
});
describe('terrain editor UI', () => {
  it('真实笔刷通过显式按钮应用，撤销精确回原高度场；重建地形使旧撤销失效', async () => {
    const original = createEnvironmentTerrain({ size: 40, amplitude: 0 }); let terrain = original;
    const render = () => root.render(<EnvironmentBrushPanel terrain={terrain} disabled={false} onChange={next => { terrain = next; render(); }} />);
    await act(async () => render()); await click(button('应用地形笔刷'));
    expect(terrain.heights[32 * 65 + 32]).toBe(1); expect(original.heights[32 * 65 + 32]).toBe(0);
    await click(button('撤销笔刷（1）')); expect(terrain).toBe(original);
    await click(button('应用地形笔刷')); terrain = createEnvironmentTerrain({ size: 80 }); await act(async () => render());
    expect(button('撤销笔刷（0）').disabled).toBe(true);
  });
});
