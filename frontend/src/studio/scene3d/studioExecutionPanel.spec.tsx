// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { webcrypto } from 'node:crypto';
import { afterEach, expect, it, vi } from 'vitest';
import DirectorSourcePanel from './DirectorSourcePanel';
import type { StudioExecutionContext } from './studioExecutionContext';

let root: Root | undefined; let host: HTMLDivElement | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; host?.remove(); vi.unstubAllGlobals(); });
const scene = { id: 2, project_id: 1, title: '客栈', location: '客栈', content: '他推门停步。', director_source_stale: false };
const shot = { id: 9, scene_id: 2, order: 1, duration: 3, performance: '推门停步', action_timeline: [] };
const context = (): StudioExecutionContext => ({ revision: 'actual-resource-revision', assets: [{ id: 'actor', name: '演员', clips: [] }], objects: [] });
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
async function change(label: string, value: string) { await act(async () => { const element = host!.querySelector(`[aria-label="${label}"]`)!; Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('change', { bubbles: true })); }); await flush(); }
async function click(text: string) {
  // Wait for the actual control after async WebCrypto/source loading, not a fixed sleep.
  await vi.waitFor(async () => { await flush(); const button = [...host!.querySelectorAll('button')].find(button => button.textContent === text); expect(button).toBeTruthy(); expect(button!.disabled).toBe(false); });
  await act(async () => { [...host!.querySelectorAll('button')].find(button => button.textContent === text)!.click(); }); await flush();
}
async function mount(fetcher: typeof fetch, capture = vi.fn(async () => context())) {
  vi.stubGlobal('crypto', webcrypto); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('fetch', fetcher);
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  await act(async () => root!.render(<DirectorSourcePanel disabled={false} captureExecutionContext={capture} />)); await flush();
  await change('导演来源项目', '1'); await change('导演来源场次', '2'); await click('读取已有导演结果');
  return capture;
}
function existing(url: string) {
  if (url === '/api/projects') return response([{ id: 1, title: '小说项目' }]);
  if (url === '/api/projects/1') return response({ scenes: [scene] });
  if (url.endsWith('/shots')) return response([shot]);
  if (url.endsWith('/source')) return response({ source_hash: 'server-hash', source: { scene, shots: [shot] } });
  throw Error(`Unexpected ${url}`);
}
const planResponse = () => ({ plan: { source_hash: 'server-hash', context_revision: context().revision, blockers: ['门交互尚未绑定'] }, persisted: false, applied: false });
const planButton = '生成待审3D执行草稿（文本模型）';

it('explicit planning sends only authoritative source hash and captured resources, never writes Scene or starts media generation', async () => {
  const calls: Array<{ url: string; method: string; body?: string }> = [];
  const capture = await mount(vi.fn(async (url: any, options: any) => {
    calls.push({ url, method: options.method, body: options.body });
    return url.endsWith('/plan') ? response(planResponse()) : existing(url);
  }));
  expect(calls.every(call => call.method === 'GET')).toBe(true);
  await click(planButton);
  const post = calls.filter(call => call.method === 'POST'); expect(post).toHaveLength(1);
  expect(post[0].url).toBe('/api/projects/1/scenes/2/studio-execution/plan');
  expect(JSON.parse(post[0].body!)).toEqual({ source_hash: 'server-hash', context: context() });
  expect(capture).toHaveBeenCalledTimes(2);
  expect(host!.textContent).toContain('未应用到场景'); expect(host!.textContent).toContain('门交互尚未绑定');
});

it('discards planning results if actual resources change while the model runs', async () => {
  const capture = vi.fn().mockResolvedValueOnce(context()).mockResolvedValueOnce({ ...context(), revision: 'edited' });
  await mount(vi.fn(async (url: any) => url.endsWith('/plan') ? response(planResponse()) : existing(url)), capture);
  await click(planButton);
  expect(host!.textContent).toContain('3D场景或资源已变化'); expect(host!.querySelector('[aria-label="待审3D执行草稿"]')).toBeNull();
});

it('does not call the model if authoritative director shots differ from the reviewed source', async () => {
  let posted = false;
  await mount(vi.fn(async (url: any) => {
    if (url.endsWith('/source')) return response({ source_hash: 'new-hash', source: { scene, shots: [{ ...shot, performance: '改变后的表演' }] } });
    if (url.endsWith('/plan')) { posted = true; return response(planResponse()); }
    return existing(url);
  }));
  await click(planButton); expect(posted).toBe(false); expect(host!.textContent).toContain('导演来源已变化');
});

it('ignores late planning results after switching source and displays provider failure without fake success', async () => {
  let resolve!: (response: Response) => void;
  await mount(vi.fn(async (url: any) => url.endsWith('/plan') ? new Promise<Response>(r => { resolve = r; }) : existing(url)));
  await click(planButton); await change('导演来源场次', '');
  await act(async () => resolve(response(planResponse()))); await flush();
  expect(host!.querySelector('[aria-label="待审3D执行草稿"]')).toBeNull();
  await change('导演来源场次', '2'); await click('读取已有导演结果');
  await click(planButton); await act(async () => resolve(response({ error: '文本模型上下文超限' }, 502))); await flush();
  expect(host!.textContent).toContain('文本模型上下文超限'); expect(host!.querySelector('[aria-label="待审3D执行草稿"]')).toBeNull();
});
