// @vitest-environment jsdom
import { webcrypto } from 'node:crypto';
import { Box3, Mesh, type Object3D } from 'three';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { snapshotDirectorSource } from './directorSource';
import { applyStudioExecution, type StudioExecutionApplyPort, type StudioExecutionCommit } from './studioExecutionApply';
import { catalogAssets } from './toonflow/assetCatalog';
import { disposeObject } from './toonflow/sceneResources';
import { createRecipeArchive, restoreRecipeObject } from './proceduralArchive';
import type { StudioExecutionContext } from './studioExecutionContext';
beforeEach(() => vi.stubGlobal('crypto', webcrypto));
afterEach(() => vi.unstubAllGlobals());
async function fixture() {
  const scene = { id: 2, project_id: 1, title: '空院', location: '院落', content: '院中一堵墙。', director_source_stale: false };
  const shots = [{ id: 9, scene_id: 2, order: 1, duration: 3, camera_movement: '固定', action_timeline: [] }];
  const reviewed = await snapshotDirectorSource(1, scene, shots);
  const asset = catalogAssets[0]!;
  const context: StudioExecutionContext = { revision: 'resources', assets: [{ id: `catalog:${asset.id}`, name: asset.name, clips: [] }], objects: [] };
  const plan = { source_hash: 'hash', context_revision: context.revision, objects: [{ id: 'planned-wall', name: '导演摆放素材', asset_id: context.assets[0].id, position: [3, 1, -2], rotation: [0, 0.73, 0], scale: [1, 1.2, 1] }], bindings: [], shots: [{ shot_id: 9, duration: 3, camera: { position: [1, 2, 5], target: [0, 1, 0], fov: 45 }, motion: null, evidence: ['院中一堵墙。'], assumptions: ['位置是导演补充'], blockers: [] }], assumptions: [], blockers: [] };
  const commit = vi.fn<(batch: StudioExecutionCommit) => void>();
  const port: StudioExecutionApplyPort = { readSource: vi.fn(async () => ({ source_hash: 'hash', source: { scene, shots } })), captureContext: vi.fn(async () => structuredClone(context)), fingerprint: () => 'editor-baseline', isCurrent: () => true, commit };
  return { scene, shots, reviewed, context, plan, port, commit, run: () => applyStudioExecution(plan, reviewed, port) };
}

it('stages real editable geometry, publishes once with exact transforms, and rebuilds through unchanged recipe archive', async () => {
  const f = await fixture(); const result = await f.run();
  expect(f.commit).toHaveBeenCalledTimes(1); expect(f.port.readSource).toHaveBeenCalledTimes(2); expect(f.port.captureContext).toHaveBeenCalledTimes(2);
  const batch = f.commit.mock.calls[0][0]; expect(batch.updates).toEqual([]);
  const object = batch.additions[0].object; let restored: Object3D | undefined;
  try {
    expect(result.shots[0].id).toBe('director-shot:9'); expect(object.position.toArray()).toEqual([3, 1, -2]); expect(object.rotation.y).toBe(0.73);
    expect(object.scale.toArray()).toEqual([1, 1.2, 1]); expect(new Box3().setFromObject(object).isEmpty()).toBe(false);
    const { archive, references } = createRecipeArchive(batch.additions); expect(references.size).toBe(1);
    restored = restoreRecipeObject(JSON.parse(JSON.stringify(archive)), references.get('planned-wall')!);
    restored.position.copy(object.position); restored.quaternion.copy(object.quaternion); restored.scale.copy(object.scale);
    expect(new Box3().setFromObject(restored).min.distanceTo(new Box3().setFromObject(object).min)).toBeLessThan(1e-6);
    expect(JSON.parse(result.diagnostics[1]).executionPlan.assumptions).toEqual([]);
  } finally { disposeObject(object); if (restored) disposeObject(restored); }
});

