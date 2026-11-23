import { describe, it, expect } from 'vitest';
import * as THREE from 'three';
import { PreviewPlayhead, PREVIEW_FPS, previewFrameCount, previewFrameTime } from './previewPlayhead';

describe('24fps session playhead', () => {
  it('clamps frame edges and represents a fractional final frame exactly', () => {
    expect(PREVIEW_FPS).toBe(24);
    expect(previewFrameCount(1.01)).toBe(25);
    expect(previewFrameTime(25, 1.01)).toEqual({ frame: 25, seconds: 1.01 });
    expect(previewFrameTime(24, 1.01).seconds).toBe(1);
    expect(previewFrameTime(-4, 1)).toEqual({ frame: 0, seconds: 0 });
    expect(previewFrameTime(Infinity, 1).seconds).toBe(0);
    expect(previewFrameCount(NaN)).toBe(0);
    expect(previewFrameTime(2, -1)).toEqual({ frame: 0, seconds: 0 });
  });
  it('seeks out of order, freezes skeletal time at the movement end and restores the exact baseline', () => {
    const camera = new THREE.PerspectiveCamera(47); camera.position.set(4, 3, 2);
    const controls = { target: new THREE.Vector3(1, 2, 3), enabled: true, update() {} };
    const mover = new THREE.Group(); mover.position.set(8, 0, 9); mover.rotation.y = .4;
    const bone = new THREE.Bone(); bone.name = 'joint'; bone.position.y = 7; mover.add(bone);
    const clip = new THREE.AnimationClip('test', 4, [new THREE.VectorKeyframeTrack('joint.position', [0, 4], [0, 0, 0, 0, 4, 0])]);
    const baseline = { p: mover.position.toArray(), q: mover.quaternion.toArray(), cp: camera.position.toArray(), cq: camera.quaternion.toArray(), target: controls.target.toArray() };
    const session = new PreviewPlayhead(camera, controls, mover, clip);
    expect(controls.enabled).toBe(false);
    const apply = (t: number) => { mover.position.x = Math.min(t, 2); camera.position.x = t + mover.position.x; camera.fov = 60; };
    for (const t of [1, .25, 3, 1]) {
      session.sample(t, 2, apply);
      expect(bone.position.y).toBeCloseTo(Math.min(t, 2), 6);
      expect(camera.position.x).toBe(t + Math.min(t, 2));
    }
    session.stop(); session.stop(); session.sample(2, 2, apply);
    expect(mover.position.toArray()).toEqual(baseline.p); expect(mover.quaternion.toArray()).toEqual(baseline.q);
    expect(camera.position.toArray()).toEqual(baseline.cp); expect(camera.quaternion.toArray()).toEqual(baseline.cq);
    expect(controls.target.toArray()).toEqual(baseline.target); expect(controls.enabled).toBe(true); expect(camera.fov).toBe(47);
    expect(bone.position.y).toBe(7);
  });
});
