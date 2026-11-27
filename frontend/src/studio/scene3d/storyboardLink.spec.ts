import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { webcrypto } from 'node:crypto';
import * as THREE from 'three';
import { storyboardSource, linkedStoryboardShot, captureStoryboardShot } from './storyboardLink';
import { createScriptShotSampler } from './scriptShotSampler';
import type { ScriptShotPlan } from './scriptShotPlan';
beforeEach(() => vi.stubGlobal('crypto', webcrypto)); afterEach(() => vi.unstubAllGlobals());
export function planFixture(): ScriptShotPlan {
  return { version: 1, sceneId: 'director-scene:2', heading: '院落', location: '院落', sourceFingerprint: 'server-hash', sceneFingerprint: 'director-revision', action: [],
    diagnostics: [JSON.stringify({ directorSource: { scene: { project_id: 1, id: 2 }, shots: [{ id: 9, scene_id: 2 }, { id: 10, scene_id: 2 }] } })],
    shots: [9, 10].map((id, i) => ({ id: `director-shot:${id}`, order: i + 1, start: i, end: i + 1, camera: { position: [i + 1, 2, 5], target: [0, 0, 0], fov: 45 }, dialogue: [] })) };
}
it('image/pack/video share a parent revision; scene, plan, frame and director source invalidate it', async () => {
  const plan = planFixture(), before = JSON.stringify(plan), a = await storyboardSource(plan, 'scene-state', '16:9');
  expect(await storyboardSource(structuredClone(plan), 'scene-state', '16:9')).toEqual(a); expect(a.revision).toMatch(/^[0-9a-f]{64}$/);
  const moved = structuredClone(plan); moved.shots[0].camera!.position[0]++;
  const source = { ...plan, sourceFingerprint: 'new-source' };
  for (const b of [await storyboardSource(plan, 'AI-moved-prop', '16:9'), await storyboardSource(moved, 'scene-state', '16:9'), await storyboardSource(source, 'scene-state', '16:9'), await storyboardSource(plan, 'scene-state', '9:16')]) expect(b.revision).not.toBe(a.revision);
  expect(JSON.stringify(plan)).toBe(before);
});
it('binds exact director IDs, rejecting cross project/scene, absent source and ordinal guesses', () => {
  const plan = planFixture(); expect(linkedStoryboardShot(plan, { projectId: 1, sceneId: 2 }, 10).start).toBe(1);
  for (const [p, c, id] of [[plan, { projectId: 3, sceneId: 2 }, 9], [plan, { projectId: 1, sceneId: 3 }, 9], [plan, { projectId: 1, sceneId: 2 }, 1], [{ ...plan, diagnostics: [] }, { projectId: 1, sceneId: 2 }, 9]] as const) expect(() => linkedStoryboardShot(p as ScriptShotPlan, c, id)).toThrow();
});
it('samples the real shared camera/actor timeline and restores baseline BEFORE delayed encoding', async () => {
  const plan = planFixture(); plan.shots[0].motion = { targetId: 'actor', targetName: '甲', start: { x: 0, z: 0 }, end: { x: 4, z: 0 }, duration: 1 };
  const actor = new THREE.Group(); actor.position.x = -2;
  const camera = new THREE.PerspectiveCamera(60); camera.position.set(9, 8, 7);
  const controls = { target: new THREE.Vector3(), enabled: true, update() {} }; let finish!: (v: string) => void;
  let current = true; const entries = [{ id: 'actor', name: '甲', object: actor }];
  const host = { assertCurrent() { if (!current) throw Error('stale'); }, sampler: () => createScriptShotSampler(plan, { camera, controls, entries: () => entries, environment: null }),
    capture: vi.fn(async (pose: any) => { expect(actor.position.x).toBe(4); expect(pose.position).toEqual([2, 2, 5]); return new Promise<string>(r => { finish = r; }); }) };
  const pending = captureStoryboardShot(plan, 'director-shot:10', host);
  expect(actor.position.x).toBe(-2); expect(camera.position.toArray()).toEqual([9, 8, 7]); expect(controls.enabled).toBe(true);
  finish('pixels'); expect(await pending).toBe('pixels');
  const stale = captureStoryboardShot(plan, 'director-shot:10', host); current = false; finish('old'); await expect(stale).rejects.toThrow('stale');
  expect(actor.position.x).toBe(-2);
});
it('restores on synchronous capture failure and refuses absent shots before sampling', async () => {
  const stop = vi.fn(), sampler = vi.fn(() => ({ sampleAt: () => ({ camera: {} }), stop }));
  const host = { assertCurrent() {}, sampler, capture: () => { throw Error('copy failed'); } };
  await expect(captureStoryboardShot(planFixture(), 'director-shot:9', host)).rejects.toThrow('copy failed'); expect(stop).toHaveBeenCalledOnce();
  sampler.mockClear(); await expect(captureStoryboardShot(planFixture(), 'unknown', host)).rejects.toThrow('不存在'); expect(sampler).not.toHaveBeenCalled();
});
