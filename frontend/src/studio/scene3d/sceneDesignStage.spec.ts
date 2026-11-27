import { readFileSync, existsSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { webcrypto } from 'node:crypto';
import { Group, Scene, Mesh, BoxGeometry, MeshStandardMaterial } from 'three';
import * as THREE from 'three';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import * as sceneDesignModule from './sceneDesign';
import * as applyModule from './sceneDesignApply';
import { disposeObject } from './toonflow/sceneResources';
import { ObjectHistory } from './kitbashEditor';

await import('./toonflowSets');
await import('./toonflow/assetLibrary');
const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
function compileProduction() {
  const stage = readFileSync(resolve(frontend, 'src/studio/scene3d/Scene3DStage.tsx'), 'utf8');
  const marker = stage.indexOf('<SceneDesignPanel');
  const start = stage.indexOf('host={', marker) + 'host={'.length;
  const end = stage.indexOf('}} />', start) + 1;
  if (marker < 0 || end < start) throw Error('Missing production scene design host');
  const expression = stage.slice(start, end).replaceAll("import('./sceneDesign')", 'Promise.resolve(sceneDesignModule)').replaceAll("import('./sceneDesignApply')", 'Promise.resolve(applyModule)');
  const code = execFileSync(process.execPath, ['-e', "process.stdout.write(require('esbuild').transformSync(require('fs').readFileSync(0,'utf8'),{loader:'ts',format:'cjs'}).code)"], { cwd: frontend, input: `const host = ${expression};`, encoding: 'utf8' });
  return new Function('scope', `with(scope) { ${code}; return host; }`) as (scope: Record<string, any>) => sceneDesignModule.SceneDesignHost;
}
// The source is immutable for this run; compile once, but instantiate fresh hosts/state per assertion.
const production = compileProduction();
beforeEach(() => vi.stubGlobal('crypto', webcrypto)); afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const object = new Group(), material = new MeshStandardMaterial(); object.add(new Mesh(new BoxGeometry(), material));
  const entries = [{ id: 'a', name: '甲', object }]; const scene = new Scene(); scene.add(object);
  const history = new ObjectHistory<typeof entries[number]>(entry => disposeObject(entry.object));
  const scope: any = {
    sceneDesignModule, applyModule, sceneRef: { current: scene }, sceneEpochRef: { current: 1 }, objectsRef: { current: entries }, executionUnavailableRef: { current: false },
    animationRef: { current: null }, cancelMoveRef: { current: null }, cancelCameraPreviewRef: { current: null }, scrubRef: { current: null },
    fingerprintRef: { current: () => ({ full: () => JSON.stringify([sceneDesignModule.captureSceneDesignEditKeys(scope.objectsRef.current), scope.sceneEpochRef.current, scope.environmentRef.current]) }) },
    environmentRef: { current: null }, designShotPlanRef: { current: null }, stopAnimation: vi.fn(), assertScriptShotApplicationSafe: vi.fn(),
    historyRef: { current: history }, syncObjects: vi.fn(), selectObject: vi.fn(), setStatus: vi.fn(),
  };
  const host = production(scope), base = await host.capture();
  const plan: sceneDesignModule.SceneDesignPlan = { context_revision: base.context.revision, summary: '移动甲', questions: [], asset_queries: [], template: null, objects: [{ ...base.context.objects[0], position: [3, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] }], remove_ids: [], facts: [], assumptions: [], limitations: [] };
  return { object, material, entries, scene, scope, host, base, plan, history };
}
it('actual Stage host applies autonomous static placements and keeps undo/redo', async () => {
  const f = await fixture();
  f.plan.objects.push({ id: 'new', name: '路人', asset_id: 'primitive:person', position: [8, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] });
  try {
    expect(await f.host.apply(f.plan, f.base, () => true)).toEqual({ added: 1, updated: 1, removed: 0, preserved: 0 });
    expect(f.scope.objectsRef.current).toHaveLength(2); expect(f.scene.children).toHaveLength(2); expect(f.object.position.x).toBe(3);
    expect(f.scope.objectsRef.current[1].object.userData.proceduralSource.source.asset.id).toBe('person');
    expect(f.history.undo(f.scope.objectsRef.current)).toEqual(f.entries); expect(f.object.position.x).toBe(0);
    expect(f.history.redo(f.entries)).toHaveLength(2); expect(f.object.position.x).toBe(3);
    expect(f.scope.setStatus.mock.calls[0][0]).toContain('未生成媒体');
  } finally { f.history.clear([]); f.scope.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); }
});
it('actual Stage first design frames all new geometry without manual focus', async () => {
  const f = await fixture(); f.scope.objectsRef.current = []; f.object.removeFromParent();
  f.scope.THREE = THREE; f.scope.cameraRef = { current: new THREE.PerspectiveCamera(45, 1, .01, 1000) };
  f.scope.controlsRef = { current: { target: new THREE.Vector3(), update: vi.fn() } };
  const base = await f.host.capture();
  f.plan.objects = [{ id: 'new', name: '路人', asset_id: 'primitive:person', position: [20, 0, 10], rotation: [0, 0, 0], scale: [1, 1, 1] }];
  f.plan.context_revision = base.context.revision;
  await f.host.apply(f.plan, base, () => true);
  expect(f.scope.controlsRef.current.target.x).toBeCloseTo(20); expect(f.scope.controlsRef.current.target.z).toBeCloseTo(10);
  expect(f.scope.controlsRef.current.update).toHaveBeenCalledTimes(1); expect(f.scope.cameraRef.current.position.distanceTo(f.scope.controlsRef.current.target)).toBeGreaterThan(1);
  f.scope.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); disposeObject(f.object);
});
it('actual Stage host preserves material edits from AI deletion and deletes only unchanged objects', async () => {
  const f = await fixture(); f.plan.objects = []; f.plan.remove_ids = ['a']; f.material.color.set('red');
  expect(await f.host.apply(f.plan, f.base, () => true)).toEqual({ added: 0, updated: 0, removed: 0, preserved: 1 }); expect(f.object.parent).toBe(f.scene);
  const nextBase = await f.host.capture();
  expect((await f.host.apply(f.plan, nextBase, () => true)).removed).toBe(1); expect(f.scope.objectsRef.current).toEqual([]); expect(f.object.parent).toBeNull();
  expect(f.history.undo([])).toHaveLength(1); disposeObject(f.object);
});
it('actual Stage host rejects scene replacement, cancellation, busy and playback before applying', async () => {
  for (const mode of ['scene', 'cancel', 'busy', 'playback']) {
    const f = await fixture(); if (mode === 'scene') f.scope.sceneEpochRef.current++; if (mode === 'busy') f.scope.executionUnavailableRef.current = true; if (mode === 'playback') f.scope.animationRef.current = {};
    await expect(f.host.apply(f.plan, f.base, () => mode !== 'cancel')).rejects.toThrow(); expect(f.object.position.x).toBe(0); expect(f.scope.syncObjects).not.toHaveBeenCalled(); disposeObject(f.object);
  }
});
it('actual Stage host protects existing director objects and calls geometry gate for new obstructions', async () => {
  const f = await fixture(); f.scope.designShotPlanRef.current = { version: 1 };
  await expect(f.host.apply(f.plan, f.base, () => true)).rejects.toThrow('已有镜头表'); expect(f.object.position.x).toBe(0);
  f.plan.objects = [{ id: 'new', name: '路人', asset_id: 'primitive:person', position: [3, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] }];
  f.scope.assertScriptShotApplicationSafe.mockImplementation(() => { throw Error('真实通路被挡'); });
  await expect(f.host.apply(f.plan, f.base, () => true)).rejects.toThrow('真实通路被挡'); expect(f.scene.children).toHaveLength(1); expect(f.scope.syncObjects).not.toHaveBeenCalled(); disposeObject(f.object);
});
it('actual Stage host rejects unsupported terrain additions and snapshot races', async () => {
  const f = await fixture(); f.scope.environmentRef.current = { terrain: {} };
  f.plan.objects = [{ id: 'new', name: '路人', asset_id: 'primitive:person', position: [3, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] }];
  await expect(f.host.apply(f.plan, f.base, () => true)).rejects.toThrow('地形落点'); expect(f.scene.children).toHaveLength(1);
  const capture = sceneDesignModule.captureSceneDesignContext;
  f.scope.sceneDesignModule = { ...sceneDesignModule, captureSceneDesignContext: async (...args: Parameters<typeof capture>) => { const context = await capture(...args); f.object.position.x++; return context; } };
  await expect(production(f.scope).capture()).rejects.toThrow('读取期间场景已变化'); disposeObject(f.object);
});
