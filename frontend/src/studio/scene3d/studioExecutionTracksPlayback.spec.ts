// @vitest-environment jsdom
// Controlled director draft, not a real model or browser/visual acceptance test.
import { webcrypto } from 'node:crypto';
import * as THREE from 'three';
import { afterEach, expect, it, vi } from 'vitest';
import { snapshotDirectorSource } from './directorSource';
import { applyStudioExecution } from './studioExecutionApply';
import { captureStudioExecutionContext } from './studioExecutionContext';
import { validateScriptShotPlan } from './scriptShotPlan';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateShotExecution } from './shotExecution';
import './toonflow/assetCatalog';
import './toonflowSets';
afterEach(() => vi.unstubAllGlobals());

it('preserves a two-character novel director excerpt through audited application and random-access multi-beat playback', async () => {
  vi.stubGlobal('crypto', webcrypto);
  const content = '沈青在院西停步，抬手示意。林禾在院东回身。两人隔院相望。';
  const scene = { id: 2, project_id: 1, title: '院中相遇', location: '院落', content, director_source_stale: false, character_roles_set: true, visible_characters: '沈青，林禾' };
  const shots = [
    { id: 7, scene_id: 2, order: 1, duration: 4, performance: '停步后抬手示意；林禾回身', camera_movement: '推近', action_timeline: [
      // Existing director timeline is sequential; concurrent actors share each source beat.
      { start: 0, end: 2, subject: '沈青，林禾', action: '沈青停步；林禾开始回身', state: '', camera: '' },
      { start: 2, end: 4, subject: '沈青，林禾', action: '沈青抬手示意；林禾回身相望', state: '', camera: '' },
    ] },
    { id: 8, scene_id: 2, order: 2, duration: 2, performance: '', camera_movement: '固定', action_timeline: [] },
  ];
  const entries = ['沈青', '林禾'].map((name, i) => {
    const object = new THREE.Group(); object.position.x = i ? 3 : -3;
    const bone = new THREE.Bone(); bone.name = 'chest'; object.add(bone);
    object.animations = [new THREE.AnimationClip('reviewed-attached', 4, [new THREE.VectorKeyframeTrack('chest.position', [0, 4], [0, 0, 0, 0, i ? 1.2 : .4, 0])])];
    return { id: `a${i}`, name, object };
  });
  const source = await snapshotDirectorSource(1, scene, shots);
  const before = JSON.stringify(source), context = await captureStudioExecutionContext(entries);
  const clip = { index: 0, name: 'reviewed-attached', duration: 4 };
  const actor = (i: number, indices: number[]) => ({ targetId: entries[i].id, targetName: entries[i].name, beats: indices.map(sourceIndex => {
    const step = shots[0].action_timeline[sourceIndex], x = entries[i].object.position.x;
    return { start: step.start, end: step.end, from: { x, z: 0 }, to: { x, z: 0 }, yaw: i ? Math.PI : 0, animation: clip, sourceIndex };
  }) });
  const plan = { source_hash: 'controlled-hash', context_revision: context.revision, objects: [], bindings: entries.map(e => ({ character: e.name, object_id: e.id })), assumptions: ['坐标与通用动作资源是待人工审阅的补充，不证明剧情动作语义'], blockers: [], shots: [
    { shot_id: 7, duration: 4, camera: null, motion: null, evidence: [content], assumptions: [], blockers: [], execution: { version: 1, camera: { keys: [
      { at: 0, position: [0, 3, 8], target: [0, 1, 0], fov: 45 }, { at: 4, position: [0, 3, 4], target: [0, 1, 0], fov: 45 },
    ] }, actors: [actor(0, [0, 1]), actor(1, [0, 1])] } },
    { shot_id: 8, duration: 2, camera: { position: [0, 3, 4], target: [0, 1, 0], fov: 45 }, motion: null, evidence: [content], assumptions: [], blockers: [] },
  ] };
  const commit = vi.fn();
  const applied = await applyStudioExecution(plan, source, { readSource: async () => ({ source_hash: 'controlled-hash', source: { scene, shots } }), captureContext: () => captureStudioExecutionContext(entries), fingerprint: () => 'editor-baseline', isCurrent: () => true, commit });
  expect(commit).toHaveBeenCalledTimes(1); expect(commit.mock.calls[0][0].additions).toEqual([]);
  expect(JSON.stringify(source)).toBe(before);
  const restored = validateScriptShotPlan(JSON.parse(JSON.stringify(applied)));
  expect(restored.shots.map(s => [s.id, s.end - s.start])).toEqual([['director-shot:7', 4], ['director-shot:8', 2]]);
  expect(JSON.parse(restored.action[0]).action_timeline).toEqual(shots[0].action_timeline);
  const camera = new THREE.PerspectiveCamera(); camera.position.set(9, 9, 9);
  const controls = { target: new THREE.Vector3(), enabled: true, update: vi.fn() };
  const sampler = createScriptShotSampler(restored, { camera, controls, entries: () => entries, environment: null });
  const sample = (t: number) => { sampler.sampleAt(t); return { camera: camera.position.toArray(), bones: entries.map(e => e.object.getObjectByName('chest')!.position.y) }; };
  try {
    const at3 = sample(3); expect(at3.camera).toEqual([0, 3, 5]); expect(at3.bones[0]).toBeCloseTo(.1); expect(at3.bones[1]).toBeCloseTo(.3);
    sample(5); sample(.5); expect(sample(3)).toEqual(at3);
  } finally { sampler.stop(); }
  expect(camera.position.toArray()).toEqual([9, 9, 9]); expect(controls.enabled).toBe(true);
  for (const e of entries) expect(e.object.getObjectByName('chest')!.position.y).toBe(0);
});

it('rejects an interior actor crossing despite safe endpoints', () => {
  const beat = (from: number, to: number) => ({ start: 0, end: 4, from: { x: from, z: 0 }, to: { x: to, z: 0 }, yaw: 0, animation: { index: 0, name: 'clip', duration: 4 } });
  expect(() => validateShotExecution({ version: 1, actors: [
    { targetId: 'a', targetName: 'a', beats: [beat(-3, 3)] }, { targetId: 'b', targetName: 'b', beats: [beat(3, -3)] },
  ] }, 4)).toThrow('间距');
});
