// @vitest-environment jsdom
import { webcrypto } from 'node:crypto';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { snapshotDirectorSource } from './directorSource';
import { prepareStudioExecution, type StudioExecutionPlan } from './studioExecutionAdapter';
import type { StudioExecutionContext } from './studioExecutionContext';

beforeEach(() => vi.stubGlobal('crypto', webcrypto));
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const source = await snapshotDirectorSource(1, { id: 2, project_id: 1, title: '门外', location: '客栈', content: '甲沿墙走到门边。', director_source_stale: false, character_roles_set: true, visible_characters: '甲', characters: '甲，画外乙' }, [
    { id: 7, scene_id: 2, order: 1, duration: 2.345, performance: '沿墙走到门边', camera_movement: '固定', action_timeline: [] },
    { id: 8, scene_id: 2, order: 2, duration: 1.5, performance: '', camera_movement: 'static', action_timeline: [] },
  ]);
  const context: StudioExecutionContext = { revision: 'resources', assets: [
    { id: 'object:actor', name: '演员', clips: [{ id: '0:uuid', name: 'Walk', duration: 1 }] },
    { id: 'catalog:wall', name: '墙', clips: [] },
  ], objects: [{ id: 'actor', asset_id: 'object:actor', name: '演员', position: [0, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] }] };
  const plan: StudioExecutionPlan = { source_hash: 'server-hash', context_revision: 'resources', objects: [
    { id: 'wall', asset_id: 'catalog:wall', name: '院墙', position: [1, 0, 2], rotation: [0, Math.PI / 2, 0], scale: [1, 1, 1] },
  ], bindings: [{ character: '甲', object_id: 'actor' }], assumptions: ['沿墙布置为导演补充'], blockers: [], shots: [
    { shot_id: 7, duration: 2.345, camera: { position: [3, 2, 4], target: [0, 1, 0], fov: 45 }, motion: { object_id: 'actor', clip_id: '0:uuid', start: [0, 0, 0], end: [2, 0, 0], duration: 2 }, evidence: ['沿墙走到门边'], assumptions: [], blockers: [] },
    { shot_id: 8, duration: 1.5, camera: { position: [2, 2, 4], target: [2, 1, 0], fov: 45 }, motion: null, evidence: ['甲沿墙走到门边。'], assumptions: [], blockers: [] },
  ] };
  return { source, context, plan, run: () => prepareStudioExecution(plan, source, 'server-hash', context) };
}

it('preserves existing shot order, IDs and exact durations rather than re-splitting or rounding', async () => {
  const f = await fixture(); const before = JSON.stringify(f); const result = await f.run();
  expect(result.blockers).toEqual([]); expect(result.shotPlan!.shots.map(s => [s.id, s.start, s.end])).toEqual([
    ['director-shot:7', 0, 2.345], ['director-shot:8', 2.345, 3.845],
  ]);
  expect(result.shotPlan!.shots[0].motion!.animation).toEqual({ index: 0, name: 'Walk', duration: 1 });
  expect(result.objects[0].rotation[1]).toBe(Math.PI / 2);
  expect(JSON.parse(result.shotPlan!.diagnostics[1]).directorSource).toEqual(f.source);
  expect(JSON.parse(result.shotPlan!.action[0])).toEqual({ shot_id: 7, performance: '沿墙走到门边', action_timeline: [] });
  expect(JSON.stringify(f)).toBe(before);
  result.plan.objects[0].position[0] = 99; expect(f.plan.objects[0].position[0]).toBe(1);
});

it('rejects stale hashes, resource revisions and source mutated after review', async () => {
  const f = await fixture(); f.plan.source_hash = 'old'; await expect(f.run()).rejects.toThrow('修订');
  f.plan.source_hash = 'server-hash'; f.context.revision = 'changed'; await expect(f.run()).rejects.toThrow('修订');
  f.context.revision = 'resources'; f.source.shots[0].performance = '推门'; await expect(f.run()).rejects.toThrow('快照已变化');
});

