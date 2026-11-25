// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { webcrypto } from 'node:crypto';
import { afterEach, expect, it, vi } from 'vitest';
import DirectorSourcePanel, { type ApplyDirectorExecution } from './DirectorSourcePanel';
let host: HTMLDivElement; let root: Root | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; host?.remove(); vi.unstubAllGlobals(); });
const scene = { id: 2, project_id: 1, title: '院落', location: '院落', content: '院中一堵墙。', director_source_stale: false };
const shots = [{ id: 9, scene_id: 2, order: 1, duration: 3, action_timeline: [] }];
const plan = { source_hash: 'hash', context_revision: 'resources', blockers: [] };
async function ready(assertion: () => void) { await vi.waitFor(async () => { await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); }); assertion(); }); }
function button(label: string) { return [...host.querySelectorAll('button')].find(item => item.textContent === label)!; }
async function click(label: string) { await ready(() => { expect(button(label)).toBeTruthy(); expect(button(label).disabled).toBe(false); }); await act(async () => button(label).click()); }
async function change(label: string, value: string) { await act(async () => { const select = host.querySelector(`[aria-label="${label}"]`)!; Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(select, value); select.dispatchEvent(new Event('change', { bubbles: true })); }); }
async function mount(apply: ApplyDirectorExecution) {
  vi.stubGlobal('crypto', webcrypto); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  const fetcher = vi.fn(async (url: string) => {
    const body = url === '/api/projects' ? [{ id: 1, title: '小说' }] : url === '/api/projects/1' ? { scenes: [scene] } : url.endsWith('/shots') ? shots : url.endsWith('/source') ? { source_hash: 'hash', source: { scene, shots } } : { plan, persisted: false, applied: false };
    return new Response(JSON.stringify(body));
  }); vi.stubGlobal('fetch', fetcher);
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  await act(async () => root!.render(<DirectorSourcePanel disabled={false} captureExecutionContext={async () => ({ revision: 'resources', assets: [], objects: [] })} onApplyExecution={apply} />));
  await ready(() => expect(host.querySelector('[aria-label="导演来源项目"]')!.textContent).toContain('小说'));
  await change('导演来源项目', '1'); await ready(() => expect(host.querySelector('[aria-label="导演来源场次"]')!.textContent).toContain('院落'));
  await change('导演来源场次', '2'); await click('读取已有导演结果'); await click('生成待审3D执行草稿（文本模型）');
  await ready(() => expect(host.querySelector('[aria-label="待审3D执行草稿"]')).not.toBeNull()); return fetcher;
}
async function acknowledge() { await act(async () => (host.querySelector('[aria-label="确认应用导演执行草稿"]') as HTMLInputElement).click()); }

it('requires a separate explicit confirmation, applies once, and does not automatically play/save/export', async () => {
  const apply = vi.fn(async () => {}); const fetcher = await mount(apply);
  expect(apply).not.toHaveBeenCalled(); expect(button('确认应用到3D编辑场景').disabled).toBe(true);
  await acknowledge(); await click('确认应用到3D编辑场景');
  expect(apply).toHaveBeenCalledTimes(1); expect(apply.mock.calls[0][0]).toEqual(plan);
  expect(host.textContent).toContain('已确认应用到编辑场景'); expect(host.textContent).toContain('未自动播放、保存或导出');
  expect(button('确认应用到3D编辑场景')).toBeUndefined(); expect(fetcher).toHaveBeenCalledTimes(7);
});

it('retains the draft and clears confirmation on refused application, without claiming success', async () => {
  const apply = vi.fn(async () => { throw Error('执行草稿存在阻塞：权威对白未绑定'); }); await mount(apply);
  await acknowledge(); await click('确认应用到3D编辑场景');
  expect(host.textContent).toContain('权威对白未绑定'); expect(host.textContent).not.toContain('已确认应用到编辑场景');
  expect(button('确认应用到3D编辑场景').disabled).toBe(true); expect(host.querySelector('[aria-label="待审3D执行草稿"]')).not.toBeNull();
});

it('source selection changes invalidate a pending application and ignore its late completion', async () => {
  let resolve!: () => void; let current!: () => boolean;
  await mount(async (_plan, _source, isCurrent) => { current = isCurrent; await new Promise<void>(done => { resolve = done; }); });
  await acknowledge(); await click('确认应用到3D编辑场景'); expect(current()).toBe(true);
  await change('导演来源场次', ''); expect(current()).toBe(false);
  await act(async () => resolve()); expect(host.textContent).not.toContain('已确认应用到编辑场景');
  expect(host.querySelector('[aria-label="待审3D执行草稿"]')).toBeNull();
});
