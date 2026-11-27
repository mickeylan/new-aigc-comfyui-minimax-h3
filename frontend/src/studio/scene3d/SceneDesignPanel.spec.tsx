import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import SceneDesignPanel from './SceneDesignPanel';
import type { SceneDesignHost, SceneDesignPlan, SceneDesignSnapshot } from './sceneDesign';

let element: HTMLDivElement, root: Root | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; element?.remove(); vi.unstubAllGlobals(); });
const plan = (): SceneDesignPlan => ({ context_revision: 'r', summary: '设计古镇渡口，中央留出通道', questions: [], asset_queries: [], template: { id: 'dock', position: [0, 0, 0], yaw: 0 }, objects: [], remove_ids: [], facts: ['古镇渡口'], assumptions: ['中央留出通道'], limitations: ['仅静态白模'] });
const snapshot = (): SceneDesignSnapshot => ({ sceneKey: 'one', context: { revision: 'r', assets: [], objects: [] } });
const button = (text: string) => [...element.querySelectorAll('button')].find(button => button.textContent === text)!;
async function click(text: string) { expect(button(text)).toBeTruthy(); await act(async () => button(text).click()); }
async function input(value: string) { await act(async () => { const target = element.querySelector('textarea')!; Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(target, value); target.dispatchEvent(new Event('input', { bubbles: true })); }); }
async function mount(request = vi.fn().mockResolvedValue({ plan: plan(), snapshot: snapshot() })) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('fetch', vi.fn(() => { throw Error('no network in panel tests'); }));
  const host: SceneDesignHost = { capture: vi.fn().mockResolvedValue(snapshot()), apply: vi.fn().mockResolvedValue({ added: 30, updated: 0, removed: 0, preserved: 0 }) };
  const props = { disabled: false, sceneKey: 'one', host, request };
  element = document.createElement('div'); document.body.append(element); root = createRoot(element);
  await act(async () => root!.render(<SceneDesignPanel {...props} />));
  return { host, request, props };
}
it('one intent produces proposal with no manual coordinates/templates; one confirmation applies whole design', async () => {
  const { host, request } = await mount();
  expect(element.querySelectorAll('select,input[type=number]')).toHaveLength(0);
  await input('古镇渡口，两人交谈'); await click('让AI设计');
  expect(request).toHaveBeenCalledTimes(1); expect(host.apply).not.toHaveBeenCalled();
  expect(element.textContent).toContain('用户原话：古镇渡口'); expect(element.textContent).toContain('AI设计补充：中央留出通道');
  expect(element.textContent).toContain('尚无AI视口视觉自检');
  expect(element.textContent).toContain('不带模板演示群演');
  expect(element.textContent).toContain('已有人工人物不会因此删除');
  await click('应用AI设计'); expect(host.apply).toHaveBeenCalledTimes(1); expect(element.textContent).toContain('实际应用：新增30');
});
it('continuous dialogue carries history and current scene instead of forcing manual edits', async () => {
  const { host, request } = await mount(); await input('古镇渡口'); await click('让AI设计'); await click('应用AI设计');
  await input('柜台往后移，其他不动'); await click('让AI设计');
  expect(request).toHaveBeenCalledTimes(2);
  const history = request.mock.calls[1][1]; expect(history.some((m: any) => m.content.includes('古镇渡口'))).toBe(true); expect(history.some((m: any) => m.content.includes('实际应用'))).toBe(true);
  expect(host.apply).toHaveBeenCalledTimes(1);
});
it('asks only model-returned clarification; does not offer apply for questions', async () => {
  const p = plan(); p.template = null; p.questions = ['保留哪一位角色？'];
  await mount(vi.fn().mockResolvedValue({ plan: p, snapshot: snapshot() })); await input('换掉这个人'); await click('让AI设计');
  expect(element.textContent).toContain('保留哪一位角色'); expect(button('应用AI设计')).toBeUndefined();
});
it('cancelled late model output cannot produce proposal or write scene', async () => {
  let resolve!: (value: any) => void; const waiting = new Promise(resolveValue => { resolve = resolveValue; });
  const { host, request } = await mount(vi.fn().mockReturnValue(waiting)); await input('古镇渡口'); await click('让AI设计'); await click('停止本轮');
  await act(async () => { resolve({ plan: plan(), snapshot: snapshot() }); await waiting; });
  expect(request.mock.calls[0][3].aborted).toBe(true); expect(host.apply).not.toHaveBeenCalled(); expect(button('应用AI设计')).toBeUndefined(); expect(element.textContent).toContain('已停止');
});
it('new scene invalidates conversation/proposal, collapsing does not lose work, new conversation retains scene', async () => {
  const { props, host } = await mount(); await input('古镇渡口'); await click('让AI设计');
  await click('AI设计场景 收起'); expect(button('应用AI设计')).toBeUndefined(); await click('AI设计场景 展开'); expect(button('应用AI设计')).toBeTruthy();
  await act(async () => root!.render(<SceneDesignPanel {...props} sceneKey="two" />)); expect(element.textContent).not.toContain('设计古镇渡口');
  await click('新建会话'); expect(host.apply).not.toHaveBeenCalled(); expect(element.textContent).toContain('当前场景保留');
});
it('errors preserve user dialogue and never claim application success', async () => {
  const { host } = await mount(vi.fn().mockRejectedValue(Error('服务未配置'))); await input('古镇渡口'); await click('让AI设计');
  expect(element.querySelector('[role=alert]')?.textContent).toContain('服务未配置'); expect(element.textContent).toContain('古镇渡口'); expect(host.apply).not.toHaveBeenCalled(); expect(button('应用AI设计')).toBeUndefined();
});
it('apply failure retains proposal, does not clear safety errors or claim completed design', async () => {
  const { host } = await mount(); (host.apply as any).mockRejectedValue(Error('场景已切换，未应用'));
  await input('古镇渡口'); await click('让AI设计'); await click('应用AI设计');
  expect(element.textContent).toContain('场景已切换'); expect(element.textContent).not.toContain('实际应用：'); expect(button('应用AI设计')).toBeTruthy();
});
