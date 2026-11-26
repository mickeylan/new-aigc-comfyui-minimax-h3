import { expect, it } from 'vitest';
import * as THREE from 'three';
import { assertScriptShotApplicationSafe, createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
import { createEnvironmentTerrain, defaultSceneEnvironment } from './ground-path';

function fixture() {
  const entries = [-3, 3].map((x, i) => {
    const object = new THREE.Group(); object.position.x = x;
    object.add(new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshBasicMaterial()));
    const bone = new THREE.Bone(); bone.name = 'joint'; object.add(bone);
    object.animations = [new THREE.AnimationClip('idle', 2, [new THREE.VectorKeyframeTrack('joint.position', [0, 2], [0, 0, 0, 0, 1, 0])])];
    return { id: `a${i}`, name: `A${i}`, object };
  });
  const actor = (i: number, x = entries[i].object.position.x) => ({ targetId: entries[i].id, targetName: entries[i].name, beats: [{ start: 0, end: 2, from: { x, z: 0 }, to: { x, z: 0 }, yaw: 0, animation: { index: 0, name: 'idle', duration: 2 } }] });
  const plan: ScriptShotPlan = { version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{ id: 'one', order: 1, start: 0, end: 2, camera: { position: [0, 3, 8], target: [0, 0, 0], fov: 45 }, dialogue: [], execution: { version: 1, actors: [actor(0), actor(1)] } }] };
  const updates = (id: string, x: number, scale = 1) => [{ id, position: [x, 0, 0], rotation: [0, .7, 0], scale: [scale, scale, scale] }];
  const snapshot = () => entries.map(e => ({ position: e.object.position.toArray(), quaternion: e.object.quaternion.toArray(), scale: e.object.scale.toArray(), parent: e.object.parent, bone: e.object.getObjectByName('joint')!.position.toArray() }));
  return { entries, plan, actor, updates, snapshot };
}

it('rejects real-box clearance before application although the 0.7m structural floor passes', () => {
  const f = fixture(), baseline = f.snapshot();
  f.plan.shots[0].execution!.actors = [f.actor(0, 0), f.actor(1, 1)];
  expect(() => validateScriptShotPlan(f.plan)).not.toThrow();
  // Box half diagonal = sqrt(.5²+.5²); runtime radius adds .35, pair threshold ≈2.114m.
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null, f.updates('a0', 0))).toThrow('间距');
  expect(f.snapshot()).toEqual(baseline);
  expect(() => createScriptShotSampler(f.plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update() {} }, entries: () => f.entries, environment: null })).toThrow('间距');
});

it('proves proposed obstacle transforms rather than old positions and restores success and failure', () => {
  const f = fixture();
  const wall = new THREE.Mesh(new THREE.BoxGeometry(1, 3, 1), new THREE.MeshBasicMaterial()); wall.position.x = 10;
  const entries = [...f.entries, { id: 'wall', name: '墙', object: wall }], baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, entries, null, f.updates('wall', -3))).toThrow('墙');
  expect(wall.position.x).toBe(10); expect(wall.rotation.y).toBe(0); expect(f.snapshot()).toEqual(baseline);
  wall.position.x = -3;
  expect(() => assertScriptShotApplicationSafe(f.plan, entries, null, f.updates('wall', 10))).not.toThrow();
  expect(wall.position.x).toBe(-3); expect(wall.rotation.y).toBe(0); expect(f.snapshot()).toEqual(baseline);
});

it('rejects oversized proposed actor geometry and restores all transforms', () => {
  const f = fixture(), baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null, f.updates('a0', -3, 20))).toThrow('包围体');
  expect(f.snapshot()).toEqual(baseline);
});

it('includes actors held before their first bound cut at the proposed editing position', () => {
  const f = fixture();
  f.plan.shots[0].execution!.actors = [f.actor(0)];
  f.plan.shots.push({ ...structuredClone(f.plan.shots[0]), id: 'two', order: 2, start: 2, end: 4, execution: { version: 1, actors: [f.actor(1)] } });
  const baseline = f.snapshot();
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null, f.updates('a1', -2))).toThrow('间距');
  expect(f.snapshot()).toEqual(baseline);
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, null)).not.toThrow();
});

it('keeps static held/beat terrain-water checks, with no mutation on rejection', () => {
  const f = fixture(), baseline = f.snapshot();
  const terrain = createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
  terrain.waterLevels = terrain.heights.map(() => 1);
  expect(() => assertScriptShotApplicationSafe(f.plan, f.entries, { ...defaultSceneEnvironment(), terrain })).toThrow('水域');
  expect(f.snapshot()).toEqual(baseline);
});
