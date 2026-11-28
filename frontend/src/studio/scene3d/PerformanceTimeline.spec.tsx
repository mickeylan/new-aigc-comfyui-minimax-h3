import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import Panel from './PerformanceCorrectionPanel';
import { validateScriptShotPlan } from './scriptShotPlan';
let host: HTMLDivElement, root: Root;
afterEach(async () => { await act(async () => root?.unmount()); host?.remove(); vi.unstubAllGlobals(); });
const clip = { index: 0, name: 'rest', duration: 2 };
function plan() { return validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 'intro', order: 1, start: 0, end: 10, camera: null, dialogue: [] }, { id: 'one', order: 2, start: 10, end: 14, camera: null, dialogue: [], execution: { version: 1, actors: ['a', 'b'].map(id => ({ targetId: id, targetName: id, beats: [0, 1].map(i => ({ start: i * 2, end: i * 2 + 2, from: { x: id === 'a' ? -3 : 3, z: 0 }, to: { x: id === 'a' ? -3 : 3, z: 0 }, yaw: 0, animation: clip, sourceIndex: i })) })) } }] }); }
async function select(label: string, value: string) { await act(async () => { const e = host.querySelector(`[aria-label="${label}"]`) as HTMLSelectElement; Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(e, value); e.dispatchEvent(new Event('change', { bubbles: true })); }); }
const segment = () => host.querySelector('[aria-label="选择 b 段2 2–4秒"]') as HTMLButtonElement;
async function mount() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('fetch', vi.fn(() => { throw Error('offline only'); }));
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  const props = { plan: plan(), targets: ['a', 'b'].map(id => ({ id, name: id, visible: true, locked: false, animations: [clip, { index: 1, name: 'raise', duration: 2 }] })), disabled: false, stale: false, onDirty: vi.fn(), onStage: vi.fn() };
  await act(async () => root.render(<Panel {...props} />)); await select('纠正镜头', 'one'); return props;
}
it('selects a real actor beat on a proportional local-time track without changing source or publishing', async () => {
  const props = await mount(), before = structuredClone(props.plan);
  expect(host.textContent).toContain('全片 10–14 秒'); expect(host.querySelectorAll('[aria-label^="动作轨道 "]')).toHaveLength(2);
  const b = segment(); expect(b.style.left).toBe('50%'); expect(b.style.width).toBe('50%'); expect(b.title).toContain('原段2');
  await act(async () => b.click());
  expect((host.querySelector('[aria-label="纠正角色"]') as HTMLSelectElement).value).toBe('b');
  expect((host.querySelector('[aria-label="纠正动作段"]') as HTMLSelectElement).value).toBe('1'); expect(b.getAttribute('aria-pressed')).toBe('true');
  expect(props.plan).toEqual(before); expect(props.onStage).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled();
  await select('纠正骨骼片段', '1'); expect(segment().disabled).toBe(true);
});
it.each(['busy', 'stale', 'locked', 'hidden', 'missing'])('blocks %s timeline selections', async mode => {
  const props = await mount();
  if (mode === 'busy') props.disabled = true;
  if (mode === 'stale') props.stale = true;
  if (mode === 'locked') props.targets[1].locked = true;
  if (mode === 'hidden') props.targets[1].visible = false;
  if (mode === 'missing') props.targets.pop();
  await act(async () => root.render(<Panel {...props} />)); expect(segment().disabled).toBe(true);
  await act(async () => segment().click()); expect((host.querySelector('[aria-label="纠正角色"]') as HTMLSelectElement).value).toBe(''); expect(props.onStage).not.toHaveBeenCalled();
});
it.each([0, Number.NaN])('does not render percentage bars for invalid duration %s', async duration => {
  const props = await mount();
  const changed = structuredClone(props.plan); changed.shots[1].end = changed.shots[1].start + duration;
  await act(async () => root.render(<Panel {...props} plan={changed} />)); await select('纠正镜头', 'one');
  expect(host.querySelector('[aria-label="镜内动作时间轴"]')).toBeNull();
});
async function moveCursor(value: string) { await act(async () => {
  const e = host.querySelector('[aria-label="审阅时间游标"]') as HTMLInputElement;
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(e, value); e.dispatchEvent(new Event('input', { bubbles: true }));
}); }
it('zooms shared tracks and snaps a local review cursor without applying or playing', async () => {
  const props = await mount(), before = structuredClone(props.plan);
  await select('时间轴缩放', '4'); expect(segment().parentElement!.parentElement!.parentElement!.style.width).toBe('400%');
  await select('时间游标吸附', 'tenth'); await moveCursor('1.26'); expect(host.textContent).toContain('镜内1.300秒 · 全片11.300秒');
  await select('时间游标吸附', 'beat'); await moveCursor('1.7'); expect(host.textContent).toContain('镜内2.000秒 · 全片12.000秒');
  await moveCursor('4'); expect(host.querySelector('[aria-label="游标活动动作"]')!.textContent).toContain('无活动段');
  expect(props.plan).toEqual(before); expect(props.onStage).not.toHaveBeenCalled(); expect(props.onDirty).not.toHaveBeenCalledWith(true); expect(fetch).not.toHaveBeenCalled();
  await act(async () => segment().click()); await select('纠正骨骼片段', '1');
  expect((host.querySelector('[aria-label="审阅时间游标"]') as HTMLInputElement).disabled).toBe(true);
});
it('resets review cursor and zoom when switching plans', async () => {
  const props = await mount(); await select('时间轴缩放', '8'); await moveCursor('3');
  await act(async () => root.render(<Panel {...props} plan={structuredClone(props.plan)} />)); await select('纠正镜头', 'one');
  expect((host.querySelector('[aria-label="时间轴缩放"]') as HTMLSelectElement).value).toBe('1'); expect(host.textContent).toContain('镜内0.000秒');
});
const element = (label: string) => host.querySelector(`[aria-label="${label}"]`) as HTMLInputElement;
async function fillValue(label: string, value: string) { await act(async () => {
  const node = element(label), prototype = node.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(node, value); node.dispatchEvent(new Event('input', { bubbles: true }));
}); }
const stageButton = () => [...host.querySelectorAll('button')].find(b => b.textContent === '暂存动作纠正')!;
async function chooseBoundary() {
  await select('纠正角色', 'b'); await select('纠正动作段', '0'); await act(async () => element('调整静止动作分界').click());
}
it('edits a shared stationary boundary only as an explicit staged draft with reason', async () => {
  const props = await mount(), before = structuredClone(props.plan); await chooseBoundary();
  expect(element('纠正骨骼片段').disabled).toBe(true); expect(element('拆分动作段').disabled).toBe(true);
  await fillValue('拖动静止动作分界', '3'); expect(props.onStage).not.toHaveBeenCalled(); expect(stageButton().disabled).toBe(true);
  await fillValue('人工动作纠正理由', '人工调整抬手切换时刻，保留对白');
  expect(stageButton().disabled).toBe(false); await act(async () => stageButton().click());
  expect(props.onStage).toHaveBeenCalledExactlyOnceWith({ shotId: 'one', targetId: 'b', beatIndex: 0, animation: clip, boundary: { at: 3 }, reason: '人工调整抬手切换时刻，保留对白' });
  expect(props.plan).toEqual(before); expect(fetch).not.toHaveBeenCalled(); expect(element('纠正镜头').disabled).toBe(true);
});
it.each(['', '0', '4', '2', '-1'])('blocks invalid boundary value %s', async value => {
  await mount(); await chooseBoundary(); await fillValue('静止动作新分界秒', value); await fillValue('人工动作纠正理由', '明确理由'); expect(stageButton().disabled).toBe(true);
});
it.each(['busy', 'stale', 'hidden', 'locked', 'missing'])('blocks %s boundary drafts without publishing', async mode => {
  const props = await mount(); await chooseBoundary(); await fillValue('静止动作新分界秒', '3'); await fillValue('人工动作纠正理由', '明确理由');
  if (mode === 'busy') props.disabled = true; if (mode === 'stale') props.stale = true; if (mode === 'hidden') props.targets[1].visible = false; if (mode === 'locked') props.targets[1].locked = true; if (mode === 'missing') props.targets.pop();
  await act(async () => root.render(<Panel {...props} />)); expect(stageButton().disabled).toBe(true); await act(async () => stageButton().click()); expect(props.onStage).not.toHaveBeenCalled();
});
it('does not offer boundary retiming for last beats, moving or turning pairs', async () => {
  const props = await mount(); await select('纠正角色', 'b'); await select('纠正动作段', '1'); expect(element('调整静止动作分界').disabled).toBe(true);
  const changed = structuredClone(props.plan); changed.shots[1].execution!.actors![1].beats[1].yaw = .5;
  await act(async () => root.render(<Panel {...props} plan={changed} />)); await select('纠正镜头', 'one'); await select('纠正角色', 'b'); await select('纠正动作段', '0'); expect(element('调整静止动作分界').disabled).toBe(true);
});
it('clears the track and selected beat on plan replacement', async () => {
  const props = await mount(); await act(async () => segment().click());
  await act(async () => root.render(<Panel {...props} plan={structuredClone(props.plan)} />));
  expect(host.querySelector('[aria-label="镜内动作时间轴"]')).toBeNull(); expect((host.querySelector('[aria-label="纠正动作段"]') as HTMLSelectElement).value).toBe('');
});
