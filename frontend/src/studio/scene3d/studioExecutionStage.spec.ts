// @vitest-environment jsdom
import { readFileSync, existsSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { webcrypto } from 'node:crypto';
import { Group, Scene, Mesh, BoxGeometry, MeshStandardMaterial, Bone, AnimationClip, VectorKeyframeTrack } from 'three';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { applyStudioExecution } from './studioExecutionApply';
import { captureStudioExecutionContext } from './studioExecutionContext';
import { assertScriptShotApplicationSafe } from './scriptShotSampler';
import { snapshotDirectorSource } from './directorSource';
import { catalogAssets } from './toonflow/assetCatalog';
import { disposeObject } from './toonflow/sceneResources';

const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
function production(scope: Record<string, any>) {
  const stage = readFileSync(resolve(frontend, 'src/studio/scene3d/Scene3DStage.tsx'), 'utf8');
  const start = stage.indexOf('onApplyExecution={') + 'onApplyExecution={'.length;
  const end = stage.indexOf('}} onApply=', start) + 1;
  if (start < 20 || end < start) throw Error('Missing production execution callback');
  const expression = stage.slice(start, end).replaceAll("import('./studioExecutionApply')", 'Promise.resolve(applyModule)').replaceAll("import('./studioExecutionContext')", 'Promise.resolve(contextModule)');
  const code = execFileSync(process.execPath, ['-e', "process.stdout.write(require('esbuild').transformSync(require('fs').readFileSync(0,'utf8'),{loader:'ts',format:'cjs'}).code)"], { cwd: frontend, input: `const callback = ${expression};`, encoding: 'utf8' });
  return new Function('scope', `with(scope) { ${code}; return callback; }`)(scope);
}
beforeEach(() => vi.stubGlobal('crypto', webcrypto));
afterEach(() => vi.unstubAllGlobals());
async function fixture(failCommit = false) {
  const object = new Group(); object.add(new Mesh(new BoxGeometry(), new MeshStandardMaterial())); object.name = '旧名称';
  const entries = [{ id: 'actor', name: '旧名称', object }]; const scene = new Scene(); scene.add(object);
  const sourceScene = { id: 2, project_id: 1, title: '院落', location: '院落', content: '院中一堵墙。', director_source_stale: false };
  const shots = [{ id: 9, scene_id: 2, order: 1, duration: 3, action_timeline: [], camera_movement: '固定' }];
  const source = await snapshotDirectorSource(1, sourceScene, shots);
  const context = await captureStudioExecutionContext(entries);
  const plan = { source_hash: 'hash', context_revision: context.revision, objects: [
    { id: 'actor', name: '复用对象新名称', asset_id: 'object:actor', position: [8, 0, 3], rotation: [0, 0.7, 0], scale: [1, 1, 1] },
    { id: 'new-prop', name: '新布景', asset_id: `catalog:${catalogAssets[0].id}`, position: [1, 0, 3], rotation: [0, 0, 0], scale: [1, 1, 1] },
  ], bindings: [], shots: [{ shot_id: 9, duration: 3, camera: { position: [1, 2, 5], target: [0, 1, 0], fov: 45 }, motion: null, evidence: ['院中一堵墙。'], assumptions: [], blockers: [] }], assumptions: [], blockers: [] };
  const scope: Record<string, any> = {
    assertScriptShotApplicationSafe, environmentRef: { current: null },
    executionUnavailableRef: { current: false }, sceneRef: { current: scene }, sceneEpochRef: { current: 1 }, objectsRef: { current: entries },
    shotPlaybackTokenRef: { current: 0 }, animationRef: { current: null }, cancelMoveRef: { current: null }, cancelCameraPreviewRef: { current: null }, scrubRef: { current: null },
    fingerprintRef: { current: () => ({ full: () => 'baseline' }) },
    stopAnimation: vi.fn(() => { object.position.set(0, 0, 0); scope.shotPlaybackTokenRef.current++; }),
    applyModule: { applyStudioExecution: async (value: any, reviewed: any, port: any) => {
      if (failCommit) { const commit = port.commit; port.commit = (batch: any) => { batch.additions[0].object.addEventListener('added', () => { throw Error('injected scene publication failure'); }); return commit(batch); }; }
      return applyStudioExecution(value, reviewed, port);
    } }, contextModule: { captureStudioExecutionContext },
    fetch: vi.fn(async () => ({ ok: true, json: async () => ({ source_hash: 'hash', source: { scene: sourceScene, shots } }) })),
    historyRef: { current: { checkpoint: vi.fn(), collect: vi.fn() } }, syncObjects: vi.fn(), setScriptShots: vi.fn(), setStatus: vi.fn(),
  };
  return { object, scene, entries, scope, plan, source, sourceScene, shots, run: () => production(scope)(plan, source, () => true) };
}

it('production Stage callback commits real staged objects and the original director shot plan together', async () => {
  const f = await fixture();
  try {
    await f.run(); expect(f.scope.objectsRef.current).toHaveLength(2); expect(f.scene.children).toHaveLength(2);
    expect(f.object.position.toArray()).toEqual([8, 0, 3]); expect(f.entries[0].name).toBe('复用对象新名称');
    expect(f.scope.stopAnimation).toHaveBeenCalledTimes(1); expect(f.scope.syncObjects).toHaveBeenCalledTimes(1);
    const plan = f.scope.setScriptShots.mock.calls[0][0]; expect(plan.shots[0].id).toBe('director-shot:9'); expect(plan.shots[0].end).toBe(3);
    expect(f.scope.fetch.mock.calls.every((call: any) => call[1].method === 'GET')).toBe(true);
    expect(f.scope.setStatus.mock.calls[0][0]).toContain('未生成媒体');
  } finally { f.scope.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); }
});

