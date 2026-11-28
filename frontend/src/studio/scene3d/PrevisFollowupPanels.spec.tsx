import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import * as THREE from 'three';
import CoveragePanel from './CoveragePanel';
import EventsPanel from './PrevisEventsPanel';
import { serializeEventSidecar } from './previsEventSidecar';
import type { PrevisEvent } from './previsEvents';
import { validateScriptShotPlan } from './scriptShotPlan';
let root: Root | undefined, host: HTMLDivElement;
afterEach(async () => { await act(async () => root?.unmount()); root = undefined; host?.remove(); vi.unstubAllGlobals(); });
async function mount(element: React.ReactNode) { vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); host = document.createElement('div'); document.body.append(host); root = createRoot(host); await act(async () => root!.render(element)); }
async function click(text: string) { const button = [...host.querySelectorAll('button')].find(b => b.textContent === text); expect(button).toBeDefined(); await act(async () => button!.click()); }
async function select(label: string, value: string) { await act(async () => { const element = host.querySelector(`[aria-label="${label}"]`) as HTMLSelectElement; Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('change', { bubbles: true })); }); }
it('requires a separate camera adoption and rejects scene revision changes', async () => {
  const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial());
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: 'h', location: 'l', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 2, dialogue: [], camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 50 }, motion: { targetId: 'p', targetName: 'p', start: { x: 0, z: 0 }, end: { x: 1, z: 0 }, duration: 2 } }] });
  let revision = 'a'; const apply = vi.fn(), capture = () => ({ revision, aspect: 1, entries: [{ id: 'p', name: 'p', object }] });
  await mount(<CoveragePanel plan={plan} disabled={false} capture={capture} apply={apply} />);
  await select('候选镜头', 's'); await click('生成三个机位候选（不应用）'); expect(apply).not.toHaveBeenCalled();
  revision = 'b'; await click('确认采用候选1机位（保留动作和对白）'); expect(apply).not.toHaveBeenCalled(); expect(host.textContent).toContain('场景或计划已变化');
  await click('生成三个机位候选（不应用）'); await click('确认采用候选1机位（保留动作和对白）'); expect(apply).toHaveBeenCalledTimes(1); expect(apply.mock.calls[0][0]).toBe('s');
});
it.each(['complete', 'unsupported', 'stale', 'busy'])('keeps D2 static ray diagnosis separate from camera adoption: %s', async mode => {
  const actor = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial()), wall = new THREE.Mesh(new THREE.BoxGeometry(10, 10, .1), new THREE.MeshBasicMaterial()); wall.position.z = 1;
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 2, dialogue: [], camera: { position: [0, 3, 8], target: [0, 0, 0], fov: 50 }, motion: { targetId: 'p', targetName: 'p', start: { x: 0, z: 0 }, end: { x: 1, z: 0 }, duration: 2 } }] });
  let revision = 'a'; const apply = vi.fn(), capture = () => ({ revision, aspect: 1, entries: [{ id: 'p', name: 'p', object: actor }, { id: 'wall', name: 'wall', object: wall }] });
  const before = JSON.stringify(plan);
  await mount(<CoveragePanel plan={plan} disabled={false} capture={capture} apply={apply} />);
  await select('候选镜头', 's'); await click('生成三个机位候选（不应用）');
  expect(host.querySelector('[aria-label="候选1静态遮挡诊断"]')).toBeNull();
  if (mode === 'unsupported') wall.material.transparent = true;
  if (mode === 'stale') revision = 'b';
  await click('检查候选静态遮挡射线（不应用）');
  expect(apply).not.toHaveBeenCalled(); expect(JSON.stringify(plan)).toBe(before);
  if (mode === 'stale') { expect(host.textContent).toContain('场景或计划已变化'); expect(host.querySelector('[aria-label="候选1静态遮挡诊断"]')).toBeNull(); }
  else if (mode === 'unsupported') expect(host.textContent).toContain('静态遮挡诊断不可用');
  else { expect(host.querySelector('[aria-label="候选1静态遮挡诊断"]')?.textContent).toContain('18/18条命中'); expect(host.textContent).toContain('动态/像素遮挡未验证'); }
  if (mode === 'busy') { await act(async () => root!.render(<CoveragePanel plan={plan} disabled capture={capture} apply={apply} />)); expect(host.querySelector('[aria-label="候选1静态遮挡诊断"]')).toBeNull(); }
});
it.each(['complete', 'stale', 'switch', 'busy', 'failure'])('keeps D3 results revision-bound and never adopts cameras: %s', async mode => {
  const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshBasicMaterial());
  const plan = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's', order: 1, start: 0, end: 2, dialogue: [], camera: { position: [0,3,8], target: [0,0,0], fov: 50 }, motion: { targetId: 'p', targetName: 'p', start: { x:0,z:0 }, end: { x:1,z:0 }, duration: 2 } }] });
  let revision = 'a', finish!: (value: any) => void, fail!: (e: Error) => void;
  const apply = vi.fn(), capture = () => ({ revision, aspect: 1, entries: [{ id:'p',name:'p',object }] });
  const diagnosePosed = vi.fn(() => new Promise<any>((resolve, reject) => { finish=resolve; fail=reject; }));
  const props = { plan, capture, apply, diagnosePosed, disabled: false };
  await mount(<CoveragePanel {...props} />); await select('候选镜头','s'); await click('生成三个机位候选（不应用）'); await click('检查候选实际骨姿主体互挡（不应用）');
  expect(diagnosePosed).toHaveBeenCalledTimes(1); expect(host.querySelector<HTMLSelectElement>('select')!.disabled).toBe(true); expect(apply).not.toHaveBeenCalled();
  if(mode==='stale') revision='b';
  if(mode==='switch') await act(async()=>root!.render(<CoveragePanel {...props} plan={structuredClone(plan)} />));
  if(mode==='busy') await act(async()=>root!.render(<CoveragePanel {...props} disabled />));
  await act(async()=> { if(mode==='failure') fail(Error('unsupported subject')); else finish([0,1,2].map(()=>({policy:'posed-subject-aabb-v1',visuallyApproved:false,frames:[{seconds:1,rays:9,blockedRays:0,blockers:[]}]}))); });
  expect(apply).not.toHaveBeenCalled();
  if(mode==='complete') expect(host.querySelector('[aria-label="候选1实际骨姿主体互挡"]')?.textContent).toContain('非无遮挡通过');
  else expect(host.querySelector('[aria-label="候选1实际骨姿主体互挡"]')).toBeNull();
  if(mode==='failure') expect(host.textContent).toContain('unsupported subject');
  if(mode==='stale') expect(host.textContent).toContain('诊断期间场景已变化');
});
it('reads real choices and writes only on explicit event confirmation', async () => {
  const write = vi.fn(); await mount(<EventsPanel disabled={false} sceneKey="s" list={() => [{ id: 'prop', name: '道具', bones: [] }]} read={() => []} write={write} preview={vi.fn()} />);
  await click('读取现有对象和骨骼'); expect(write).not.toHaveBeenCalled(); await select('事件类型', 'fire'); await select('事件对象', 'prop'); await click('确认追加临时事件');
  expect(write).toHaveBeenCalledExactlyOnceWith([expect.objectContaining({ kind: 'fire', objectId: 'prop', start: 0, end: 1 })]); expect(host.textContent).toContain('不随存档保存');
});
async function pasteSidecar(text: string) { await act(async () => {
  const element = host.querySelector('[aria-label="待导入事件旁档JSON"]') as HTMLTextAreaElement;
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(element, text);
  element.dispatchEvent(new Event('input', { bubbles: true }));
}); }
const importedEvents = (): PrevisEvent[] => [{ kind: 'fire', objectId: 'prop', start: 0, end: 1, offset: [0, 0, 0], rotation: [0, 0, 0], velocity: [0, 0, 0], scale: 1 }];
it('only replaces events after separate sidecar preview and confirmation', async () => {
  const write = vi.fn(); await mount(<EventsPanel disabled={false} sceneKey="s" list={() => []} read={() => []} write={write} preview={vi.fn()} sidecarSource={() => 'source'} />);
  await pasteSidecar(serializeEventSidecar('source', importedEvents())); await click('预览旁档导入（不应用）');
  expect(write).not.toHaveBeenCalled(); expect(host.textContent).toContain('待确认整体替换：1事件');
  await click('确认替换为旁档事件'); expect(write).toHaveBeenCalledExactlyOnceWith(importedEvents());
});
it.each(['source', 'events'])('rejects stale sidecar confirmation after %s changes', async mode => {
  let source = 'source', current: PrevisEvent[] = []; const write = vi.fn();
  await mount(<EventsPanel disabled={false} sceneKey="s" list={() => []} read={() => current} write={write} preview={vi.fn()} sidecarSource={() => source} />);
  await pasteSidecar(serializeEventSidecar(source, importedEvents())); await click('预览旁档导入（不应用）');
  if (mode === 'source') source = 'changed'; else current = importedEvents();
  await click('确认替换为旁档事件'); expect(write).not.toHaveBeenCalled(); expect(host.textContent).toContain('请重新预览旁档');
});
it('rejects invalid JSON without writing and clears pending import on scene switch', async () => {
  const write = vi.fn(), props = { disabled: false, list: () => [], read: () => [], write, preview: vi.fn(), sidecarSource: () => 'source' };
  await mount(<EventsPanel {...props} sceneKey="s" />);
  await pasteSidecar('{'); await click('预览旁档导入（不应用）'); expect(write).not.toHaveBeenCalled();
  await pasteSidecar(serializeEventSidecar('source', importedEvents())); await click('预览旁档导入（不应用）');
  await act(async () => root!.render(<EventsPanel {...props} sceneKey="new" />));
  expect(host.textContent).not.toContain('确认替换为旁档事件'); expect((host.querySelector('textarea') as HTMLTextAreaElement).value).toBe(''); expect(write).not.toHaveBeenCalled();
});
it('ignores a late preview after unmount without creating a leaked image URL', async () => {
  let finish!: (blob: Blob) => void; const preview = vi.fn(() => new Promise<Blob>(resolve => { finish = resolve; })), createURL = vi.fn();
  vi.stubGlobal('URL', { createObjectURL: createURL, revokeObjectURL: vi.fn() });
  await mount(<EventsPanel disabled={false} sceneKey="s" list={() => []} read={() => []} write={vi.fn()} preview={preview} />);
  await click('试演该时刻并取真实视口图（随后还原）'); await act(async () => root!.unmount()); root = undefined;
  await act(async () => { finish(new Blob(['png'])); await Promise.resolve(); }); expect(createURL).not.toHaveBeenCalled();
});
