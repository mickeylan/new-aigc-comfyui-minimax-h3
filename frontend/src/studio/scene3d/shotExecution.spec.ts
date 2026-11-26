import { describe, it, expect } from 'vitest';
import * as THREE from 'three';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
const plan = (): ScriptShotPlan => ({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{ id: 's', order: 1, start: 0, end: 4, camera: { position: [0, 3, 5], target: [0, 0, 0], fov: 45 }, dialogue: [], execution: { version: 1, actors: [{ targetId: 'a', targetName: 'A', beats: [{ start: 0, end: 2, from: { x: 0, z: 0 }, to: { x: 2, z: 0 }, yaw: 1, animation: { index: 0, name: 'idle', duration: 2 } }] }] } }] });

import { validateShotExecution, sampleExecutionCamera, sampleExecutionActors } from './shotExecution';
const clip = { index: 0, name: 'idle', duration: 2 };
const beat = { start: 1, end: 2, from: { x: 0, z: 0 }, to: { x: 1, z: 0 }, yaw: 1, animation: clip };
const actor = { targetId: 'a', targetName: 'A', beats: [beat] };
describe('bounded shot execution', () => {
  it('rejects mixed legacy execution modes plan-wide', () => {
    const p = plan();
    p.shots[0].motion = { targetId: 'a', targetName: 'A', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2 };
    expect(() => validateScriptShotPlan(p)).toThrow();
  });
  it('rejects a plan-wide union of 34 actors even though each cut only binds 17', () => {
    const p = plan();
    const tracks = (offset: number) => Array.from({ length: 17 }, (_, i) => ({ targetId: `a${offset + i}`, targetName: `A${offset + i}`, beats: [{ ...beat, from: { x: i * 3, z: offset }, to: { x: i * 3 + 1, z: offset } }] }));
    p.shots[0].execution = { version: 1, actors: tracks(0) };
    p.shots.push({ ...structuredClone(p.shots[0]), id: 'second', order: 2, start: 4, end: 8, execution: { version: 1, actors: tracks(17) } });
    expect(() => validateScriptShotPlan(p)).toThrow('整份执行计划超过32个角色');
    p.shots[1].execution!.actors = tracks(0);
    expect(() => validateScriptShotPlan(p)).not.toThrow();
  });
  it('seeks real objects deterministically and restores on stop', () => {
    const object = new THREE.Group(); object.position.set(9, 1, 0);
    const bone = new THREE.Bone(); bone.name = 'joint'; object.add(bone);
    object.animations = [new THREE.AnimationClip('idle', 2, [new THREE.VectorKeyframeTrack('joint.position', [0, 2], [0, 0, 0, 0, 2, 0])])];
    const camera = new THREE.PerspectiveCamera();
    const controls = { target: new THREE.Vector3(), enabled: true, update() {} };
    const entries = [{ id: 'a', name: 'A', object }];
    const sampler = createScriptShotSampler(plan(), { camera, controls, entries: () => entries, environment: null });
    sampler.sampleAt(3); expect(object.position.x).toBe(2);
    sampler.sampleAt(1); expect(object.position.x).toBe(1); expect(object.rotation.y).toBeCloseTo(1);
    sampler.sampleAt(3); expect(object.position.x).toBe(2);
    sampler.stop(); expect(object.position.toArray()).toEqual([9, 1, 0]); expect(bone.position.y).toBe(0); expect(controls.enabled).toBe(true);
  });
  it('holds endpoints and samples absolute clip time across gaps', () => {
    const execution = validateShotExecution({ version: 1, actors: [actor] }, 4);
    expect(sampleExecutionActors(execution, 0)[0].position.x).toBe(0);
    expect(sampleExecutionActors(execution, 1.5)[0].position.x).toBe(.5);
    expect(sampleExecutionActors(execution, 3)[0].animationSeconds).toBe(1);
    expect(sampleExecutionActors(execution, 1.5)[0].yaw).toBe(1);
  });
  it('rejects unknown fields, overlap, discontinuity and oversized arrays', () => {
    expect(() => validateShotExecution({ version: 1, typo: true }, 4)).toThrow();
    expect(() => validateShotExecution({ version: 1, actors: [actor, actor] }, 4)).toThrow();
    expect(() => validateShotExecution({ version: 1, actors: [{ ...actor, beats: [beat, beat] }] }, 4)).toThrow();
    expect(() => validateShotExecution({ version: 1, actors: [{ ...actor, beats: [beat, { ...beat, start: 3, end: 4 }] }] }, 4)).toThrow();
    expect(() => validateShotExecution({ version: 1, actors: Array(33).fill(actor) }, 4)).toThrow();
  });
  it('accepts accumulated decimal cuts without rounding authored keys/beats and rejects real overruns', () => {
    for (const start of [2, 4.1]) {
      const p = plan(); p.shots[0].end = start; p.shots[0].execution!.actors![0].beats[0].end = start;
      const second = structuredClone(p.shots[0]); second.id = 'second'; second.order = 2; second.start = start; second.end = start + 1.2;
      second.execution!.actors![0].beats[0].end = 1.2;
      second.execution!.camera = { keys: [
        { at: 0, position: [0, 3, 5], target: [0, 0, 0], fov: 45 },
        { at: 1.2, position: [0, 3, 4], target: [0, 0, 0], fov: 45 },
      ] };
      p.shots.push(second); expect(() => validateScriptShotPlan(p)).not.toThrow();
      expect(second.execution!.camera.keys[1].at).toBe(1.2);
      second.execution!.actors![0].beats[0].end += .0001;
      expect(() => validateScriptShotPlan(p)).toThrow();
    }
  });
  it('interpolates cameras and rejects interior eye-target crossing', () => {
    const keys = [{ at: 0, position: [0, 0, 2], target: [0, 0, 0], fov: 30 }, { at: 4, position: [4, 0, 2], target: [4, 0, 0], fov: 60 }];
    const execution = validateShotExecution({ version: 1, camera: { keys } }, 4);
    expect(sampleExecutionCamera(execution, 2)?.position).toEqual([2, 0, 2]);
    expect(sampleExecutionCamera(execution, 2)?.fov).toBe(45);
    expect(() => validateShotExecution({ version: 1, camera: { keys: [keys[0], { ...keys[1], position: [4, 0, -2] }] } }, 4)).toThrow();
  });
});
