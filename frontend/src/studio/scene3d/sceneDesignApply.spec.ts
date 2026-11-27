import { it, expect, vi } from 'vitest';
import { Group, Scene } from 'three';
import { applySceneDesign, type SceneDesignApplyHost } from './sceneDesignApply';
import type { SceneDesignPlan, SceneDesignSnapshot } from './sceneDesign';

const make = () => {
  const entry = { id: 'a', name: '甲', object: new Group() }; const scene = new Scene(); scene.add(entry.object);
  const base: SceneDesignSnapshot = { sceneKey: 'one', context: { revision: 'r', assets: [{ id: 'object:a', name: '甲', clips: [] }, { id: 'primitive:person', name: '人', clips: [] }], objects: [{ id: 'a', name: '甲', asset_id: 'object:a', position: [0, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] }] } };
  const plan: SceneDesignPlan = { context_revision: 'r', summary: '移动甲', questions: [], asset_queries: [], template: null, objects: [{ ...base.context.objects[0], position: [3, 0, 0] as [number, number, number], rotation: [0, 0, 0], scale: [1, 1, 1] }], remove_ids: [], facts: [], assumptions: [], limitations: [] };
  const state = { sceneKey: 'one', fingerprint: 'f', available: true };
  const host: SceneDesignApplyHost<typeof entry> = { state: () => state, entries: () => [entry], scene, capture: vi.fn().mockResolvedValue(structuredClone(base)), checkpoint: vi.fn(), validate: vi.fn(), publish: vi.fn() };
  return { entry, base, plan, state, host };
};
it('production apply stages then merges generation-time edits without overwriting user field', async () => {
  const { entry, base, plan, host } = make();
  entry.object.position.x = 8;
  const latest = structuredClone(base); latest.context.objects[0].position = [8, 0, 0];
  host.capture = async () => latest; plan.objects[0].rotation = [0, 1, 0];
  const result = await applySceneDesign(plan, base, () => true, host);
  expect(entry.object.position.x).toBe(8); expect(entry.object.rotation.y).toBe(1);
  expect(result).toEqual({ added: 0, updated: 1, removed: 0, preserved: 1 }); expect(host.checkpoint).toHaveBeenCalledTimes(1); expect(host.publish).toHaveBeenCalledTimes(1);
});
it('rejects scene replacement, cancellation and busy state without history or mutations', async () => {
  for (const kind of ['scene', 'cancel', 'busy']) {
    const { entry, base, plan, state, host } = make(); if (kind === 'scene') state.sceneKey = 'two'; if (kind === 'busy') state.available = false;
    await expect(applySceneDesign(plan, base, () => kind !== 'cancel', host)).rejects.toThrow('未应用');
    expect(host.checkpoint).not.toHaveBeenCalled(); expect(entry.object.position.x).toBe(0);
  }
});
it('rejects scene or fingerprint changes during async capture', async () => {
  for (const kind of ['scene', 'fingerprint']) {
    const { entry, base, plan, state, host } = make();
    host.capture = async () => { if (kind === 'scene') state.sceneKey = 'two'; else state.fingerprint = 'changed'; return base; };
    await expect(applySceneDesign(plan, base, () => true, host)).rejects.toThrow('未应用'); expect(host.checkpoint).not.toHaveBeenCalled(); expect(entry.object.position.x).toBe(0);
  }
});
it('retains existing safety gate and rejects changes during validation', async () => {
  const { entry, base, plan, state, host } = make();
  host.validate = () => { throw Error('已有导演轨道引用，不能修改'); };
  await expect(applySceneDesign(plan, base, () => true, host)).rejects.toThrow('已有导演'); expect(host.publish).not.toHaveBeenCalled();
  host.validate = () => { state.fingerprint = 'changed'; };
  await expect(applySceneDesign(plan, base, () => true, host)).rejects.toThrow('未应用'); expect(entry.object.position.x).toBe(0);
});
it('skips user-deleted objects without resurrection, no-op makes no checkpoint', async () => {
  const { base, plan, host } = make(); host.capture = async () => ({ ...base, context: { ...base.context, objects: [] } });
  expect(await applySceneDesign(plan, base, () => true, host)).toEqual({ added: 0, updated: 0, removed: 0, preserved: 1 }); expect(host.checkpoint).not.toHaveBeenCalled();
});
