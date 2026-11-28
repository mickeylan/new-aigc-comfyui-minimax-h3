import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import Panel, { type ScriptVideoArtifact } from './ScriptVideoPanel';
import { validateScriptShotPlan } from './scriptShotPlan';
import './previsVideoDelivery';
let root: Root, host: HTMLDivElement;
afterEach(async () => { await act(async () => root?.unmount()); host?.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const button = (text: string) => [...host.querySelectorAll('button')].find(b => b.textContent === text)!;
const label = '下载统一视频交付ZIP（WebM＋镜头表/EDL，无音频）';
async function mount(fps = 24) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('fetch', vi.fn());
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:test'), revokeObjectURL: vi.fn() });
  const anchor = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 1, camera: null, dialogue: [] }] });
  const artifact: ScriptVideoArtifact = { blob: new Blob(['video']), source: 'src', epoch: 1, projectId: null, manifest: { kind: 'studio-script-shot-video', fps, container: 'webm', sourceDurationMs: 1000, durationMs: 1000, frameCount: 24, width: 832, height: 480, shots: plan } };
  const onExport = vi.fn(async () => artifact), current = { value: true };
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  await act(async () => root.render(<Panel plan={plan} disabled={false} onExport={onExport} isCurrent={() => current.value} />));
  await act(async () => (host.querySelector('input[type="checkbox"]') as HTMLElement).click());
  await act(async () => button('渲染逐镜视频').click());
  return { artifact, anchor, onExport, current };
}
it('downloads only on explicit packaging and reuses the existing video without upload or rerender', async () => {
  const { anchor, onExport } = await mount(); expect(anchor).not.toHaveBeenCalled();
  await act(async () => { button(label).click(); await new Promise(resolve => setTimeout(resolve, 30)); });
  expect(anchor).toHaveBeenCalledOnce(); expect((anchor.mock.instances[0] as HTMLAnchorElement).download).toBe('previs-video-delivery.zip');
  expect(onExport).toHaveBeenCalledOnce(); expect(fetch).not.toHaveBeenCalled();
});
it('does not download a cancelled package after asynchronous blob reading', async () => {
  const { artifact, anchor } = await mount(); let finish!: (data: ArrayBuffer) => void;
  vi.spyOn(artifact.blob, 'arrayBuffer').mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  await act(async () => { button(label).click(); await new Promise(resolve => setTimeout(resolve, 30)); });
  expect(finish).toBeDefined(); await act(async () => button('取消视频导出').click());
  await act(async () => { finish(new ArrayBuffer(5)); }); expect(anchor).not.toHaveBeenCalled(); expect(host.textContent).toContain('取消'); expect(fetch).not.toHaveBeenCalled();
});
it('leaves non-24fps video individually downloadable but disables the EDL bundle', async () => {
  await mount(12); expect(button(label).disabled).toBe(true); expect(host.querySelector('a[download="studio-shot-previs.webm"]')).not.toBeNull();
});
