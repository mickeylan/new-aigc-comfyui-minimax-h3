import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import SetTemplatePanel from './SetTemplatePanel';
import { assertScriptShotMotionTargets, validateScriptShotPlan, type ScriptShotMotionTarget } from './scriptShotPlan';
vi.mock('./toonflowSets', () => ({ listSetTemplates: () => [], assertScriptShotMotionTargets }));
let host: HTMLDivElement, root: Root | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; host?.remove(); vi.unstubAllGlobals(); });
const clips = [{ index: 0, name: 'rest', duration: 2 }, { index: 1, name: 'raise', duration: 2 }];
const makePlan = () => validateScriptShotPlan({ version: 1, sceneId: 'scene', heading: '动作稿', location: '平地', sourceFingerprint: 'source', sceneFingerprint: 'review', diagnostics: [], action: ['抬手'], shots: [{ id: 'one', order: 1, start: 0, end: 4, camera: { position: [0, 3, 8], target: [0, 1, 0], fov: 45 }, dialogue: [], execution: { version: 1, actors: [{ targetId: 'a', targetName: '演员', beats: [0, 1].map(i => ({ start: i * 2, end: i * 2 + 2, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0, animation: clips[0], sourceIndex: i })) }] } }] });
const button = (text: string) => [...host.querySelectorAll('button')].find(b => b.textContent === text)!;
async function click(text: string) { await act(async () => button(text).click()); }
async function input(label: string, value: string) {
  await act(async () => {
    const e = host.querySelector(`[aria-label="${label}"]`)!;
    expect(e).toBeTruthy();
    const p = e instanceof HTMLSelectElement ? HTMLSelectElement.prototype : e instanceof HTMLInputElement ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype;
    Object.getOwnPropertyDescriptor(p, 'value')!.set!.call(e, value); e.dispatchEvent(new Event(e instanceof HTMLSelectElement ? 'change' : 'input', { bubbles: true }));
  });
}
async function checked() { await act(async () => (host.querySelector('[aria-label="确认镜头表"]') as HTMLInputElement).click()); }
async function mount(apply = vi.fn(), readTargets?: () => ScriptShotMotionTarget[]) {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); vi.stubGlobal('fetch', vi.fn(() => { throw Error('manual correction must not fetch'); }));
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  const plan = makePlan(), targets = [{ id: 'a', name: '演员', visible: true, locked: false, animations: structuredClone(clips) }];
  const props = { disabled: false, onApply: vi.fn(), onApplyShots: apply, appliedPlan: plan, listMotionTargets: readTargets ?? (() => targets) };
  await act(async () => root!.render(<SetTemplatePanel {...props} />));
  await vi.waitFor(() => expect(button('载入场景镜头表审阅').disabled).toBe(false));
  await click('载入场景镜头表审阅');
  return { props, plan, targets, apply };
}
async function choose() {
  await input('纠正镜头', 'one'); await input('纠正角色', 'a'); await input('纠正动作段', '1');
  await input('纠正骨骼片段', '1'); await input('人工动作纠正理由', '第2段由人选抬手；需另验剧情');
}

