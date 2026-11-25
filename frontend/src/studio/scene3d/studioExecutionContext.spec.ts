import { describe, expect, it } from 'vitest';
import { AnimationClip, Object3D } from 'three';
import { captureStudioExecutionContext } from './studioExecutionContext';
import { catalogAssets } from './toonflow/assetCatalog';

describe('actual editor execution directory', () => {
  it('includes the complete real catalog without silently truncating to a keyword template', async () => {
    const context = await captureStudioExecutionContext([]);
    expect(context.assets.map(asset => asset.id)).toEqual(catalogAssets.map(asset => `catalog:${asset.id}`));
    expect(context.assets.length).toBeGreaterThan(10000);
    expect(context.objects).toEqual([]);
    expect(context.assets.every(asset => asset.clips.length === 0)).toBe(true);
    expect(context.revision).toMatch(/^[a-f0-9]{64}$/);
  });
  it('copies actual transforms and attached clips, excluding hidden/locked/splat objects from executable targets', async () => {
    const actor = new Object3D(); actor.position.set(1, 2, 3); actor.animations = [new AnimationClip('walk', 2, [])];
    const hidden = new Object3D(); hidden.visible = false;
    const entries = [{ id: 'actor', name: '真实演员', object: actor }, { id: 'hidden', name: '隐藏', object: hidden }, { id: 'locked', name: '锁定', object: new Object3D(), locked: true }, { id: 'splat', name: '泼溅', object: new Object3D(), splat: {} }];
    const promise = captureStudioExecutionContext(entries); actor.position.x = 99;
    const context = await promise;
    expect(context.objects).toHaveLength(1);
    expect(context.objects[0].position).toEqual([1, 2, 3]);
    const asset = context.assets.find(asset => asset.id === 'object:actor')!;
    expect(asset.clips).toEqual([{ id: `0:${actor.animations[0].uuid}`, name: 'walk', duration: 2 }]);
    const newer = await captureStudioExecutionContext(entries);
    expect(newer.revision).not.toBe(context.revision);
    expect(actor.animations).toHaveLength(1);
  });
  it('invalidates revision on visibility, locking and clip changes and rejects duplicate object identities', async () => {
    const object = new Object3D(); const entry = { id: 'a', name: 'a', object, locked: false };
    const first = await captureStudioExecutionContext([entry]);
    entry.locked = true; const locked = await captureStudioExecutionContext([entry]); expect(locked.revision).not.toBe(first.revision);
    entry.locked = false; object.visible = false; expect((await captureStudioExecutionContext([entry])).revision).not.toBe(first.revision);
    object.visible = true; object.animations.push(new AnimationClip('run', 1, [])); expect((await captureStudioExecutionContext([entry])).revision).not.toBe(first.revision);
    await expect(captureStudioExecutionContext([entry, entry])).rejects.toThrow(/重复/);
  });
});
