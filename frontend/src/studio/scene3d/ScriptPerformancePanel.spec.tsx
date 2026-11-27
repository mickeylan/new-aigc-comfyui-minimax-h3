import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import * as THREE from 'three';
import SetTemplatePanel from './SetTemplatePanel';
import { captureScriptPerformanceContext } from './scriptPerformance';
import { assertScriptShotApplicationSafe } from './scriptShotSampler';
import { validateScriptShotPlan } from './scriptShotPlan';

vi.mock('./toonflowSets', async importOriginal => {
  const real = await importOriginal<typeof import('./toonflowSets')>();
  return { ...real, listSetTemplates: () => [{ id: 'ancientStreet', name: '古街', genre: '外景', width: 40, depth: 40 }],
    buildScriptSceneDraft: async (index: any, id: string) => {
      const scene = real.selectScriptScene(index, id);
      return { version: 1, requiresConfirmation: true, sceneId: id, templateId: 'ancientStreet', templateName: '古街', recommendation: { matchedBy: 'manual' },
        setDraft: { version: 1, templateId: 'ancientStreet', name: '古街', document: { objects: [{ id: 'remote-prop', name: '远处道具', position: { x: 100, y: 0, z: 100 }, rotation: { x: 0, y: 0, z: 0 }, scale: { x: 1, y: 1, z: 1 }, visible: true, color: '#ccc', asset: { id: 'sphere', kind: 'primitive', shape: 'sphere' } }] } },
        review: { ...scene, instanceCount: 1, diagnostics: [], conflicts: [], sourceFingerprint: index.fingerprint,
          sceneFingerprint: real.scriptFingerprint(`${scene.heading}\n${scene.action.join('\n')}\n${scene.dialogue.map((l: any) => `${l.speaker}:${l.line}`).join('\n')}`) } };
    } };
});
// Load the real catalog during collection, before any mounted React act scope.
await import('./toonflowSets');
let host: HTMLDivElement, root: Root | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; host?.remove(); vi.unstubAllGlobals(); });
const button = (text: string) => [...host.querySelectorAll('button')].find(b => b.textContent === text)!;
async function click(text: string) { const b = button(text); expect(b, text).toBeTruthy(); await act(async () => b.click()); }
async function check(label: string) { await act(async () => (host.querySelector(`[aria-label="${label}"]`) as HTMLInputElement).click()); }
async function input(label: string, value: string) {
  await act(async () => {
    const e = host.querySelector(`[aria-label="${label}"]`)!; expect(e, label).toBeTruthy();
    const p = e instanceof HTMLSelectElement ? HTMLSelectElement.prototype : e instanceof HTMLInputElement ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype;
    Object.getOwnPropertyDescriptor(p, 'value')!.set!.call(e, value); e.dispatchEvent(new Event(e instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
}
const source = '1. 外景 古街 日\n张三：等一等。\n（张三走到李四面前。）\n李四：请说。\n（张三抓住李四手腕。）';
async function mount() {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('fetch', vi.fn(() => { throw Error('local staging must not fetch'); }));
  const entries: { id: string; name: string; object: THREE.Object3D }[] = [];
  const apply = vi.fn((plan: any) => { assertScriptShotApplicationSafe(plan, entries, null); });
  const append = vi.fn((_draft: any, additions: any[], x: number, z: number) => { additions.forEach(e => { e.object.position.x += x; e.object.position.z += z; entries.push(e); }); });
  const props = { disabled: false, onApply: append, onApplyShots: apply, onCaptureCamera: () => ({ position: [0, 4, 20] as [number, number, number], target: [0, 1, 0] as [number, number, number], fov: 45 }),
    listMotionTargets: () => captureScriptPerformanceContext(entries).targets, capturePerformanceContext: () => captureScriptPerformanceContext(entries) };
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  await act(async () => root!.render(<SetTemplatePanel {...props} />));
  await vi.waitFor(() => expect(button('解析剧本场次').disabled).toBe(false));
  await input('剧本原文', source); await click('解析剧本场次'); await check('场次 scene-1'); await click('生成该场草稿');
  return { entries, apply, append, props };
}
async function insertCast() {
  await click('生成演员站位建议');
  expect((host.querySelector('[aria-label="角色 张三 绑定"]') as HTMLSelectElement).value).toBe('skeletal');
  await check('确认场次与模板'); await click('确认追加该场布景');
}
async function chooseClips() {
  await click('读取已确认演员');
  for (const name of ['张三', '李四']) for (const [label, clip] of [['待机', 'cc0Idle'], ['对白', 'cc0Talk'], ['行走', 'cc0Walk']]) {
    const select = host.querySelector(`[aria-label="${name} ${label}片段"]`) as HTMLSelectElement;
    const option = [...select.options].find(o => o.textContent?.includes(clip))!;
    await input(`${name} ${label}片段`, option.value);
  }
}
it('keeps suggested cast editable and unapproved; records confirmed IDs, stages actual beats and requires a second approval', async () => {
  const f = await mount(); await check('确认场次与模板'); await click('生成演员站位建议');
  expect((host.querySelector('[aria-label="确认场次与模板"]') as HTMLInputElement).checked).toBe(false);
  expect(f.append).not.toHaveBeenCalled(); expect(host.textContent).toContain('不是原文事实');
  await input('角色 张三 站位X', '-6'); await check('确认场次与模板'); await click('确认追加该场布景');
  expect(f.entries.filter(e => ['张三', '李四'].includes(e.name))).toHaveLength(2); expect(f.apply).not.toHaveBeenCalled();
  await chooseClips(); await click('生成对白与走位表演草稿');
  expect(host.textContent).toContain('1 行动作未覆盖'); expect(host.textContent).toContain('张三抓住李四手腕');
  expect(button('确认应用镜头表').disabled).toBe(true); expect(f.apply).not.toHaveBeenCalled();
  await check('确认镜头表'); await click('确认应用镜头表');
  expect(f.apply).toHaveBeenCalledTimes(1); const plan = f.apply.mock.calls[0][0];
  expect(plan.shots).toHaveLength(3); expect(plan.shots[0].execution.actors[0].targetId).toBe(f.entries.find(e => e.name === '张三')!.id);
  expect(plan.shots[0].execution.actors[0].beats[0].from.x).toBe(-6);
  expect(validateScriptShotPlan(JSON.parse(JSON.stringify(plan))).shots).toEqual(plan.shots); expect(fetch).not.toHaveBeenCalled();
});
it('uses inserted world offsets and never silently picks clip zero or an actor by name', async () => {
  const f = await mount(); await input('剧本布景中心X', '10'); await input('剧本布景中心Z', '20'); await insertCast();
  await click('读取已确认演员'); expect(button('生成对白与走位表演草稿').disabled).toBe(true);
  expect((host.querySelector('[aria-label="张三 对白片段"]') as HTMLSelectElement).value).toBe('');
  await chooseClips(); await click('生成对白与走位表演草稿'); await check('确认镜头表'); await click('确认应用镜头表');
  expect(f.apply.mock.calls[0][0].shots[0].execution.actors[0].beats[0].from).toEqual({ x: 6, z: 20 });
});
it.each(['position', 'lock', 'clip', 'archive'])('rejects stale prepared actors before final application: %s', async mode => {
  const f = await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿');
  const actor = f.entries.find(e => e.name === '张三')!;
  if (mode === 'position') actor.object.position.x += 1;
  if (mode === 'lock') (actor as any).locked = true;
  if (mode === 'clip') actor.object.animations[0].duration += 1;
  if (mode === 'archive') f.entries.splice(0);
  await check('确认镜头表'); await click('确认应用镜头表');
  expect(f.apply).not.toHaveBeenCalled(); expect(host.textContent).toContain('场景或演员已变化');
});
it('blocks publication after geometry rejection and does not claim cast-plus-performance atomicity', async () => {
  const f = await mount(); await insertCast();
  const wall = new THREE.Mesh(new THREE.BoxGeometry(1, 3, 1)); wall.position.set(-2, 1.5, 0); f.entries.push({ id: 'wall', name: '墙', object: wall });
  await chooseClips(); await click('生成对白与走位表演草稿'); await check('确认镜头表'); await click('确认应用镜头表');
  expect(host.textContent).toContain('墙'); expect(host.textContent).not.toContain('已接入 3 镜');
  expect(f.append).toHaveBeenCalledTimes(1); expect(f.entries.some(e => e.name === '张三')).toBe(true);
});
it('revokes staging approval when settings change, allows discard, and clears mappings on source change', async () => {
  const f = await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿'); await check('确认镜头表');
  await input('表演走位速度', '1'); expect(button('确认应用镜头表').disabled).toBe(true);
  await click('放弃表演参数修改'); expect((host.querySelector('[aria-label="确认镜头表"]') as HTMLInputElement).checked).toBe(false);
  await input('剧本原文', source + '\n李四：改了。'); expect(button('读取已确认演员')).toBeUndefined(); expect(button('确认应用镜头表')).toBeUndefined();
  expect(f.apply).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled();
});
it('still allows recording cameras and correcting the proposed beats without dropping baseline protection', async () => {
  const f = await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿');
  await act(async () => (host.querySelector('[aria-label="用当前视口机位 performance-1"]') as HTMLButtonElement).click());
  await check('确认镜头表'); await click('确认应用镜头表');
  expect(f.apply).toHaveBeenCalledTimes(1); expect(f.apply.mock.calls[0][0].shots[0].camera.fov).toBe(45);
});
it('shows readable original-line gaps and chronology instead of a wall of audit JSON', async () => {
  await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿');
  const review = host.querySelector('[aria-label="表演原文覆盖审阅"]')!;
  expect(review).toBeTruthy(); expect(review.textContent).toContain('未覆盖 · 原文第 5 行：');
  expect(review.textContent).toContain('原文第 3 行 · 走位 → performance-2');
  expect(review.querySelectorAll('details')).toHaveLength(2);
  expect([...review.querySelectorAll('details')].every(e => !e.open)).toBe(true);
});
it('revokes confirmation across disabled playback rather than reviving an old checked submission', async () => {
  const f = await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿'); await check('确认镜头表');
  await act(async () => root!.render(<SetTemplatePanel {...f.props} disabled />));
  expect(button('确认应用镜头表').disabled).toBe(true); await click('确认应用镜头表');
  expect(f.apply).not.toHaveBeenCalled();
  await act(async () => root!.render(<SetTemplatePanel {...f.props} />));
  expect(button('确认应用镜头表').disabled).toBe(true);
});
it('does not keep usable confirmed actor mappings after static insertion fails', async () => {
  const f = await mount(); f.append.mockImplementationOnce(() => { throw Error('静态布景提交失败'); }); await insertCast();
  expect(host.textContent).toContain('静态布景提交失败'); expect(button('读取已确认演员')).toBeUndefined();
  expect(f.entries).toHaveLength(0); expect(f.apply).not.toHaveBeenCalled();
});
it('clears confirmed mapping when the selected scene or template changes', async () => {
  const f = await mount(); await insertCast();
  await input('剧本布景模板', 'ancientStreet');
  expect(button('读取已确认演员')).toBeUndefined(); expect(f.append).toHaveBeenCalledTimes(1);
});
it('prevents an old proposed performance from overwriting a newly loaded saved plan', async () => {
  const f = await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿');
  await check('确认镜头表'); await click('确认应用镜头表');
  const other = structuredClone(f.apply.mock.calls[0][0]); other.sceneFingerprint = 'other-archive';
  await act(async () => root!.render(<SetTemplatePanel {...f.props} appliedPlan={other} />));
  expect(button('确认应用镜头表').disabled).toBe(true); expect(host.textContent).toContain('场景镜头表已变化');
  expect(f.apply).toHaveBeenCalledTimes(1);
});
it('allows actual clip correction and archive reopening while preserving source gaps and both audit records', async () => {
  const f = await mount(); await insertCast(); await chooseClips(); await click('生成对白与走位表演草稿');
  const actor = f.entries.find(e => e.name === '张三')!, rest = actor.object.animations.findIndex(c => c.name === 'cc0Idle');
  await input('纠正镜头', 'performance-1'); await input('纠正角色', actor.id); await input('纠正动作段', '0');
  await input('纠正骨骼片段', String(rest)); await input('人工动作纠正理由', '该段改为静候；不宣称抓腕已完成');
  await click('暂存动作纠正'); await check('确认镜头表'); await click('确认应用镜头表');
  const restored = validateScriptShotPlan(JSON.parse(JSON.stringify(f.apply.mock.calls[0][0])));
  expect(restored.shots[0].execution!.actors![0].beats[0].animation.name).toBe('cc0Idle');
  expect(restored.diagnostics.some(t => { try { return JSON.parse(t).kind === 'script-performance-proposal'; } catch { return false; } })).toBe(true);
  expect(restored.diagnostics.at(-1)).toContain('该段改为静候');
  await act(async () => root!.render(<SetTemplatePanel {...f.props} appliedPlan={restored} />)); await click('载入场景镜头表审阅');
  expect(host.querySelector('[aria-label="表演原文覆盖审阅"]')!.textContent).toContain('未覆盖 · 原文第 5 行：');
  expect(fetch).not.toHaveBeenCalled();
});
