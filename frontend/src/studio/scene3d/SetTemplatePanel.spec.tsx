import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SetTemplatePanel from './SetTemplatePanel';

const engine = vi.hoisted(() => ({
  listSetTemplates: vi.fn(() => [{ id: 'bedroom', name: '卧室', genre: '内景' }]),
  listSetPersonObjects: vi.fn(() => []),
  parseScriptScenes: vi.fn(), buildScriptSceneDraft: vi.fn(), materializeSetDraft: vi.fn(), applyScriptCharacterBindings: vi.fn(),
  buildScriptShotPlan: vi.fn(), updateScriptShotDuration: vi.fn(), setScriptShotCamera: vi.fn(), clearScriptShotCamera: vi.fn(), assertScriptShotPlanSource: vi.fn(),
  setScriptShotMotion: vi.fn(), clearScriptShotMotion: vi.fn(), assertScriptShotMotionTargets: vi.fn(),
  assertScriptShotPlaybackReady: vi.fn(), assertScriptShotReferenceReady: vi.fn(),
}));
vi.mock('./toonflowSets', () => engine);
// Import the large real catalog during suite collection, before any React root
// owns an act() scope. Cold module loading is not a UI interaction under test.
const actual = await vi.importActual<typeof import('./toonflowSets')>('./toonflowSets');
let host: HTMLDivElement, root: Root;
const onApply = vi.fn();
const onApplyShots = vi.fn();
const onPlayShots = vi.fn();
const onStopShots = vi.fn();
const onPreviewShot = vi.fn();
const shotCamera = { position: [1, 2, 3], target: [0, 1, 0], fov: 40 };
const onCaptureCamera = vi.fn(() => shotCamera);
const scene = { id: 'scene-2', heading: '2. 外景 古街 日', location: '古街', timeOfDay: 'day', characters: ['张三'], dialogue: [{ speaker: '张三', line: '走吧。' }] };
const draft = { sceneId: 'scene-2', templateId: 'bedroom', templateName: '卧室', recommendation: { matchedBy: 'manual' }, setDraft: { name: '卧室', document: { objects: [] } }, review: { instanceCount: 1, characters: ['张三'], dialogue: scene.dialogue, diagnostics: [], conflicts: [] } };
const boundSetDraft = { name: '卧室', document: { objects: [] }, bound: true };
const shotPlan = {
  version: 1, sceneId: 'scene-2', heading: '2. 外景 古街 日', location: '古街',
  sourceFingerprint: 'src-1', sceneFingerprint: 'scene-1', action: ['两人对视。'], diagnostics: ['镜头时长按原文汉字数估算'],
  shots: [
    { id: 'shot-1', order: 1, start: 0, end: 1.5, camera: null, dialogue: [{ index: 0, speaker: '张三', line: '走吧。', start: 0, end: 1.5 }] },
    { id: 'shot-2', order: 2, start: 1.5, end: 3, camera: null, dialogue: [{ index: 1, speaker: '李四', line: '好。', start: 1.5, end: 3 }] },
  ],
} as any;
const readyPlan = { ...shotPlan, shots: shotPlan.shots.map((shot: any, index: number) => ({ ...shot, camera: index === 0 ? shotCamera : { position: [-2, 2, 5], target: [0, 1, 0], fov: 50 } })) };
const button = (text: string) => [...host.querySelectorAll('button')].find(node => node.textContent === text)!;
async function click(node: Element) { await act(async () => { (node as HTMLElement).click(); }); }
async function input(label: string, value: string) {
  const node = host.querySelector(`[aria-label="${label}"]`)! as HTMLTextAreaElement | HTMLInputElement | HTMLSelectElement;
  const prototype = node instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : node instanceof HTMLInputElement ? HTMLInputElement.prototype : HTMLSelectElement.prototype;
  const event = node instanceof HTMLSelectElement ? 'change' : 'input';
  await act(async () => { Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(node, value); node.dispatchEvent(new Event(event, { bubbles: true })); });
}
async function parse() {
  await input('剧本原文', '1. 内景 客栈 夜\n2. 外景 古街 日\n张三：走吧。');
  await click(button('解析剧本场次'));
}
async function openDraft() {
  await parse();
  await click(host.querySelector('[aria-label="场次 scene-2"]')!);
  await click(button('生成该场草稿'));
}
beforeEach(async () => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  vi.clearAllMocks();
  // 镜头表相关 mock 每个用例重新设定，避免上一例的返回值漏到下一例。
  engine.buildScriptShotPlan.mockReset(); engine.updateScriptShotDuration.mockReset();
  engine.setScriptShotCamera.mockReset(); engine.clearScriptShotCamera.mockReset(); engine.assertScriptShotPlanSource.mockReset();
  engine.setScriptShotMotion.mockReset(); engine.clearScriptShotMotion.mockReset(); engine.assertScriptShotMotionTargets.mockReset();
  engine.assertScriptShotMotionTargets.mockImplementation((value: any) => value);
  engine.assertScriptShotPlaybackReady.mockReset();
  engine.assertScriptShotPlaybackReady.mockImplementation((value: any) => value);
  engine.assertScriptShotReferenceReady.mockReset();
  engine.assertScriptShotReferenceReady.mockImplementation((value: any) => value);
  engine.listSetPersonObjects.mockReturnValue([]);
  engine.parseScriptScenes.mockReturnValue({ explicitHeadings: true, scenes: [{ ...scene, id: 'scene-1' }, scene], diagnostics: [] });
  engine.buildScriptSceneDraft.mockResolvedValue(draft);
  engine.materializeSetDraft.mockReturnValue([{ id: 'actor', object: { traverse: vi.fn() } }]);
  engine.applyScriptCharacterBindings.mockImplementation((value: any) => ({ ...value, setDraft: boundSetDraft, review: { ...value.review, instanceCount: 2 } }));
  host = document.createElement('div'); document.body.appendChild(host); root = createRoot(host);
  await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onCaptureCamera={onCaptureCamera} onPreviewShot={onPreviewShot} onApplyShots={onApplyShots} onPlayShots={onPlayShots} onStopShots={onStopShots} />); });
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); });
describe('script staging human gates', () => {
  it('requires explicit scene selection, explicit character binding and human confirmation; allows a manual template', async () => {
    await parse();
    expect(button('生成该场草稿').disabled).toBe(true);
    expect(host.querySelectorAll('input[type="radio"]:checked')).toHaveLength(0);
    await click(host.querySelector('[aria-label="场次 scene-2"]')!);
    await input('剧本布景模板', 'bedroom');
    await click(button('生成该场草稿'));
    expect(engine.buildScriptSceneDraft.mock.calls[0][1]).toBe('scene-2');
    expect(engine.buildScriptSceneDraft.mock.calls[0][3]).toBe('bedroom');
    expect(onApply).not.toHaveBeenCalled();
    // 生成后角色一律未绑定：勾选核对也不足以放行。
    await click(host.querySelector('[aria-label="确认场次与模板"]')!);
    expect(button('确认追加该场布景').disabled).toBe(true);
    expect(engine.applyScriptCharacterBindings).not.toHaveBeenCalled();
    await input('角色 张三 绑定', 'placeholder');
    await input('角色 张三 站位X', '2.5');
    expect(host.textContent).toContain('布景 1 个实例 + 新增 1 个占位人');
    // 改动绑定会撤销原确认，必须重新勾选。
    expect(button('确认追加该场布景').disabled).toBe(true);
    await click(host.querySelector('[aria-label="确认场次与模板"]')!);
    expect(button('确认追加该场布景').disabled).toBe(false);
    await click(button('确认追加该场布景'));
    expect(engine.applyScriptCharacterBindings).toHaveBeenCalledTimes(1);
    expect(engine.applyScriptCharacterBindings.mock.calls[0][1]).toEqual([
      { character: '张三', target: { kind: 'placeholder', position: { x: 2.5, z: 0 } } },
    ]);
    expect(onApply).toHaveBeenCalledTimes(1);
    expect(onApply.mock.calls[0][0]).toBe(boundSetDraft);
  });
  it('lists real set people and sends the explicitly selected person, never a name match', async () => {
    engine.listSetPersonObjects.mockReturnValue([{ id: 'p1', name: '男主', shape: 'person', position: { x: 1.2, y: 0, z: 0.8 } }]);
    await openDraft();
    await input('角色 张三 绑定', 'set:p1');
    expect(host.textContent).toContain('布景 1 个实例 + 新增 0 个占位人');
    await click(host.querySelector('[aria-label="确认场次与模板"]')!);
    expect(button('确认追加该场布景').disabled).toBe(false);
    await click(button('确认追加该场布景'));
    expect(engine.applyScriptCharacterBindings.mock.calls[0][1]).toEqual([
      { character: '张三', target: { kind: 'set-person', objectId: 'p1' } },
    ]);
    expect(onApply.mock.calls[0][0]).toBe(boundSetDraft);
  });
  it('invalidates an in-flight draft when the source changes, even if the worker resolves late', async () => {
    let resolve!: (value: any) => void;
    engine.buildScriptSceneDraft.mockImplementationOnce(() => new Promise(value => { resolve = value; }));
    await parse(); await click(host.querySelector('[aria-label="场次 scene-2"]')!);
    await click(button('生成该场草稿'));
    const signal = engine.buildScriptSceneDraft.mock.calls[0][2] as AbortSignal;
    await input('剧本原文', '1. 内景 卧室 日\n张三：改了。');
    expect(signal.aborted).toBe(true);
    await act(async () => { resolve(draft); await Promise.resolve(); });
    expect(button('确认追加该场布景')).toBeUndefined();
    expect(onApply).not.toHaveBeenCalled();
  });
  it('surfaces construction errors without applying partial results', async () => {
    engine.buildScriptSceneDraft.mockRejectedValueOnce(new Error('没有匹配的内置布景模板'));
    await parse(); await click(host.querySelector('[aria-label="场次 scene-2"]')!);
    await click(button('生成该场草稿'));
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('没有匹配');
    expect(onApply).not.toHaveBeenCalled();
  });
  it('keeps the draft and reports the binding error instead of applying an overlapping placement', async () => {
    engine.applyScriptCharacterBindings.mockImplementationOnce(() => { throw new Error('角色「张三」的站位与另一个角色重合'); });
    await openDraft();
    await input('角色 张三 绑定', 'placeholder');
    await click(host.querySelector('[aria-label="确认场次与模板"]')!);
    await click(button('确认追加该场布景'));
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('重合');
    expect(onApply).not.toHaveBeenCalled();
    expect(button('确认追加该场布景')).toBeDefined();
  });
  it('builds the shot plan from the reviewed draft and needs a second explicit confirmation before it reaches the scene', async () => {
    engine.buildScriptShotPlan.mockReturnValue(shotPlan);
    engine.assertScriptShotPlanSource.mockReturnValue(shotPlan);
    await openDraft();
    await click(button('生成镜头计划（每句一镜）'));
    expect(engine.buildScriptShotPlan).toHaveBeenCalledWith(draft);
    expect(host.textContent).toContain('共 2 镜');
    expect(host.textContent).toContain('未指定机位 2 镜');
    expect(host.textContent).toContain('未指定机位（不会自动补）');
    // 未勾选核对前不得接入场景，也不得换用旧的布景应用入口。
    expect(button('确认应用镜头表').disabled).toBe(true);
    await click(host.querySelector('[aria-label="确认镜头表"]')!);
    await click(button('确认应用镜头表'));
    expect(engine.assertScriptShotPlanSource).toHaveBeenCalledWith(shotPlan, draft);
    expect(onApplyShots).toHaveBeenCalledTimes(1);
    expect(onApplyShots.mock.calls[0][0]).toBe(shotPlan);
    expect(onApply).not.toHaveBeenCalled();
  });
  it('edits one shot duration through the module and records the viewport camera explicitly', async () => {
    const edited = { ...shotPlan, shots: [{ ...shotPlan.shots[0], end: 3 }, { ...shotPlan.shots[1], start: 3, end: 4.5 }] };
    const framed = { ...edited, shots: [{ ...edited.shots[0], camera: shotCamera }, edited.shots[1]] };
    engine.buildScriptShotPlan.mockReturnValue(shotPlan);
    engine.updateScriptShotDuration.mockReturnValue(edited);
    engine.setScriptShotCamera.mockReturnValue(framed);
    await openDraft();
    await click(button('生成镜头计划（每句一镜）'));
    await input('镜 1 时长', '3');
    expect(engine.updateScriptShotDuration).toHaveBeenCalledWith(shotPlan, 'shot-1', 3);
    expect(host.textContent).toContain('3.00–4.50');
    // 改时长会撤销上次确认，必须重新勾选。
    expect(button('确认应用镜头表').disabled).toBe(true);
    await click(host.querySelector('[aria-label="用当前视口机位 shot-1"]')!);
    expect(onCaptureCamera).toHaveBeenCalledTimes(1);
    expect(engine.setScriptShotCamera).toHaveBeenCalledWith(edited, 'shot-1', shotCamera);
    expect(host.textContent).toContain('已记录机位');
  });
  it('binds per-shot movement only to an explicitly selected scene object and validates it before applying', async () => {
    const targets = [{ id: 'actor-1', name: '张三', visible: true, locked: false, animations: [{ index: 0, name: 'walk', duration: 1.2 }] }];
    const motion = {
      targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 2, z: 1 }, duration: 1.5,
      animation: { index: 0, name: 'walk', duration: 1.2 },
    };
    const moved = { ...shotPlan, shots: [{ ...shotPlan.shots[0], motion }, shotPlan.shots[1]] };
    engine.buildScriptShotPlan.mockReturnValue(shotPlan);
    engine.setScriptShotMotion.mockReturnValue(moved);
    engine.clearScriptShotMotion.mockReturnValue(shotPlan);
    engine.assertScriptShotPlanSource.mockReturnValue(moved);
    engine.assertScriptShotMotionTargets.mockReturnValue(moved);
    await act(async () => {
      root.render(<SetTemplatePanel disabled={false} onApply={onApply} onApplyShots={onApplyShots} listMotionTargets={() => targets} />);
    });
    await openDraft();
    await click(button('生成镜头计划（每句一镜）'));
    // 对象与骨骼动作都来自真实场景列表；没选对象时按钮保持禁用。
    expect((host.querySelector('[aria-label="镜 1 应用走位"]') as HTMLButtonElement).disabled).toBe(true);
    await input('镜 1 走位对象', 'actor-1');
    await input('镜 1 走位终点X', '2');
    await input('镜 1 走位终点Z', '1');
    await input('镜 1 走位时长', '1.5');
    await input('镜 1 骨骼动作', '0');
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    expect(engine.setScriptShotMotion).toHaveBeenCalledWith(shotPlan, 'shot-1', motion);
    expect(host.textContent).toContain('已绑定走位：张三');
    // 接入前必须把目标对到当前场景；缺目标/隐藏/锁定/clip 变化由模块报错。
    await click(host.querySelector('[aria-label="确认镜头表"]')!);
    await click(button('确认应用镜头表'));
    expect(engine.assertScriptShotMotionTargets).toHaveBeenCalledWith(moved, targets);
    expect(onApplyShots).toHaveBeenCalledWith(moved);
    // 清除走位同样走模块，不保留旧动画。
    await click(host.querySelector('[aria-label="镜 1 清除走位"]')!);
    expect(engine.clearScriptShotMotion).toHaveBeenCalledWith(moved, 'shot-1');
  });
  it.each(['repeat', 'coordinates', 'duration', 'navigation-only', 'character-only'])('preserves enhancements through basic movement editing: %s', async mode => {
    const animation = { index: 0, name: 'walk', duration: 1.2 };
    const characterMotion = { version: 1, blend: { animation: { index: 1, name: 'idle', duration: 2 }, weight: 0.4, fadeSeconds: 0.2 },
      feet: { left: { hip: 'lh', knee: 'lk', ankle: 'la' }, right: { hip: 'rh', knee: 'rk', ankle: 'ra' }, soleOffset: 0.09, maxCorrection: 0.3 } };
    const navigation = { version: 1, mode: 'avoid', radius: 0.4, speed: 3 };
    const motion = { targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 2, z: 1 }, duration: 1,
      animation, ...(mode !== 'navigation-only' ? { characterMotion } : {}), ...(mode !== 'character-only' ? { navigation } : {}) };
    const plan = { ...readyPlan, shots: [{ ...readyPlan.shots[0], motion }, readyPlan.shots[1]] };
    const before = JSON.stringify(plan);
    const targets = [{ id: 'actor-1', name: '改名后的张三', visible: true, locked: false, animations: [animation, characterMotion.blend.animation] }];
    // Keep the UI event path, but use real replacement and target validation here.
    engine.setScriptShotMotion.mockImplementation(actual.setScriptShotMotion);
    engine.assertScriptShotMotionTargets.mockImplementation(actual.assertScriptShotMotionTargets);
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onApplyShots={onApplyShots} appliedPlan={plan} listMotionTargets={() => targets} />); });
    await click(button('载入场景镜头表审阅'));
    if (mode === 'coordinates') await input('镜 1 走位终点X', '4');
    if (mode === 'duration') await input('镜 1 走位时长', '1.4');
    if (mode === 'navigation-only') await input('镜 1 骨骼动作', '1');
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    const expected = { ...motion, targetName: targets[0].name,
      ...(mode === 'coordinates' ? { end: { x: 4, z: 1 } } : {}), ...(mode === 'duration' ? { duration: 1.4 } : {}),
      ...(mode === 'navigation-only' ? { animation: characterMotion.blend.animation } : {}) };
    expect(engine.setScriptShotMotion).toHaveBeenLastCalledWith(plan, 'shot-1', expected);
    await click(host.querySelector('[aria-label="确认镜头表"]')!);
    await click(button('确认应用镜头表'));
    expect(onApplyShots.mock.calls.at(-1)![0].shots[0].motion).toEqual(expected);
    const restored = actual.validateScriptShotPlan(JSON.parse(JSON.stringify(onApplyShots.mock.calls.at(-1)![0])));
    expect(restored.shots[0].motion).toEqual(expected);
    expect(JSON.stringify(plan)).toBe(before);
  });
  it.each(['target', 'clip', 'clear-clip', 'clip-name', 'clip-duration'])('rejects incompatible enhanced movement without writing the plan: %s', async mode => {
    const animation = { index: 0, name: 'walk', duration: 1.2 };
    const motion = { targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 2, z: 1 }, duration: 1,
      animation, characterMotion: { version: 1, blend: { animation: { index: 1, name: 'idle', duration: 2 }, weight: 0.4, fadeSeconds: 0 } },
      navigation: { version: 1, mode: 'avoid', radius: 0.4, speed: 3 } };
    const plan = { ...readyPlan, shots: [{ ...readyPlan.shots[0], motion }, readyPlan.shots[1]] };
    const before = JSON.stringify(plan);
    const changed = { ...animation, ...(mode === 'clip-name' ? { name: 'replacement' } : {}), ...(mode === 'clip-duration' ? { duration: 3 } : {}) };
    const targets = ['actor-1', 'actor-2'].map(id => ({ id, name: '张三', visible: true, locked: false, animations: [changed, motion.characterMotion.blend.animation] }));
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onApplyShots={onApplyShots} appliedPlan={plan} listMotionTargets={() => targets} />); });
    await click(button('载入场景镜头表审阅'));
    if (mode === 'target') await input('镜 1 走位对象', 'actor-2');
    if (mode === 'clip') await input('镜 1 骨骼动作', '1');
    if (mode === 'clear-clip') await input('镜 1 骨骼动作', '');
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('请先');
    expect(engine.setScriptShotMotion).not.toHaveBeenCalled();
    expect(onApplyShots).not.toHaveBeenCalled();
    expect(JSON.stringify(plan)).toBe(before);
    // Explicit clearing remains available after a rejected edit.
    engine.clearScriptShotMotion.mockReturnValue(readyPlan);
    await click(host.querySelector('[aria-label="镜 1 清除走位"]')!);
    expect(engine.clearScriptShotMotion).toHaveBeenCalledWith(plan, 'shot-1');
  });
  it('never selects clip zero implicitly and reports a disappeared explicit clip without applying', async () => {
    let targets = [{ id: 'actor-1', name: '张三', visible: true, locked: false, animations: [{ index: 0, name: 'walk', duration: 1.2 }] }];
    engine.buildScriptShotPlan.mockReturnValue(shotPlan);
    engine.setScriptShotMotion.mockReturnValue(shotPlan);
    const render = () => <SetTemplatePanel disabled={false} onApply={onApply} onApplyShots={onApplyShots} listMotionTargets={() => targets} />;
    await act(async () => { root.render(render()); });
    await openDraft();
    await click(button('生成镜头计划（每句一镜）'));
    await input('镜 1 走位对象', 'actor-1');
    await input('镜 1 走位终点X', '2');
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    expect(engine.setScriptShotMotion).toHaveBeenLastCalledWith(shotPlan, 'shot-1', {
      targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 1,
    });
    expect(engine.setScriptShotMotion.mock.calls.at(-1)![2]).not.toHaveProperty('animation');
    // Explicit choice is required; returning to the blank option removes, rather than reselects, clip zero.
    await input('镜 1 骨骼动作', '0');
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    expect(engine.setScriptShotMotion.mock.calls.at(-1)![2].animation).toEqual(targets[0].animations[0]);
    await input('镜 1 骨骼动作', '');
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    expect(engine.setScriptShotMotion.mock.calls.at(-1)![2]).not.toHaveProperty('animation');
    await input('镜 1 骨骼动作', '0');
    targets = [{ ...targets[0], animations: [] }];
    await act(async () => { root.render(render()); });
    engine.setScriptShotMotion.mockClear();
    await click(host.querySelector('[aria-label="镜 1 应用走位"]')!);
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('所选骨骼动作已变化');
    expect(engine.setScriptShotMotion).not.toHaveBeenCalled();
    expect(onApplyShots).not.toHaveBeenCalled();
  });
  it('keeps the shot plan when the module rejects an edit and surfaces the reason', async () => {
    engine.buildScriptShotPlan.mockReturnValue(shotPlan);
    engine.updateScriptShotDuration.mockImplementationOnce(() => { throw new Error('第 1 镜的台词需要 1.5 秒，镜头时长不得短于台词'); });
    await openDraft();
    await click(button('生成镜头计划（每句一镜）'));
    await input('镜 1 时长', '0.2');
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('不得短于台词');
    expect(button('确认应用镜头表')).toBeDefined();
    expect(engine.updateScriptShotDuration).toHaveBeenCalledWith(shotPlan, 'shot-1', 0.2);
  });
  it('drops an unapplied shot plan when the script source changes', async () => {
    engine.buildScriptShotPlan.mockReturnValue(shotPlan);
    await openDraft();
    await click(button('生成镜头计划（每句一镜）'));
    expect(button('确认应用镜头表')).toBeDefined();
    await input('剧本原文', '1. 内景 卧室 日\n张三：改了。');
    expect(button('确认应用镜头表')).toBeUndefined();
    expect(onApplyShots).not.toHaveBeenCalled();
  });
  it('shows the archived shot plan for review without silently replacing the scene', async () => {
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onApplyShots={onApplyShots} appliedPlan={shotPlan} />); });
    expect(host.textContent).toContain('场景中已保存镜头表：2 镜');
    await click(button('载入场景镜头表审阅'));
    expect(host.textContent).toContain('共 2 镜');
    expect(host.textContent).toContain('已载入存档镜头表');
    expect(onApplyShots).not.toHaveBeenCalled();
  });
  it('refuses to play a saved plan whose camera is missing and never asks the viewport to fake one', async () => {
    engine.assertScriptShotPlaybackReady.mockImplementationOnce(() => { throw new Error('镜头表还有 2 镜未指定机位（第 1、2 镜），不能连续逐镜播放；请先在视口逐镜记录机位，不会自动补机位'); });
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onPlayShots={onPlayShots} onStopShots={onStopShots} appliedPlan={shotPlan} />); });
    await click(button('连续逐镜播放'));
    expect(engine.assertScriptShotPlaybackReady).toHaveBeenCalledWith(shotPlan);
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('未指定机位');
    expect(onPlayShots).not.toHaveBeenCalled();
  });
  it('plays the confirmed plan through the existing viewport and shows the current shot line, with stop delegated', async () => {
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onPlayShots={onPlayShots} onStopShots={onStopShots} appliedPlan={readyPlan} />); });
    expect(button('停止并还原视口').disabled).toBe(true);
    await click(button('连续逐镜播放'));
    expect(engine.assertScriptShotPlaybackReady).toHaveBeenCalledWith(readyPlan);
    expect(onPlayShots).toHaveBeenCalledWith(readyPlan);
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onPlayShots={onPlayShots} onStopShots={onStopShots} appliedPlan={readyPlan} shotPlayback={{ index: 1, shots: 2, line: '李四：好。' }} />); });
    expect(host.textContent).toContain('正在播放第 2 镜 / 共 2 镜');
    expect(host.textContent).toContain('当前台词：李四：好。');
    expect(button('连续逐镜播放').disabled).toBe(true);
    await click(button('停止并还原视口'));
    expect(onStopShots).toHaveBeenCalledTimes(1);
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} appliedPlan={readyPlan} shotPlayback={{ index: 1, shots: 2, line: null }} />); });
    expect(host.textContent).toContain('当前台词：（停顿／无台词）');
    expect(host.textContent).not.toContain('当前台词：李四：好。');
  });
  it('needs an explicit pack confirmation and a ready plan before asking the viewport to export', async () => {
    const onExportShotPack = vi.fn(async () => {});
    engine.assertScriptShotReferenceReady.mockImplementationOnce(() => { throw new Error('镜头表还有 1 镜未指定机位（第 2 镜），不能连续逐镜播放；请先在视口逐镜记录机位，不会自动补机位'); });
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} appliedPlan={shotPlan} onExportShotPack={onExportShotPack} />); });
    // 未勾选前按钮保持禁用，不靠点一下默认同意。
    expect(button('导出逐镜参考包 ZIP').disabled).toBe(true);
    await click(host.querySelector('[aria-label="确认导出逐镜参考包"]')!);
    expect(button('导出逐镜参考包 ZIP').disabled).toBe(false);
    await click(button('导出逐镜参考包 ZIP'));
    expect(engine.assertScriptShotReferenceReady).toHaveBeenCalledWith(shotPlan);
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('未指定机位');
    expect(onExportShotPack).not.toHaveBeenCalled();
  });
  it('hands the confirmed shot plan to the viewport exporter and clears the confirmation afterwards', async () => {
    const onExportShotPack = vi.fn(async () => true);
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} appliedPlan={readyPlan} onExportShotPack={onExportShotPack} />); });
    expect(host.textContent).toContain('2 镜 × 3 张 = 6 张 PNG');
    await click(host.querySelector('[aria-label="确认导出逐镜参考包"]')!);
    await click(button('导出逐镜参考包 ZIP'));
    expect(engine.assertScriptShotReferenceReady).toHaveBeenCalledWith(readyPlan);
    expect(onExportShotPack).toHaveBeenCalledWith(readyPlan);
    expect((host.querySelector('[aria-label="确认导出逐镜参考包"]') as HTMLInputElement).checked).toBe(false);
    expect(button('导出逐镜参考包 ZIP').disabled).toBe(true);
    // 没有当前草稿时不假装比对来源，也不改动镜头表或其它接入入口。
    expect(engine.assertScriptShotPlanSource).not.toHaveBeenCalled();
    expect(onApplyShots).not.toHaveBeenCalled();
    // 视口报告没有产出下载时，保留本次确认供重试，不替用户重新勾上也不假装成功。
    onExportShotPack.mockResolvedValueOnce(false);
    await click(host.querySelector('[aria-label="确认导出逐镜参考包"]')!);
    await click(button('导出逐镜参考包 ZIP'));
    expect((host.querySelector('[aria-label="确认导出逐镜参考包"]') as HTMLInputElement).checked).toBe(true);
    expect(button('导出逐镜参考包 ZIP').disabled).toBe(false);
  });
  it('refuses to package a saved plan that the current script draft has invalidated', async () => {
    const onExportShotPack = vi.fn(async () => {});
    engine.assertScriptShotPlanSource.mockImplementationOnce(() => { throw new Error('剧本原文或本场内容已改变，镜头表已失效，请重新生成'); });
    await openDraft();
    await act(async () => { root.render(<SetTemplatePanel disabled={false} onApply={onApply} onApplyShots={onApplyShots} appliedPlan={readyPlan} onExportShotPack={onExportShotPack} />); });
    await click(host.querySelector('[aria-label="确认导出逐镜参考包"]')!);
    await click(button('导出逐镜参考包 ZIP'));
    expect(engine.assertScriptShotPlanSource).toHaveBeenCalledWith(readyPlan, draft);
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('失效');
    expect(onExportShotPack).not.toHaveBeenCalled();
  });
});