it('rejects invented assets, duplicates, replacing existing assets and cloning object clip assets', async () => {
  for (const change of [
    (f: Awaited<ReturnType<typeof fixture>>) => { f.plan.objects[0].asset_id = 'catalog:invented'; },
    (f: Awaited<ReturnType<typeof fixture>>) => { f.plan.objects.push(structuredClone(f.plan.objects[0])); },
    (f: Awaited<ReturnType<typeof fixture>>) => { f.plan.objects[0].id = 'actor'; },
    (f: Awaited<ReturnType<typeof fixture>>) => { f.plan.objects[0].asset_id = 'object:actor'; },
  ]) { const f = await fixture(); change(f); await expect(f.run()).rejects.toThrow(); }
});

it('rejects unknown fields, NaN, incomplete shots, changed duration/order and false evidence', async () => {
  for (const change of [
    (p: any) => { p.execute_code = 'bad'; }, (p: any) => { p.objects[0].scale[0] = NaN; },
    (p: any) => { p.shots.pop(); }, (p: any) => { p.shots.reverse(); },
    (p: any) => { p.shots[0].duration = 2; }, (p: any) => { p.shots[0].evidence = ['不存在的原文']; },
    (p: any) => { p.shots[0].camera.target = [...p.shots[0].camera.position]; },
  ]) { const f = await fixture(); change(f.plan); await expect(f.run()).rejects.toThrow(); }
});

it('does not permit offscreen characters or duplicate character/object bindings', async () => {
  const f = await fixture(); f.plan.bindings[0].character = '画外乙'; await expect(f.run()).rejects.toThrow('绑定');
  f.plan.bindings[0].character = '甲'; f.plan.bindings.push({ ...f.plan.bindings[0] }); await expect(f.run()).rejects.toThrow('绑定');
  f.plan.bindings = []; const r = await f.run(); expect(r.shotPlan).toBeNull(); expect(r.blockers.join()).toContain('甲');
});

it('requires actual original attached clip index and refuses invalid movement', async () => {
  for (const change of [
    (m: any) => { m.clip_id = '0:invented'; }, (m: any) => { m.clip_id = '1:uuid'; },
    (m: any) => { m.end = [...m.start]; }, (m: any) => { m.end[1] = 1; },
    (m: any) => { m.duration = 3; }, (m: any) => { m.object_id = 'missing'; },
  ]) { const f = await fixture(); change(f.plan.shots[0].motion); await expect(f.run()).rejects.toThrow(); }
});

it('retains blockers and never manufactures a partial/static playable substitute', async () => {
  const f = await fixture(); f.plan.blockers.push('外观缺口'); f.plan.shots[0].blockers.push('动作语义待验'); f.plan.shots[0].motion = null; f.plan.shots[1].camera = null;
  const result = await f.run(); expect(result.shotPlan).toBeNull();
  expect(result.blockers).toEqual(expect.arrayContaining(['外观缺口', '镜7：动作语义待验', '镜7：原始表演未绑定，不得用静止画面替代', '镜8：缺少机位']));
});

it('reports unsupported camera movement, multi-beat action and authoritative dialogue mapping without losing original text', async () => {
  const f = await fixture(); const shots = structuredClone(f.source.shots);
  shots[0] = { ...shots[0], camera_movement: '推近', dialogue: '乙：不要进去', dialogue_ranges: [{ dialogue_id: 1, start_rune: 0, end_rune: 4 }], action_timeline: [
    { start: 0, end: 1, subject: '甲', action: '走', state: '', camera: '' },
    { start: 1, end: 2.345, subject: '甲', action: '推门', state: '', camera: '' },
  ] };
  const source = await snapshotDirectorSource(1, f.source.scene, shots);
  const result = await prepareStudioExecution(f.plan, source, 'server-hash', f.context);
  expect(result.shotPlan).toBeNull(); expect(result.blockers.join()).toContain('原运镜');
  expect(result.blockers.join()).toContain('多段动作'); expect(result.blockers.join()).toContain('权威对白');
  expect(result.source.shots[0]).toEqual(shots[0]);
});