it('does not publish if source or resources change during staging or selection is cancelled', async () => {
  for (const failure of ['source', 'resource', 'cancel', 'fingerprint']) {
    const f = await fixture(); let checks = 0;
    if (failure === 'source') f.port.readSource = vi.fn().mockResolvedValueOnce({ source_hash: 'hash', source: { scene: f.scene, shots: f.shots } }).mockResolvedValueOnce({ source_hash: 'changed', source: { scene: f.scene, shots: f.shots } });
    if (failure === 'resource') f.port.captureContext = vi.fn().mockResolvedValueOnce(f.context).mockResolvedValueOnce({ ...f.context, revision: 'changed' });
    if (failure === 'cancel') f.port.isCurrent = () => ++checks < 7;
    if (failure === 'fingerprint') f.port.fingerprint = () => ++checks < 7 ? 'editor-baseline' : 'new-editor';
    await expect(f.run()).rejects.toThrow('变化'); expect(f.commit).not.toHaveBeenCalled();
  }
});

it('refuses blocked drafts before materializing or publishing any geometry', async () => {
  const f = await fixture(); f.plan.blockers.push('缺少动作资源');
  await expect(f.run()).rejects.toThrow('缺少动作资源'); expect(f.commit).not.toHaveBeenCalled(); expect(f.port.readSource).toHaveBeenCalledTimes(1);
});

it('rejects a forged client-only asset even if its metadata appears in submitted context', async () => {
  const f = await fixture(); f.plan.objects[0].asset_id = 'catalog:not-real'; f.context.assets.push({ id: 'catalog:not-real', name: '伪造', clips: [] });
  await expect(f.run()).rejects.toThrow('真实目录'); expect(f.commit).not.toHaveBeenCalled();
});

it('cleans staged geometry if atomic editor commit refuses publication', async () => {
  const f = await fixture(); let disposed = 0; let meshes = 0;
  f.port.commit = batch => {
    batch.additions.forEach(entry => entry.object.traverse(node => { if ((node as Mesh).isMesh) { meshes++; (node as Mesh).geometry.addEventListener('dispose', () => disposed++); } }));
    throw Error('编辑器拒绝提交');
  };
  await expect(f.run()).rejects.toThrow('编辑器拒绝提交'); expect(meshes).toBeGreaterThan(0); expect(disposed).toBeGreaterThan(0);
});

function requirementReview(f: Awaited<ReturnType<typeof fixture>>) {
  return { version: 1, sourceRevision: f.reviewed.revision, sourceContent: f.scene.content,
    entities: [{ id: 'wall', label: '墙', count: 1, objectIds: ['planned-wall'], basis: { kind: 'fact', start: 0, end: f.scene.content.length, quote: f.scene.content } }], relations: [], unresolved: [] };
}
it('attaches human-reviewed original requirements to the published shot plan without changing model contracts', async () => {
  const f = await fixture(), review = requirementReview(f);
  const result = await applyStudioExecution(f.plan, f.reviewed, f.port, review);
  try {
    expect(result.sceneRequirements).toEqual(review);
    expect(f.commit.mock.calls[0][0].shotPlan.sceneRequirements).toEqual(review);
    const loaded = JSON.parse(JSON.stringify(result)); expect(loaded.sceneRequirements.entities[0].basis.quote).toBe(f.scene.content);
    result.sceneRequirements!.entities[0].label = 'edited'; expect(review.entities[0].label).toBe('墙');
  } finally { f.commit.mock.calls[0][0].additions.forEach(e => disposeObject(e.object)); }
});
it('rejects requirements from another source or unresolved mapping before geometry publication', async () => {
  for (const change of [
    (r: any) => r.sourceRevision = 'stale',
    (r: any) => r.sourceContent = '另一场次',
    (r: any) => r.entities[0].objectIds = ['not-in-context'],
    (r: any) => r.unresolved.push('小路尚未连通'),
  ]) {
    const f = await fixture(), review = requirementReview(f); change(review);
    await expect(applyStudioExecution(f.plan, f.reviewed, f.port, review)).rejects.toThrow(); expect(f.commit).not.toHaveBeenCalled();
  }
});
it('checks reviewed shots and content again instead of accepting a different current scene', async () => {
  const f = await fixture(); f.shots[0].duration = 4;
  await expect(f.run()).rejects.toThrow('来源已变化'); expect(f.commit).not.toHaveBeenCalled();
});
