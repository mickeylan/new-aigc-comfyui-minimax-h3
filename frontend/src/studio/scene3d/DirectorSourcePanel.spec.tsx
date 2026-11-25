// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { webcrypto } from 'node:crypto';
import { afterEach, expect, it, vi } from 'vitest';
import DirectorSourcePanel from './DirectorSourcePanel';

let root: Root | undefined; let host: HTMLDivElement | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; host?.remove(); vi.unstubAllGlobals(); });
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 15)); });
const sourceScene = { id: 2, project_id: 1, title: '门口', content: '他推门，停步。', location: '客栈', director_source_stale: false };
const sourceShot = { id: 9, scene_id: 2, order: 1, duration: 3, performance: '推门后停步', dialogue: '', action_timeline: [] };
async function mount(fetcher: typeof fetch) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('crypto', webcrypto); vi.stubGlobal('fetch', fetcher);
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  await act(async () => root!.render(<DirectorSourcePanel disabled={false} />)); await flush();
}
async function choose(label: string, value: string) {
  await act(async () => { const element = host!.querySelector(`[aria-label="${label}"]`)!; Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('change', { bubbles: true })); }); await flush();
}
async function read() { await act(async () => host!.querySelector('button')!.click()); await flush(); }
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });

it('requires explicit selection and reads original director data only through GET, never a model or generation endpoint', async () => {
  const calls: Array<{ url: string; method: string }> = [];
  await mount(vi.fn(async (url: any, options: any) => {
    calls.push({ url, method: options.method });
    if (url === '/api/projects') return response([{ id: 1, title: '已有小说项目' }]);
    if (url === '/api/projects/1') return response({ scenes: [sourceScene] });
    if (url === '/api/projects/1/scenes/2/shots') return response([sourceShot]);
    throw Error('unexpected endpoint');
  }));
  expect(host!.querySelector('button')!.disabled).toBe(true);
  await choose('导演来源项目', '1'); expect(host!.querySelector('button')!.disabled).toBe(true);
  await choose('导演来源场次', '2'); await read();
  expect(host!.textContent).toContain('已读取 1 镜');
  expect(host!.textContent).toContain('推门后停步');
  expect(host!.textContent).toContain('尚未绑定');
  expect(calls.every(call => call.method === 'GET')).toBe(true);
  expect(calls.filter(call => call.url.endsWith('/shots'))).toHaveLength(1);
  await choose('导演来源场次', ''); expect(host!.querySelector('[aria-label="导演来源快照"]')).toBeNull();
});

it('ignores late reads after changing source even if a server ignores AbortSignal', async () => {
  let resolveShots!: (value: Response) => void;
  await mount(vi.fn(async (url: any) => {
    if (url === '/api/projects') return response([{ id: 1, title: '项目' }]);
    if (url.endsWith('/shots')) return new Promise<Response>(resolve => { resolveShots = resolve; });
    return response({ scenes: [sourceScene] });
  }));
  await choose('导演来源项目', '1'); await choose('导演来源场次', '2'); await read();
  await choose('导演来源项目', '');
  await act(async () => resolveShots(response([sourceShot]))); await flush();
  expect(host!.querySelector('[aria-label="导演来源快照"]')).toBeNull();
  expect(host!.querySelector('[role="alert"]')).toBeNull();
});

it('surfaces backend errors and stale director sources, never reports success', async () => {
  let mode = 'error';
  await mount(vi.fn(async (url: any) => {
    if (url === '/api/projects') return response([{ id: 1, title: '项目' }]);
    if (url.endsWith('/shots')) return mode === 'error' ? response({ error: '镜头读取失败' }, 503) : response([sourceShot]);
    return response({ scenes: [{ ...sourceScene, director_source_stale: mode === 'stale' }] });
  }));
  await choose('导演来源项目', '1'); await choose('导演来源场次', '2'); await read();
  expect(host!.textContent).toContain('镜头读取失败'); expect(host!.textContent).not.toContain('已读取 1 镜');
  mode = 'stale'; await read();
  expect(host!.textContent).toContain('导演来源已过期'); expect(host!.querySelector('[aria-label="导演来源快照"]')).toBeNull();
});

it('rejects a scene changed during the separate API reads', async () => {
  let reads = 0;
  await mount(vi.fn(async (url: any) => {
    if (url === '/api/projects') return response([{ id: 1, title: '项目' }]);
    if (url.endsWith('/shots')) return response([sourceShot]);
    reads++; return response({ scenes: [{ ...sourceScene, content: reads >= 3 ? '读取期间被修改的正文' : sourceScene.content }] });
  }));
  await choose('导演来源项目', '1'); await choose('导演来源场次', '2'); await read();
  expect(host!.textContent).toContain('读取期间场次已变化'); expect(host!.querySelector('[aria-label="导演来源快照"]')).toBeNull();
});