async function tracksFixture() {
  const f = await fixture();
  const shots = structuredClone(f.source.shots);
  shots[0] = { ...shots[0], camera_movement: '推近', action_timeline: [
    { start: 0, end: 1, subject: '甲', action: '停下', state: '', camera: '' },
    { start: 1, end: 2.345, subject: '甲', action: '挥手', state: '', camera: '' },
  ] };
  const source = await snapshotDirectorSource(1, f.source.scene, shots);
  f.plan.shots[0].camera = null; f.plan.shots[0].motion = null;
  f.plan.shots[0].execution = { version: 1, camera: { keys: [
    { at: 0, position: [0, 2, 5], target: [0, 1, 0], fov: 45 },
    { at: 2.345, position: [0, 2, 3], target: [0, 1, 0], fov: 45 },
  ] }, actors: [{ targetId: 'actor', targetName: '演员', beats: shots[0].action_timeline!.map((step, sourceIndex) => ({
    start: step.start, end: step.end, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0,
    animation: { index: 0, name: 'Walk', duration: 1 }, sourceIndex,
  })) }] };
  return { ...f, source, run: () => prepareStudioExecution(f.plan, source, 'server-hash', f.context) };
}
it('accepts audited moving cameras and multi-beat in-place clips without changing source or duration', async () => {
  const f = await tracksFixture(); const before = JSON.stringify(f.source); const r = await f.run();
  expect(r.blockers).toEqual([]); expect(r.shotPlan!.shots[0].execution).toEqual(f.plan.shots[0].execution);
  expect(r.shotPlan!.shots[0].end).toBe(2.345); expect(JSON.stringify(f.source)).toBe(before);
  r.plan.shots[0].execution!.actors![0].beats[0].yaw = 1;
  expect(f.plan.shots[0].execution!.actors![0].beats[0].yaw).toBe(0);
});
it('rejects execution clips, source indices, times, subjects and bindings not matching actual source/context', async () => {
  for (const change of [
    (p: any) => { p.shots[0].execution.actors[0].beats[0].animation.name = 'invented'; },
    (p: any) => { p.shots[0].execution.actors[0].beats[0].sourceIndex = 1; },
    (p: any) => { p.shots[0].execution.actors[0].beats[0].end = .9; },
    (p: any) => { p.shots[0].execution.actors[0].targetId = 'wall'; },
    (p: any) => { p.shots[0].execution.actors[0].beats[0].typo = true; },
    (p: any) => { p.shots[0].execution.camera.keys[1].at = 2; },
  ]) { const f = await tracksFixture(); change(f.plan); await expect(f.run()).rejects.toThrow(); }
});
it('does not substitute partial action coverage or missing moving camera with a static playable plan', async () => {
  const f = await tracksFixture(); f.plan.shots[0].execution!.actors![0].beats.pop();
  const r = await f.run(); expect(r.shotPlan).toBeNull(); expect(r.blockers.join()).toContain('未完整绑定');
  const g = await tracksFixture(); delete g.plan.shots[0].execution!.camera;
  const q = await g.run(); expect(q.shotPlan).toBeNull(); expect(q.blockers.join()).toContain('原运镜');
});

it('does not silently discard unsupported world height or normalize motion duration precision', async () => {
  const f = await fixture(); f.plan.shots[0].motion!.start[1] = 1; f.plan.shots[0].motion!.end[1] = 1; f.plan.shots[0].motion!.duration = 1.234;
  const r = await f.run(); expect(r.shotPlan).toBeNull(); expect(r.blockers.join()).toContain('高度'); expect(r.blockers.join()).toContain('精度');
});