it('production Stage rolls back reused transforms/names and new scene children if publication fails', async () => {
  const f = await fixture(true);
  try {
    await expect(f.run()).rejects.toThrow('injected scene publication failure');
    expect(f.scope.objectsRef.current).toBe(f.entries); expect(f.scene.children).toEqual([f.object]);
    expect(f.object.position.toArray()).toEqual([0, 0, 0]); expect(f.object.name).toBe('旧名称'); expect(f.entries[0].name).toBe('旧名称');
    expect(f.scope.setScriptShots).not.toHaveBeenCalled(); expect(f.scope.syncObjects).not.toHaveBeenCalled();
  } finally { disposeObject(f.object); }
});

async function executionFixture(distance: number) {
  const f = await fixture();
  const other = new Group(); other.add(new Mesh(new BoxGeometry(), new MeshStandardMaterial())); other.position.x = 10;
  const second = { id: 'other', name: '乙', object: other };
  f.entries.push(second); f.scene.add(other);
  for (const entry of f.entries) {
    const bone = new Bone(); bone.name = 'joint'; entry.object.add(bone);
    entry.object.animations = [new AnimationClip('idle', 3, [new VectorKeyframeTrack('joint.position', [0, 3], [0, 0, 0, 0, 1, 0])])];
  }
  Object.assign(f.sourceScene, { character_roles_set: true, visible_characters: '甲，乙', content: '甲和乙隔院停步。' });
  Object.assign(f.shots[0], { action_timeline: [{ start: 0, end: 3, subject: '甲，乙', action: '停步', state: '', camera: '' }] });
  const source = await snapshotDirectorSource(1, f.sourceScene, f.shots);
  const plan: any = structuredClone(f.plan);
  plan.context_revision = (await captureStudioExecutionContext(f.entries)).revision;
  plan.objects = [plan.objects[0]];
  plan.bindings = [{ character: '甲', object_id: 'actor' }, { character: '乙', object_id: 'other' }];
  plan.shots[0].evidence = [f.sourceScene.content];
  plan.shots[0].execution = { version: 1, actors: f.entries.map((entry, i) => ({ targetId: entry.id, targetName: i === 0 ? plan.objects[0].name : entry.name, beats: [{ start: 0, end: 3, from: { x: i * distance, z: 0 }, to: { x: i * distance, z: 0 }, yaw: 0, animation: { index: 0, name: 'idle', duration: 3 }, sourceIndex: 0 }] })) };
  return { ...f, run: () => production(f.scope)(plan, source, () => true) };
}

it('production Stage rejects structural-valid close actors before history, publication or shot replacement', async () => {
  const f = await executionFixture(1);
  try {
    await expect(f.run()).rejects.toThrow('间距');
    expect(f.scope.historyRef.current.checkpoint).not.toHaveBeenCalled(); expect(f.scope.historyRef.current.collect).not.toHaveBeenCalled();
    expect(f.scope.setScriptShots).not.toHaveBeenCalled(); expect(f.scope.syncObjects).not.toHaveBeenCalled();
    expect(f.scope.objectsRef.current).toBe(f.entries); expect(f.scene.children).toEqual(f.entries.map(e => e.object));
    expect(f.object.position.toArray()).toEqual([0, 0, 0]); expect(f.object.rotation.y).toBe(0); expect(f.object.name).toBe('旧名称');
  } finally { f.entries.forEach(e => disposeObject(e.object)); }
});

it('production Stage publishes a safe execution plan only after the same runtime geometry proof', async () => {
  const f = await executionFixture(6);
  try {
    await f.run(); expect(f.scope.historyRef.current.checkpoint).toHaveBeenCalledTimes(1);
    expect(f.scope.setScriptShots).toHaveBeenCalledTimes(1); expect(f.object.position.toArray()).toEqual([8, 0, 3]);
    expect(f.scope.setScriptShots.mock.calls[0][0].shots[0].execution.actors).toHaveLength(2);
  } finally { f.entries.forEach(e => disposeObject(e.object)); }
});