it('stages one actual clip correction offline; dirty edits revoke approval, and only re-confirmation hands it to production application', async () => {
  const { plan, apply } = await mount(); const before = structuredClone(plan);
  await checked(); expect(button('确认应用镜头表').disabled).toBe(false);
  await choose(); expect(button('确认应用镜头表').disabled).toBe(true);
  expect((host.querySelector('[aria-label="确认镜头表"]') as HTMLInputElement).disabled).toBe(true);
  expect(apply).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled();
  expect(host.textContent).toContain('整镜腕表面贴合'); expect(host.textContent).toContain('不是抓紧、扑胸或剧情已完成');
  await click('暂存动作纠正'); expect(button('确认应用镜头表').disabled).toBe(true);
  expect(apply).not.toHaveBeenCalled(); await checked(); await click('确认应用镜头表');
  expect(apply).toHaveBeenCalledTimes(1); const result = apply.mock.calls[0][0];
  expect(result.shots[0].execution.actors[0].beats[1].animation).toEqual(clips[1]);
  expect(result.shots[0].execution.actors[0].beats[0].animation).toEqual(clips[0]);
  expect(JSON.parse(result.diagnostics.at(-1)).reason).toBe('第2段由人选抬手；需另验剧情'); expect(plan).toEqual(before);
});
it('requires explicit split point and both clips, stages without publishing, then applies two sourced phases only after renewed approval', async () => {
  const { plan, apply } = await mount(); const before = structuredClone(plan);
  await checked(); await input('纠正镜头', 'one'); await input('纠正角色', 'a'); await input('纠正动作段', '1');
  await act(async () => (host.querySelector('[aria-label="拆分动作段"]') as HTMLInputElement).click());
  expect(button('确认应用镜头表').disabled).toBe(true);
  await input('拆分分界秒', '3'); await input('纠正骨骼片段', '0'); await input('人工动作纠正理由', '前半静置，后半抬手；只作人工分阶段动作稿');
  expect(button('暂存动作纠正').disabled).toBe(true);
  await input('后段骨骼片段', '1'); expect(host.textContent).toContain('各自从零秒播放');
  await click('暂存动作纠正'); expect(apply).not.toHaveBeenCalled(); expect(button('确认应用镜头表').disabled).toBe(true);
  await checked(); await click('确认应用镜头表');
  expect(apply).toHaveBeenCalledTimes(1);
  const beats = apply.mock.calls[0][0].shots[0].execution.actors[0].beats;
  expect(beats).toHaveLength(3); expect(beats.slice(1).map((b: any) => [b.start, b.end, b.animation.name, b.sourceIndex])).toEqual([[2, 3, 'rest', 1], [3, 4, 'raise', 1]]);
  expect(JSON.parse(apply.mock.calls[0][0].diagnostics.at(-1)).operation).toBe('split-beat');
  expect(plan).toEqual(before); expect(fetch).not.toHaveBeenCalled();
});
it('keeps invalid split input dirty until abandoned and does not secretly stage a normal replacement', async () => {
  const { apply } = await mount(); await choose();
  await act(async () => (host.querySelector('[aria-label="拆分动作段"]') as HTMLInputElement).click());
  await input('拆分分界秒', '4'); await input('后段骨骼片段', '0'); await click('暂存动作纠正');
  expect(host.textContent).toContain('拆分分界'); expect(button('确认应用镜头表').disabled).toBe(true);
  await click('放弃本次动作修改');
  expect((host.querySelector('[aria-label="拆分动作段"]') as HTMLInputElement).checked).toBe(false);
  expect((host.querySelector('[aria-label="人工动作纠正理由"]') as HTMLTextAreaElement).value).toBe('');
  expect(apply).not.toHaveBeenCalled();
});
it('discards un-staged edits without changing the saved plan or secretly restoring prior approval', async () => {
  const { plan, apply } = await mount(); await checked(); await choose(); await click('放弃本次动作修改');
  expect(button('确认应用镜头表').disabled).toBe(true); await checked(); await click('确认应用镜头表');
  expect(apply.mock.calls[0][0]).toEqual(plan);
});
it('rejects stale selected clip resources at staging and leaves un-staged edits blocked', async () => {
  const { targets, apply } = await mount(); await choose(); targets[0].animations[1].duration = 7;
  await click('暂存动作纠正'); expect(host.textContent).toContain('资源已变化'); expect(apply).not.toHaveBeenCalled();
  expect(button('确认应用镜头表').disabled).toBe(true);
});
it('rechecks live resources on final application and preserves the staged draft when rejected', async () => {
  const { targets, apply } = await mount(); await choose(); await click('暂存动作纠正'); targets[0].locked = true;
  await checked(); await click('确认应用镜头表'); expect(apply).not.toHaveBeenCalled(); expect(host.textContent).toContain('unavailable');
});
it('does not claim application when the production geometric gate rejects the corrected pose', async () => {
  const apply = vi.fn(() => { throw Error('蒙皮接触安全证明失败'); }); await mount(apply); await choose(); await click('暂存动作纠正');
  await checked(); await click('确认应用镜头表'); expect(host.textContent).toContain('蒙皮接触安全证明失败');
  expect(host.textContent).not.toContain('已接入 1 镜');
});
it('blocks a correction based on another loaded archive after the active saved plan changes', async () => {
  const { props, apply } = await mount(); await choose(); await click('暂存动作纠正'); await checked();
  const changed = makePlan(); changed.sceneFingerprint = 'other-review';
  await act(async () => root!.render(<SetTemplatePanel {...props} appliedPlan={changed} />));
  expect(button('确认应用镜头表').disabled).toBe(true); expect(host.textContent).toContain('场景镜头表已变化，请重新载入'); expect(apply).not.toHaveBeenCalled();
});
it('allows abandoning dirty inputs after an archive switch without permitting stale staging or application', async () => {
  const { props, apply } = await mount(); await choose();
  const changed = makePlan(); changed.sceneFingerprint = 'new-archive';
  await act(async () => root!.render(<SetTemplatePanel {...props} appliedPlan={changed} />));
  expect(button('暂存动作纠正').disabled).toBe(true); expect(button('确认应用镜头表').disabled).toBe(true);
  expect(button('放弃本次动作修改').disabled).toBe(false);
  await click('放弃本次动作修改'); expect(button('载入场景镜头表审阅').disabled).toBe(false);
  await click('载入场景镜头表审阅'); expect(host.textContent).not.toContain('场景镜头表已变化，请重新载入');
  expect(apply).not.toHaveBeenCalled();
});
it('does not offer a fake execution correction on legacy camera-only plans', async () => {
  const { props } = await mount(); const legacy = makePlan(); delete legacy.shots[0].execution;
  await act(async () => root!.render(<SetTemplatePanel {...props} appliedPlan={legacy} />)); await click('载入场景镜头表审阅');
  expect(host.querySelector('[aria-label="纠正骨骼片段"]')).toBeNull(); expect(host.textContent).toContain('没有可纠正的逐段骨骼执行');
});
it('stages an authored clip window and rate offline, refuses an overflowing window, and applies only after renewed approval', async () => {
  const { plan, apply } = await mount(); const before = structuredClone(plan);
  await input('纠正镜头', 'one'); await input('纠正角色', 'a'); await input('纠正动作段', '0');
  await act(async () => (host.querySelector('[aria-label="调整片段源区间与速率"]') as HTMLInputElement).click());
  expect(host.textContent).toContain('人物位置仍按区间进度线性移动');
  await input('片段入点秒', '0'); await input('片段播放速率', '4');
  expect(host.textContent).toContain('不超过片段时长'); expect(button('暂存源区间与速率').disabled).toBe(true);
  await input('片段播放速率', '0.5'); await input('人工动作纠正理由', '第1段慢放；只作人工节奏稿');
  expect(button('暂存源区间与速率').disabled).toBe(false);
  expect(button('暂存动作纠正').disabled).toBe(true);
  await click('暂存源区间与速率');
  expect(apply).not.toHaveBeenCalled(); expect(button('确认应用镜头表').disabled).toBe(true);
  expect(host.textContent).toContain('尚未应用');
  await act(async () => (host.querySelector('[aria-label="调整片段源区间与速率"]') as HTMLInputElement).click());
  await checked(); await click('确认应用镜头表');
  expect(apply).toHaveBeenCalledTimes(1);
  const result = apply.mock.calls[0][0];
  expect(result.shots[0].execution.actors[0].beats[0]).toMatchObject({ start: 0, end: 2, timing: { sourceIn: 0, rate: 0.5 } });
  expect(result.shots[0].execution.actors[0].beats[1].timing).toBeUndefined();
  expect(JSON.parse(result.diagnostics.at(-1))).toMatchObject({ operation: 'retime-beat', reason: '第1段慢放；只作人工节奏稿' });
  expect(plan).toEqual(before); expect(fetch).not.toHaveBeenCalled();
});
